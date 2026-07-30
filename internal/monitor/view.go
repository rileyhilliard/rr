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

	// Render host cards via viewport for scrolling support
	if m.viewportReady {
		b.WriteString(m.listViewport.View())
	} else {
		// Fallback: render cards directly (no scrolling)
		cards := m.renderHostCards()
		b.WriteString(cards)
	}

	// Render footer only if terminal is tall enough
	if m.ShowFooter() {
		footer := m.renderListFooterWithScroll()
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
	return m.layoutCards(cards)
}

// Grid sizing for the multi-column layouts.
const (
	// perCardOverhead is the non-content width each card occupies:
	// borders (2) + marginRight (1).
	perCardOverhead = 3
	// minCardWidth is the narrowest content width a full card stays readable at.
	// Columns are only added while every card can hold at least this much.
	minCardWidth = 55
	// maxCardColumns caps the grid so very wide terminals don't shrink cards
	// into an unreadable wall of columns.
	maxCardColumns = 4
)

// cardColumns returns how many card columns fit at the current width, for the
// multi-column (Standard/Wide) layouts. Columns are added only while each card
// keeps at least minCardWidth of content, and the count is capped at
// maxCardColumns.
func (m Model) cardColumns() int {
	if m.width <= 0 {
		return 1
	}
	cols := m.width / (minCardWidth + perCardOverhead)
	if cols < 1 {
		return 1
	}
	if cols > maxCardColumns {
		return maxCardColumns
	}
	return cols
}

// calculateCardWidth determines the optimal card width based on terminal width and layout mode.
func (m Model) calculateCardWidth() int {
	if m.width == 0 {
		return 40 // Default width
	}

	layout := m.LayoutMode()

	switch layout {
	case LayoutMinimal:
		// Single column, use full width minus overhead
		return m.width - perCardOverhead

	case LayoutCompact:
		// Single column with slight margin
		return m.width - perCardOverhead - 1

	case LayoutStandard, LayoutWide:
		// Divide the available width evenly across the fitted columns
		// (contentWidth = width/N - overhead)
		return m.width/m.cardColumns() - perCardOverhead

	default:
		return 40
	}
}

// layoutCards arranges cards in rows based on terminal width and layout mode.
func (m Model) layoutCards(cards []string) string {
	if len(cards) == 0 {
		return ""
	}

	// Calculate cards per row based on layout mode
	cardsPerRow := m.cardsPerRow()

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
func (m Model) cardsPerRow() int {
	switch m.LayoutMode() {
	case LayoutMinimal, LayoutCompact:
		// Always single column for narrow terminals
		return 1

	case LayoutStandard, LayoutWide:
		return m.cardColumns()

	default:
		return 1
	}
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

// generateListContent generates the scrollable content for the list view (just the cards).
func (m Model) generateListContent() string {
	return m.renderHostCards()
}

// updateListViewportContent updates the list viewport with the current card content.
// This must be called when content changes (metrics updates, sorting, etc.).
func (m *Model) updateListViewportContent() {
	if m.viewportReady {
		content := m.generateListContent()
		m.listViewport.SetContent(content)
	}
}

// renderListFooterWithScroll renders the footer with scroll position indicator for list view.
func (m Model) renderListFooterWithScroll() string {
	layout := m.LayoutMode()

	var hints []string

	// Add scroll indicator if viewport is ready and content is scrollable
	isScrollable := m.viewportReady && m.listViewport.TotalLineCount() > m.listViewport.Height
	if isScrollable {
		scrollPercent := m.listViewport.ScrollPercent() * 100
		hints = append(hints, fmt.Sprintf("%.0f%%", scrollPercent))
	}

	switch layout {
	case LayoutMinimal:
		if isScrollable {
			hints = append(hints, "pgup/dn scroll")
		}
		hints = append(hints, "q quit", "? help")
	case LayoutCompact:
		if isScrollable {
			hints = append(hints, "pgup/dn scroll")
		}
		hints = append(hints, "q quit", "r refresh", "s sort", "? help")
	default:
		if isScrollable {
			hints = append(hints, "pgup/dn scroll")
		}
		hints = append(hints,
			"q quit",
			"r refresh",
			"s sort",
			"\u2191\u2193 select",
			"Enter expand",
			"? help",
		)
	}

	return FooterStyle.Render(strings.Join(hints, " | "))
}
