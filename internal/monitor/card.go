package monitor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderCard renders a single host card with metrics.
func (m Model) renderCard(host string, width int, selected bool) string {
	metrics := m.metrics[host]
	status := m.status[host]

	// Choose card style based on selection
	style := CardStyle.Width(width)
	if selected {
		style = CardSelectedStyle.Width(width)
	}

	var lines []string

	// Host name with status indicator
	hostLine := m.renderHostLine(host, status)
	lines = append(lines, hostLine)

	// If no metrics available, show placeholder
	if metrics == nil {
		lines = append(lines, "")
		lines = append(lines, LabelStyle.Render("  Waiting for data..."))
		lines = append(lines, "")
	} else {
		lines = append(lines, "")

		// CPU metrics
		cpuLine := m.renderCPULine(metrics.CPU, width-4)
		lines = append(lines, cpuLine)

		// RAM metrics
		ramLine := m.renderRAMLine(metrics.RAM, width-4)
		lines = append(lines, ramLine)

		// GPU metrics (if available)
		if metrics.GPU != nil {
			gpuLine := m.renderGPULine(metrics.GPU, width-4)
			lines = append(lines, gpuLine)
		}

		// Network metrics
		if len(metrics.Network) > 0 {
			lines = append(lines, "")
			netLine := m.renderNetworkLine(metrics.Network, width-4)
			lines = append(lines, netLine)
		}
	}

	content := strings.Join(lines, "\n")
	return style.Render(content)
}

// renderHostLine renders the host name with status indicator.
func (m Model) renderHostLine(host string, status HostStatus) string {
	var indicator string
	var indicatorStyle lipgloss.Style

	switch status {
	case StatusConnectedState:
		indicator = StatusConnected
		indicatorStyle = StatusConnectedStyle
	case StatusSlowState:
		indicator = StatusConnected
		indicatorStyle = StatusSlowStyle
	case StatusUnreachableState:
		indicator = StatusUnreachable
		indicatorStyle = StatusUnreachableStyle
	}

	return indicatorStyle.Render(indicator) + " " + HostNameStyle.Render(host)
}

// renderCPULine renders the CPU usage with progress bar.
func (m Model) renderCPULine(cpu CPUMetrics, barWidth int) string {
	label := LabelStyle.Render("CPU")
	percent := cpu.Percent

	// Calculate bar width (label + space + bar + space + percentage)
	actualBarWidth := barWidth - 4 - 6 // "CPU " and " XX.X%"
	if actualBarWidth < 10 {
		actualBarWidth = 10
	}

	bar := CompactProgressBar(actualBarWidth, percent)
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))

	return fmt.Sprintf("%s %s %s", label, bar, pctText)
}

// renderRAMLine renders the RAM usage with progress bar.
func (m Model) renderRAMLine(ram RAMMetrics, barWidth int) string {
	label := LabelStyle.Render("RAM")

	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	// Calculate bar width
	actualBarWidth := barWidth - 4 - 6
	if actualBarWidth < 10 {
		actualBarWidth = 10
	}

	bar := CompactProgressBar(actualBarWidth, percent)
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))

	return fmt.Sprintf("%s %s %s", label, bar, pctText)
}

// renderGPULine renders the GPU usage with progress bar.
func (m Model) renderGPULine(gpu *GPUMetrics, barWidth int) string {
	label := LabelStyle.Render("GPU")
	percent := gpu.Percent

	// Calculate bar width
	actualBarWidth := barWidth - 4 - 6
	if actualBarWidth < 10 {
		actualBarWidth = 10
	}

	bar := CompactProgressBar(actualBarWidth, percent)
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))

	return fmt.Sprintf("%s %s %s", label, bar, pctText)
}

// renderNetworkLine renders network I/O rates.
func (m Model) renderNetworkLine(interfaces []NetworkInterface, width int) string {
	// Sum up all interface bytes for a total rate display
	// In a real implementation, you'd calculate rate from historical data
	var totalIn, totalOut int64
	for _, iface := range interfaces {
		// Skip loopback
		if iface.Name == "lo" || iface.Name == "lo0" {
			continue
		}
		totalIn += iface.BytesIn
		totalOut += iface.BytesOut
	}

	inLabel := LabelStyle.Render("\u2193")
	outLabel := LabelStyle.Render("\u2191")

	inText := ValueStyle.Render(formatBytes(totalIn))
	outText := ValueStyle.Render(formatBytes(totalOut))

	return fmt.Sprintf("%s %s  %s %s", inLabel, inText, outLabel, outText)
}

// RenderLoadAvg renders the system load average.
func RenderLoadAvg(loadAvg [3]float64) string {
	label := LabelStyle.Render("Load")
	values := ValueStyle.Render(fmt.Sprintf("%.2f %.2f %.2f", loadAvg[0], loadAvg[1], loadAvg[2]))
	return fmt.Sprintf("%s %s", label, values)
}
