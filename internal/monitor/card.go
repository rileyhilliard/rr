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
func (m Model) renderNetworkLine(interfaces []NetworkInterface, _ int) string {
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

// renderCompactCard renders a compact card for terminals 80-120 columns wide.
// Shows inline metrics with abbreviated labels and small progress bars.
func (m Model) renderCompactCard(host string, width int, selected bool) string {
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
		lines = append(lines, LabelStyle.Render("  Waiting..."))
	} else {
		// CPU with compact bar
		cpuLine := m.renderCompactCPULine(metrics.CPU, width-4)
		lines = append(lines, cpuLine)

		// RAM with compact bar
		ramLine := m.renderCompactRAMLine(metrics.RAM, width-4)
		lines = append(lines, ramLine)

		// GPU only if available and space permits
		if metrics.GPU != nil && width >= 50 {
			gpuLine := m.renderCompactGPULine(metrics.GPU, width-4)
			lines = append(lines, gpuLine)
		}
	}

	content := strings.Join(lines, "\n")
	return style.Render(content)
}

// renderMinimalCard renders a minimal card for terminals < 80 columns.
// Shows only essential metrics as text, no progress bars.
func (m Model) renderMinimalCard(host string, width int, selected bool) string {
	metrics := m.metrics[host]
	status := m.status[host]

	// Choose card style based on selection
	style := CardStyle.Width(width)
	if selected {
		style = CardSelectedStyle.Width(width)
	}

	var lines []string

	// Host name with status indicator (abbreviated if necessary)
	hostLine := m.renderMinimalHostLine(host, status, width-4)
	lines = append(lines, hostLine)

	// If no metrics available, show short placeholder
	if metrics == nil {
		lines = append(lines, LabelStyle.Render("..."))
	} else {
		// Single line with CPU and RAM percentages
		metricsLine := m.renderMinimalMetricsLine(metrics, width-4)
		lines = append(lines, metricsLine)
	}

	content := strings.Join(lines, "\n")
	return style.Render(content)
}

// renderCompactCPULine renders CPU with a smaller progress bar for compact mode.
func (m Model) renderCompactCPULine(cpu CPUMetrics, lineWidth int) string {
	label := LabelStyle.Render("CPU")
	percent := cpu.Percent

	// Smaller bar for compact mode
	barWidth := lineWidth - 12 // "CPU " + " XX.X%"
	if barWidth < 8 {
		barWidth = 8
	}

	bar := CompactProgressBar(barWidth, percent)
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))

	return fmt.Sprintf("%s %s %s", label, bar, pctText)
}

// renderCompactRAMLine renders RAM with a smaller progress bar for compact mode.
func (m Model) renderCompactRAMLine(ram RAMMetrics, lineWidth int) string {
	label := LabelStyle.Render("RAM")

	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	barWidth := lineWidth - 12
	if barWidth < 8 {
		barWidth = 8
	}

	bar := CompactProgressBar(barWidth, percent)
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))

	return fmt.Sprintf("%s %s %s", label, bar, pctText)
}

// renderCompactGPULine renders GPU with a smaller progress bar for compact mode.
func (m Model) renderCompactGPULine(gpu *GPUMetrics, lineWidth int) string {
	label := LabelStyle.Render("GPU")
	percent := gpu.Percent

	barWidth := lineWidth - 12
	if barWidth < 8 {
		barWidth = 8
	}

	bar := CompactProgressBar(barWidth, percent)
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))

	return fmt.Sprintf("%s %s %s", label, bar, pctText)
}

// renderMinimalHostLine renders the hostname, truncating if necessary.
func (m Model) renderMinimalHostLine(host string, status HostStatus, maxWidth int) string {
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

	// Truncate hostname if too long (account for indicator and space)
	displayHost := host
	availableWidth := maxWidth - 2 // indicator + space
	if len(displayHost) > availableWidth && availableWidth > 3 {
		displayHost = displayHost[:availableWidth-2] + ".."
	}

	return indicatorStyle.Render(indicator) + " " + HostNameStyle.Render(displayHost)
}

// renderMinimalMetricsLine renders a single line with CPU and RAM percentages.
func (m Model) renderMinimalMetricsLine(metrics *HostMetrics, width int) string {
	cpuPct := metrics.CPU.Percent

	var ramPct float64
	if metrics.RAM.TotalBytes > 0 {
		ramPct = float64(metrics.RAM.UsedBytes) / float64(metrics.RAM.TotalBytes) * 100
	}

	// Format: "CPU: 45% | RAM: 67%"
	cpuText := MetricStyle(cpuPct).Render(fmt.Sprintf("%.0f%%", cpuPct))
	ramText := MetricStyle(ramPct).Render(fmt.Sprintf("%.0f%%", ramPct))

	// Choose format based on available width
	if width >= 30 {
		return fmt.Sprintf("%s %s  %s %s",
			LabelStyle.Render("CPU:"), cpuText,
			LabelStyle.Render("RAM:"), ramText)
	}

	// Super compact format
	return fmt.Sprintf("C:%s R:%s", cpuText, ramText)
}
