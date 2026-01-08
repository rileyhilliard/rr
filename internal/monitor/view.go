package monitor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderDashboard renders the complete dashboard view.
func (m Model) renderDashboard() string {
	var b strings.Builder

	// Render header
	header := m.renderHeader()
	b.WriteString(header)
	b.WriteString("\n\n")

	// Render host cards
	cards := m.renderHostCards()
	b.WriteString(cards)

	// Render footer
	footer := m.renderFooter()
	b.WriteString("\n")
	b.WriteString(footer)

	return b.String()
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

	stats := lipgloss.NewStyle().
		Foreground(ColorTextSecondary).
		Render(fmt.Sprintf(" | %d hosts | %d online | last update %s", totalHosts, onlineHosts, updateText))

	return HeaderStyle.Render(title + stats)
}

// renderHostCards renders the grid of host cards.
func (m Model) renderHostCards() string {
	if len(m.hosts) == 0 {
		return LabelStyle.Render("No hosts configured")
	}

	// Calculate card dimensions based on terminal width
	cardWidth := m.calculateCardWidth()

	var cards []string
	for i, host := range m.hosts {
		isSelected := i == m.selected
		card := m.renderCard(host, cardWidth, isSelected)
		cards = append(cards, card)
	}

	// Arrange cards in a grid
	return m.layoutCards(cards, cardWidth)
}

// calculateCardWidth determines the optimal card width based on terminal width.
func (m Model) calculateCardWidth() int {
	if m.width == 0 {
		return 40 // Default width
	}

	// Try to fit 2-3 cards per row with some margin
	if m.width >= 120 {
		return 38 // 3 cards with margins
	} else if m.width >= 80 {
		return 38 // 2 cards with margins
	}
	return m.width - 4 // Single column with margin
}

// layoutCards arranges cards in rows based on terminal width.
func (m Model) layoutCards(cards []string, cardWidth int) string {
	if len(cards) == 0 {
		return ""
	}

	// Calculate cards per row
	cardsPerRow := 1
	if m.width > 0 {
		// Account for card margins and borders
		effectiveCardWidth := cardWidth + 3 // margin + border
		cardsPerRow = m.width / effectiveCardWidth
		if cardsPerRow < 1 {
			cardsPerRow = 1
		}
	}

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

// renderFooter renders the keyboard help footer.
func (m Model) renderFooter() string {
	hints := []string{
		"q quit",
		"r refresh",
		"\u2191\u2193 select",
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
