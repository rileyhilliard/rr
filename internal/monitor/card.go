package monitor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Card layout constants
const (
	cardGraphHeight = 2  // braille graph rows
	cardMinBarWidth = 10 // minimum graph width
)

// cardDividerStyle creates a subtle divider line with matching background
var cardDividerStyle = lipgloss.NewStyle().
	Foreground(ColorBorder).
	Background(ColorSurfaceBg)

// renderCardDivider creates a subtle thin divider line
func renderCardDivider(width int) string {
	divider := strings.Repeat("─", width)
	return cardDividerStyle.Render(divider)
}

// parseErrorParts extracts the core error and suggestion from a structured error message.
// Structured errors have format: "✗ Message\n\n  cause\n\n  suggestion"
func parseErrorParts(errMsg string) (core string, suggestion string) {
	lines := strings.Split(errMsg, "\n")

	var coreLines []string
	var suggestionLines []string
	inSuggestion := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip the ✗ symbol line (main message)
		if strings.HasPrefix(line, "✗") {
			continue
		}

		// Heuristic: suggestions often contain actionable words
		if strings.Contains(line, "Try:") ||
			strings.Contains(line, "Check") ||
			strings.Contains(line, "Make sure") ||
			strings.Contains(line, "might be") ||
			strings.Contains(line, "may be") ||
			strings.Contains(line, "ssh-add") ||
			strings.Contains(line, "ssh ") {
			inSuggestion = true
		}

		if inSuggestion {
			suggestionLines = append(suggestionLines, line)
		} else {
			coreLines = append(coreLines, line)
		}
	}

	core = strings.Join(coreLines, " ")
	suggestion = strings.Join(suggestionLines, " ")
	return core, suggestion
}

