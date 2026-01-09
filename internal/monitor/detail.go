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
)

// ProcSortOrder determines how processes are sorted in the table.
type ProcSortOrder int

const (
	ProcSortByCPU ProcSortOrder = iota
	ProcSortByMemory
	ProcSortByPID
)

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

// renderDetailCPUSection renders the CPU section with high-resolution braille graph.
// Inspired by btop's CPU visualization with tall graphs and per-core display.
func (m Model) renderDetailCPUSection(host string, cpu CPUMetrics, width int) string {
	var lines []string

	// Section header with right-aligned percentage
	pctText := fmt.Sprintf("%.1f%%", cpu.Percent)
	lines = append(lines, SectionHeader("CPU", pctText, width))

	// Content area (width - 4 for borders and padding: "│  " on left and " │" on right)
	graphWidth := width - 4
	if graphWidth < 20 {
		graphWidth = 20
	}

	// High-resolution braille graph - 8 rows for detailed visibility
	// Each braille char is 2x4 dots, giving us 32 vertical levels
	// Request full history (300 points = ~10 min at 2s interval) for longer time window
	history := m.history.GetCPUHistory(host, DefaultHistorySize)
	if len(history) > 0 {
		graph := RenderBrailleSparkline(history, graphWidth, 8, ColorGraph)
		for _, line := range strings.Split(graph, "\n") {
			lines = append(lines, SectionContentLine(line, width))
		}
	} else {
		// Show empty graph placeholder while collecting
		emptyLine := strings.Repeat(" ", graphWidth)
		for i := 0; i < 8; i++ {
			if i == 4 {
				lines = append(lines, SectionContentLine(LabelStyle.Render(centerText("Collecting data...", graphWidth)), width))
			} else {
				lines = append(lines, SectionContentLine(emptyLine, width))
			}
		}
	}

	// Load average and cores on same line
	loadText := fmt.Sprintf("Load: %.2f / %.2f / %.2f", cpu.LoadAvg[0], cpu.LoadAvg[1], cpu.LoadAvg[2])
	if cpu.Cores > 0 {
		loadText += fmt.Sprintf("  ·  Cores: %d", cpu.Cores)
	}
	lines = append(lines, SectionContentLine(LabelStyle.Render(loadText), width))

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
}

// centerText centers a string within the given width
func centerText(s string, width int) string {
	if len(s) >= width {
		return s
	}
	padding := (width - len(s)) / 2
	return strings.Repeat(" ", padding) + s + strings.Repeat(" ", width-len(s)-padding)
}

// padToWidth pads a string with spaces to reach the target visible width.
// Uses lipgloss.Width to account for ANSI codes and unicode characters.
func padToWidth(s string, width int) string {
	visibleWidth := lipgloss.Width(s)
	if visibleWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visibleWidth)
}

// renderDetailRAMSection renders the RAM section with a visual bar graph.
func (m Model) renderDetailRAMSection(host string, ram RAMMetrics, width int) string {
	var lines []string

	// Calculate percentage
	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	// Section header with right-aligned percentage
	pctText := fmt.Sprintf("%.1f%%", percent)
	lines = append(lines, SectionHeader("Memory", pctText, width))

	// Content area (width - 4 for borders and padding)
	barWidth := width - 4
	if barWidth < 10 {
		barWidth = 10
	}

	// Multi-row visual bar for better granularity (6 rows tall for better visibility)
	// Request full history for ~10 min time window
	history := m.history.GetRAMHistory(host, DefaultHistorySize)
	if len(history) > 0 {
		graph := RenderBrailleSparkline(history, barWidth, 6, ColorGraph)
		for _, line := range strings.Split(graph, "\n") {
			lines = append(lines, SectionContentLine(line, width))
		}
	} else {
		// Show current value as a solid bar while collecting history
		bar := RenderGradientBar(barWidth, percent, ColorGraph)
		lines = append(lines, SectionContentLine(bar, width))
	}

	// Memory breakdown - compact single line
	usedStr := formatBytes(ram.UsedBytes)
	totalStr := formatBytes(ram.TotalBytes)
	memText := fmt.Sprintf("%s / %s", usedStr, totalStr)
	if ram.Available > 0 {
		memText += fmt.Sprintf("  ·  Avail: %s", formatBytes(ram.Available))
	}
	lines = append(lines, SectionContentLine(LabelStyle.Render(memText), width))

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
}

