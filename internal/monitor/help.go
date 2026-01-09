package monitor

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
)

// Help overlay styles
var (
	helpBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorAccent).
			Background(ColorSurfaceBg).
			Padding(1, 2)

	helpTitleStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			MarginBottom(1)
)

// renderHelpOverlay renders a centered help box with keyboard shortcuts.
// Uses the Bubbles help component for consistent styling.
func (m Model) renderHelpOverlay(_ string) string {
	// Create help model and configure styles
	h := help.New()
	h.ShowAll = true

	// Style the help view to match our theme
	h.Styles.ShortKey = lipgloss.NewStyle().
		Foreground(ColorTextPrimary).
		Bold(true)
	h.Styles.ShortDesc = lipgloss.NewStyle().
		Foreground(ColorTextSecondary)
	h.Styles.ShortSeparator = lipgloss.NewStyle().
		Foreground(ColorTextMuted)
	h.Styles.FullKey = lipgloss.NewStyle().
		Foreground(ColorTextPrimary).
		Bold(true)
	h.Styles.FullDesc = lipgloss.NewStyle().
		Foreground(ColorTextSecondary)
	h.Styles.FullSeparator = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	// Render the help content using the keybinding map
	helpContent := helpTitleStyle.Render("Keyboard Shortcuts") + "\n\n"
	helpContent += h.View(keys)
	helpContent += "\n\n" + LabelStyle.Render("Press ? to close")

	helpBox := helpBoxStyle.Render(helpContent)

	// Center the help box using lipgloss.Place
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		helpBox,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(ColorDarkBg),
	)
}
