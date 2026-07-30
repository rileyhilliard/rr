package monitor

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// cpuJiffies stores CPU jiffies for delta calculation.
type cpuJiffies struct {
	total int64
	idle  int64
}

// diskSample stores cumulative disk I/O byte counters for rate calculation.
type diskSample struct {
	readBytes  int64
	writeBytes int64
	at         time.Time
}

// Collector gathers system metrics from multiple remote hosts.
type Collector struct {
	hosts           map[string]config.Host
	pool            *Pool
	timeout         time.Duration
	prevJiffies     map[string]cpuJiffies   // Previous aggregate CPU jiffies per host for delta calculation
	prevCoreJiffies map[string][]cpuJiffies // Previous per-core CPU jiffies per host for delta calculation
	prevDisk        map[string]diskSample   // Previous disk I/O counters per host for rate calculation
	mu              sync.Mutex              // Protects prevJiffies, prevCoreJiffies, and prevDisk

	// Lock checking configuration (optional)
	lockConfig *config.LockConfig
}

// NewCollector creates a new metrics collector for the specified hosts.
func NewCollector(hosts map[string]config.Host) *Collector {
	return &Collector{
		hosts:           hosts,
		pool:            NewPool(hosts, 10*time.Second),
		timeout:         30 * time.Second,
		prevJiffies:     make(map[string]cpuJiffies),
		prevCoreJiffies: make(map[string][]cpuJiffies),
		prevDisk:        make(map[string]diskSample),
	}
}

// SetLockConfig configures lock checking for the collector.
// If set, the collector will check lock status for each host during collection.
func (c *Collector) SetLockConfig(lockCfg config.LockConfig) {
	c.lockConfig = &lockCfg
}

// SetTimeout sets the per-host collection timeout.
func (c *Collector) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

// CollectStreaming gathers metrics from all hosts, streaming results as each completes.
// Returns a channel that will receive HostResult for each host as it completes.
// The channel is closed when all hosts have been processed.
// This allows the UI to update independently per host instead of waiting for all hosts.
func (c *Collector) CollectStreaming(ctx context.Context) <-chan HostResult {
	// Collect from all hosts
	hostList := make([]string, 0, len(c.hosts))
	for alias := range c.hosts {
		hostList = append(hostList, alias)
	}
	return c.CollectStreamingHosts(ctx, hostList)
}

