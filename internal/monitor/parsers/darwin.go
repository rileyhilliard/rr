package parsers

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/rileyhilliard/rr/internal/monitor"
)

// ParseDarwinCPU parses CPU metrics from macOS top command output.
// Expected input is from: top -l 1 -n 0
func ParseDarwinCPU(topOutput string) (*monitor.CPUMetrics, error) {
	metrics := &monitor.CPUMetrics{}
	scanner := bufio.NewScanner(strings.NewReader(topOutput))

	for scanner.Scan() {
		line := scanner.Text()

		// Parse CPU usage line: "CPU usage: 5.26% user, 10.52% sys, 84.21% idle"
		if strings.HasPrefix(line, "CPU usage:") {
			metrics.Percent = parseDarwinCPUUsage(line)
		}

		// Parse load averages: "Load Avg: 1.23, 2.34, 3.45"
		if strings.HasPrefix(line, "Load Avg:") {
			loadAvg := parseDarwinLoadAvg(line)
			metrics.LoadAvg = loadAvg
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning top output: %w", err)
	}

	// Default core count - in practice you'd get this from sysctl
	metrics.Cores = 0

	return metrics, nil
}

// parseDarwinCPUUsage extracts total CPU usage from top's CPU usage line.
func parseDarwinCPUUsage(line string) float64 {
	// Format: "CPU usage: 5.26% user, 10.52% sys, 84.21% idle"
	// We want to return (100 - idle)

	parts := strings.Split(line, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "idle") {
			// Extract the percentage value
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

// parseDarwinLoadAvg extracts load averages from top's Load Avg line.
func parseDarwinLoadAvg(line string) [3]float64 {
	var loadAvg [3]float64

	// Format: "Load Avg: 1.23, 2.34, 3.45"
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

// ParseDarwinMemory parses memory metrics from macOS vm_stat command output.
// Expected input is from: vm_stat
func ParseDarwinMemory(vmStatOutput string) (*monitor.RAMMetrics, error) {
	metrics := &monitor.RAMMetrics{}
	scanner := bufio.NewScanner(strings.NewReader(vmStatOutput))

	// vm_stat reports pages, we need to get the page size first
	// Default page size on macOS is 16384 bytes (16KB) on Apple Silicon, 4096 (4KB) on Intel
	pageSize := int64(16384)

	var pagesActive, pagesWired, pagesInactive, pagesSpeculative, pagesFree int64
	var pagesCompressed, pagesPurgeable, pagesCached int64

	for scanner.Scan() {
		line := scanner.Text()

		// First line contains page size: "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
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

		// Parse key-value pairs like "Pages active:    123456."
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

	// Calculate memory values
	// Note: vm_stat doesn't give us total memory directly, we'd need sysctl for that
	// For now, we calculate what we can from the available data

	usedPages := pagesActive + pagesWired + pagesCompressed + pagesSpeculative
	availablePages := pagesFree + pagesInactive + pagesPurgeable

	// Total is approximated from used + available
	totalPages := usedPages + availablePages + pagesInactive

	metrics.UsedBytes = usedPages * pageSize
	metrics.TotalBytes = totalPages * pageSize
	metrics.Available = availablePages * pageSize
	metrics.Cached = pagesCached * pageSize

	return metrics, nil
}

// ParseDarwinNetwork parses network interface metrics from macOS netstat command output.
// Expected input is from: netstat -ib
func ParseDarwinNetwork(netstatOutput string) ([]monitor.NetworkInterface, error) {
	var interfaces []monitor.NetworkInterface
	scanner := bufio.NewScanner(strings.NewReader(netstatOutput))

	// Skip header line
	headerSkipped := false
	seenInterfaces := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip header
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

		// netstat -ib format:
		// Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
		// en0   1500  <Link#4>      xx:xx:xx:xx:xx:xx  12345     0   12345678    67890     0    9876543     0

		name := fields[0]

		// Skip duplicate entries (netstat shows multiple rows per interface)
		if seenInterfaces[name] {
			continue
		}

		// We want the link-level stats (the row with <Link#...>)
		// These have the total bytes, not per-protocol
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

		// Find the numeric columns - they're after the address field
		// Format varies, so we look for the pattern: name mtu network address ipkts ierrs ibytes opkts oerrs obytes
		// The address field contains colons (MAC address), so we find it and parse from there

		var ipkts, ibytes, opkts, obytes int64
		numericFields := []int64{}

		// Parse all numeric fields from the line
		for i := 1; i < len(fields); i++ {
			val, err := strconv.ParseInt(fields[i], 10, 64)
			if err == nil {
				numericFields = append(numericFields, val)
			}
		}

		// We expect at least: mtu, ipkts, ierrs, ibytes, opkts, oerrs, obytes, coll (8 numbers)
		if len(numericFields) >= 7 {
			// Skip MTU (first numeric), then: ipkts, ierrs, ibytes, opkts, oerrs, obytes
			ipkts = numericFields[1]  // packets in
			ibytes = numericFields[3] // bytes in
			opkts = numericFields[4]  // packets out
			obytes = numericFields[6] // bytes out
		}

		interfaces = append(interfaces, monitor.NetworkInterface{
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
