package monitor

import (
	"fmt"
	"sort"
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

	detailSectionTitleStyle = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(ColorTextPrimary)
)

// ProcSortOrder determines how processes are sorted in the table.
type ProcSortOrder int

const (
	ProcSortByCPU ProcSortOrder = iota
	ProcSortByMemory
	ProcSortByPID
)

// ProcSortOrder determines how processes are sorted in the table.
type ProcSortOrder int

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

	// CPU Section with braille graph
	cpuSection := m.renderDetailCPUSection(host, metrics.CPU, contentWidth)
	b.WriteString(cpuSection)
	b.WriteString("\n")

	// Two-column layout for RAM and Network if wide enough
	halfWidth := (contentWidth - 2) / 2
	if contentWidth >= 80 {
		ramSection := m.renderDetailRAMSection(host, metrics.RAM, halfWidth)
		netSection := m.renderDetailNetworkSection(host, metrics.Network, halfWidth)

		// Join side by side
		ramLines := strings.Split(ramSection, "\n")
		netLines := strings.Split(netSection, "\n")

		// Pad to same height
		maxLines := len(ramLines)
		if len(netLines) > maxLines {
			maxLines = len(netLines)
		}
		for len(ramLines) < maxLines {
			ramLines = append(ramLines, strings.Repeat(" ", halfWidth))
		}
		for len(netLines) < maxLines {
			netLines = append(netLines, strings.Repeat(" ", halfWidth))
		}

		for i := 0; i < maxLines; i++ {
			b.WriteString(ramLines[i])
			b.WriteString(" ")
			b.WriteString(netLines[i])
			b.WriteString("\n")
		}
	} else {
		// Single column for narrow terminals
		ramSection := m.renderDetailRAMSection(host, metrics.RAM, contentWidth)
		b.WriteString(ramSection)
		b.WriteString("\n")

		netSection := m.renderDetailNetworkSection(host, metrics.Network, contentWidth)
		b.WriteString(netSection)
		b.WriteString("\n")
	}

	// GPU Section (if present)
	if metrics.GPU != nil {
		gpuSection := m.renderDetailGPUSection(host, metrics.GPU, contentWidth)
		b.WriteString(gpuSection)
		b.WriteString("\n")
	}

	// Process Table Section
	if len(metrics.Processes) > 0 {
		procSection := m.renderDetailProcessSection(metrics.Processes, contentWidth)
		b.WriteString(procSection)
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

// renderDetailCPUSection renders the CPU section with braille history graph.
func (m Model) renderDetailCPUSection(host string, cpu CPUMetrics, width int) string {
	var lines []string

	lines = append(lines, detailSectionTitleStyle.Render("CPU"))
	lines = append(lines, "")

	// Braille graph - 2 rows high for higher resolution
	graphWidth := width - 20
	if graphWidth < 20 {
		graphWidth = 20
	}

	history := m.history.GetCPUHistory(host, graphWidth*2)
	if len(history) > 0 {
		graph := RenderBrailleSparkline(history, graphWidth, 2, ColorGraph)
		for _, line := range strings.Split(graph, "\n") {
			lines = append(lines, "  "+line)
		}
	}

	// Current usage with percentage
	pctText := MetricStyle(cpu.Percent).Render(fmt.Sprintf("%.1f%%", cpu.Percent))
	lines = append(lines, fmt.Sprintf("  Current: %s", pctText))

	// Load average and cores on same line
	loadText := fmt.Sprintf("Load: %.2f / %.2f / %.2f", cpu.LoadAvg[0], cpu.LoadAvg[1], cpu.LoadAvg[2])
	coresText := ""
	if cpu.Cores > 0 {
		coresText = fmt.Sprintf("  Cores: %d", cpu.Cores)
	}
	lines = append(lines, "  "+LabelStyle.Render(loadText+coresText))

	content := strings.Join(lines, "\n")
	return detailSectionStyle.Width(width).Render(content)
}

// renderDetailRAMSection renders the RAM section with sparkline.
func (m Model) renderDetailRAMSection(host string, ram RAMMetrics, width int) string {
	var lines []string

	lines = append(lines, detailSectionTitleStyle.Render("Memory"))
	lines = append(lines, "")

	// Calculate percentage
	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	// Mini sparkline from history
	ramHistory := m.history.GetRAMHistory(host, 20)
	if len(ramHistory) > 0 {
		sparkline := RenderColoredMiniSparkline(ramHistory, 20)
		lines = append(lines, "  "+sparkline)
	}

	// Progress bar
	barWidth := width - 14
	if barWidth < 10 {
		barWidth = 10
	}
	bar := CompactProgressBar(barWidth, percent)
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))
	lines = append(lines, fmt.Sprintf("  %s %s", bar, pctText))

	// Memory breakdown - compact format
	usedStr := formatBytes(ram.UsedBytes)
	totalStr := formatBytes(ram.TotalBytes)
	lines = append(lines, LabelStyle.Render(fmt.Sprintf("  %s / %s", usedStr, totalStr)))

	if ram.Available > 0 {
		lines = append(lines, LabelStyle.Render(fmt.Sprintf("  Avail: %s", formatBytes(ram.Available))))
	}

	return strings.Join(lines, "\n")
}