// CollectStreamingHosts collects metrics from only the specified hosts.
// This is useful for implementing backoff - skip hosts that are in backoff period.
func (c *Collector) CollectStreamingHosts(ctx context.Context, hostList []string) <-chan HostResult {
	results := make(chan HostResult, len(hostList))

	if len(hostList) == 0 {
		close(results)
		return results
	}

	var wg sync.WaitGroup
	for _, alias := range hostList {
		// Skip hosts not in our config
		if _, ok := c.hosts[alias]; !ok {
			continue
		}
		wg.Add(1)
		go func(alias string) {
			defer wg.Done()

			// Use per-host timeout, respecting parent context cancellation
			hostCtx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()

			metrics, hostLock, latency, err := c.collectOneWithContext(hostCtx, alias)

			result := HostResult{
				Alias:   alias,
				Metrics: metrics,
				Latency: latency,
			}

			if err != nil {
				result.Error = err
			}

			// Lock status rides along in the batched metrics output
			if metrics != nil {
				result.LockInfo = hostLock
				result.ConnectedVia = c.pool.GetConnectedVia(alias)
			}

			results <- result
		}(alias)
	}

	// Close channel when all hosts complete
	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// lockDir returns the per-host rr lock directory to check on remote hosts.
// With per-host locking, there's a single lock at /tmp/rr.lock per host.
func (c *Collector) lockDir() string {
	baseDir := "/tmp"
	if c.lockConfig != nil && c.lockConfig.Dir != "" {
		baseDir = c.lockConfig.Dir
	}
	return baseDir + "/rr.lock"
}

// parseLockSection parses the lock info.json payload from the batched metrics
// output. Returns lock info if locked, nil otherwise (empty section = unlocked).
func (c *Collector) parseLockSection(section string) *HostLockInfo {
	section = strings.TrimSpace(section)
	if section == "" {
		// No lock held
		return nil
	}

	// Parse the lock info
	info, err := lock.ParseLockInfo([]byte(section))
	if err != nil {
		return nil
	}

	// Check if lock is stale (default to 30 minutes if no config)
	staleThreshold := 30 * time.Minute
	if c.lockConfig != nil && c.lockConfig.Stale > 0 {
		staleThreshold = c.lockConfig.Stale
	}
	if info.Age() > staleThreshold {
		return nil // Stale locks don't count
	}

	return &HostLockInfo{
		IsLocked: true,
		Holder:   info.String(),
		Started:  info.Started,
		Command:  info.Command,
	}
}

// collectOneWithContext gathers metrics from a single host with context for timeout.
// Returns the metrics, the lock status, the SSH probe latency, and any error.
// The latency is measured using a lightweight echo command, not the metrics collection time.
func (c *Collector) collectOneWithContext(ctx context.Context, alias string) (*HostMetrics, *HostLockInfo, time.Duration, error) {
	// Check for context cancellation early
	select {
	case <-ctx.Done():
		return nil, nil, 0, ctx.Err()
	default:
	}

	// Get connection with platform detection
	client, platform, err := c.pool.GetWithPlatform(alias)
	if err != nil {
		return nil, nil, 0, err
	}

	// First, measure actual SSH latency with a lightweight probe command.
	// This gives us real network latency, not metrics collection time.
	probeLatency, err := c.probeLatency(ctx, client)
	if err != nil {
		// Probe failed, but we can still try to collect metrics
		probeLatency = 0
	}

	// Build and execute the batched metrics command (includes lock status)
	cmd := BuildMetricsCommand(platform, c.lockDir())

	// Use embedded ssh.Client's NewSession directly for full session capabilities
	session, err := client.Client.NewSession()
	if err != nil {
		c.pool.CloseOne(alias)
		return nil, nil, probeLatency, err
	}
	defer session.Close()

	// Run metrics command with timeout (we don't track this time since it's not network latency)
	type result struct {
		output []byte
		err    error
	}
	resultCh := make(chan result, 1)

	go func() {
		out, err := session.CombinedOutput(cmd)
		resultCh <- result{out, err}
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		return nil, nil, probeLatency, ctx.Err()
	case r := <-resultCh:
		if r.err != nil {
			return nil, nil, probeLatency, r.err
		}
		metrics, lockInfo := c.parseOutput(alias, platform, string(r.output))
		return metrics, lockInfo, probeLatency, nil
	}
}

// probeLatency measures the actual SSH round-trip latency using a lightweight command.
// This is separate from metrics collection time, giving users accurate network latency.
func (c *Collector) probeLatency(ctx context.Context, client *sshutil.Client) (time.Duration, error) {
	session, err := client.Client.NewSession()
	if err != nil {
		return 0, err
	}
	defer session.Close()

	// Use a minimal command that completes instantly on the remote
	type result struct {
		latency time.Duration
		err     error
	}
	resultCh := make(chan result, 1)

	go func() {
		start := time.Now()
		// "echo 1" is fast and reliable across all platforms
		_, err := session.Output("echo 1")
		latency := time.Since(start)
		resultCh <- result{latency, err}
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		return 0, ctx.Err()
	case r := <-resultCh:
		return r.latency, r.err
	}
}

// parseOutput parses the batched command output into HostMetrics and lock status.
func (c *Collector) parseOutput(alias string, platform Platform, output string) (*HostMetrics, *HostLockInfo) {
	metrics := &HostMetrics{
		Timestamp: time.Now(),
	}

	// Split output by separator
	sections := strings.Split(output, OutputSeparator+"\n")

	lockSection := linuxLockSection
	switch platform {
	case PlatformLinux:
		metrics = c.parseLinuxOutput(alias, metrics, sections)
	case PlatformDarwin:
		metrics = c.parseDarwinOutput(metrics, sections)
		lockSection = darwinLockSection
	default:
		// Try Linux parsing as fallback
		metrics = c.parseLinuxOutput(alias, metrics, sections)
	}

	// The lock payload is the last section on both platforms
	var lockInfo *HostLockInfo
	if len(sections) > lockSection {
		lockInfo = c.parseLockSection(sections[lockSection])
	}

	return metrics, lockInfo
}

// parseLinuxOutput parses Linux metrics from the batched command output.
// Sections: 0=/proc/stat, 1=/proc/loadavg, 2=/proc/meminfo, 3=/proc/net/dev, 4=nvidia-smi,
// 5=ps aux, 6=df, 7=/proc/diskstats, 8=hwmon temps, 9=uptime+kernel, 10=lock info (parsed in parseOutput)
func (c *Collector) parseLinuxOutput(alias string, metrics *HostMetrics, sections []string) *HostMetrics {
	if len(sections) >= 2 {
		procStat := strings.TrimSpace(sections[0])
		procLoadavg := strings.TrimSpace(sections[1])

		cpu, err := c.parseLinuxCPUWithDelta(alias, procStat, procLoadavg)
		if err == nil && cpu != nil {
			metrics.CPU = *cpu
		}
	}

	if len(sections) >= 3 {
		procMeminfo := strings.TrimSpace(sections[2])
		ram, err := parseLinuxMemory(procMeminfo)
		if err == nil && ram != nil {
			metrics.RAM = *ram
		}
	}

	if len(sections) >= 4 {
		procNetDev := strings.TrimSpace(sections[3])
		network, err := parseLinuxNetwork(procNetDev)
		if err == nil {
			metrics.Network = network
		}
	}

	if len(sections) >= 5 {
		nvidiaSmi := strings.TrimSpace(sections[4])
		gpu, err := parseNvidiaSMI(nvidiaSmi)
		if err == nil && gpu != nil {
			metrics.GPU = gpu
		}
	}

	if len(sections) >= 6 {
		psOutput := strings.TrimSpace(sections[5])
		procs, err := parseProcesses(psOutput)
		if err == nil {
			metrics.Processes = procs
		}
	}

	if len(sections) >= 7 {
		if disk, ok := parseDF(strings.TrimSpace(sections[6])); ok {
			metrics.Disk = disk
		}
	}

	if len(sections) >= 8 {
		readRate, writeRate := c.parseLinuxDiskIOWithDelta(alias, strings.TrimSpace(sections[7]), time.Now())
		metrics.Disk.ReadBytesPerSec = readRate
		metrics.Disk.WriteBytesPerSec = writeRate
	}

	if len(sections) >= 9 {
		metrics.CPU.TempC = parseHwmonTemps(strings.TrimSpace(sections[8]))
	}

	if len(sections) >= 10 {
		metrics.System = parseLinuxSystemInfo(strings.TrimSpace(sections[9]))
	}

	return metrics
}

// parseDarwinOutput parses macOS metrics from the batched command output.
// Sections: 0=top, 1=vm_stat, 2=netstat, 3=ioreg GPU, 4=ps aux, 5=df,
// 6=hw.ncpu, 7=boottime+kernel, 8=lock info (parsed in parseOutput)
func (c *Collector) parseDarwinOutput(metrics *HostMetrics, sections []string) *HostMetrics {
	if len(sections) >= 1 {
		topOutput := strings.TrimSpace(sections[0])
		cpu, err := parseDarwinCPU(topOutput)
		if err == nil && cpu != nil {
			metrics.CPU = *cpu
		}
	}

	if len(sections) >= 2 {
		vmStatOutput := strings.TrimSpace(sections[1])
		ram, err := parseDarwinMemory(vmStatOutput)
		if err == nil && ram != nil {
			metrics.RAM = *ram
		}
	}

	if len(sections) >= 3 {
		netstatOutput := strings.TrimSpace(sections[2])
		network, err := parseDarwinNetwork(netstatOutput)
		if err == nil {
			metrics.Network = network
		}
	}

	// Parse Apple Silicon GPU metrics from ioreg output
	if len(sections) >= 4 {
		ioregOutput := strings.TrimSpace(sections[3])
		if gpu := parseAppleGPU(ioregOutput); gpu != nil {
			metrics.GPU = gpu
		}
	}

	if len(sections) >= 5 {
		psOutput := strings.TrimSpace(sections[4])
		procs, err := parseProcesses(psOutput)
		if err == nil {
			metrics.Processes = procs
		}
	}

	if len(sections) >= 6 {
		if disk, ok := parseDF(strings.TrimSpace(sections[5])); ok {
			metrics.Disk = disk
		}
	}

	if len(sections) >= 7 {
		if cores, err := strconv.Atoi(strings.TrimSpace(sections[6])); err == nil && cores > 0 {
			metrics.CPU.Cores = cores
		}
	}

	if len(sections) >= 8 {
		metrics.System = parseDarwinSystemInfo(strings.TrimSpace(sections[7]), time.Now())
	}

	return metrics
}

// Close closes all connections in the pool.
func (c *Collector) Close() {
	c.pool.Close()
}

// Hosts returns the list of host aliases being monitored.
func (c *Collector) Hosts() []string {
	aliases := make([]string, 0, len(c.hosts))
	for alias := range c.hosts {
		aliases = append(aliases, alias)
	}
	return aliases
}

// Inline parsing functions to avoid import cycle with parsers package

// parseLinuxCPUWithDelta calculates CPU usage from delta between two readings.
// This gives instantaneous CPU usage rather than average-since-boot.
func (c *Collector) parseLinuxCPUWithDelta(alias, procStat, procLoadavg string) (*CPUMetrics, error) {
	metrics := &CPUMetrics{}

	scanner := bufio.NewScanner(strings.NewReader(procStat))
	coreCount := 0
	var totalJiffies, idleJiffies int64
	var coreJiffies []cpuJiffies

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			coreCount++
			if j, ok := parseCPUJiffies(strings.Fields(line)); ok {
				coreJiffies = append(coreJiffies, j)
			}
			continue
		}

		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return nil, fmt.Errorf("invalid /proc/stat cpu line: %s", line)
			}

			for i := 1; i < len(fields); i++ {
				val, err := strconv.ParseInt(fields[i], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("failed to parse cpu field %d: %w", i, err)
				}
				totalJiffies += val

				// idle is field 4 (index 4), iowait is field 5 (index 5)
				if i == 4 || i == 5 {
					idleJiffies += val
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning /proc/stat: %w", err)
	}

	metrics.Cores = coreCount

	// Calculate CPU percentage from delta between this reading and previous
	c.mu.Lock()
	prev, hasPrev := c.prevJiffies[alias]
	c.prevJiffies[alias] = cpuJiffies{total: totalJiffies, idle: idleJiffies}
	prevCores := c.prevCoreJiffies[alias]
	c.prevCoreJiffies[alias] = coreJiffies
	c.mu.Unlock()

	if hasPrev && totalJiffies > prev.total {
		totalDelta := totalJiffies - prev.total
		idleDelta := idleJiffies - prev.idle
		if totalDelta > 0 {
			metrics.Percent = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
		}
	}
	// If no previous reading, Percent stays 0 (will show correct on next poll)

	// Per-core percentages from deltas. If the core count changed between
	// samples (e.g. host swap behind an alias), skip this round; the freshly
	// stored counters produce rates again on the next poll.
	if len(coreJiffies) > 0 && len(prevCores) == len(coreJiffies) {
		perCore := make([]float64, len(coreJiffies))
		for i, cur := range coreJiffies {
			totalDelta := cur.total - prevCores[i].total
			if totalDelta <= 0 {
				continue
			}
			idleDelta := cur.idle - prevCores[i].idle
			pct := float64(totalDelta-idleDelta) / float64(totalDelta) * 100
			if pct < 0 {
				pct = 0
			}
			perCore[i] = pct
		}
		metrics.PerCore = perCore
	}
	// First sample (or core-count change) leaves PerCore empty

	// Parse load averages
	if procLoadavg != "" {
		fields := strings.Fields(strings.TrimSpace(procLoadavg))
		if len(fields) >= 3 {
			for i := 0; i < 3; i++ {
				val, err := strconv.ParseFloat(fields[i], 64)
				if err != nil {
					return nil, fmt.Errorf("failed to parse loadavg field %d: %w", i, err)
				}
				metrics.LoadAvg[i] = val
			}
		}
	}

	return metrics, nil
}

