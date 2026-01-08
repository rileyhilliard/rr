package parsers

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/rileyhilliard/rr/internal/monitor"
)

// ParseLinuxCPU parses CPU metrics from /proc/stat and /proc/loadavg output.
// The input should contain both files concatenated, with /proc/stat first
// followed by /proc/loadavg on a separate line.
func ParseLinuxCPU(procStat, procLoadavg string) (*monitor.CPUMetrics, error) {
	metrics := &monitor.CPUMetrics{}

	// Parse /proc/stat for CPU usage
	scanner := bufio.NewScanner(strings.NewReader(procStat))
	coreCount := 0
	var totalJiffies, idleJiffies int64

	for scanner.Scan() {
		line := scanner.Text()

		// Count individual CPU cores (cpu0, cpu1, etc.)
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			coreCount++
			continue
		}

		// Parse aggregate CPU line for usage calculation
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return nil, fmt.Errorf("invalid /proc/stat cpu line: %s", line)
			}

			// Fields: cpu user nice system idle iowait irq softirq steal guest guest_nice
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

	if totalJiffies > 0 {
		metrics.Percent = float64(totalJiffies-idleJiffies) / float64(totalJiffies) * 100
	}
	metrics.Cores = coreCount

	// Parse /proc/loadavg for load averages
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

// ParseLinuxMemory parses memory metrics from /proc/meminfo output.
func ParseLinuxMemory(procMeminfo string) (*monitor.RAMMetrics, error) {
	metrics := &monitor.RAMMetrics{}
	scanner := bufio.NewScanner(strings.NewReader(procMeminfo))

	var memTotal, memFree, memAvailable, buffers, cached int64
	foundFields := 0

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		// Values in /proc/meminfo are in kB
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}

		// Convert from kB to bytes
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
	metrics.Cached = cached + buffers // Combined cache
	metrics.UsedBytes = memTotal - memFree - buffers - cached

	return metrics, nil
}

// ParseLinuxNetwork parses network interface metrics from /proc/net/dev output.
func ParseLinuxNetwork(procNetDev string) ([]monitor.NetworkInterface, error) {
	var interfaces []monitor.NetworkInterface
	scanner := bufio.NewScanner(strings.NewReader(procNetDev))

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip header lines (first two lines)
		if lineNum <= 2 {
			continue
		}

		// Format: "  iface: bytes packets errs drop fifo frame compressed multicast | bytes packets..."
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])

		// Need at least 16 fields (8 receive + 8 transmit)
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

		interfaces = append(interfaces, monitor.NetworkInterface{
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