// renderDetailGPUSection renders GPU details with clean sparkline.
func (m Model) renderDetailGPUSection(host string, gpu *GPUMetrics, width int) string {
	var lines []string

	// Section header with right-aligned percentage
	// Include GPU name in title if available
	title := "GPU"
	if gpu.Name != "" {
		title = fmt.Sprintf("GPU (%s)", gpu.Name)
	}
	pctText := fmt.Sprintf("%.1f%%", gpu.Percent)
	lines = append(lines, SectionHeader(title, pctText, width))

	// Content area (width - 4 for borders and padding)
	graphWidth := width - 4
	if graphWidth < 10 {
		graphWidth = 10
	}

	gpuHistory := m.history.GetGPUHistory(host, graphWidth)
	if len(gpuHistory) > 0 {
		graph := RenderCleanSparkline(gpuHistory, graphWidth, ColorGraph)
		lines = append(lines, SectionContentLine(graph, width))
	}

	// VRAM, temp, power on one line
	var details []string
	if gpu.MemoryTotal > 0 {
		vramPercent := float64(gpu.MemoryUsed) / float64(gpu.MemoryTotal) * 100
		details = append(details, fmt.Sprintf("VRAM: %s (%.0f%%)", formatBytes(gpu.MemoryUsed), vramPercent))
	}
	if gpu.Temperature > 0 {
		tempColor := ColorHealthy
		if gpu.Temperature >= 80 {
			tempColor = ColorCritical
		} else if gpu.Temperature >= 70 {
			tempColor = ColorWarning
		}
		tempStyle := lipgloss.NewStyle().Foreground(tempColor)
		details = append(details, tempStyle.Render(fmt.Sprintf("%dC", gpu.Temperature)))
	}
	if gpu.PowerWatts > 0 {
		details = append(details, fmt.Sprintf("%dW", gpu.PowerWatts))
	}
	if len(details) > 0 {
		lines = append(lines, SectionContentLine(LabelStyle.Render(strings.Join(details, "  ·  ")), width))
	}

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
}

// renderDetailNetworkSection renders network with rates and activity graph.
func (m Model) renderDetailNetworkSection(host string, width int) string {
	var lines []string

	// Get total rates
	inRate, outRate := m.history.GetTotalNetworkRate(host, m.interval.Seconds())

	// Section header with rates
	downArrow := lipgloss.NewStyle().Foreground(ColorAccent).Render("↓")
	upArrow := lipgloss.NewStyle().Foreground(ColorAccent).Render("↑")
	rateText := fmt.Sprintf("%s%s  %s%s", downArrow, FormatRate(inRate), upArrow, FormatRate(outRate))
	lines = append(lines, SectionHeader("Network", rateText, width))

	// Content area (width - 4 for borders and padding)
	barWidth := width - 4
	if barWidth < 10 {
		barWidth = 10
	}

	// Get network rate history for visualization (~10 min window)
	netHistory := m.history.GetNetworkRateHistory(host, DefaultHistorySize, m.interval.Seconds())
	if len(netHistory) > 0 {
		// Show activity graph (6 rows for better visibility)
		graph := RenderBrailleSparkline(netHistory, barWidth, 6, ColorGraph)
		for _, line := range strings.Split(graph, "\n") {
			lines = append(lines, SectionContentLine(line, width))
		}
	} else {
		// Calculate activity percentage (log scale for visibility) as fallback
		totalRate := inRate + outRate
		var activityPercent float64
		if totalRate > 100*1024*1024 { // > 100 MB/s
			activityPercent = 100
		} else if totalRate > 10*1024*1024 { // > 10 MB/s
			activityPercent = 80
		} else if totalRate > 1024*1024 { // > 1 MB/s
			activityPercent = 60
		} else if totalRate > 100*1024 { // > 100 KB/s
			activityPercent = 40
		} else if totalRate > 10*1024 { // > 10 KB/s
			activityPercent = 20
		} else if totalRate > 1024 { // > 1 KB/s
			activityPercent = 10
		} else if totalRate > 0 {
			activityPercent = 5
		}

		bar := RenderGradientBar(barWidth, activityPercent, ColorGraph)
		lines = append(lines, SectionContentLine(bar, width))
	}

	// Status text
	totalRate := inRate + outRate
	var statusText string
	if totalRate == 0 {
		statusText = "idle"
	} else {
		statusText = fmt.Sprintf("Total: %s", FormatRate(totalRate))
	}
	lines = append(lines, SectionContentLine(LabelStyle.Render(statusText), width))

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
}

// renderDetailProcessSection renders the process table with consistent styling.
func (m Model) renderDetailProcessSection(procs []ProcessInfo, width int) string {
	var lines []string

	// Section header
	lines = append(lines, SectionHeader("Processes", "by CPU", width))

	// Sort by CPU (already should be, but ensure)
	sorted := make([]ProcessInfo, len(procs))
	copy(sorted, procs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CPU > sorted[j].CPU
	})

	// Table header
	headerStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)
	header := fmt.Sprintf("%-6s %-10s %6s %6s  %s", "PID", "USER", "CPU%", "MEM%", "COMMAND")
	lines = append(lines, SectionContentLine(headerStyle.Render(header), width))

	// Show up to 5 processes (reduced from 10 for cleaner layout)
	maxProcs := 5
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
		cmdWidth := width - 40
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

		line := fmt.Sprintf("%-6d %-10s %s %s  %s",
			proc.PID,
			user,
			cpuStyle.Render(fmt.Sprintf("%5.1f%%", proc.CPU)),
			memStyle.Render(fmt.Sprintf("%5.1f%%", proc.Memory)),
			cmd)
		lines = append(lines, SectionContentLine(line, width))
	}

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
}

