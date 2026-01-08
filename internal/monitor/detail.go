package monitor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Detail view styles
var (
	detailContainerStyle = lipgloss.NewStyle().
				Padding(1, 2)

	detailSectionStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorBorder).
				Padding(0, 1).
				MarginBottom(1)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(ColorTextPrimary)

	sparklineChars = []rune{'_', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
)

// renderDetailView renders the expanded single-host detail view.
func (m Model) renderDetailView() string {
	host := m.SelectedHost()
	if host == "" {
		return LabelStyle.Render("No host selected")
	}

	metrics := m.metrics[host]
	status := m.status[host]

	var b strings.Builder

	// Header with host name and status
	header := m.renderDetailHeader(host, status)
	b.WriteString(header)
	b.WriteString("\n\n")

	// Content width based on terminal
	contentWidth := m.width - 6
	if contentWidth < 40 {
		contentWidth = 40
	}

	// If no metrics, show waiting message
	if metrics == nil {
		b.WriteString(detailSectionStyle.Width(contentWidth).Render(
			LabelStyle.Render("Waiting for metrics data...")))
		b.WriteString("\n\n")
		b.WriteString(m.renderDetailFooter())
		return detailContainerStyle.Render(b.String())
	}

	// CPU Section
	cpuSection := m.renderDetailCPUSection(host, metrics.CPU, contentWidth)
	b.WriteString(cpuSection)
	b.WriteString("\n")

	// RAM Section
	ramSection := m.renderDetailRAMSection(metrics.RAM, contentWidth)
	b.WriteString(ramSection)
	b.WriteString("\n")

	// GPU Section (if present)
	if metrics.GPU != nil {
		gpuSection := m.renderDetailGPUSection(metrics.GPU, contentWidth)
		b.WriteString(gpuSection)
		b.WriteString("\n")
	}

	// Network Section
	if len(metrics.Network) > 0 {
		netSection := m.renderDetailNetworkSection(metrics.Network, contentWidth)
		b.WriteString(netSection)
		b.WriteString("\n")
	}

	// Footer with navigation hints
	b.WriteString("\n")
	b.WriteString(m.renderDetailFooter())

	return detailContainerStyle.Render(b.String())
}

// renderDetailHeader renders the host name and status prominently.
func (m Model) renderDetailHeader(host string, status HostStatus) string {
	var indicator string
	var indicatorStyle lipgloss.Style

	switch status {
	case StatusConnectedState:
		indicator = StatusConnected + " Connected"
		indicatorStyle = StatusConnectedStyle
	case StatusSlowState:
		indicator = StatusConnected + " Slow"
		indicatorStyle = StatusSlowStyle
	case StatusUnreachableState:
		indicator = StatusUnreachable + " Unreachable"
		indicatorStyle = StatusUnreachableStyle
	}

	hostTitle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true).
		Render(host)

	statusText := indicatorStyle.Render(indicator)

	return fmt.Sprintf("%s  %s", hostTitle, statusText)
}

// renderDetailCPUSection renders the CPU section with history sparkline.
func (m Model) renderDetailCPUSection(host string, cpu CPUMetrics, width int) string {
	var lines []string

	// Section title
	titleStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	lines = append(lines, titleStyle.Render("CPU"))
	lines = append(lines, "")

	// Current usage with large progress bar
	barWidth := width - 20
	if barWidth < 20 {
		barWidth = 20
	}
	bar := ProgressBar(barWidth, cpu.Percent)
	pctText := MetricStyle(cpu.Percent).Render(fmt.Sprintf("%5.1f%%", cpu.Percent))
	lines = append(lines, fmt.Sprintf("  Usage: %s %s", bar, pctText))

	// Load average
	loadLine := fmt.Sprintf("  Load:  %.2f, %.2f, %.2f (1m, 5m, 15m)",
		cpu.LoadAvg[0], cpu.LoadAvg[1], cpu.LoadAvg[2])
	lines = append(lines, LabelStyle.Render(loadLine))

	// Cores if available
	if cpu.Cores > 0 {
		coresLine := fmt.Sprintf("  Cores: %d", cpu.Cores)
		lines = append(lines, LabelStyle.Render(coresLine))
	}

	// History sparkline
	history := m.history.GetCPUHistory(host, 30)
	if len(history) > 0 {
		lines = append(lines, "")
		sparkline := renderSparkline(history, width-4)
		lines = append(lines, fmt.Sprintf("  %s", sparkline))
		lines = append(lines, LabelStyle.Render("  History (30 samples)"))
	}

	content := strings.Join(lines, "\n")
	return detailSectionStyle.Width(width).Render(content)
}

// renderDetailRAMSection renders the RAM section with breakdown.
func (m Model) renderDetailRAMSection(ram RAMMetrics, width int) string {
	var lines []string

	titleStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	lines = append(lines, titleStyle.Render("Memory"))
	lines = append(lines, "")

	// Calculate percentage
	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	// Main progress bar
	barWidth := width - 20
	if barWidth < 20 {
		barWidth = 20
	}
	bar := ProgressBar(barWidth, percent)
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))
	lines = append(lines, fmt.Sprintf("  Usage: %s %s", bar, pctText))

	// Memory breakdown
	usedText := fmt.Sprintf("  Used:      %s", formatBytes(ram.UsedBytes))
	availText := fmt.Sprintf("  Available: %s", formatBytes(ram.Available))
	cachedText := fmt.Sprintf("  Cached:    %s", formatBytes(ram.Cached))
	totalText := fmt.Sprintf("  Total:     %s", formatBytes(ram.TotalBytes))

	lines = append(lines, LabelStyle.Render(usedText))
	lines = append(lines, LabelStyle.Render(availText))
	lines = append(lines, LabelStyle.Render(cachedText))
	lines = append(lines, LabelStyle.Render(totalText))

	content := strings.Join(lines, "\n")
	return detailSectionStyle.Width(width).Render(content)
}