// parseCPUJiffies parses total and idle jiffies from a /proc/stat cpu line's fields.
// fields[0] is the "cpuN" label; the rest are jiffy counters.
func parseCPUJiffies(fields []string) (cpuJiffies, bool) {
	if len(fields) < 5 {
		return cpuJiffies{}, false
	}
	var j cpuJiffies
	for i := 1; i < len(fields); i++ {
		val, err := strconv.ParseInt(fields[i], 10, 64)
		if err != nil {
			return cpuJiffies{}, false
		}
		j.total += val
		// idle is field 4, iowait is field 5 (same convention as the aggregate line)
		if i == 4 || i == 5 {
			j.idle += val
		}
	}
	return j, true
}

// parseDF parses `df -P -k /` output into root filesystem usage.
// POSIX -P output has fixed trailing columns, so fields are indexed from the
// right to tolerate device names containing spaces.
func parseDF(output string) (DiskMetrics, bool) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		n := len(fields)
		if n < 6 || fields[0] == "Filesystem" {
			continue
		}
		// Trailing columns: 1024-blocks, Used, Available, Capacity, Mounted on
		totalKB, err1 := strconv.ParseInt(fields[n-5], 10, 64)
		usedKB, err2 := strconv.ParseInt(fields[n-4], 10, 64)
		percent, err3 := strconv.ParseFloat(strings.TrimSuffix(fields[n-2], "%"), 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		return DiskMetrics{
			UsedBytes:  usedKB * 1024,
			TotalBytes: totalKB * 1024,
			Percent:    percent,
		}, true
	}
	return DiskMetrics{}, false
}

