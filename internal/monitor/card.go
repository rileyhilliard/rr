package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Card layout constants
const (
	cardGraphHeight         = 2  // braille graph rows
	cardGraphHeightExtended = 4  // braille graph rows on tall terminals
	cardMinBarWidth         = 10 // minimum graph width
	cardTopProcs            = 1  // top processes listed on a full card
	cardTopProcsExtended    = 3  // top processes listed on a tall terminal
)

// cardGraphRows returns the braille graph height for full cards. Tall terminals
// get a taller graph since there's vertical room to spare.
func (m Model) cardGraphRows() int {
	if m.CanShowExtendedInfo() {
		return cardGraphHeightExtended
	}
	return cardGraphHeight
}

// cardProcessCount returns how many top processes a full card lists. Tall
// terminals show more of the already-collected process list.
func (m Model) cardProcessCount() int {
	if m.CanShowExtendedInfo() {
		return cardTopProcsExtended
	}
	return cardTopProcs
}

// cpuWarmingUpText is shown in place of the CPU percentage until a real reading
// is available. See CPUMetrics.Valid.
const cpuWarmingUpText = "warming up"

// renderCPUWarmingUp renders the dim placeholder shown instead of "0.0%" while
// the first CPU sample is still being established.
func renderCPUWarmingUp() string {
	return lipgloss.NewStyle().Foreground(ColorTextMuted).Render(cpuWarmingUpText)
}

// genZErrorMessage converts a technical error message to gen-z themed text.
func genZErrorMessage(err string) string {
	errLower := strings.ToLower(err)
	switch {
	case strings.Contains(errLower, "timeout") || strings.Contains(errLower, "timed out"):
		return "host ghosted us (timeout)"
	case strings.Contains(errLower, "refused") || strings.Contains(errLower, "connection refused"):
		return "host left us on read (refused)"
	case strings.Contains(errLower, "unreachable") || strings.Contains(errLower, "no route"):
		return "host is MIA (unreachable)"
	case strings.Contains(errLower, "handshake"):
		return "couldn't vibe (handshake failed)"
	case strings.Contains(errLower, "permission") || strings.Contains(errLower, "denied"):
		return "got gatekept (permission denied)"
	case strings.Contains(errLower, "host key"):
		return "trust issues (host key problem)"
	default:
		return "connection didn't vibe"
	}
}

