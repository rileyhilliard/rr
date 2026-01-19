package monitor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderDashboard renders the complete dashboard view.
func (m Model) renderDashboard() string {
	// If in detail view mode, render the expanded host view with viewport
	if m.viewMode == ViewDetail {
		content := m.renderDetailViewWithViewport()
		// If help is showing, overlay the help box
		if m.showHelp {
			return m.renderHelpOverlay(content)
		}
		return content
	}

	var b strings.Builder

	// Render header (always shown, but may be compact)
	header := m.renderHeader()
	b.WriteString(header)
	b.WriteString("\n\n")

	// Render host cards based on layout mode
	cards := m.renderHostCards()
	b.WriteString(cards)

	// Render footer only if terminal is tall enough
	if m.ShowFooter() {
		footer := m.renderFooter()
		b.WriteString("\n")
		b.WriteString(footer)
	}

	content := b.String()

	// If help is showing, overlay the help box
	if m.showHelp {
		return m.renderHelpOverlay(content)
	}

	return content
}

// renderHeader renders the dashboard header with summary stats.
func (m Model) renderHeader() string {
	totalHosts := len(m.hosts)
	onlineHosts := m.OnlineCount()
	lastUpdate := m.SecondsSinceUpdate()

	var updateText string
	switch lastUpdate {
	case 0:
		updateText = "just now"
	case 1:
		updateText = "1s ago"
	default:
		updateText = fmt.Sprintf("%ds ago", lastUpdate)
	}

	title := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true).
		Render("rr monitor")

	// Sort indicator: default has no arrow, name is ascending, others descending
	var sortArrow string
	switch m.sortOrder {
	case SortByDefault:
		sortArrow = "" // No arrow for default (online first, config order)
	case SortByName:
		sortArrow = " \u2191" // up arrow for ascending (alphabetical)
	default:
		sortArrow = " \u2193" // down arrow for descending
	}
	sortIndicator := fmt.Sprintf(" sorted by: %s%s", m.sortOrder.String(), sortArrow)

	// Adjust stats display based on layout mode
	var stats string
	layout := m.LayoutMode()
	switch layout {
	case LayoutMinimal:
		// Most compact: just online count
		stats = lipgloss.NewStyle().
			Foreground(ColorTextSecondary).
			Render(fmt.Sprintf(" %d/%d", onlineHosts, totalHosts))
	case LayoutCompact:
		// Abbreviated labels with sort indicator
		stats = lipgloss.NewStyle().
			Foreground(ColorTextSecondary).
			Render(fmt.Sprintf(" | %d/%d online | %s |%s", onlineHosts, totalHosts, updateText, sortIndicator))
	default:
		// Full stats for standard and wide
		stats = lipgloss.NewStyle().
			Foreground(ColorTextSecondary).
			Render(fmt.Sprintf(" | %d hosts | %d online | last update %s |%s", totalHosts, onlineHosts, updateText, sortIndicator))
	}

	return HeaderStyle.Render(title + stats)
}

// renderHostCards renders the grid of host cards.
func (m Model) renderHostCards() string {
	if len(m.hosts) == 0 {
		return LabelStyle.Render("No hosts configured")
	}

	// Calculate card dimensions based on terminal width
	cardWidth := m.calculateCardWidth()
	layout := m.LayoutMode()

	var cards []string
	for i, host := range m.hosts {
		isSelected := i == m.selected
		var card string

		// Use different card renderers based on layout mode
		switch layout {
		case LayoutMinimal:
			card = m.renderMinimalCard(host, cardWidth, isSelected)
		case LayoutCompact:
			card = m.renderCompactCard(host, cardWidth, isSelected)
		default:
			card = m.renderCard(host, cardWidth, isSelected)
		}

		cards = append(cards, card)
	}

	// Arrange cards in a grid
	return m.layoutCards(cards, cardWidth)
}

// calculateCardWidth determines the optimal card width based on terminal width and layout mode.
func (m Model) calculateCardWidth() int {
	if m.width == 0 {
		return 40 // Default width
	}

	layout := m.LayoutMode()

	// Card overhead per card: borders (2) + marginRight (1) = 3
	// For N cards: N * (contentWidth + 3) = availableWidth
	// So: contentWidth = availableWidth / N - 3
	const perCardOverhead = 3

	switch layout {
	case LayoutMinimal:
		// Single column, use full width minus overhead
		return m.width - perCardOverhead

	case LayoutCompact:
		// Single column with slight margin
		return m.width - perCardOverhead - 1

	case LayoutStandard:
		// Try to fit 2 cards per row
		cardWidth := m.width/2 - perCardOverhead
		if cardWidth < 40 {
			// Fall back to single column if cards would be too narrow
			return m.width - perCardOverhead
		}
		return cardWidth

	case LayoutWide:
		// Fit 2 cards per row
		cardWidth := m.width/2 - perCardOverhead
		if cardWidth > 70 {
			// Cap card width to prevent overly wide cards
			cardWidth = 70
		}
		return cardWidth

	default:
		return 40
	}
}

// layoutCards arranges cards in rows based on terminal width and layout mode.
func (m Model) layoutCards(cards []string, cardWidth int) string {
	if len(cards) == 0 {
		return ""
	}

	// Calculate cards per row based on layout mode
	cardsPerRow := m.cardsPerRow(cardWidth)

	var rows []string
	for i := 0; i < len(cards); i += cardsPerRow {
		end := i + cardsPerRow
		if end > len(cards) {
			end = len(cards)
		}

		rowCards := cards[i:end]
		row := lipgloss.JoinHorizontal(lipgloss.Top, rowCards...)
		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// cardsPerRow returns the number of cards to display per row based on layout mode.
func (m Model) cardsPerRow(cardWidth int) int {
	layout := m.LayoutMode()

	switch layout {
	case LayoutMinimal, LayoutCompact:
		// Always single column for narrow terminals
		return 1

	case LayoutStandard, LayoutWide:
		// Calculate how many cards fit
		if m.width <= 0 {
			return 1
		}
		// Account for card borders (2) and marginRight (1)
		effectiveCardWidth := cardWidth + 3
		perRow := m.width / effectiveCardWidth
		if perRow < 1 {
			return 1
		}
		// Cap at 2 for readability
		if perRow > 2 {
			return 2
		}
		return perRow

	default:
		return 1
	}
}

// renderFooter renders the keyboard help footer.
func (m Model) renderFooter() string {
	layout := m.LayoutMode()

	var hints []string
	switch layout {
	case LayoutMinimal:
		// Most compact: minimal hints
		hints = []string{"q quit", "? help"}
	case LayoutCompact:
		// Compact hints
		hints = []string{"q quit", "r refresh", "s sort", "? help"}
	default:
		// Full hints for wider terminals
		hints = []string{
			"q quit",
			"r refresh",
			"s sort",
			"\u2191\u2193 select",
			"Enter expand",
			"? help",
		}
	}

	return FooterStyle.Render(strings.Join(hints, " | "))
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}

// FormatRate formats a bytes-per-second rate as a human-readable string.
func FormatRate(bytesPerSecond float64) string {
	if bytesPerSecond < 1024 {
		return fmt.Sprintf("%.0f B/s", bytesPerSecond)
	} else if bytesPerSecond < 1024*1024 {
		return fmt.Sprintf("%.1f KB/s", bytesPerSecond/1024)
	} else if bytesPerSecond < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB/s", bytesPerSecond/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB/s", bytesPerSecond/(1024*1024*1024))
}
