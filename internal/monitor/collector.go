package monitor

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
)

// Collector gathers system metrics from multiple remote hosts.
type Collector struct {
	hosts   map[string]config.Host
	pool    *Pool
	timeout time.Duration
}

// NewCollector creates a new metrics collector for the specified hosts.
func NewCollector(hosts map[string]config.Host) *Collector {
	return &Collector{
		hosts:   hosts,
		pool:    NewPool(10 * time.Second),
		timeout: 30 * time.Second,
	}
}

// SetTimeout sets the per-host collection timeout.
func (c *Collector) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

// Collect gathers metrics from all configured hosts in parallel.
// Returns a map of alias -> metrics. Hosts that fail to connect will have nil metrics.
func (c *Collector) Collect() map[string]*HostMetrics {
	results := make(map[string]*HostMetrics)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for alias := range c.hosts {
		wg.Add(1)
		go func(alias string) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
			defer cancel()

			metrics, err := c.collectOneWithContext(ctx, alias)
			if err != nil {
				// Log error but continue - don't block other hosts
				metrics = nil
			}

			mu.Lock()
			results[alias] = metrics
			mu.Unlock()
		}(alias)
	}

	wg.Wait()
	return results
}

// CollectOne gathers metrics from a single host.
func (c *Collector) CollectOne(alias string) (*HostMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.collectOneWithContext(ctx, alias)
}

// collectOneWithContext gathers metrics from a single host with context for timeout.
func (c *Collector) collectOneWithContext(ctx context.Context, alias string) (*HostMetrics, error) {
	// Check for context cancellation early
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Get connection with platform detection
	client, platform, err := c.pool.GetWithPlatform(alias)
	if err != nil {
		return nil, err
	}

	// Build and execute the batched metrics command
	cmd := BuildMetricsCommand(platform)

	// Use embedded ssh.Client's NewSession directly for full session capabilities
	session, err := client.Client.NewSession()
	if err != nil {
		c.pool.CloseOne(alias)
		return nil, err
	}
	defer session.Close()

	// Run command with timeout
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
		return nil, ctx.Err()
	case r := <-resultCh:
		if r.err != nil {
			return nil, r.err
		}
		return c.parseOutput(platform, string(r.output))
	}
}

// parseOutput parses the batched command output into HostMetrics.
func (c *Collector) parseOutput(platform Platform, output string) (*HostMetrics, error) {
	metrics := &HostMetrics{
		Timestamp: time.Now(),
	}

	// Split output by separator
	sections := strings.Split(output, OutputSeparator+"\n")

	switch platform {
	case PlatformLinux:
		return c.parseLinuxOutput(metrics, sections)
	case PlatformDarwin:
		return c.parseDarwinOutput(metrics, sections)
	default:
		// Try Linux parsing as fallback
		return c.parseLinuxOutput(metrics, sections)
	}
}

// parseLinuxOutput parses Linux metrics from the batched command output.
// Sections: 0=/proc/stat, 1=/proc/loadavg, 2=/proc/meminfo, 3=/proc/net/dev, 4=nvidia-smi
func (c *Collector) parseLinuxOutput(metrics *HostMetrics, sections []string) (*HostMetrics, error) {
	if len(sections) >= 2 {
		procStat := strings.TrimSpace(sections[0])
		procLoadavg := strings.TrimSpace(sections[1])

		cpu, err := parseLinuxCPU(procStat, procLoadavg)
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

	return metrics, nil
}

// parseDarwinOutput parses macOS metrics from the batched command output.
// Sections: 0=top, 1=vm_stat, 2=netstat
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

	// macOS doesn't have nvidia-smi GPU support in this implementation
	metrics.GPU = nil

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

// parseDarwinMemory parses memory metrics from macOS vm_stat command output.
func parseDarwinMemory(vmStatOutput string) (*RAMMetrics, error) {
	metrics := &RAMMetrics{}
	scanner := bufio.NewScanner(strings.NewReader(vmStatOutput))

	pageSize := int64(16384)

	var pagesActive, pagesWired, pagesInactive, pagesSpeculative, pagesFree int64
	var pagesCompressed, pagesPurgeable, pagesCached int64

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

	usedPages := pagesActive + pagesWired + pagesCompressed + pagesSpeculative
	availablePages := pagesFree + pagesInactive + pagesPurgeable
	totalPages := usedPages + availablePages + pagesInactive

	metrics.UsedBytes = usedPages * pageSize
	metrics.TotalBytes = totalPages * pageSize
	metrics.Available = availablePages * pageSize
	metrics.Cached = pagesCached * pageSize

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