// renderCardDivider creates a subtle thin divider line
func renderCardDivider(width int) string {
	// Divider fills content area (width minus 2 for padding)
	dividerWidth := width - 2
	if dividerWidth < 1 {
		dividerWidth = 1
	}
	divider := strings.Repeat("─", dividerWidth)
	// Apply same padding as renderCardLine for alignment
	dividerStyle := lipgloss.NewStyle().Foreground(ColorBorder).Background(ColorSurfaceBg)
	return dividerStyle.Render(" " + divider + " ")
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

// renderCommandLine renders the command being executed, truncated to fit width.
// Returns empty string if no command is available.
func (m Model) renderCommandLine(host string, width int) string {
	lockInfo, ok := m.lockInfo[host]
	if !ok || lockInfo == nil || lockInfo.Command == "" {
		return ""
	}

	// Truncate command to fit within available width
	// Account for prefix "  > " (4 chars) and padding (2 chars)
	maxCmdWidth := width - 6
	if maxCmdWidth < 10 {
		return "" // Too narrow to show command
	}

	cmd := truncateWithEllipsis(lockInfo.Command, maxCmdWidth)
	cmdStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)
	return cmdStyle.Render("  > " + cmd)
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

// cachedCardBody returns the cached rendered body (graphs/metric sections) for
// a host, invoking render on a cache miss. The body depends only on the host's
// metrics, history, and the terminal dimensions (width sets the card width;
// height selects graph rows and process count via CanShowExtendedInfo), so it
// stays valid between results. Invalidation happens in model.go: per-host when
// that host gets a new result, and wholesale on resize, which covers both
// dimensions. Dynamic chrome (spinners, countdowns, selection border) is never
// cached.
func (m Model) cachedCardBody(host string, render func() string) string {
	if m.cardBodyCache == nil {
		return render()
	}
	if body, ok := m.cardBodyCache[host]; ok {
		return body
	}
	body := render()
	m.cardBodyCache[host] = body
	return body
}

// renderCardLine renders a text line with symmetric padding and background fill.
// Adds 1-space padding on each side for consistent card layout.
func renderCardLine(content string, width int) string {
	contentWidth := lipgloss.Width(content)
	// Content area is width minus 2 spaces for padding (1 left, 1 right)
	contentArea := width - 2
	rightPadding := ""
	if contentArea > contentWidth {
		rightPadding = strings.Repeat(" ", contentArea-contentWidth)
	}
	// Apply background to entire line: " " + content + rightPadding + " "
	lineStyle := lipgloss.NewStyle().Background(ColorSurfaceBg)
	return lineStyle.Render(" " + content + rightPadding + " ")
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

	// width IS the content area (lipgloss Width sets content area, borders add to total)
	innerWidth := width
	contentWidth := innerWidth - 2 // Space for actual content (minus 1-space padding each side)

	var lines []string

	// Host name with status indicator and SSH alias on the right
	hostLine := m.renderHostLineWithSSH(host, status, contentWidth)
	lines = append(lines, renderCardLine(hostLine, innerWidth))

	// Show command line for running hosts (only in standard/wide modes)
	if status == StatusRunningState {
		if cmdLine := m.renderCommandLine(host, contentWidth); cmdLine != "" {
			lines = append(lines, renderCardLine(cmdLine, innerWidth))
		}
	}

	// If no metrics available, show status-appropriate placeholder with error details
	if metrics == nil {
		lines = append(lines, renderCardDivider(innerWidth))
		switch status {
		case StatusConnectingState:
			// Check for retry state (Attempts tracks actual failures)
			connState := m.connState[host]
			if connState != nil && connState.Attempts >= 1 {
				// Check if in backoff (not actively retrying)
				if !connState.NextRetry.IsZero() && time.Now().Before(connState.NextRetry) {
					// In backoff - show offline state with next retry time
					secsUntil := int(time.Until(connState.NextRetry).Seconds())
					if secsUntil < 1 {
						secsUntil = 1
					}
					offlineText := fmt.Sprintf("Offline · retry in %ds", secsUntil)
					lines = append(lines, renderCardLine(StatusUnreachableStyle.Render("  "+offlineText), innerWidth))
				} else {
					// Actively retrying (not in backoff yet)
					failedText := fmt.Sprintf("Connecting · %d failed", connState.Attempts)
					lines = append(lines, renderCardLine(StatusConnectingStyle.Render("  "+failedText), innerWidth))
				}
				lines = append(lines, renderCardLine("", innerWidth))
				lines = append(lines, renderCardDivider(innerWidth))

				// Show last error with gen-z spin if available
				if connState.LastError != "" {
					errText := genZErrorMessage(connState.LastError)
					lines = append(lines, renderCardLine(LabelStyle.Render("  "+errText), innerWidth))
				} else {
					lines = append(lines, renderCardLine(LabelStyle.Render("  connection failed"), innerWidth))
				}
			} else {
				// First attempt: show standard "Linking up"
				lines = append(lines, renderCardLine(StatusConnectingStyle.Render("  "+m.ConnectingText()), innerWidth))
				lines = append(lines, renderCardLine("", innerWidth))
				lines = append(lines, renderCardDivider(innerWidth))
				lines = append(lines, renderCardLine(LabelStyle.Render("  "+m.ConnectingSubtext()), innerWidth))
			}
			// Add padding lines to roughly match online card height
			lines = append(lines, renderCardLine("", innerWidth))
			lines = append(lines, renderCardLine("", innerWidth))
		case StatusUnreachableState:
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
		}
	} else {
		// Expensive metric body: cached per host, invalidated on new results
		lines = append(lines, m.cachedCardBody(host, func() string {
			return m.renderCardBody(host, metrics, innerWidth)
		}))
	}

	content := strings.Join(lines, "\n")
	return style.Render(content)
}

// renderCardBody renders the metric sections of a full card: divider,
// CPU/GPU/latency/RAM graphs, top process, and network rates.
func (m Model) renderCardBody(host string, metrics *HostMetrics, innerWidth int) string {
	var lines []string

	// Divider after host name
	lines = append(lines, renderCardDivider(innerWidth))

	// CPU metrics with braille graph
	cpuLines := m.renderCardCPUSection(host, metrics.CPU, innerWidth)
	lines = append(lines, cpuLines...)

	// GPU metrics with braille graph (if available)
	if metrics.GPU != nil {
		lines = append(lines, renderCardDivider(innerWidth))
		gpuLines := m.renderCardGPUSection(host, metrics.GPU, innerWidth)
		lines = append(lines, gpuLines...)
	}

	// Latency metrics with braille graph (if available)
	latencyLines := m.renderCardLatencySection(host, innerWidth)
	if len(latencyLines) > 0 {
		lines = append(lines, renderCardDivider(innerWidth))
		lines = append(lines, latencyLines...)
	}

	// RAM metrics with braille graph
	lines = append(lines, renderCardDivider(innerWidth))
	ramLines := m.renderCardRAMSection(host, metrics.RAM, innerWidth)
	lines = append(lines, ramLines...)

	// Top processes (with divider if present)
	if len(metrics.Processes) > 0 {
		lines = append(lines, renderCardDivider(innerWidth))
		for _, procLine := range m.renderCardTopProcesses(metrics.Processes, innerWidth) {
			lines = append(lines, renderCardLine(procLine, innerWidth))
		}
	}

	// Network rates (with divider if present)
	netLine := m.renderCardNetworkLine(host, innerWidth)
	if netLine != "" {
		lines = append(lines, renderCardDivider(innerWidth))
		lines = append(lines, renderCardLine(netLine, innerWidth))
	}

	// Disk usage (with divider if present)
	diskLine := m.renderCardDiskLine(metrics.Disk, innerWidth)
	if diskLine != "" {
		lines = append(lines, renderCardDivider(innerWidth))
		lines = append(lines, renderCardLine(diskLine, innerWidth))
	}

	return strings.Join(lines, "\n")
}

// renderHostLine renders the host name with status indicator and status text.
func (m Model) renderHostLine(host string, status HostStatus) string {
	var indicator string
	var indicatorStyle lipgloss.Style
	var statusText string

	switch status {
	case StatusConnectingState:
		indicator = m.ConnectingSpinner() // Animated spinner
		indicatorStyle = StatusConnectingStyle
		// No status text for connecting - card content shows the message
	case StatusIdleState:
		indicator = StatusIdle
		indicatorStyle = StatusIdleStyle
		statusText = StatusTextStyle.Render(" - idle")
	case StatusRunningState:
		indicator, indicatorStyle = m.RunningSpinner() // Animated braille spinner with color cycling
		statusText = m.renderRunningStatusText(host)
	case StatusUnreachableState:
		indicator = StatusUnreachable
		indicatorStyle = StatusUnreachableStyle
		statusText = StatusTextStyle.Render(" - offline")
	}

	return indicatorStyle.Render(indicator) + " " + HostNameStyle.Render(host) + statusText
}

// renderHostLineWithSSH renders the host name with SSH alias on the right side.
// Format: "◉ m4-mini - idle                          ssh m4-tailscale"
func (m Model) renderHostLineWithSSH(host string, status HostStatus, width int) string {
	// Get left side (indicator + host + status)
	leftPart := m.renderHostLine(host, status)
	leftWidth := lipgloss.Width(leftPart)

	// Get SSH alias if available (only show for online hosts)
	sshAlias, hasSSH := m.sshAlias[host]
	if !hasSSH || sshAlias == "" || status == StatusConnectingState || status == StatusUnreachableState {
		// No SSH info to show, just return left part
		return leftPart
	}

	// Format right side: "ssh <alias>"
	sshCmd := "ssh " + sshAlias
	sshStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)
	rightPart := sshStyle.Render(sshCmd)
	rightWidth := lipgloss.Width(rightPart)

	// Calculate padding between left and right
	padding := width - leftWidth - rightWidth
	if padding < 1 {
		// Not enough space, skip SSH display
		return leftPart
	}

	return leftPart + strings.Repeat(" ", padding) + rightPart
}