// renderDetailFooter renders navigation hints for the detail view.
func (m Model) renderDetailFooter() string {
	hints := []string{"Esc:back", "?:help", "q:quit"}
	return FooterStyle.Render(strings.Join(hints, "  "))
}

// renderDetailViewWithViewport renders the detail view with viewport scrolling support.
// The viewport allows scrolling when content exceeds the visible area.
func (m Model) renderDetailViewWithViewport() string {
	host := m.SelectedHost()
	if host == "" {
		return LabelStyle.Render("No host selected")
	}

	// Build the header (fixed at top)
	metrics := m.metrics[host]
	status := m.status[host]
	header := m.renderDetailHeader(host, status)

	// Build the scrollable content
	var content strings.Builder
	contentWidth := m.width - 6
	if contentWidth < 40 {
		contentWidth = 40
	}

	// If no metrics, show waiting message
	if metrics == nil {
		content.WriteString(detailSectionStyle.Width(contentWidth).Render(
			LabelStyle.Render("Waiting for metrics data...")))
	} else {
		// 1. CPU Section
		cpuSection := m.renderDetailCPUSection(host, metrics.CPU, contentWidth)
		content.WriteString(cpuSection)
		content.WriteString("\n")

		// 2. Process Table
		if len(metrics.Processes) > 0 {
			procSection := m.renderDetailProcessSection(metrics.Processes, contentWidth)
			content.WriteString(procSection)
			content.WriteString("\n")
		}

		// 3. Memory and Network side by side (or stacked on narrow terminals)
		halfWidth := (contentWidth - 2) / 2
		if contentWidth >= 80 {
			ramSection := m.renderDetailRAMSection(host, metrics.RAM, halfWidth)
			netSection := m.renderDetailNetworkSection(host, halfWidth)

			// Join side by side
			ramLines := strings.Split(ramSection, "\n")
			netLines := strings.Split(netSection, "\n")

			// Pad to same height
			maxLines := len(ramLines)
			if len(netLines) > maxLines {
				maxLines = len(netLines)
			}

			// Pad lines to correct width and height
			for len(ramLines) < maxLines {
				ramLines = append(ramLines, "")
			}
			for len(netLines) < maxLines {
				netLines = append(netLines, "")
			}

			for i := 0; i < maxLines; i++ {
				// Pad each line to halfWidth using visible character count
				ramLine := padToWidth(ramLines[i], halfWidth)
				netLine := padToWidth(netLines[i], halfWidth)
				content.WriteString(ramLine)
				content.WriteString(" ")
				content.WriteString(netLine)
				content.WriteString("\n")
			}
		} else {
			// Single column for narrow terminals
			ramSection := m.renderDetailRAMSection(host, metrics.RAM, contentWidth)
			content.WriteString(ramSection)
			content.WriteString("\n")

			netSection := m.renderDetailNetworkSection(host, contentWidth)
			content.WriteString(netSection)
			content.WriteString("\n")
		}

		// 4. GPU Section (if present)
		if metrics.GPU != nil {
			gpuSection := m.renderDetailGPUSection(host, metrics.GPU, contentWidth)
			content.WriteString(gpuSection)
		}
	}

	// Build the footer with scroll indicator
	footer := m.renderDetailFooterWithScroll()

	// If viewport is ready, use it for scrolling
	if m.viewportReady {
		// Set the content for the viewport
		// We need a mutable copy to set content
		vp := m.detailViewport
		vp.SetContent(content.String())

		// Build the full view: header + viewport + footer
		return fmt.Sprintf("%s\n\n%s\n%s",
			detailContainerStyle.Render(header),
			vp.View(),
			footer,
		)
	}

	// Fallback: render without viewport
	return detailContainerStyle.Render(fmt.Sprintf("%s\n\n%s\n\n%s", header, content.String(), m.renderDetailFooter()))
}

// renderDetailFooterWithScroll renders the footer with scroll position indicator.
func (m Model) renderDetailFooterWithScroll() string {
	var hints []string

	// Add scroll indicator if viewport is ready and content is scrollable
	if m.viewportReady && m.detailViewport.TotalLineCount() > m.detailViewport.Height {
		scrollPercent := m.detailViewport.ScrollPercent() * 100
		hints = append(hints, fmt.Sprintf("%.0f%%", scrollPercent))
		hints = append(hints, "j/k:scroll")
	}

	hints = append(hints, "Esc:back", "?:help", "q:quit")
	return FooterStyle.Render(strings.Join(hints, "  "))
}
