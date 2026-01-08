package monitor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HelpBinding represents a single keyboard shortcut entry.
type HelpBinding struct {
	Key  string
	Desc string
}

// helpBindings defines all keyboard shortcuts shown in the help overlay.
var helpBindings = []HelpBinding{
	{Key: "q / Ctrl+C", Desc: "Quit"},
	{Key: "r", Desc: "Force refresh"},
	{Key: "s", Desc: "Cycle sort order"},
	{Key: "up / k", Desc: "Select previous host"},
	{Key: "down / j", Desc: "Select next host"},
	{Key: "Home", Desc: "Select first host"},
	{Key: "End", Desc: "Select last host"},
	{Key: "Enter", Desc: "Expand selected host"},
	{Key: "Esc", Desc: "Collapse / close"},
	{Key: "?", Desc: "Toggle this help"},
}

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

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorTextPrimary).
			Bold(true).
			Width(14)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(ColorTextSecondary)
)

// renderHelpOverlay renders a centered help box with keyboard shortcuts.
// The baseContent parameter is preserved for future overlay blending.
func (m Model) renderHelpOverlay(_ string) string {
	// Build help content
	var lines []string
	lines = append(lines, helpTitleStyle.Render("Keyboard Shortcuts"))
	lines = append(lines, "")

	for _, binding := range helpBindings {
		line := helpKeyStyle.Render(binding.Key) + helpDescStyle.Render(binding.Desc)
		lines = append(lines, line)
	}

	lines = append(lines, "")
	lines = append(lines, LabelStyle.Render("Press ? to close"))

	helpContent := strings.Join(lines, "\n")
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
