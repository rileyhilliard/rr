package monitor

import "github.com/charmbracelet/lipgloss"

// Dashboard color palette - btop-inspired monochrome with cyan accent
const (
	// Background colors
	ColorDarkBg    = lipgloss.Color("#0d1117")
	ColorSurfaceBg = lipgloss.Color("#161b22")
	ColorBorder    = lipgloss.Color("#30363d")

	// Semantic colors for metrics
	ColorHealthy  = lipgloss.Color("#3fb950")
	ColorWarning  = lipgloss.Color("#d29922")
	ColorCritical = lipgloss.Color("#f85149")

	// Text colors
	ColorTextPrimary   = lipgloss.Color("#e6edf3")
	ColorTextSecondary = lipgloss.Color("#8b949e")
	ColorTextMuted     = lipgloss.Color("#6e7681")

	// Accent colors - cyan for btop-style look
	ColorAccent    = lipgloss.Color("#00d7d7")
	ColorAccentDim = lipgloss.Color("#005f5f")

	// Graph colors
	ColorGraph = lipgloss.Color("#00d7d7")
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

	// Card styles
	CardStyle = lipgloss.NewStyle().
			Background(ColorSurfaceBg).
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
	StatusConnectedStyle = lipgloss.NewStyle().
				Foreground(ColorHealthy)

	StatusSlowStyle = lipgloss.NewStyle().
			Foreground(ColorWarning)

	StatusUnreachableStyle = lipgloss.NewStyle().
				Foreground(ColorCritical)
)

// Status indicator characters
const (
	StatusConnected   = "●"
	StatusUnreachable = "○"
)

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
// The bar uses threshold-based coloring.
func ProgressBar(width int, percent float64) string {
	if width < 3 {
		width = 3
	}

	// Clamp percentage to 0-100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// Calculate filled portion
	innerWidth := width - 2 // Account for brackets
	filled := int(percent / 100.0 * float64(innerWidth))
	if filled > innerWidth {
		filled = innerWidth
	}

	// Build the bar
	bar := ""
	for i := 0; i < innerWidth; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}

	// Apply color based on percentage
	barStyle := lipgloss.NewStyle().Foreground(MetricColor(percent))
	bracketStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)

	return bracketStyle.Render("[") + barStyle.Render(bar) + bracketStyle.Render("]")
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
			bar += "█"
		} else {
			bar += "░"
		}
	}

	return lipgloss.NewStyle().Foreground(MetricColorWithThresholds(percent, warning, critical)).Render(bar)
}