// diskDeviceRe matches whole-disk device names in /proc/diskstats.
// Partitions (sda1, nvme0n1p2, mmcblk0p1) and virtual devices (loop, ram, dm)
// are excluded so I/O isn't double-counted.
var diskDeviceRe = regexp.MustCompile(`^(sd[a-z]+|vd[a-z]+|nvme\d+n\d+|mmcblk\d+)$`)

// parseDiskstatsCounters sums cumulative read/written bytes across physical
// whole-disk devices from /proc/diskstats output. Sector counts (fields 5 and 9)
// are fixed 512-byte units regardless of the device's logical sector size.
func parseDiskstatsCounters(output string) (readBytes, writeBytes int64, ok bool) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || !diskDeviceRe.MatchString(fields[2]) {
			continue
		}
		sectorsRead, err := strconv.ParseInt(fields[5], 10, 64)
		if err != nil {
			continue
		}
		sectorsWritten, err := strconv.ParseInt(fields[9], 10, 64)
		if err != nil {
			continue
		}
		readBytes += sectorsRead * 512
		writeBytes += sectorsWritten * 512
		ok = true
	}
	return readBytes, writeBytes, ok
}

// parseLinuxDiskIOWithDelta computes disk read/write bytes/sec from the delta
// between consecutive /proc/diskstats samples, following the prevJiffies
// pattern. The first sample for a host yields zero rates.
func (c *Collector) parseLinuxDiskIOWithDelta(alias, diskstats string, now time.Time) (readRate, writeRate float64) {
	readBytes, writeBytes, ok := parseDiskstatsCounters(diskstats)
	if !ok {
		return 0, 0
	}

	c.mu.Lock()
	prev, hasPrev := c.prevDisk[alias]
	c.prevDisk[alias] = diskSample{readBytes: readBytes, writeBytes: writeBytes, at: now}
	c.mu.Unlock()

	if !hasPrev {
		return 0, 0
	}
	elapsed := now.Sub(prev.at).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}
	if readBytes >= prev.readBytes {
		readRate = float64(readBytes-prev.readBytes) / elapsed
	}
	if writeBytes >= prev.writeBytes {
		writeRate = float64(writeBytes-prev.writeBytes) / elapsed
	}
	return readRate, writeRate
}

