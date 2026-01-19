package monitor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Dashboard color palette - Gen Z Electric Synthwave
const (
	// Background colors (glassmorphism-inspired)
	ColorDarkBg    = lipgloss.Color("#0A0A0F") // Deep void
	ColorSurfaceBg = lipgloss.Color("#12121A") // Dark surface
	ColorBorder    = lipgloss.Color("#2A2A4A") // Glass border (purple tint)

	// Semantic colors for metrics - neon style
	ColorHealthy  = lipgloss.Color("#39FF14") // Neon green
	ColorWarning  = lipgloss.Color("#FFAA00") // Electric amber
	ColorCritical = lipgloss.Color("#FF0055") // Hot red-pink

	// Text colors
	ColorTextPrimary   = lipgloss.Color("#FFFFFF") // Pure white
	ColorTextSecondary = lipgloss.Color("#B4B4D0") // Lavender gray
	ColorTextMuted     = lipgloss.Color("#6B6B8D") // Purple-gray

	// Accent colors - neon pink primary, cyan secondary
	ColorAccent    = lipgloss.Color("#FF2E97") // Neon pink
	ColorAccentDim = lipgloss.Color("#BF40FF") // Neon purple

	// Graph colors
	ColorGraph = lipgloss.Color("#00FFFF") // Neon cyan
)

// Thresholds for metric severity levels
const (
	WarningThreshold  = 70.0
	CriticalThreshold = 90.0
)

// Base styles for the dashboard
var (
	// Container styles
	DashboardStyle = lipgloss.NewStyle().
			Background(ColorDarkBg)

	HeaderStyle = lipgloss.NewStyle().
			Foreground(ColorTextPrimary).
			Background(ColorSurfaceBg).
			Bold(true).
			Padding(0, 1)

	FooterStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Padding(0, 1)

	// Card styles - no background set here, each line handles its own
	CardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1).
			MarginRight(1).
			MarginBottom(1)

	CardSelectedStyle = CardStyle.
				BorderForeground(ColorAccent)

	// Text styles
	HostNameStyle = lipgloss.NewStyle().
			Foreground(ColorTextPrimary).
			Bold(true)

	LabelStyle = lipgloss.NewStyle().
			Foreground(ColorTextSecondary)

	ValueStyle = lipgloss.NewStyle().
			Foreground(ColorTextPrimary)

	// Status indicator styles
	StatusConnectingStyle = lipgloss.NewStyle().
				Foreground(ColorTextSecondary)

	StatusIdleStyle = lipgloss.NewStyle().
			Foreground(ColorHealthy)

	StatusRunningStyle = lipgloss.NewStyle().
				Foreground(ColorWarning)

	StatusSlowStyle = lipgloss.NewStyle().
			Foreground(ColorWarning)

	StatusUnreachableStyle = lipgloss.NewStyle().
				Foreground(ColorCritical)

	// Status text styles (for the "- idle", "- running" suffix)
	StatusTextStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted)

	StatusRunningTextStyle = lipgloss.NewStyle().
				Foreground(ColorWarning)
)

// Status indicator characters - cyber glyphs
const (
	StatusConnecting  = "◐" // Half-filled (used as fallback when animation not available)
	StatusIdle        = "◉" // Filled target - ready for work
	StatusRunning     = "⣿" // Braille full (used as fallback when animation not available)
	StatusUnreachable = "◌" // Dashed circle
	StatusSlow        = "◔" // Partially filled
)

// ConnectingSpinnerFrames are the animation frames for the connecting state
// Rotates through half-circle positions for a smooth spin effect
var ConnectingSpinnerFrames = []string{"◐", "◓", "◑", "◒"}

// RunningSpinnerFrames are the animation frames for the running/locked state
// Uses braille dots for a subtle "working" animation
var RunningSpinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// SpinnerColorFrames defines the gen-z color cycling for animated spinners
// Cycles through neon colors for a vibrant effect
var SpinnerColorFrames = []lipgloss.Color{
	lipgloss.Color("#FFAA00"), // Electric amber
	lipgloss.Color("#FF8800"), // Orange
	lipgloss.Color("#FFCC00"), // Gold
	lipgloss.Color("#FFAA00"), // Electric amber
	lipgloss.Color("#FF9900"), // Amber-orange
	lipgloss.Color("#FFBB00"), // Yellow-amber
	lipgloss.Color("#FFAA00"), // Electric amber
	lipgloss.Color("#FF7700"), // Deep amber
}

// ConnectingTextFrames are the animated text frames for the connecting message.
// Cycles through dot progression for a calm, non-frantic loading indicator.
var ConnectingTextFrames = []string{
	"Linking up",
	"Linking up.",
	"Linking up..",
	"Linking up...",
}

// ConnectingTextSlowdown controls how many spinner frames pass before advancing
// the connecting text animation. At 150ms spinner interval, 3 gives ~450ms per text frame.
const ConnectingTextSlowdown = 3

// GetSpinnerColor returns the color for the current spinner frame index.
// Used for gen-z style color cycling on animated spinners.
func GetSpinnerColor(frameIndex int) lipgloss.Color {
	return SpinnerColorFrames[frameIndex%len(SpinnerColorFrames)]
}

// GetRunningSpinner returns the spinner character and style for the running state.
func GetRunningSpinner(frameIndex int) (string, lipgloss.Style) {
	char := RunningSpinnerFrames[frameIndex%len(RunningSpinnerFrames)]
	color := GetSpinnerColor(frameIndex)
	style := lipgloss.NewStyle().Foreground(color)
	return char, style
}