// truncateWithEllipsis truncates a string to maxLen, adding ellipsis if needed.
func truncateWithEllipsis(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// truncateErrorMsg extracts the most useful part of an error message and truncates to fit.
func truncateErrorMsg(errMsg string, maxLen int) string {
	// Extract the most relevant part of the error
	// Common patterns: "Can't reach 'host' at addr: details" or "dial tcp: timeout"
	msg := errMsg

	// Look for common patterns and extract useful info
	if idx := strings.Index(msg, ": "); idx > 0 && idx < len(msg)-2 {
		// Get the part after the first colon, which usually has more detail
		msg = msg[idx+2:]
	}

	// Truncate if too long
	if len(msg) > maxLen && maxLen > 3 {
		msg = msg[:maxLen-3] + "..."
	}

	return msg
}

// renderCardLine renders a text line with proper background fill.
// Applies background to the entire line including content and padding.
func renderCardLine(content string, width int) string {
	contentWidth := lipgloss.Width(content)
	padding := ""
	if width > contentWidth {
		padding = strings.Repeat(" ", width-contentWidth)
	}
	// Apply background to entire line (content + padding)
	lineStyle := lipgloss.NewStyle().Background(ColorSurfaceBg)
	return lineStyle.Render(content + padding)
}

// renderCard renders a single host card with metrics.
func (m Model) renderCard(host string, width int, selected bool) string {
	metrics := m.metrics[host]
	status := m.status[host]

	// Choose card style based on selection
	style := CardStyle.Width(width)
	if selected {
		style = CardSelectedStyle.Width(width)
	}

	// Inner width for content (account for card padding)
	innerWidth := width - 4

	var lines []string

	// Host name with status indicator
	hostLine := m.renderHostLine(host, status)
	lines = append(lines, renderCardLine(hostLine, innerWidth))

	// If no metrics available, show status-appropriate placeholder with error details
	if metrics == nil {
		lines = append(lines, renderCardDivider(innerWidth))
		if status == StatusUnreachableState {
			lines = append(lines, renderCardLine(StatusUnreachableStyle.Render("  Unreachable"), innerWidth))

			// Show error details and suggestion on separate lines
			if errMsg, ok := m.errors[host]; ok && errMsg != "" {
				core, suggestion := parseErrorParts(errMsg)

				// Show core error (e.g., "connection refused")
				if core != "" {
					errDisplay := truncateWithEllipsis(core, innerWidth-4)
					lines = append(lines, renderCardLine(LabelStyle.Render("  "+errDisplay), innerWidth))
				}

				// Add divider before suggestion
				lines = append(lines, renderCardDivider(innerWidth))

				// Show suggestion with word wrapping for readability
				if suggestion != "" {
					// Wrap long suggestions across multiple lines
					suggestionWidth := innerWidth - 4
					words := strings.Fields(suggestion)
					var currentLine string
					for _, word := range words {
						if currentLine == "" {
							currentLine = word
						} else if len(currentLine)+1+len(word) <= suggestionWidth {
							currentLine += " " + word
						} else {
							lines = append(lines, renderCardLine(LabelStyle.Render("  "+currentLine), innerWidth))
							currentLine = word
						}
					}
					if currentLine != "" {
						lines = append(lines, renderCardLine(LabelStyle.Render("  "+currentLine), innerWidth))
					}
				} else {
					// Default suggestion if none parsed
					lines = append(lines, renderCardLine(LabelStyle.Render("  Host may be offline or unreachable"), innerWidth))
				}
			} else {
				// No error message available
				lines = append(lines, renderCardDivider(innerWidth))
				lines = append(lines, renderCardLine(LabelStyle.Render("  No connection details available"), innerWidth))
			}

			// Add padding lines to roughly match online card height
			// Online cards have: CPU header + 2 graph rows + RAM header + 2 graph rows + net + top = ~10 lines
			// We have: Unreachable + error + divider + suggestion = ~4 lines
			// Add a few blank lines for visual consistency
			lines = append(lines, renderCardLine("", innerWidth))
			lines = append(lines, renderCardLine("", innerWidth))
		} else {
			lines = append(lines, renderCardLine(LabelStyle.Render("  Connecting..."), innerWidth))
		}
	} else {
		// Divider after host name
		lines = append(lines, renderCardDivider(innerWidth))

		// CPU metrics with braille graph
		cpuLines := m.renderCardCPUSection(host, metrics.CPU, innerWidth)
		lines = append(lines, cpuLines...)

		// Divider before RAM
		lines = append(lines, renderCardDivider(innerWidth))

		// RAM metrics with braille graph
		ramLines := m.renderCardRAMSection(host, metrics.RAM, innerWidth)
		lines = append(lines, ramLines...)

		// Network rates (with divider if present)
		netLine := m.renderCardNetworkLine(host, innerWidth)
		if netLine != "" {
			lines = append(lines, renderCardDivider(innerWidth))
			lines = append(lines, renderCardLine(netLine, innerWidth))
		}

		// Top process (with divider if present)
		if len(metrics.Processes) > 0 {
			lines = append(lines, renderCardDivider(innerWidth))
			topLine := m.renderCardTopProcess(metrics.Processes, innerWidth)
			lines = append(lines, renderCardLine(topLine, innerWidth))
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

// renderCardCPUSection renders CPU with a braille sparkline graph.
// Returns multiple lines: header line + graph rows.
func (m Model) renderCardCPUSection(host string, cpu CPUMetrics, lineWidth int) []string {
	var lines []string

	// Header line: "CPU" label + right-aligned percentage and load
	label := LabelStyle.Render("CPU")
	pctText := MetricStyle(cpu.Percent).Render(fmt.Sprintf("%5.1f%%", cpu.Percent))
	loadText := LabelStyle.Render(fmt.Sprintf("L:%.1f", cpu.LoadAvg[0]))

	// Right side content
	rightContent := pctText + " " + loadText
	rightWidth := lipgloss.Width(rightContent)

	// Calculate padding for right alignment
	padding := ""
	if lineWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", lineWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + rightContent
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Graph width
	graphWidth := lineWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	// Braille graph
	cpuHistory := m.history.GetCPUHistory(host, DefaultHistorySize)
	if len(cpuHistory) > 0 {
		graph := RenderBrailleSparkline(cpuHistory, graphWidth, cardGraphHeight, ColorGraph)
		graphLines := strings.Split(graph, "\n")
		for _, gl := range graphLines {
			lines = append(lines, renderCardLine(gl, lineWidth))
		}
	} else {
		// Show gradient bar while collecting history
		bar := RenderGradientBar(graphWidth, cpu.Percent, ColorGraph)
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCardRAMSection renders RAM with a braille sparkline graph.
func (m Model) renderCardRAMSection(host string, ram RAMMetrics, lineWidth int) []string {
	var lines []string

	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	// Header line: "RAM" label + right-aligned percentage
	label := LabelStyle.Render("RAM")
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))

	// Calculate padding for right alignment
	rightWidth := lipgloss.Width(pctText)
	padding := ""
	if lineWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", lineWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + pctText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Graph width
	graphWidth := lineWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	// Braille graph
	ramHistory := m.history.GetRAMHistory(host, DefaultHistorySize)
	if len(ramHistory) > 0 {
		graph := RenderBrailleSparkline(ramHistory, graphWidth, cardGraphHeight, ColorGraph)
		graphLines := strings.Split(graph, "\n")
		for _, gl := range graphLines {
			lines = append(lines, renderCardLine(gl, lineWidth))
		}
	} else {
		// Show gradient bar while collecting history
		bar := RenderGradientBar(graphWidth, percent, ColorGraph)
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCardNetworkLine renders network throughput rates in a single line.
func (m Model) renderCardNetworkLine(host string, lineWidth int) string {
	inRate, outRate := m.history.GetTotalNetworkRate(host, m.interval.Seconds())

	// Skip if no rate data yet
	if inRate == 0 && outRate == 0 {
		return ""
	}

	label := LabelStyle.Render("NET")
	downArrow := lipgloss.NewStyle().Foreground(ColorAccent).Render("↓")
	upArrow := lipgloss.NewStyle().Foreground(ColorAccent).Render("↑")

	inText := ValueStyle.Render(FormatRate(inRate))
	outText := ValueStyle.Render(FormatRate(outRate))

	// Right-align the rates
	rightContent := downArrow + inText + " " + upArrow + outText
	rightWidth := lipgloss.Width(rightContent)
	padding := ""
	if lineWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", lineWidth-lipgloss.Width(label)-rightWidth)
	}

	return label + padding + rightContent
}

// renderCardTopProcess renders the top process by CPU in a single line.
func (m Model) renderCardTopProcess(procs []ProcessInfo, maxWidth int) string {
	if len(procs) == 0 {
		return ""
	}

	label := LabelStyle.Render("TOP")
	proc := procs[0]

	// Extract command name (first path component or word)
	cmd := proc.Command
	if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
		cmd = cmd[idx+1:]
	}
	if idx := strings.Index(cmd, " "); idx > 0 {
		cmd = cmd[:idx]
	}

	// Format percentage with color
	pctColor := MetricColor(proc.CPU)
	pctText := lipgloss.NewStyle().Foreground(pctColor).Render(fmt.Sprintf("%.0f%%", proc.CPU))

	// Truncate command if needed (leave room for label + padding + cmd(pct))
	maxCmdLen := 15
	if len(cmd) > maxCmdLen {
		cmd = cmd[:maxCmdLen-2] + ".."
	}

	// Right-align: "cmd(pct)"
	rightContent := cmd + "(" + pctText + ")"
	rightWidth := lipgloss.Width(rightContent)
	padding := ""
	if maxWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", maxWidth-lipgloss.Width(label)-rightWidth)
	}

	return label + padding + rightContent
}

// RenderLoadAvg renders the system load average.
func RenderLoadAvg(loadAvg [3]float64) string {
	label := LabelStyle.Render("Load")
	values := ValueStyle.Render(fmt.Sprintf("%.2f %.2f %.2f", loadAvg[0], loadAvg[1], loadAvg[2]))
	return fmt.Sprintf("%s %s", label, values)
}

// renderCompactCard renders a compact card for terminals 80-120 columns wide.
// Uses the same braille graph layout as full cards but with smaller graphs.
func (m Model) renderCompactCard(host string, width int, selected bool) string {
	metrics := m.metrics[host]
	status := m.status[host]

	// Choose card style based on selection
	style := CardStyle.Width(width)
	if selected {
		style = CardSelectedStyle.Width(width)
	}

	innerWidth := width - 4
	var lines []string

	// Host name with status indicator
	hostLine := m.renderHostLine(host, status)
	lines = append(lines, renderCardLine(hostLine, innerWidth))

	// If no metrics available, show status-appropriate placeholder with error details
	if metrics == nil {
		lines = append(lines, renderCardDivider(innerWidth))
		if status == StatusUnreachableState {
			lines = append(lines, renderCardLine(StatusUnreachableStyle.Render("  Unreachable"), innerWidth))

			// Show error details and suggestion
			if errMsg, ok := m.errors[host]; ok && errMsg != "" {
				core, suggestion := parseErrorParts(errMsg)

				// Show core error
				if core != "" {
					errDisplay := truncateWithEllipsis(core, innerWidth-4)
					lines = append(lines, renderCardLine(LabelStyle.Render("  "+errDisplay), innerWidth))
				}

				// Add divider before suggestion
				lines = append(lines, renderCardDivider(innerWidth))

				// Show suggestion (compact: single line with truncation)
				if suggestion != "" {
					suggDisplay := truncateWithEllipsis(suggestion, innerWidth-4)
					lines = append(lines, renderCardLine(LabelStyle.Render("  "+suggDisplay), innerWidth))
				} else {
					lines = append(lines, renderCardLine(LabelStyle.Render("  Host may be offline or unreachable"), innerWidth))
				}
			} else {
				lines = append(lines, renderCardDivider(innerWidth))
				lines = append(lines, renderCardLine(LabelStyle.Render("  No connection details"), innerWidth))
			}
		} else {
			lines = append(lines, renderCardLine(LabelStyle.Render("  Connecting..."), innerWidth))
		}
	} else {
		lines = append(lines, renderCardDivider(innerWidth))

		// CPU with single-row sparkline
		cpuLines := m.renderCompactCPUSection(host, metrics.CPU, innerWidth)
		lines = append(lines, cpuLines...)

		lines = append(lines, renderCardDivider(innerWidth))

		// RAM with single-row sparkline
		ramLines := m.renderCompactRAMSection(host, metrics.RAM, innerWidth)
		lines = append(lines, ramLines...)
	}

	content := strings.Join(lines, "\n")
	return style.Render(content)
}

// renderCompactCPUSection renders CPU with a single-row braille graph for compact mode.
func (m Model) renderCompactCPUSection(host string, cpu CPUMetrics, lineWidth int) []string {
	var lines []string

	label := LabelStyle.Render("CPU")
	pctText := MetricStyle(cpu.Percent).Render(fmt.Sprintf("%5.1f%%", cpu.Percent))

	// Right-aligned percentage
	rightWidth := lipgloss.Width(pctText)
	padding := ""
	if lineWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", lineWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + pctText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Single-row braille graph
	graphWidth := lineWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	cpuHistory := m.history.GetCPUHistory(host, DefaultHistorySize)
	if len(cpuHistory) > 0 {
		graph := RenderBrailleSparkline(cpuHistory, graphWidth, 1, ColorGraph)
		lines = append(lines, renderCardLine(graph, lineWidth))
	} else {
		bar := RenderGradientBar(graphWidth, cpu.Percent, ColorGraph)
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCompactRAMSection renders RAM with a single-row braille graph for compact mode.
func (m Model) renderCompactRAMSection(host string, ram RAMMetrics, lineWidth int) []string {
	var lines []string

	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	label := LabelStyle.Render("RAM")
	pctText := MetricStyle(percent).Render(fmt.Sprintf("%5.1f%%", percent))

	// Right-aligned percentage
	rightWidth := lipgloss.Width(pctText)
	padding := ""
	if lineWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", lineWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + pctText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Single-row braille graph
	graphWidth := lineWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	ramHistory := m.history.GetRAMHistory(host, DefaultHistorySize)
	if len(ramHistory) > 0 {
		graph := RenderBrailleSparkline(ramHistory, graphWidth, 1, ColorGraph)
		lines = append(lines, renderCardLine(graph, lineWidth))
	} else {
		bar := RenderGradientBar(graphWidth, percent, ColorGraph)
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
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

	innerWidth := width - 4
	var lines []string

	// Host name with status indicator (abbreviated if necessary)
	hostLine := m.renderMinimalHostLine(host, status, innerWidth)
	lines = append(lines, renderCardLine(hostLine, innerWidth))

	// If no metrics available, show status-appropriate placeholder
	if metrics == nil {
		placeholder := "..."
		if status == StatusUnreachableState {
			placeholder = "Offline"
		}
		lines = append(lines, renderCardLine(LabelStyle.Render(placeholder), innerWidth))
	} else {
		lines = append(lines, renderCardDivider(innerWidth))
		// Single line with CPU and RAM percentages
		metricsLine := m.renderMinimalMetricsLine(metrics, innerWidth)
		lines = append(lines, renderCardLine(metricsLine, innerWidth))
	}

	content := strings.Join(lines, "\n")
	return style.Render(content)
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