// parseHwmonTemps picks a CPU temperature from hwmon "name:millidegrees" lines.
// Prefers CPU package sensors (coretemp on Intel, k10temp on AMD); if neither
// is present, falls back to the max across all sensors that reported a value.
// Returns 0 when no sensor is available.
func parseHwmonTemps(output string) float64 {
	var preferred, maxTemp float64
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		if valStr == "" {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		temp := val / 1000 // millidegrees C -> degrees C
		if name == "coretemp" || name == "k10temp" {
			if temp > preferred {
				preferred = temp
			}
		}
		if temp > maxTemp {
			maxTemp = temp
		}
	}
	if preferred > 0 {
		return preferred
	}
	return maxTemp
}

// parseLinuxSystemInfo parses the combined /proc/uptime + uname -r section.
// The uptime line starts with seconds-since-boot as a float; any other
// non-empty line is the kernel release.
func parseLinuxSystemInfo(section string) SystemInfo {
	info := SystemInfo{}
	scanner := bufio.NewScanner(strings.NewReader(section))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if info.Uptime == 0 {
			if secs, err := strconv.ParseFloat(fields[0], 64); err == nil {
				info.Uptime = time.Duration(secs * float64(time.Second))
				continue
			}
		}
		if info.Kernel == "" {
			info.Kernel = line
		}
	}
	if info.Uptime > 0 || info.Kernel != "" {
		info.OS = "Linux"
	}
	return info
}

// darwinBoottimeRe extracts the epoch seconds from `sysctl -n kern.boottime`
// output, e.g. `{ sec = 1753837432, usec = 314159 } Tue Jul 29 16:03:52 2026`.
var darwinBoottimeRe = regexp.MustCompile(`sec\s*=\s*(\d+)`)

// parseDarwinSystemInfo parses the combined kern.boottime + uname -r section.
// now is passed in so uptime math is testable.
func parseDarwinSystemInfo(section string, now time.Time) SystemInfo {
	info := SystemInfo{}
	scanner := bufio.NewScanner(strings.NewReader(section))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if match := darwinBoottimeRe.FindStringSubmatch(line); len(match) > 1 {
			if sec, err := strconv.ParseInt(match[1], 10, 64); err == nil {
				boot := time.Unix(sec, 0)
				if boot.Before(now) {
					info.Uptime = now.Sub(boot)
				}
			}
			continue
		}
		if info.Kernel == "" {
			info.Kernel = line
		}
	}
	if info.Uptime > 0 || info.Kernel != "" {
		info.OS = "macOS"
	}
	return info
}