// renderDetailGPUSection renders GPU details including VRAM, temp, and power.
func (m Model) renderDetailGPUSection(host string, gpu *GPUMetrics, width int) string {
	var lines []string

	lines = append(lines, detailSectionTitleStyle.Render("GPU"))
	lines = append(lines, "")

	// GPU name
	if gpu.Name != "" {
		lines = append(lines, fmt.Sprintf("  %s", detailValueStyle.Render(gpu.Name)))
	}

	// GPU history sparkline
	gpuHistory := m.history.GetGPUHistory(host, 20)
	if len(gpuHistory) > 0 {
		sparkline := RenderColoredMiniSparkline(gpuHistory, 20)
		lines = append(lines, "  "+sparkline)
	}

	// Utilization
	pctText := MetricStyle(gpu.Percent).Render(fmt.Sprintf("%.1f%%", gpu.Percent))
	lines = append(lines, fmt.Sprintf("  Usage: %s", pctText))

	// VRAM, temp, power on one line
	var details []string
	if gpu.MemoryTotal > 0 {
		vramPercent := float64(gpu.MemoryUsed) / float64(gpu.MemoryTotal) * 100
		vramPct := MetricStyle(vramPercent).Render(fmt.Sprintf("%.1f%%", vramPercent))
		vramStr := fmt.Sprintf("%s / %s", formatBytes(gpu.MemoryUsed), formatBytes(gpu.MemoryTotal))
		lines = append(lines, fmt.Sprintf("  VRAM: %s (%s)", vramPct, LabelStyle.Render(vramStr)))
	}

	// Temperature and Power on same line
	extras := []string{}
	if gpu.Temperature > 0 {
		tempColor := ColorHealthy
		if gpu.Temperature >= 80 {
			tempColor = ColorCritical
		} else if gpu.Temperature >= 70 {
			tempColor = ColorWarning
		}
		tempStyle := lipgloss.NewStyle().Foreground(tempColor)
		extras = append(extras, tempStyle.Render(fmt.Sprintf("%dC", gpu.Temperature)))
	}
	if gpu.PowerWatts > 0 {
		extras = append(extras, LabelStyle.Render(fmt.Sprintf("%dW", gpu.PowerWatts)))
	}
	if len(extras) > 0 {
		lines = append(lines, "  "+strings.Join(extras, " | "))
	}

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
}

// renderDetailNetworkSection renders network with rates and per-interface sparklines.
func (m Model) renderDetailNetworkSection(host string, interfaces []NetworkInterface, width int) string {
	var lines []string

	lines = append(lines, detailSectionTitleStyle.Render("Network"))
	lines = append(lines, "")

	// Get total rates
	inRate, outRate := m.history.GetTotalNetworkRate(host, m.interval.Seconds())

	// Total throughput with arrows
	downArrow := lipgloss.NewStyle().Foreground(ColorAccent).Render("↓")
	upArrow := lipgloss.NewStyle().Foreground(ColorAccent).Render("↑")
	lines = append(lines, fmt.Sprintf("  %s %s  %s %s",
		downArrow, ValueStyle.Render(FormatRate(inRate)),
		upArrow, ValueStyle.Render(FormatRate(outRate))))

	// Per-interface breakdown (skip loopback)
	for _, iface := range interfaces {
		if iface.Name == "lo" || iface.Name == "lo0" {
			continue
		}

		// Interface name with traffic
		ifaceName := LabelStyle.Render(iface.Name + ":")
		inText := formatBytes(iface.BytesIn)
		outText := formatBytes(iface.BytesOut)
		lines = append(lines, fmt.Sprintf("  %s ↓%s ↑%s", ifaceName, inText, outText))
	}

	content := strings.Join(lines, "\n")
	return detailSectionStyle.Width(width).Render(content)
}

// renderDetailProcessSection renders the process table.
func (m Model) renderDetailProcessSection(procs []ProcessInfo, width int) string {
	var lines []string

	lines = append(lines, detailSectionTitleStyle.Render("Processes (sorted by CPU)"))
	lines = append(lines, "")

	// Sort by CPU (already should be, but ensure)
	sorted := make([]ProcessInfo, len(procs))
	copy(sorted, procs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CPU > sorted[j].CPU
	})

	// Header
	headerStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)
	header := fmt.Sprintf("  %-6s %-10s %6s %6s  %s", "PID", "USER", "CPU%", "MEM%", "COMMAND")
	lines = append(lines, headerStyle.Render(header))

	// Show up to 10 processes
	maxProcs := 10
	if len(sorted) < maxProcs {
		maxProcs = len(sorted)
	}

	for i := 0; i < maxProcs; i++ {
		proc := sorted[i]

		// Truncate user and command
		user := proc.User
		if len(user) > 10 {
			user = user[:9] + "+"
		}

		cmd := proc.Command
		cmdWidth := width - 36
		if cmdWidth < 10 {
			cmdWidth = 10
		}
		if len(cmd) > cmdWidth {
			cmd = cmd[:cmdWidth-3] + "..."
		}

		// Color CPU and MEM based on thresholds
		cpuColor := MetricColor(proc.CPU)
		memColor := MetricColor(proc.Memory)
		cpuStyle := lipgloss.NewStyle().Foreground(cpuColor)
		memStyle := lipgloss.NewStyle().Foreground(memColor)

		line := fmt.Sprintf("  %-6d %-10s %s %s  %s",
			proc.PID,
			user,
			cpuStyle.Render(fmt.Sprintf("%5.1f%%", proc.CPU)),
			memStyle.Render(fmt.Sprintf("%5.1f%%", proc.Memory)),
			cmd)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// renderDetailFooter renders navigation hints for the detail view.
func (m Model) renderDetailFooter() string {
	hints := []string{"Esc:back", "?:help", "q:quit"}
	return FooterStyle.Render(strings.Join(hints, "  "))
}