// renderCardCPUSection renders CPU with a braille sparkline graph.
// Returns multiple lines: header line + graph rows.
func (m Model) renderCardCPUSection(host string, cpu CPUMetrics, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	// Header line: "CPU" label + right-aligned percentage and load
	label := LabelStyle.Render("CPU")
	loadText := LabelStyle.Render(fmt.Sprintf("1m:%.1f", cpu.LoadAvg[0]))

	// Right side content ("warming up" replaces the percentage until the first
	// real delta is available)
	var rightContent string
	if cpu.Valid() {
		pctText := thresholdStyle(cpu.Percent, m.thresholds.CPU).Render(fmt.Sprintf("%5.1f%%", cpu.Percent))
		rightContent = pctText + " " + loadText
	} else {
		rightContent = renderCPUWarmingUp() + " " + loadText
	}
	rightWidth := lipgloss.Width(rightContent)

	// Calculate padding for right alignment
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + rightContent
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Graph width (content area)
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	// Braille graph
	cpuHistory := m.history.GetCPUHistory(host, DefaultHistorySize)
	if len(cpuHistory) > 0 {
		graph := RenderBrailleSparklineWithColorFunc(cpuHistory, graphWidth, m.cardGraphRows(), ColorGraph, thresholdColorFunc(m.thresholds.CPU))
		graphLines := strings.Split(graph, "\n")
		for _, gl := range graphLines {
			lines = append(lines, renderCardLine(gl, lineWidth))
		}
	} else {
		// Show gradient bar while collecting history
		bar := RenderGradientBarWithColorFunc(graphWidth, cpu.Percent, thresholdColorFunc(m.thresholds.CPU))
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCardRAMSection renders RAM with a braille sparkline graph.
func (m Model) renderCardRAMSection(host string, ram RAMMetrics, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	// Header line: "RAM" label + right-aligned percentage
	label := LabelStyle.Render("RAM")
	pctText := thresholdStyle(percent, m.thresholds.RAM).Render(fmt.Sprintf("%5.1f%%", percent))

	// Calculate padding for right alignment
	rightWidth := lipgloss.Width(pctText)
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + pctText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Graph width (content area)
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	// Braille graph
	ramHistory := m.history.GetRAMHistory(host, DefaultHistorySize)
	if len(ramHistory) > 0 {
		graph := RenderBrailleSparklineWithColorFunc(ramHistory, graphWidth, m.cardGraphRows(), ColorGraph, thresholdColorFunc(m.thresholds.RAM))
		graphLines := strings.Split(graph, "\n")
		for _, gl := range graphLines {
			lines = append(lines, renderCardLine(gl, lineWidth))
		}
	} else {
		// Show gradient bar while collecting history
		bar := RenderGradientBarWithColorFunc(graphWidth, percent, thresholdColorFunc(m.thresholds.RAM))
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCardGPUSection renders GPU with a braille sparkline graph.
// Returns multiple lines: header line + graph rows.
func (m Model) renderCardGPUSection(host string, gpu *GPUMetrics, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	// Header line: "GPU" label + right-aligned percentage + temperature
	label := LabelStyle.Render("GPU")
	pctText := thresholdStyle(gpu.Percent, m.thresholds.GPU).Render(fmt.Sprintf("%5.1f%%", gpu.Percent))

	// Add temperature if available
	var rightContent string
	if gpu.Temperature > 0 {
		tempText := GPUTempStyle(gpu.Temperature).Render(fmt.Sprintf("%dC", gpu.Temperature))
		rightContent = pctText + " " + tempText
	} else {
		rightContent = pctText
	}
	rightWidth := lipgloss.Width(rightContent)

	// Calculate padding for right alignment
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + rightContent
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Graph width (content area)
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	// Braille graph
	gpuHistory := m.history.GetGPUHistory(host, DefaultHistorySize)
	if len(gpuHistory) > 0 {
		graph := RenderBrailleSparklineWithColorFunc(gpuHistory, graphWidth, m.cardGraphRows(), ColorGraph, thresholdColorFunc(m.thresholds.GPU))
		graphLines := strings.Split(graph, "\n")
		for _, gl := range graphLines {
			lines = append(lines, renderCardLine(gl, lineWidth))
		}
	} else {
		// Show gradient bar while collecting history
		bar := RenderGradientBarWithColorFunc(graphWidth, gpu.Percent, thresholdColorFunc(m.thresholds.GPU))
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCardLatencySection renders latency with a braille sparkline graph.
func (m Model) renderCardLatencySection(host string, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	// Get current latency
	latencyDur, ok := m.latency[host]
	if !ok || latencyDur == 0 {
		return nil // No latency data yet
	}

	latencyMs := float64(latencyDur.Milliseconds())

	// Header line: "LAT" label + right-aligned latency value + quality text
	label := LabelStyle.Render("LAT")
	latencyText := LatencyStyle(latencyMs).Render(fmt.Sprintf("%3.0fms", latencyMs))
	qualityText := LabelStyle.Render(LatencyQualityText(latencyMs))

	// Right side content
	rightContent := latencyText + " " + qualityText
	rightWidth := lipgloss.Width(rightContent)

	// Calculate padding for right alignment
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + rightContent
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Graph width (content area)
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	// Braille graph for latency history
	latencyHistory := m.history.GetLatencyHistory(host, DefaultHistorySize)
	if len(latencyHistory) > 0 {
		// Apply moving average to smooth noise while preserving trends
		smoothedHistory := SmoothWithMovingAverage(latencyHistory, 5)
		// Use per-column coloring based on latency thresholds (green=fast, red=degraded)
		// forceZeroMin=true so high latency shows high on the graph, not at the bottom
		graph := RenderBrailleSparklineWithOptions(smoothedHistory, graphWidth, m.cardGraphRows(), ColorGraph, LatencyColor, true)
		graphLines := strings.Split(graph, "\n")
		for _, gl := range graphLines {
			lines = append(lines, renderCardLine(gl, lineWidth))
		}
	}

	return lines
}

// renderCardNetworkLine renders network throughput rates in a single line.
func (m Model) renderCardNetworkLine(host string, lineWidth int) string {
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine
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
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}

	return label + padding + rightContent
}

// renderCardDiskLine renders root filesystem usage in a single line.
// Returns empty string when no disk data is available.
func (m Model) renderCardDiskLine(disk DiskMetrics, lineWidth int) string {
	if disk.TotalBytes == 0 {
		return ""
	}
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	label := LabelStyle.Render("DISK")
	pctText := thresholdStyle(disk.Percent, diskThresholds).Render(fmt.Sprintf("%5.1f%%", disk.Percent))
	usageText := LabelStyle.Render(formatBytes(disk.UsedBytes) + "/" + formatBytes(disk.TotalBytes))

	// Right-align the usage
	rightContent := pctText + " " + usageText
	rightWidth := lipgloss.Width(rightContent)
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}

	return label + padding + rightContent
}

// renderCardTopProcesses renders the top processes by CPU, one per line. The
// count comes from cardProcessCount (tall terminals show more). Only the first
// line carries the "TOP" label; the rest are indented under it.
func (m Model) renderCardTopProcesses(procs []ProcessInfo, maxWidth int) []string {
	count := m.cardProcessCount()
	if count > len(procs) {
		count = len(procs)
	}

	lines := make([]string, 0, count)
	for i := 0; i < count; i++ {
		lines = append(lines, m.renderCardProcessLine(procs[i], i, maxWidth))
	}
	return lines
}

// renderCardProcessLine renders one process row. rank 0 gets the "TOP" label;
// later ranks are blank-labelled so they read as a continuation of the list.
func (m Model) renderCardProcessLine(proc ProcessInfo, rank, maxWidth int) string {
	contentWidth := maxWidth - 2 // Account for 1-space padding each side in renderCardLine

	labelText := "TOP"
	if rank > 0 {
		labelText = "   "
	}
	label := LabelStyle.Render(labelText)

	// Extract command name (first path component or word)
	cmd := proc.Command
	if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
		cmd = cmd[idx+1:]
	}
	if idx := strings.Index(cmd, " "); idx > 0 {
		cmd = cmd[:idx]
	}

	// Format percentage with color
	pctColor := MetricColorWithThresholds(proc.CPU, m.thresholds.CPU.Warning, m.thresholds.CPU.Critical)
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
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}

	return label + padding + rightContent
}

// RenderLoadAvg renders the system load average with time period labels.
func RenderLoadAvg(loadAvg [3]float64) string {
	label := LabelStyle.Render("Load")
	values := ValueStyle.Render(fmt.Sprintf("%.2f (1m) · %.2f (5m) · %.2f (15m)", loadAvg[0], loadAvg[1], loadAvg[2]))
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

	// width IS the content area (lipgloss Width sets content area, borders add to total)
	innerWidth := width
	contentWidth := innerWidth - 2 // Space for actual content (minus 1-space padding each side)
	var lines []string

	// Host name with status indicator and SSH alias on the right
	hostLine := m.renderHostLineWithSSH(host, status, contentWidth)
	lines = append(lines, renderCardLine(hostLine, innerWidth))

	// Show command line for running hosts
	if status == StatusRunningState {
		if cmdLine := m.renderCommandLine(host, contentWidth); cmdLine != "" {
			lines = append(lines, renderCardLine(cmdLine, innerWidth))
		}
	}

	// If no metrics available, show status-appropriate placeholder with error details
	if metrics == nil {
		lines = append(lines, renderCardDivider(innerWidth))
		switch status {
		case StatusConnectingState:
			// Check for retry state (Attempts tracks actual failures)
			connState := m.connState[host]
			if connState != nil && connState.Attempts >= 1 {
				// Check if in backoff
				if !connState.NextRetry.IsZero() && time.Now().Before(connState.NextRetry) {
					secsUntil := int(time.Until(connState.NextRetry).Seconds())
					if secsUntil < 1 {
						secsUntil = 1
					}
					offlineText := fmt.Sprintf("Offline · retry in %ds", secsUntil)
					lines = append(lines, renderCardLine(StatusUnreachableStyle.Render("  "+offlineText), innerWidth))
				} else {
					failedText := fmt.Sprintf("Connecting · %d failed", connState.Attempts)
					lines = append(lines, renderCardLine(StatusConnectingStyle.Render("  "+failedText), innerWidth))
				}
				lines = append(lines, renderCardDivider(innerWidth))
				if connState.LastError != "" {
					errText := genZErrorMessage(connState.LastError)
					lines = append(lines, renderCardLine(LabelStyle.Render("  "+errText), innerWidth))
				} else {
					lines = append(lines, renderCardLine(LabelStyle.Render("  connection failed"), innerWidth))
				}
			} else {
				// First attempt
				lines = append(lines, renderCardLine(StatusConnectingStyle.Render("  "+m.ConnectingText()), innerWidth))
				lines = append(lines, renderCardDivider(innerWidth))
				lines = append(lines, renderCardLine(LabelStyle.Render("  "+m.ConnectingSubtext()), innerWidth))
			}
		case StatusUnreachableState:
			lines = append(lines, renderCardLine(StatusUnreachableStyle.Render("  Unreachable"), innerWidth))

			// Show error details and suggestion
			if errMsg, ok := m.errors[host]; ok && errMsg != "" {
				core, suggestion := parseErrorParts(errMsg)

				// Show core error
				if core != "" {
					errDisplay := truncateWithEllipsis(core, contentWidth-2)
					lines = append(lines, renderCardLine(LabelStyle.Render("  "+errDisplay), innerWidth))
				}

				// Add divider before suggestion
				lines = append(lines, renderCardDivider(innerWidth))

				// Show suggestion (compact: single line with truncation)
				if suggestion != "" {
					suggDisplay := truncateWithEllipsis(suggestion, contentWidth-2)
					lines = append(lines, renderCardLine(LabelStyle.Render("  "+suggDisplay), innerWidth))
				} else {
					lines = append(lines, renderCardLine(LabelStyle.Render("  Host may be offline or unreachable"), innerWidth))
				}
			} else {
				lines = append(lines, renderCardDivider(innerWidth))
				lines = append(lines, renderCardLine(LabelStyle.Render("  No connection details"), innerWidth))
			}
		}
	} else {
		// Expensive metric body: cached per host, invalidated on new results
		lines = append(lines, m.cachedCardBody(host, func() string {
			return m.renderCompactCardBody(host, metrics, innerWidth)
		}))
	}

	content := strings.Join(lines, "\n")
	return style.Render(content)
}

// renderCompactCardBody renders the metric sections of a compact card:
// divider plus single-row CPU/GPU/latency/RAM sparklines.
func (m Model) renderCompactCardBody(host string, metrics *HostMetrics, innerWidth int) string {
	var lines []string

	lines = append(lines, renderCardDivider(innerWidth))

	// CPU with single-row sparkline
	cpuLines := m.renderCompactCPUSection(host, metrics.CPU, innerWidth)
	lines = append(lines, cpuLines...)

	// GPU with single-row sparkline (if available)
	if metrics.GPU != nil {
		lines = append(lines, renderCardDivider(innerWidth))
		gpuLines := m.renderCompactGPUSection(host, metrics.GPU, innerWidth)
		lines = append(lines, gpuLines...)
	}

	// Latency with single-row sparkline (if available)
	latencyLines := m.renderCompactLatencySection(host, innerWidth)
	if len(latencyLines) > 0 {
		lines = append(lines, renderCardDivider(innerWidth))
		lines = append(lines, latencyLines...)
	}

	// RAM with single-row sparkline
	lines = append(lines, renderCardDivider(innerWidth))
	ramLines := m.renderCompactRAMSection(host, metrics.RAM, innerWidth)
	lines = append(lines, ramLines...)

	return strings.Join(lines, "\n")
}

// renderCompactCPUSection renders CPU with a single-row braille graph for compact mode.
func (m Model) renderCompactCPUSection(host string, cpu CPUMetrics, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	label := LabelStyle.Render("CPU")
	pctText := thresholdStyle(cpu.Percent, m.thresholds.CPU).Render(fmt.Sprintf("%5.1f%%", cpu.Percent))
	if !cpu.Valid() {
		pctText = renderCPUWarmingUp()
	}

	// Right-aligned percentage
	rightWidth := lipgloss.Width(pctText)
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + pctText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Single-row braille graph
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	cpuHistory := m.history.GetCPUHistory(host, DefaultHistorySize)
	if len(cpuHistory) > 0 {
		graph := RenderBrailleSparklineWithColorFunc(cpuHistory, graphWidth, 1, ColorGraph, thresholdColorFunc(m.thresholds.CPU))
		lines = append(lines, renderCardLine(graph, lineWidth))
	} else {
		bar := RenderGradientBarWithColorFunc(graphWidth, cpu.Percent, thresholdColorFunc(m.thresholds.CPU))
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCompactRAMSection renders RAM with a single-row braille graph for compact mode.
func (m Model) renderCompactRAMSection(host string, ram RAMMetrics, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	var percent float64
	if ram.TotalBytes > 0 {
		percent = float64(ram.UsedBytes) / float64(ram.TotalBytes) * 100
	}

	label := LabelStyle.Render("RAM")
	pctText := thresholdStyle(percent, m.thresholds.RAM).Render(fmt.Sprintf("%5.1f%%", percent))

	// Right-aligned percentage
	rightWidth := lipgloss.Width(pctText)
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + pctText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Single-row braille graph
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	ramHistory := m.history.GetRAMHistory(host, DefaultHistorySize)
	if len(ramHistory) > 0 {
		graph := RenderBrailleSparklineWithColorFunc(ramHistory, graphWidth, 1, ColorGraph, thresholdColorFunc(m.thresholds.RAM))
		lines = append(lines, renderCardLine(graph, lineWidth))
	} else {
		bar := RenderGradientBarWithColorFunc(graphWidth, percent, thresholdColorFunc(m.thresholds.RAM))
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCompactGPUSection renders GPU with a single-row braille graph for compact mode.
func (m Model) renderCompactGPUSection(host string, gpu *GPUMetrics, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	label := LabelStyle.Render("GPU")
	pctText := thresholdStyle(gpu.Percent, m.thresholds.GPU).Render(fmt.Sprintf("%5.1f%%", gpu.Percent))

	// Right-aligned percentage (no temp in compact mode for space)
	rightWidth := lipgloss.Width(pctText)
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + pctText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Single-row braille graph
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	gpuHistory := m.history.GetGPUHistory(host, DefaultHistorySize)
	if len(gpuHistory) > 0 {
		graph := RenderBrailleSparklineWithColorFunc(gpuHistory, graphWidth, 1, ColorGraph, thresholdColorFunc(m.thresholds.GPU))
		lines = append(lines, renderCardLine(graph, lineWidth))
	} else {
		bar := RenderGradientBarWithColorFunc(graphWidth, gpu.Percent, thresholdColorFunc(m.thresholds.GPU))
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderCompactLatencySection renders latency with a single-row braille graph for compact mode.
func (m Model) renderCompactLatencySection(host string, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	// Get current latency
	latencyDur, ok := m.latency[host]
	if !ok || latencyDur == 0 {
		return nil // No latency data yet
	}

	latencyMs := float64(latencyDur.Milliseconds())

	label := LabelStyle.Render("LAT")
	latencyText := LatencyStyle(latencyMs).Render(fmt.Sprintf("%3.0fms", latencyMs))

	// Right-aligned latency value
	rightWidth := lipgloss.Width(latencyText)
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + latencyText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Single-row braille graph
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	latencyHistory := m.history.GetLatencyHistory(host, DefaultHistorySize)
	if len(latencyHistory) > 0 {
		// Apply moving average to smooth noise while preserving trends
		smoothedHistory := SmoothWithMovingAverage(latencyHistory, 5)
		// Use per-column coloring based on latency thresholds
		// forceZeroMin=true so high latency shows high on the graph
		graph := RenderBrailleSparklineWithOptions(smoothedHistory, graphWidth, 1, ColorGraph, LatencyColor, true)
		lines = append(lines, renderCardLine(graph, lineWidth))
	}

	return lines
}

// renderMinimalCPUSection renders CPU with a single-row braille graph for minimal mode.
func (m Model) renderMinimalCPUSection(host string, cpu CPUMetrics, lineWidth int) []string {
	var lines []string
	contentWidth := lineWidth - 2 // Account for 1-space padding each side in renderCardLine

	label := LabelStyle.Render("CPU:")
	pctText := thresholdStyle(cpu.Percent, m.thresholds.CPU).Render(fmt.Sprintf("%3.0f%%", cpu.Percent))
	if !cpu.Valid() {
		pctText = renderCPUWarmingUp()
	}

	// Right-aligned percentage
	rightWidth := lipgloss.Width(pctText)
	padding := ""
	if contentWidth > lipgloss.Width(label)+rightWidth {
		padding = strings.Repeat(" ", contentWidth-lipgloss.Width(label)-rightWidth)
	}
	headerLine := label + padding + pctText
	lines = append(lines, renderCardLine(headerLine, lineWidth))

	// Single-row braille graph
	graphWidth := contentWidth
	if graphWidth < cardMinBarWidth {
		graphWidth = cardMinBarWidth
	}

	cpuHistory := m.history.GetCPUHistory(host, DefaultHistorySize)
	if len(cpuHistory) > 0 {
		graph := RenderBrailleSparklineWithColorFunc(cpuHistory, graphWidth, 1, ColorGraph, thresholdColorFunc(m.thresholds.CPU))
		lines = append(lines, renderCardLine(graph, lineWidth))
	} else {
		bar := RenderGradientBarWithColorFunc(graphWidth, cpu.Percent, thresholdColorFunc(m.thresholds.CPU))
		lines = append(lines, renderCardLine(bar, lineWidth))
	}

	return lines
}

// renderMinimalCard renders a minimal card for terminals < 80 columns.
// Shows CPU sparkline graph and text metrics for RAM.
func (m Model) renderMinimalCard(host string, width int, selected bool) string {
	metrics := m.metrics[host]
	status := m.status[host]

	// Choose card style based on selection
	style := CardStyle.Width(width)
	if selected {
		style = CardSelectedStyle.Width(width)
	}

	// width IS the content area (lipgloss Width sets content area, borders add to total)
	innerWidth := width
	contentWidth := innerWidth - 2 // Space for actual content (minus 1-space padding each side)
	var lines []string

	// Host name with status indicator (abbreviated if necessary)
	hostLine := m.renderMinimalHostLine(host, status, contentWidth)
	lines = append(lines, renderCardLine(hostLine, innerWidth))

	// If no metrics available, show status-appropriate placeholder
	if metrics == nil {
		var placeholder string
		switch status {
		case StatusConnectingState:
			placeholder = m.ConnectingText()
		case StatusUnreachableState:
			placeholder = "Offline"
		default:
			placeholder = "..."
		}
		lines = append(lines, renderCardLine(LabelStyle.Render(placeholder), innerWidth))
	} else {
		// Expensive metric body: cached per host, invalidated on new results
		lines = append(lines, m.cachedCardBody(host, func() string {
			return m.renderMinimalCardBody(host, metrics, innerWidth, contentWidth)
		}))
	}

	content := strings.Join(lines, "\n")
	return style.Render(content)
}

// renderMinimalCardBody renders the metric sections of a minimal card:
// CPU sparkline plus a text metrics line.
func (m Model) renderMinimalCardBody(host string, metrics *HostMetrics, innerWidth, contentWidth int) string {
	var lines []string

	lines = append(lines, renderCardDivider(innerWidth))

	// CPU with single-row sparkline
	cpuLines := m.renderMinimalCPUSection(host, metrics.CPU, innerWidth)
	lines = append(lines, cpuLines...)

	lines = append(lines, renderCardDivider(innerWidth))

	// RAM as text only (keep it minimal)
	metricsLine := m.renderMinimalMetricsLine(metrics, contentWidth)
	lines = append(lines, renderCardLine(metricsLine, innerWidth))

	return strings.Join(lines, "\n")
}

// renderMinimalHostLine renders the hostname, truncating if necessary.
// Minimal mode doesn't show status text to save space.
func (m Model) renderMinimalHostLine(host string, status HostStatus, maxWidth int) string {
	var indicator string
	var indicatorStyle lipgloss.Style

	switch status {
	case StatusConnectingState:
		indicator = m.ConnectingSpinner() // Animated spinner
		indicatorStyle = StatusConnectingStyle
	case StatusIdleState:
		indicator = StatusIdle
		indicatorStyle = StatusIdleStyle
	case StatusRunningState:
		indicator, indicatorStyle = m.RunningSpinner() // Animated braille spinner
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

// renderMinimalMetricsLine renders a single line with CPU, RAM, and optionally GPU percentages.
func (m Model) renderMinimalMetricsLine(metrics *HostMetrics, width int) string {
	cpuPct := metrics.CPU.Percent

	var ramPct float64
	if metrics.RAM.TotalBytes > 0 {
		ramPct = float64(metrics.RAM.UsedBytes) / float64(metrics.RAM.TotalBytes) * 100
	}

	cpuText := thresholdStyle(cpuPct, m.thresholds.CPU).Render(fmt.Sprintf("%.0f%%", cpuPct))
	ramText := thresholdStyle(ramPct, m.thresholds.RAM).Render(fmt.Sprintf("%.0f%%", ramPct))

	// Include GPU if available
	hasGPU := metrics.GPU != nil
	var gpuText string
	if hasGPU {
		gpuText = thresholdStyle(metrics.GPU.Percent, m.thresholds.GPU).Render(fmt.Sprintf("%.0f%%", metrics.GPU.Percent))
	}

	// Choose format based on available width and GPU presence
	if hasGPU {
		if width >= 42 {
			// Format: "CPU: 45%  RAM: 67%  GPU: 89%"
			return fmt.Sprintf("%s %s  %s %s  %s %s",
				LabelStyle.Render("CPU:"), cpuText,
				LabelStyle.Render("RAM:"), ramText,
				LabelStyle.Render("GPU:"), gpuText)
		}
		// Super compact format with GPU
		return fmt.Sprintf("C:%s R:%s G:%s", cpuText, ramText, gpuText)
	}

	// No GPU - original format
	if width >= 30 {
		return fmt.Sprintf("%s %s  %s %s",
			LabelStyle.Render("CPU:"), cpuText,
			LabelStyle.Render("RAM:"), ramText)
	}

	// Super compact format
	return fmt.Sprintf("C:%s R:%s", cpuText, ramText)
}