// parseLinuxMemory parses memory metrics from /proc/meminfo output.
func parseLinuxMemory(procMeminfo string) (*RAMMetrics, error) {
	metrics := &RAMMetrics{}
	scanner := bufio.NewScanner(strings.NewReader(procMeminfo))

	var memTotal, memFree, memAvailable, buffers, cached int64
	foundFields := 0

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}

		valBytes := val * 1024

		switch key {
		case "MemTotal":
			memTotal = valBytes
			foundFields++
		case "MemFree":
			memFree = valBytes
			foundFields++
		case "MemAvailable":
			memAvailable = valBytes
			foundFields++
		case "Buffers":
			buffers = valBytes
			foundFields++
		case "Cached":
			cached = valBytes
			foundFields++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning /proc/meminfo: %w", err)
	}

	if foundFields < 3 {
		return nil, fmt.Errorf("insufficient memory info found in /proc/meminfo")
	}

	metrics.TotalBytes = memTotal
	metrics.Available = memAvailable
	metrics.Cached = cached + buffers
	metrics.UsedBytes = memTotal - memFree - buffers - cached

	return metrics, nil
}

// parseLinuxNetwork parses network interface metrics from /proc/net/dev output.
func parseLinuxNetwork(procNetDev string) ([]NetworkInterface, error) {
	var interfaces []NetworkInterface
	scanner := bufio.NewScanner(strings.NewReader(procNetDev))

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if lineNum <= 2 {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])

		if len(fields) < 16 {
			continue
		}

		bytesIn, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse bytes_in for %s: %w", name, err)
		}

		packetsIn, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse packets_in for %s: %w", name, err)
		}

		bytesOut, err := strconv.ParseInt(fields[8], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse bytes_out for %s: %w", name, err)
		}

		packetsOut, err := strconv.ParseInt(fields[9], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse packets_out for %s: %w", name, err)
		}

		interfaces = append(interfaces, NetworkInterface{
			Name:       name,
			BytesIn:    bytesIn,
			BytesOut:   bytesOut,
			PacketsIn:  packetsIn,
			PacketsOut: packetsOut,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning /proc/net/dev: %w", err)
	}

	return interfaces, nil
}

// parseNvidiaSMI parses GPU metrics from nvidia-smi CSV output.
func parseNvidiaSMI(output string) (*GPUMetrics, error) {
	output = strings.TrimSpace(output)

	if output == "" {
		return nil, nil
	}

	lowerOutput := strings.ToLower(output)
	if strings.Contains(lowerOutput, "no devices") ||
		strings.Contains(lowerOutput, "not found") ||
		strings.Contains(lowerOutput, "failed") ||
		strings.Contains(lowerOutput, "error") ||
		strings.Contains(lowerOutput, "command not found") {
		return nil, nil
	}

	fields := strings.Split(output, ",")
	if len(fields) < 6 {
		return nil, fmt.Errorf("nvidia-smi output has insufficient fields: expected 6, got %d", len(fields))
	}

	metrics := &GPUMetrics{}
	metrics.Name = strings.TrimSpace(fields[0])

	utilStr := strings.TrimSpace(fields[1])
	if utilStr != "" && utilStr != "[N/A]" {
		util, err := strconv.ParseFloat(utilStr, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU utilization '%s': %w", utilStr, err)
		}
		metrics.Percent = util
	}

	memUsedStr := strings.TrimSpace(fields[2])
	if memUsedStr != "" && memUsedStr != "[N/A]" {
		memUsed, err := strconv.ParseInt(memUsedStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU memory used '%s': %w", memUsedStr, err)
		}
		metrics.MemoryUsed = memUsed * 1024 * 1024
	}

	memTotalStr := strings.TrimSpace(fields[3])
	if memTotalStr != "" && memTotalStr != "[N/A]" {
		memTotal, err := strconv.ParseInt(memTotalStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU memory total '%s': %w", memTotalStr, err)
		}
		metrics.MemoryTotal = memTotal * 1024 * 1024
	}

	tempStr := strings.TrimSpace(fields[4])
	if tempStr != "" && tempStr != "[N/A]" {
		temp, err := strconv.Atoi(tempStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU temperature '%s': %w", tempStr, err)
		}
		metrics.Temperature = temp
	}

	powerStr := strings.TrimSpace(fields[5])
	if powerStr != "" && powerStr != "[N/A]" {
		power, err := strconv.ParseFloat(powerStr, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU power '%s': %w", powerStr, err)
		}
		metrics.PowerWatts = int(power)
	}

	return metrics, nil
}

