package dashboard

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/ui"
)

// Layout breakpoints for responsive design
const (
	BreakpointCompact  = 80
	BreakpointStandard = 120
)

// Height breakpoints
const (
	HeightMinimal = 20
)

// LayoutMode represents the responsive layout mode based on terminal size.
type LayoutMode int

const (
	LayoutMinimal LayoutMode = iota
	LayoutCompact
	LayoutStandard
)

// Styles for the dashboard
var (
	// Task row styles
	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a1a2e")).
			Padding(0, 1)

	unselectedStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// Status-specific styles
	pendingStyle = lipgloss.NewStyle().
			Foreground(ui.ColorMuted)

	syncingStyle = lipgloss.NewStyle().
			Foreground(ui.ColorInfo)

	runningStyle = lipgloss.NewStyle().
			Foreground(ui.ColorNeonPink)

	passedStyle = lipgloss.NewStyle().
			Foreground(ui.ColorSuccess)

	failedStyle = lipgloss.NewStyle().
			Foreground(ui.ColorError)

	// Text styles
	hostStyle = lipgloss.NewStyle().
			Foreground(ui.ColorMuted)

	durationStyle = lipgloss.NewStyle().
			Foreground(ui.ColorMuted)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ui.ColorPrimary)

	footerStyle = lipgloss.NewStyle().
			Foreground(ui.ColorMuted)

	warningStyle = lipgloss.NewStyle().
			Foreground(ui.ColorWarning)

	summaryPassedStyle = lipgloss.NewStyle().
				Foreground(ui.ColorSuccess).
				Bold(true)

	summaryFailedStyle = lipgloss.NewStyle().
				Foreground(ui.ColorError).
				Bold(true)
)

// GetLayoutMode returns the layout mode based on terminal width.
func GetLayoutMode(width int) LayoutMode {
	switch {
	case width >= BreakpointStandard:
		return LayoutStandard
	case width >= BreakpointCompact:
		return LayoutCompact
	default:
		return LayoutMinimal
	}
}

// ShowFooter returns true if the terminal is tall enough for the footer.
func ShowFooter(height int) bool {
	return height >= HeightMinimal
}