// MetricColor returns the appropriate color for a percentage-based metric.
// Uses threshold-based coloring: green < 70%, yellow 70-90%, red > 90%.
func MetricColor(percent float64) lipgloss.Color {
	return MetricColorWithThresholds(percent, int(WarningThreshold), int(CriticalThreshold))
}

// MetricColorWithThresholds returns the appropriate color for a percentage-based metric
// using the provided warning and critical threshold values.
func MetricColorWithThresholds(percent float64, warning, critical int) lipgloss.Color {
	switch {
	case percent >= float64(critical):
		return ColorCritical
	case percent >= float64(warning):
		return ColorWarning
	default:
		return ColorHealthy
	}
}

// MetricStyle returns a style with the appropriate foreground color for the metric.
func MetricStyle(percent float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(MetricColor(percent))
}

// MetricStyleWithThresholds returns a style with the appropriate foreground color
// using custom warning and critical thresholds.
func MetricStyleWithThresholds(percent float64, warning, critical int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(MetricColorWithThresholds(percent, warning, critical))
}

// ProgressBar renders a progress bar with the given width and percentage.
// Uses bracketless Gen Z style with threshold-based coloring.
func ProgressBar(width int, percent float64) string {
	if width < 1 {
		width = 1
	}

	// Clamp percentage to 0-100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// Calculate filled portion
	filled := int(percent / 100.0 * float64(width))
	if filled > width {
		filled = width
	}

	// Build the bar with Gen Z style characters
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "▰"
		} else {
			bar += "▱"
		}
	}

	// Apply color based on percentage
	barStyle := lipgloss.NewStyle().Foreground(MetricColor(percent))

	return barStyle.Render(bar)
}

// CompactProgressBar renders a minimal progress bar without brackets.
func CompactProgressBar(width int, percent float64) string {
	return CompactProgressBarWithThresholds(width, percent, int(WarningThreshold), int(CriticalThreshold))
}

// CompactProgressBarWithThresholds renders a minimal progress bar using custom thresholds.
func CompactProgressBarWithThresholds(width int, percent float64, warning, critical int) string {
	if width < 1 {
		width = 1
	}

	// Clamp percentage to 0-100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100.0 * float64(width))
	if filled > width {
		filled = width
	}

	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "▰"
		} else {
			bar += "▱"
		}
	}

	return lipgloss.NewStyle().Foreground(MetricColorWithThresholds(percent, warning, critical)).Render(bar)
}

// ThinProgressBar renders a minimal line-based progress bar using thin characters.
// Uses ━ for filled segments and ─ for empty segments.
func ThinProgressBar(width int, percent float64) string {
	return ThinProgressBarWithThresholds(width, percent, int(WarningThreshold), int(CriticalThreshold))
}

// ThinProgressBarWithThresholds renders a thin progress bar with custom thresholds.
func ThinProgressBarWithThresholds(width int, percent float64, warning, critical int) string {
	if width < 1 {
		width = 1
	}

	// Clamp percentage to 0-100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100.0 * float64(width))
	if filled > width {
		filled = width
	}

	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "━"
		} else {
			bar += "─"
		}
	}

	return lipgloss.NewStyle().Foreground(MetricColorWithThresholds(percent, warning, critical)).Render(bar)
}

// SectionHeader renders a section header with the title on the left and value on the right.
// Format: ╭─ Title ────────────────────────────────────── Value ╮
func SectionHeader(title, value string, width int) string {
	if width < 10 {
		width = 10
	}

	// Calculate visible widths using lipgloss.Width for ANSI-aware measurement
	// Left: "╭─ " (3 chars) + title + " " (1 char)
	leftWidth := 3 + lipgloss.Width(title) + 1

	// Right: " " (1 char) + value + " ╮" (2 chars)
	rightWidth := 1 + lipgloss.Width(value) + 2

	// Calculate middle fill width
	fillWidth := width - leftWidth - rightWidth
	if fillWidth < 1 {
		fillWidth = 1
	}

	// Build middle with ─ characters
	middle := strings.Repeat("─", fillWidth)

	// Style the parts - neon pink title, cyan value
	borderStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	titleStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true)

	return borderStyle.Render("╭─ ") +
		titleStyle.Render(title) +
		borderStyle.Render(" "+middle+" ") +
		valueStyle.Render(value) +
		borderStyle.Render(" ╮")
}

// SectionFooter renders the bottom border of a section.
// Format: ╰────────────────────────────────────────────────────╯
func SectionFooter(width int) string {
	if width < 2 {
		width = 2
	}

	// ╰ and ╯ are each 1 display character
	middle := strings.Repeat("─", width-2)

	borderStyle := lipgloss.NewStyle().Foreground(ColorBorder)
	return borderStyle.Render("╰" + middle + "╯")
}

// SectionBorder renders the left border character for section content.
func SectionBorder() string {
	return lipgloss.NewStyle().Foreground(ColorBorder).Render("│")
}

// SectionContentLine renders a content line with left and right borders, properly padded to width.
// Format: │ content                                              │
func SectionContentLine(content string, width int) string {
	if width < 4 {
		width = 4
	}

	borderStyle := lipgloss.NewStyle().Foreground(ColorBorder)

	// Calculate the visible width of the content (accounting for ANSI codes)
	contentWidth := lipgloss.Width(content)

	// Inner width is total width minus the borders and padding: "│ " on left and " │" on right
	innerWidth := width - 4

	// Pad content to fill the inner width
	padding := innerWidth - contentWidth
	if padding < 0 {
		padding = 0
	}

	return borderStyle.Render("│") + " " + content + strings.Repeat(" ", padding) + " " + borderStyle.Render("│")
}
