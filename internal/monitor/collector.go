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

// Collector gathers system metrics from multiple remote hosts.
type Collector struct {
	hosts       map[string]config.Host
	pool        *Pool
	timeout     time.Duration
	prevJiffies map[string]cpuJiffies // Previous CPU jiffies per host for delta calculation
	mu          sync.Mutex            // Protects prevJiffies

	// Lock checking configuration (optional)
	lockConfig *config.LockConfig
}

// NewCollector creates a new metrics collector for the specified hosts.
func NewCollector(hosts map[string]config.Host) *Collector {
	return &Collector{
		hosts:       hosts,
		pool:        NewPool(hosts, 10*time.Second),
		timeout:     30 * time.Second,
		prevJiffies: make(map[string]cpuJiffies),
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

// Collect gathers metrics from all configured hosts in parallel.
// Returns a map of alias -> metrics, a map of alias -> error message,
// and a map of alias -> lock info.
// Hosts that fail to connect will have nil metrics and an error message.
func (c *Collector) Collect() (map[string]*HostMetrics, map[string]string, map[string]*HostLockInfo) {
	results := make(map[string]*HostMetrics)
	errors := make(map[string]string)
	lockInfo := make(map[string]*HostLockInfo)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for alias := range c.hosts {
		wg.Add(1)
		go func(alias string) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
			defer cancel()

			metrics, _, err := c.collectOneWithContext(ctx, alias)

			mu.Lock()
			if err != nil {
				// Store error message for diagnostics
				errors[alias] = err.Error()
				metrics = nil
			}
			results[alias] = metrics

			// Check lock status if we have a connection
			// Always check for locks so monitor shows when hosts are busy
			if metrics != nil {
				info := c.checkLockStatus(alias)
				if info != nil {
					lockInfo[alias] = info
				}
			}
			mu.Unlock()
		}(alias)
	}

	wg.Wait()
	return results, errors, lockInfo
}

// CollectOne gathers metrics from a single host.
func (c *Collector) CollectOne(alias string) (*HostMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	metrics, _, err := c.collectOneWithContext(ctx, alias)
	return metrics, err
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

			metrics, latency, err := c.collectOneWithContext(hostCtx, alias)

			result := HostResult{
				Alias:   alias,
				Metrics: metrics,
				Latency: latency,
			}

			if err != nil {
				result.Error = err
			}

			// Check lock status and get connection info if we got metrics
			if metrics != nil {
				result.LockInfo = c.checkLockStatus(alias)
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

// checkLockStatus checks if the rr lock is held on the specified host.
// Returns lock info if locked, nil otherwise.
// With per-host locking, there's a single lock at /tmp/rr.lock per host.
func (c *Collector) checkLockStatus(alias string) *HostLockInfo {
	client, err := c.pool.Get(alias)
	if err != nil {
		return nil
	}

	// Check for the per-host lock at /tmp/rr.lock
	baseDir := "/tmp"
	if c.lockConfig != nil && c.lockConfig.Dir != "" {
		baseDir = c.lockConfig.Dir
	}
	lockDir := baseDir + "/rr.lock"

	// Check if lock directory exists and read its info
	findCmd := fmt.Sprintf(
		`if [ -d %q ] && [ -f %q/info.json ]; then cat %q/info.json; else exit 1; fi`,
		lockDir, lockDir, lockDir,
	)

	// Use embedded ssh.Client's NewSession directly
	session, err := client.Client.NewSession()
	if err != nil {
		return nil
	}
	defer session.Close()

	output, err := session.Output(findCmd)
	if err != nil {
		// No locks found or error reading
		return nil
	}

	// Parse the lock info
	info, err := lock.ParseLockInfo(output)
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
// Returns the metrics, the SSH probe latency, and any error.
// The latency is measured using a lightweight echo command, not the metrics collection time.
func (c *Collector) collectOneWithContext(ctx context.Context, alias string) (*HostMetrics, time.Duration, error) {
	// Check for context cancellation early
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	default:
	}

	// Get connection with platform detection
	client, platform, err := c.pool.GetWithPlatform(alias)
	if err != nil {
		return nil, 0, err
	}

	// First, measure actual SSH latency with a lightweight probe command.
	// This gives us real network latency, not metrics collection time.
	probeLatency, err := c.probeLatency(ctx, client)
	if err != nil {
		// Probe failed, but we can still try to collect metrics
		probeLatency = 0
	}

	// Build and execute the batched metrics command
	cmd := BuildMetricsCommand(platform)

	// Use embedded ssh.Client's NewSession directly for full session capabilities
	session, err := client.Client.NewSession()
	if err != nil {
		c.pool.CloseOne(alias)
		return nil, probeLatency, err
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
		return nil, probeLatency, ctx.Err()
	case r := <-resultCh:
		if r.err != nil {
			return nil, probeLatency, r.err
		}
		metrics, err := c.parseOutput(alias, platform, string(r.output))
		return metrics, probeLatency, err
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

// parseOutput parses the batched command output into HostMetrics.
func (c *Collector) parseOutput(alias string, platform Platform, output string) (*HostMetrics, error) {
	metrics := &HostMetrics{
		Timestamp: time.Now(),
	}

	// Split output by separator
	sections := strings.Split(output, OutputSeparator+"\n")

	switch platform {
	case PlatformLinux:
		return c.parseLinuxOutput(alias, metrics, sections)
	case PlatformDarwin:
		return c.parseDarwinOutput(metrics, sections)
	default:
		// Try Linux parsing as fallback
		return c.parseLinuxOutput(alias, metrics, sections)
	}
}

// parseLinuxOutput parses Linux metrics from the batched command output.
// Sections: 0=/proc/stat, 1=/proc/loadavg, 2=/proc/meminfo, 3=/proc/net/dev, 4=nvidia-smi, 5=ps aux
func (c *Collector) parseLinuxOutput(alias string, metrics *HostMetrics, sections []string) (*HostMetrics, error) {
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

	return metrics, nil
}

// parseDarwinOutput parses macOS metrics from the batched command output.
// Sections: 0=top, 1=vm_stat, 2=netstat, 3=ioreg GPU, 4=ps aux
func (c *Collector) parseDarwinOutput(metrics *HostMetrics, sections []string) (*HostMetrics, error) {
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

	return metrics, nil
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

// parseLinuxCPU parses CPU metrics from /proc/stat and /proc/loadavg output.
func parseLinuxCPU(procStat, procLoadavg string) (*CPUMetrics, error) {
	metrics := &CPUMetrics{}

	scanner := bufio.NewScanner(strings.NewReader(procStat))
	coreCount := 0
	var totalJiffies, idleJiffies int64

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			coreCount++
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

				if i == 4 || i == 5 {
					idleJiffies += val
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning /proc/stat: %w", err)
	}

	if totalJiffies > 0 {
		metrics.Percent = float64(totalJiffies-idleJiffies) / float64(totalJiffies) * 100
	}
	metrics.Cores = coreCount

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

// parseLinuxCPUWithDelta calculates CPU usage from delta between two readings.
// This gives instantaneous CPU usage rather than average-since-boot.
func (c *Collector) parseLinuxCPUWithDelta(alias, procStat, procLoadavg string) (*CPUMetrics, error) {
	metrics := &CPUMetrics{}

	scanner := bufio.NewScanner(strings.NewReader(procStat))
	coreCount := 0
	var totalJiffies, idleJiffies int64

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			coreCount++
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
	c.mu.Unlock()

	if hasPrev && totalJiffies > prev.total {
		totalDelta := totalJiffies - prev.total
		idleDelta := idleJiffies - prev.idle
		if totalDelta > 0 {
			metrics.Percent = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
		}
	}
	// If no previous reading, Percent stays 0 (will show correct on next poll)

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
	modelRe := regexp.MustCompile(`"model"\s*=\s*"([^"]+)"`)
	if match := modelRe.FindStringSubmatch(output); len(match) > 1 {
		metrics.Name = match[1]
	}

	// Parse PerformanceStatistics
	perfRe := regexp.MustCompile(`"PerformanceStatistics"\s*=\s*\{([^}]+)\}`)
	if match := perfRe.FindStringSubmatch(output); len(match) > 1 {
		stats := match[1]

		// Device Utilization % - this is the main GPU utilization metric
		if val := extractAppleGPUStat(stats, "Device Utilization %"); val >= 0 {
			metrics.Percent = val
		}

		// In use system memory (bytes)
		if val := extractAppleGPUStatInt(stats, "In use system memory"); val >= 0 {
			metrics.MemoryUsed = val
		}

		// Alloc system memory (bytes) - use as total
		if val := extractAppleGPUStatInt(stats, "Alloc system memory"); val >= 0 {
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
func extractAppleGPUStat(stats, key string) float64 {
	escapedKey := regexp.QuoteMeta(key)
	re := regexp.MustCompile(`"` + escapedKey + `"\s*=\s*([\d.]+)`)
	if match := re.FindStringSubmatch(stats); len(match) > 1 {
		val, err := strconv.ParseFloat(match[1], 64)
		if err == nil {
			return val
		}
	}
	return -1
}

// extractAppleGPUStatInt extracts an int64 value from the PerformanceStatistics string.
func extractAppleGPUStatInt(stats, key string) int64 {
	escapedKey := regexp.QuoteMeta(key)
	re := regexp.MustCompile(`"` + escapedKey + `"\s*=\s*(\d+)`)
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
