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
	var statusLabel string

	switch status {
	case StatusIdleState:
		indicator = StatusIdle
		indicatorStyle = StatusIdleStyle
		statusLabel = " Idle"
	case StatusRunningState:
		indicator, indicatorStyle = m.RunningSpinner()
		statusLabel = " Running"
	case StatusSlowState:
		indicator = StatusIdle
		indicatorStyle = StatusSlowStyle
		statusLabel = " Slow"
	case StatusUnreachableState:
		indicator = StatusUnreachable
		indicatorStyle = StatusUnreachableStyle
		statusLabel = " Offline"
	}

	hostTitle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true).
		Render(host)

	statusText := indicatorStyle.Render(indicator + statusLabel)

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
	loadText := fmt.Sprintf("Load: %.2f (1m) · %.2f (5m) · %.2f (15m)", cpu.LoadAvg[0], cpu.LoadAvg[1], cpu.LoadAvg[2])
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

// renderDetailLatencySection renders the latency section with a sparkline graph.
func (m Model) renderDetailLatencySection(host string, width int) string {
	var lines []string

	// Get current latency
	latencyDur, ok := m.latency[host]
	latencyMs := float64(0)
	if ok && latencyDur > 0 {
		latencyMs = float64(latencyDur.Milliseconds())
	}

	// Section header with right-aligned latency value and quality
	var headerValue string
	if latencyMs > 0 {
		headerValue = fmt.Sprintf("%s %s",
			LatencyStyle(latencyMs).Render(fmt.Sprintf("%.0fms", latencyMs)),
			LabelStyle.Render(LatencyQualityText(latencyMs)))
	} else {
		headerValue = LabelStyle.Render("--")
	}
	lines = append(lines, SectionHeader("Latency", headerValue, width))

	// Content area (width - 4 for borders and padding)
	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	// Braille graph with y-axis - 8 rows to match CPU section height
	history := m.history.GetLatencyHistory(host, DefaultHistorySize)
	if len(history) > 0 {
		// Reserve space for y-axis labels (7 chars for label + 1 space = 8 total)
		// Labels like "1234ms" (6 chars) or "12.5s" (5 chars)
		axisLabelWidth := 7
		graphWidth := contentWidth - axisLabelWidth - 1 // -1 for space between label and graph
		if graphWidth < 10 {
			graphWidth = 10
		}

		// Apply moving average to smooth latency data while preserving shape.
		// This makes the graph more readable - trends are visible without wild spikes.
		// Window of 5 samples smooths noise while keeping increases/decreases visible.
		smoothedHistory := SmoothWithMovingAverage(history, 5)

		// Render graph with y-axis labels
		formatLatency := func(v float64) string {
			if v >= 1000 {
				return fmt.Sprintf("%.1fs", v/1000)
			}
			return fmt.Sprintf("%.0fms", v)
		}
		// Use LatencyColor for per-column coloring based on latency thresholds
		// Green = fast (<50ms), Yellow = normal (<200ms), Orange = slow (<500ms), Red = degraded
		// forceZeroMin=true so 950ms shows partway up the graph, not at the bottom
		graph := RenderGraphWithYAxis(smoothedHistory, graphWidth, 8, ColorGraph, formatLatency, axisLabelWidth, LatencyColor, true)
		for _, line := range strings.Split(graph, "\n") {
			lines = append(lines, SectionContentLine(line, width))
		}
	} else {
		// Show empty graph placeholder while collecting
		emptyLine := strings.Repeat(" ", contentWidth)
		for i := 0; i < 8; i++ {
			if i == 4 {
				lines = append(lines, SectionContentLine(LabelStyle.Render(centerText("Collecting data...", contentWidth)), width))
			} else {
				lines = append(lines, SectionContentLine(emptyLine, width))
			}
		}
	}

	// Latency stats line
	var statsText string
	if len(history) > 0 {
		// Calculate min/max/avg from recent history
		minVal, maxVal, sum := history[0], history[0], float64(0)
		for _, v := range history {
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
			sum += v
		}
		avg := sum / float64(len(history))
		statsText = fmt.Sprintf("Min: %.0fms  ·  Avg: %.0fms  ·  Max: %.0fms", minVal, avg, maxVal)
	} else {
		statsText = "Waiting for data..."
	}
	lines = append(lines, SectionContentLine(LabelStyle.Render(statsText), width))

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
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
	contentWidth := width - 4
	if contentWidth < 10 {
		contentWidth = 10
	}

	// Get network rate history for visualization (~10 min window)
	// Use linear rates (actual bytes/sec) so the graph height reflects actual throughput
	netHistory := m.history.GetNetworkRateHistoryLinear(host, DefaultHistorySize, m.interval.Seconds())
	if len(netHistory) > 0 {
		// Reserve space for y-axis labels (10 chars for label + 1 space = 11 total)
		// Labels like "100 MB/s" (8 chars) or "1.5 GB/s" (8 chars)
		axisLabelWidth := 10
		graphWidth := contentWidth - axisLabelWidth - 1 // -1 for space between label and graph
		if graphWidth < 10 {
			graphWidth = 10
		}

		// Get the peak rate from history for stable y-axis scaling
		peakRate := m.history.GetPeakNetworkRate(host, DefaultHistorySize, m.interval.Seconds())
		if peakRate < 1024 {
			peakRate = 1024 // minimum 1 KB/s for display
		}

		// Normalize the linear rates to 0-100 percentage based on peak
		// This way the graph fills the full height when at peak rate
		normalizedHistory := make([]float64, len(netHistory))
		for i, rate := range netHistory {
			if peakRate > 0 {
				normalizedHistory[i] = (rate / peakRate) * 100
			}
		}

		// Format function shows actual rates on y-axis
		formatNetRate := func(v float64) string {
			// v is 0-100 percentage, convert back to actual rate for display
			actualRate := (v / 100) * peakRate
			return FormatRate(actualRate)
		}
		// Use constant accent color - network activity isn't inherently "bad" at high values
		constantColor := func(_ float64) lipgloss.Color { return ColorAccent }
		graph := RenderGraphWithYAxis(normalizedHistory, graphWidth, 6, ColorGraph, formatNetRate, axisLabelWidth, constantColor, false)
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

		bar := RenderGradientBar(contentWidth, activityPercent, ColorGraph)
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

// generateDetailContent builds the scrollable content for the detail view.
// This is separated so it can be called from Update to set viewport content.
func (m Model) generateDetailContent() string {
	host := m.SelectedHost()
	if host == "" {
		return ""
	}

	metrics := m.metrics[host]
	contentWidth := m.width - 4 // Account for container padding (2 left, 2 right)
	if contentWidth < 40 {
		contentWidth = 40
	}

	var content strings.Builder

	// If no metrics, show waiting message
	if metrics == nil {
		content.WriteString(detailSectionStyle.Width(contentWidth).Render(
			LabelStyle.Render("Waiting for metrics data...")))
		return content.String()
	}

	// 1. CPU and Latency side by side (or stacked on narrow terminals)
	halfWidth := (contentWidth - 1) / 2 // -1 for the space between sections
	if contentWidth >= 80 {
		cpuSection := m.renderDetailCPUSection(host, metrics.CPU, halfWidth)
		latSection := m.renderDetailLatencySection(host, halfWidth)

		// Join side by side
		cpuLines := strings.Split(cpuSection, "\n")
		latLines := strings.Split(latSection, "\n")

		// Pad to same height
		maxLines := len(cpuLines)
		if len(latLines) > maxLines {
			maxLines = len(latLines)
		}
		for len(cpuLines) < maxLines {
			cpuLines = append(cpuLines, "")
		}
		for len(latLines) < maxLines {
			latLines = append(latLines, "")
		}

		for i := 0; i < maxLines; i++ {
			cpuLine := padToWidth(cpuLines[i], halfWidth)
			latLine := padToWidth(latLines[i], halfWidth)
			content.WriteString(cpuLine)
			content.WriteString(" ")
			content.WriteString(latLine)
			content.WriteString("\n")
		}
	} else {
		// Single column for narrow terminals
		cpuSection := m.renderDetailCPUSection(host, metrics.CPU, contentWidth)
		content.WriteString(cpuSection)
		content.WriteString("\n")

		latSection := m.renderDetailLatencySection(host, contentWidth)
		content.WriteString(latSection)
		content.WriteString("\n")
	}

	// 2. Process Table
	if len(metrics.Processes) > 0 {
		procSection := m.renderDetailProcessSection(metrics.Processes, contentWidth)
		content.WriteString(procSection)
		content.WriteString("\n")
	}

	// 3. Memory and Network side by side (or stacked on narrow terminals)
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

	return content.String()
}

// renderDetailViewWithViewport renders the detail view with viewport scrolling support.
// The viewport allows scrolling when content exceeds the visible area.
func (m Model) renderDetailViewWithViewport() string {
	host := m.SelectedHost()
	if host == "" {
		return LabelStyle.Render("No host selected")
	}

	// Build the header (fixed at top)
	status := m.status[host]
	header := m.renderDetailHeader(host, status)

	// Build the footer with scroll indicator
	footer := m.renderDetailFooterWithScroll()

	// If viewport is ready, use it for scrolling
	if m.viewportReady {
		// The content should already be set via updateDetailViewportContent
		return fmt.Sprintf("%s\n\n%s\n%s",
			detailContainerStyle.Render(header),
			m.detailViewport.View(),
			footer,
		)
	}

	// Fallback: render without viewport
	content := m.generateDetailContent()
	return detailContainerStyle.Render(fmt.Sprintf("%s\n\n%s\n\n%s", header, content, m.renderDetailFooter()))
}

// updateDetailViewportContent updates the viewport with the current detail content.
// This must be called when entering detail view or when content changes.
func (m *Model) updateDetailViewportContent() {
	if m.viewportReady {
		content := m.generateDetailContent()
		m.detailViewport.SetContent(content)
	}
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
