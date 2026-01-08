package parsers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rileyhilliard/rr/internal/monitor"
)

// ParseNvidiaSMI parses GPU metrics from nvidia-smi CSV output.
// Expected input is from: nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv,noheader,nounits
//
// Returns nil, nil if the GPU is not available (empty output or command failure indicator).
func ParseNvidiaSMI(output string) (*monitor.GPUMetrics, error) {
	output = strings.TrimSpace(output)

	// Handle missing GPU gracefully
	if output == "" {
		return nil, nil
	}

	// Check for common error indicators
	lowerOutput := strings.ToLower(output)
	if strings.Contains(lowerOutput, "no devices") ||
		strings.Contains(lowerOutput, "not found") ||
		strings.Contains(lowerOutput, "failed") ||
		strings.Contains(lowerOutput, "error") ||
		strings.Contains(lowerOutput, "command not found") {
		return nil, nil
	}

	// Parse CSV format: name, utilization.gpu, memory.used, memory.total, temperature.gpu, power.draw
	// Example: "NVIDIA GeForce RTX 3080, 45, 2048, 10240, 65, 220"
	fields := strings.Split(output, ",")
	if len(fields) < 6 {
		return nil, fmt.Errorf("nvidia-smi output has insufficient fields: expected 6, got %d", len(fields))
	}

	metrics := &monitor.GPUMetrics{}

	// Field 0: GPU name
	metrics.Name = strings.TrimSpace(fields[0])

	// Field 1: GPU utilization percentage
	utilStr := strings.TrimSpace(fields[1])
	if utilStr != "" && utilStr != "[N/A]" {
		util, err := strconv.ParseFloat(utilStr, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU utilization '%s': %w", utilStr, err)
		}
		metrics.Percent = util
	}

	// Field 2: Memory used (MiB)
	memUsedStr := strings.TrimSpace(fields[2])
	if memUsedStr != "" && memUsedStr != "[N/A]" {
		memUsed, err := strconv.ParseInt(memUsedStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU memory used '%s': %w", memUsedStr, err)
		}
		// Convert MiB to bytes
		metrics.MemoryUsed = memUsed * 1024 * 1024
	}

	// Field 3: Memory total (MiB)
	memTotalStr := strings.TrimSpace(fields[3])
	if memTotalStr != "" && memTotalStr != "[N/A]" {
		memTotal, err := strconv.ParseInt(memTotalStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU memory total '%s': %w", memTotalStr, err)
		}
		// Convert MiB to bytes
		metrics.MemoryTotal = memTotal * 1024 * 1024
	}

	// Field 4: Temperature (Celsius)
	tempStr := strings.TrimSpace(fields[4])
	if tempStr != "" && tempStr != "[N/A]" {
		temp, err := strconv.Atoi(tempStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU temperature '%s': %w", tempStr, err)
		}
		metrics.Temperature = temp
	}

	// Field 5: Power draw (Watts)
	powerStr := strings.TrimSpace(fields[5])
	if powerStr != "" && powerStr != "[N/A]" {
		// Power might have decimal places, parse as float and convert to int
		power, err := strconv.ParseFloat(powerStr, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GPU power '%s': %w", powerStr, err)
		}
		metrics.PowerWatts = int(power)
	}

	return metrics, nil
}