// Precompiled regexes for Apple Silicon GPU parsing (hot path: every tick per macOS host).
var (
	appleGPUModelRe      = regexp.MustCompile(`"model"\s*=\s*"([^"]+)"`)
	appleGPUPerfRe       = regexp.MustCompile(`"PerformanceStatistics"\s*=\s*\{([^}]+)\}`)
	appleGPUDeviceUtilRe = regexp.MustCompile(`"` + regexp.QuoteMeta("Device Utilization %") + `"\s*=\s*([\d.]+)`)
	appleGPUInUseMemRe   = regexp.MustCompile(`"` + regexp.QuoteMeta("In use system memory") + `"\s*=\s*(\d+)`)
	appleGPUAllocMemRe   = regexp.MustCompile(`"` + regexp.QuoteMeta("Alloc system memory") + `"\s*=\s*(\d+)`)
)

// parseAppleGPU parses GPU metrics from Apple Silicon ioreg output.
// Expected input format (filtered grep output):
//
//	"PerformanceStatistics" = {"Device Utilization %"=0,"In use system memory"=123456,...}
//	"model" = "Apple M4"
//	"gpu-core-count" = 10
//
// Returns nil if no GPU data is available or parsing fails.
func parseAppleGPU(output string) *GPUMetrics {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil // No GPU data
	}

	metrics := &GPUMetrics{}

	// Parse model name: "model" = "Apple M4"
	if match := appleGPUModelRe.FindStringSubmatch(output); len(match) > 1 {
		metrics.Name = match[1]
	}

	// Parse PerformanceStatistics
	if match := appleGPUPerfRe.FindStringSubmatch(output); len(match) > 1 {
		stats := match[1]

		// Device Utilization % - this is the main GPU utilization metric
		if val := extractAppleGPUStat(stats, appleGPUDeviceUtilRe); val >= 0 {
			metrics.Percent = val
		}

		// In use system memory (bytes)
		if val := extractAppleGPUStatInt(stats, appleGPUInUseMemRe); val >= 0 {
			metrics.MemoryUsed = val
		}

		// Alloc system memory (bytes) - use as total
		if val := extractAppleGPUStatInt(stats, appleGPUAllocMemRe); val >= 0 {
			metrics.MemoryTotal = val
		}
	}

	// If we didn't get any useful data, return nil
	if metrics.Name == "" && metrics.Percent == 0 && metrics.MemoryUsed == 0 {
		return nil
	}

	return metrics
}

// extractAppleGPUStat extracts a float value from the PerformanceStatistics string.
func extractAppleGPUStat(stats string, re *regexp.Regexp) float64 {
	if match := re.FindStringSubmatch(stats); len(match) > 1 {
		val, err := strconv.ParseFloat(match[1], 64)
		if err == nil {
			return val
		}
	}
	return -1
}

// extractAppleGPUStatInt extracts an int64 value from the PerformanceStatistics string.
func extractAppleGPUStatInt(stats string, re *regexp.Regexp) int64 {
	if match := re.FindStringSubmatch(stats); len(match) > 1 {
		val, err := strconv.ParseInt(match[1], 10, 64)
		if err == nil {
			return val
		}
	}
	return -1
}

// parseDarwinCPU parses CPU metrics from macOS top command output.
func parseDarwinCPU(topOutput string) (*CPUMetrics, error) {
	metrics := &CPUMetrics{}
	scanner := bufio.NewScanner(strings.NewReader(topOutput))

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "CPU usage:") {
			metrics.Percent = parseDarwinCPUUsage(line)
		}

		if strings.HasPrefix(line, "Load Avg:") {
			loadAvg := parseDarwinLoadAvg(line)
			metrics.LoadAvg = loadAvg
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning top output: %w", err)
	}

	metrics.Cores = 0

	return metrics, nil
}

func parseDarwinCPUUsage(line string) float64 {
	parts := strings.Split(line, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "idle") {
			fields := strings.Fields(part)
			if len(fields) >= 1 {
				pctStr := strings.TrimSuffix(fields[0], "%")
				idle, err := strconv.ParseFloat(pctStr, 64)
				if err == nil {
					return 100 - idle
				}
			}
		}
	}
	return 0
}

func parseDarwinLoadAvg(line string) [3]float64 {
	var loadAvg [3]float64

	colonIdx := strings.Index(line, ":")
	if colonIdx < 0 {
		return loadAvg
	}

	valuesStr := strings.TrimSpace(line[colonIdx+1:])
	parts := strings.Split(valuesStr, ",")

	for i := 0; i < 3 && i < len(parts); i++ {
		val, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		if err == nil {
			loadAvg[i] = val
		}
	}

	return loadAvg
}