// renderDetailGPUSection renders GPU details including VRAM, temp, and power.
func (m Model) renderDetailGPUSection(gpu *GPUMetrics, width int) string {
	var lines []string

	titleStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	lines = append(lines, titleStyle.Render("GPU"))
	lines = append(lines, "")

	// GPU name
	if gpu.Name != "" {
		lines = append(lines, fmt.Sprintf("  %s", detailValueStyle.Render(gpu.Name)))
		lines = append(lines, "")
	}

	// Utilization progress bar
	barWidth := width - 20
	if barWidth < 20 {
		barWidth = 20
	}
	bar := ProgressBar(barWidth, gpu.Percent)
	pctText := MetricStyle(gpu.Percent).Render(fmt.Sprintf("%5.1f%%", gpu.Percent))
	lines = append(lines, fmt.Sprintf("  Usage: %s %s", bar, pctText))

	// VRAM
	if gpu.MemoryTotal > 0 {
		vramPercent := float64(gpu.MemoryUsed) / float64(gpu.MemoryTotal) * 100
		vramBar := ProgressBar(barWidth, vramPercent)
		vramPct := MetricStyle(vramPercent).Render(fmt.Sprintf("%5.1f%%", vramPercent))
		lines = append(lines, fmt.Sprintf("  VRAM:  %s %s", vramBar, vramPct))

		vramDetail := fmt.Sprintf("         %s / %s", formatBytes(gpu.MemoryUsed), formatBytes(gpu.MemoryTotal))
		lines = append(lines, LabelStyle.Render(vramDetail))
	}

	// Temperature
	if gpu.Temperature > 0 {
		tempColor := ColorHealthy
		if gpu.Temperature >= 80 {
			tempColor = ColorCritical
		} else if gpu.Temperature >= 70 {
			tempColor = ColorWarning
		}
		tempStyle := lipgloss.NewStyle().Foreground(tempColor)
		tempText := fmt.Sprintf("  Temp:  %s", tempStyle.Render(fmt.Sprintf("%d C", gpu.Temperature)))
		lines = append(lines, tempText)
	}

	// Power
	if gpu.PowerWatts > 0 {
		powerText := fmt.Sprintf("  Power: %dW", gpu.PowerWatts)
		lines = append(lines, LabelStyle.Render(powerText))
	}

	content := strings.Join(lines, "\n")
	return detailSectionStyle.Width(width).Render(content)
}

// renderDetailNetworkSection renders network interfaces with per-interface stats.
func (m Model) renderDetailNetworkSection(interfaces []NetworkInterface, width int) string {
	var lines []string

	titleStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	lines = append(lines, titleStyle.Render("Network"))
	lines = append(lines, "")

	// Filter out loopback and show each interface
	for _, iface := range interfaces {
		if iface.Name == "lo" || iface.Name == "lo0" {
			continue
		}

		// Interface name
		ifaceName := lipgloss.NewStyle().Foreground(ColorTextPrimary).Bold(true).Render(iface.Name)
		lines = append(lines, fmt.Sprintf("  %s", ifaceName))

		// Traffic stats with arrows
		inArrow := lipgloss.NewStyle().Foreground(ColorHealthy).Render("\u2193")  // down arrow
		outArrow := lipgloss.NewStyle().Foreground(ColorWarning).Render("\u2191") // up arrow

		trafficLine := fmt.Sprintf("    %s %s  %s %s",
			inArrow, formatBytes(iface.BytesIn),
			outArrow, formatBytes(iface.BytesOut))
		lines = append(lines, trafficLine)

		// Packet counts
		packetsLine := fmt.Sprintf("    Packets: %d in, %d out", iface.PacketsIn, iface.PacketsOut)
		lines = append(lines, LabelStyle.Render(packetsLine))
		lines = append(lines, "")
	}

	// Remove trailing empty line
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	content := strings.Join(lines, "\n")
	return detailSectionStyle.Width(width).Render(content)
}

// renderDetailFooter renders navigation hints for the detail view.
func (m Model) renderDetailFooter() string {
	hints := []string{"Esc back", "r refresh", "q quit"}
	return FooterStyle.Render(strings.Join(hints, " | "))
}

// renderSparkline converts a slice of values into a sparkline string.
func renderSparkline(values []float64, maxWidth int) string {
	if len(values) == 0 {
		return ""
	}

	// Limit to maxWidth characters
	if len(values) > maxWidth {
		values = values[len(values)-maxWidth:]
	}

	// Find min and max for scaling
	minVal, maxVal := values[0], values[0]
	for _, v := range values {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// For percentage values (0-100), use fixed range
	if maxVal <= 100 && minVal >= 0 {
		minVal = 0
		maxVal = 100
	}

	// Build the sparkline
	var result strings.Builder
	for _, v := range values {
		// Map value to sparkline character index
		var idx int
		if maxVal == minVal {
			idx = len(sparklineChars) / 2
		} else {
			normalized := (v - minVal) / (maxVal - minVal)
			idx = int(normalized * float64(len(sparklineChars)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparklineChars) {
			idx = len(sparklineChars) - 1
		}
		result.WriteRune(sparklineChars[idx])
	}

	// Color based on the last (most recent) value
	lastVal := values[len(values)-1]
	color := MetricColor(lastVal)
	return lipgloss.NewStyle().Foreground(color).Render(result.String())
}
