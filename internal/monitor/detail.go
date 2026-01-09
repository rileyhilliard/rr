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

// renderDetailCPUSection renders the CPU section with time series graph and right-aligned percentage.
func (m Model) renderDetailCPUSection(host string, cpu CPUMetrics, width int) string {
	var lines []string

	// Section header with right-aligned percentage
	pctText := fmt.Sprintf("%.1f%%", cpu.Percent)
	lines = append(lines, SectionHeader("CPU", pctText, width))

	// Content area
	graphWidth := width - 6
	if graphWidth < 20 {
		graphWidth = 20
	}

	// Time series graph - 4 rows tall for good visibility
	// Request more history points for smoother graph
	history := m.history.GetCPUHistory(host, graphWidth)
	if len(history) > 0 {
		graph := RenderTimeSeriesGraph(history, graphWidth, 4, ColorGraph)
		for _, line := range strings.Split(graph, "\n") {
			lines = append(lines, SectionBorder()+"  "+line+"  "+SectionBorder())
		}
	} else {
		lines = append(lines, SectionBorder()+"  "+LabelStyle.Render("Collecting data...")+"  "+SectionBorder())
	}

	// Load average and cores on same line
	loadText := fmt.Sprintf("Load: %.2f / %.2f / %.2f", cpu.LoadAvg[0], cpu.LoadAvg[1], cpu.LoadAvg[2])
	if cpu.Cores > 0 {
		loadText += fmt.Sprintf("  ·  Cores: %d", cpu.Cores)
	}
	lines = append(lines, SectionBorder()+"  "+LabelStyle.Render(loadText))

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
}

// renderDetailRAMSection renders the RAM section with thin progress bar.
func (m Model) renderDetailRAMSection(_ string, ram RAMMetrics, width int) string {
	var lines []string

	// Calculate percentage
	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	// Section header with right-aligned percentage
	pctText := fmt.Sprintf("%.1f%%", percent)
	lines = append(lines, SectionHeader("Memory", pctText, width))

	// Thin progress bar
	barWidth := width - 6
	if barWidth < 10 {
		barWidth = 10
	}
	bar := ThinProgressBar(barWidth, percent)
	lines = append(lines, SectionBorder()+"  "+bar+"  "+SectionBorder())

	// Memory breakdown - compact single line
	usedStr := formatBytes(ram.UsedBytes)
	totalStr := formatBytes(ram.TotalBytes)
	memText := fmt.Sprintf("%s / %s", usedStr, totalStr)
	if ram.Available > 0 {
		memText += fmt.Sprintf("  ·  Avail: %s", formatBytes(ram.Available))
	}
	lines = append(lines, SectionBorder()+"  "+LabelStyle.Render(memText))

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

	// Clean single-row sparkline
	graphWidth := width - 6
	if graphWidth < 10 {
		graphWidth = 10
	}

	gpuHistory := m.history.GetGPUHistory(host, graphWidth)
	if len(gpuHistory) > 0 {
		graph := RenderCleanSparkline(gpuHistory, graphWidth, ColorGraph)
		lines = append(lines, SectionBorder()+"  "+graph+"  "+SectionBorder())
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
		lines = append(lines, SectionBorder()+"  "+LabelStyle.Render(strings.Join(details, "  ·  ")))
	}

	// Section footer
	lines = append(lines, SectionFooter(width))

	return strings.Join(lines, "\n")
}

// renderDetailNetworkSection renders network with rates and activity indicator.
func (m Model) renderDetailNetworkSection(host string, width int) string {
	var lines []string

	// Get total rates
	inRate, outRate := m.history.GetTotalNetworkRate(host, m.interval.Seconds())

	// Section header with rates
	downArrow := lipgloss.NewStyle().Foreground(ColorAccent).Render("↓")
	upArrow := lipgloss.NewStyle().Foreground(ColorAccent).Render("↑")
	rateText := fmt.Sprintf("%s%s  %s%s", downArrow, FormatRate(inRate), upArrow, FormatRate(outRate))
	lines = append(lines, SectionHeader("Network", rateText, width))

	// Activity indicator using thin bar style
	barWidth := width - 6
	if barWidth < 10 {
		barWidth = 10
	}

	// Calculate activity percentage (log scale for visibility)
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

	// Use thin bar for activity (always green since network activity isn't "bad")
	bar := ThinProgressBarWithThresholds(barWidth, activityPercent, 101, 102) // thresholds > 100 means always green
	lines = append(lines, SectionBorder()+"  "+bar+"  "+SectionBorder())

	// Status text
	var statusText string
	if totalRate == 0 {
		statusText = "idle"
	} else {
		statusText = fmt.Sprintf("Total: %s", FormatRate(totalRate))
	}
	lines = append(lines, SectionBorder()+"  "+LabelStyle.Render(statusText))

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
	lines = append(lines, SectionBorder()+"  "+headerStyle.Render(header))

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
		lines = append(lines, SectionBorder()+"  "+line)
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
			for len(ramLines) < maxLines {
				ramLines = append(ramLines, strings.Repeat(" ", halfWidth))
			}
			for len(netLines) < maxLines {
				netLines = append(netLines, strings.Repeat(" ", halfWidth))
			}

			for i := 0; i < maxLines; i++ {
				content.WriteString(ramLines[i])
				content.WriteString(" ")
				content.WriteString(netLines[i])
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