// parseDarwinMemory parses memory metrics from macOS vm_stat and sysctl hw.memsize output.
func parseDarwinMemory(vmStatOutput string) (*RAMMetrics, error) {
	metrics := &RAMMetrics{}
	scanner := bufio.NewScanner(strings.NewReader(vmStatOutput))

	pageSize := int64(16384)

	var pagesActive, pagesWired, pagesInactive, pagesSpeculative, pagesFree int64
	var pagesCompressed, pagesPurgeable, pagesCached int64
	var totalMemBytes int64

	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, "page size of") {
			start := strings.Index(line, "page size of")
			if start >= 0 {
				rest := line[start+len("page size of"):]
				rest = strings.TrimSpace(rest)
				fields := strings.Fields(rest)
				if len(fields) >= 1 {
					size, err := strconv.ParseInt(fields[0], 10, 64)
					if err == nil {
						pageSize = size
					}
				}
			}
			continue
		}

		// Parse sysctl hw.memsize output: "hw.memsize: 17179869184"
		if strings.HasPrefix(line, "hw.memsize:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				val, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
				if err == nil {
					totalMemBytes = val
				}
			}
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:colonIdx])
		valStr := strings.TrimSpace(line[colonIdx+1:])
		valStr = strings.TrimSuffix(valStr, ".")

		val, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "Pages active":
			pagesActive = val
		case "Pages wired down":
			pagesWired = val
		case "Pages inactive":
			pagesInactive = val
		case "Pages speculative":
			pagesSpeculative = val
		case "Pages free":
			pagesFree = val
		case "Pages occupied by compressor":
			pagesCompressed = val
		case "Pages purgeable":
			pagesPurgeable = val
		case "File-backed pages":
			pagesCached = val
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning vm_stat output: %w", err)
	}

	// Used = active + wired + compressed (speculative is part of free)
	usedPages := pagesActive + pagesWired + pagesCompressed
	// Available = free + inactive + purgeable + speculative (memory that can be reclaimed)
	availablePages := pagesFree + pagesInactive + pagesPurgeable + pagesSpeculative

	metrics.UsedBytes = usedPages * pageSize
	metrics.Available = availablePages * pageSize
	metrics.Cached = pagesCached * pageSize

	// Use sysctl hw.memsize for accurate total, fall back to calculation if not available
	if totalMemBytes > 0 {
		metrics.TotalBytes = totalMemBytes
	} else {
		// Fallback: estimate from page counts (less accurate)
		metrics.TotalBytes = (usedPages + availablePages) * pageSize
	}

	return metrics, nil
}

// parseDarwinNetwork parses network interface metrics from macOS netstat command output.
func parseDarwinNetwork(netstatOutput string) ([]NetworkInterface, error) {
	var interfaces []NetworkInterface
	scanner := bufio.NewScanner(strings.NewReader(netstatOutput))

	headerSkipped := false
	seenInterfaces := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Text()

		if !headerSkipped {
			if strings.HasPrefix(line, "Name") {
				headerSkipped = true
			}
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		name := fields[0]

		if seenInterfaces[name] {
			continue
		}

		isLinkRow := false
		for _, f := range fields {
			if strings.HasPrefix(f, "<Link#") {
				isLinkRow = true
				break
			}
		}

		if !isLinkRow {
			continue
		}

		seenInterfaces[name] = true

		var ipkts, ibytes, opkts, obytes int64
		numericFields := []int64{}

		for i := 1; i < len(fields); i++ {
			val, err := strconv.ParseInt(fields[i], 10, 64)
			if err == nil {
				numericFields = append(numericFields, val)
			}
		}

		if len(numericFields) >= 7 {
			ipkts = numericFields[1]
			ibytes = numericFields[3]
			opkts = numericFields[4]
			obytes = numericFields[6]
		}

		interfaces = append(interfaces, NetworkInterface{
			Name:       name,
			BytesIn:    ibytes,
			BytesOut:   obytes,
			PacketsIn:  ipkts,
			PacketsOut: opkts,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning netstat output: %w", err)
	}

	return interfaces, nil
}

// parseProcesses parses ps aux output into a slice of ProcessInfo.
// Works for both Linux and macOS ps aux output formats.
// ps aux columns: USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND
func parseProcesses(output string) ([]ProcessInfo, error) {
	var procs []ProcessInfo
	scanner := bufio.NewScanner(strings.NewReader(output))

	// Skip header line (USER PID %CPU %MEM ...)
	scanner.Scan()

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		cpu, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			cpu = 0
		}

		mem, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			mem = 0
		}

		// TIME is typically at index 9, COMMAND starts at index 10
		timeStr := fields[9]
		command := strings.Join(fields[10:], " ")

		// Truncate command to reasonable length
		if len(command) > 50 {
			command = command[:47] + "..."
		}

		procs = append(procs, ProcessInfo{
			PID:     pid,
			User:    fields[0],
			CPU:     cpu,
			Memory:  mem,
			Time:    timeStr,
			Command: command,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning ps output: %w", err)
	}

	return procs, nil
}
