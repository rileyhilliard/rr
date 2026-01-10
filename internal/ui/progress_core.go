package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Progress bar block characters.
const (
	BarFilled = '█'
	BarEmpty  = '░'
)

// ProgressColorFunc is a function that returns a color based on percentage.
// Different use cases need different color schemes:
//   - Resource monitoring: higher % = worse (red)
//   - Progress bars: higher % = better (green)
type ProgressColorFunc func(percent float64) lipgloss.Color

// ProgressColorThreshold returns colors for resource monitoring (CPU, memory).
// Higher values indicate problems: 0-60% green, 60-80% yellow, 80%+ red.
func ProgressColorThreshold(percent float64) lipgloss.Color {
	return getThresholdColor(percent)
}

// ProgressColorProgress returns colors for progress bars.
// Higher values are better: 0-50% secondary (blue), 50-80% warning (yellow), 80%+ success (green).
func ProgressColorProgress(percent float64) lipgloss.Color {
	switch {
	case percent >= 80:
		return ColorSuccess
	case percent >= 50:
		return ColorWarning
	default:
		return ColorSecondary
	}
}

// BarConfig configures progress bar rendering.
type BarConfig struct {
	Width       int               // Width of the bar in characters
	Brackets    bool              // Whether to wrap bar in [ ]
	ColorFunc   ProgressColorFunc // Function to determine bar color
	ShowPercent bool              // Whether to append percentage
}

// DefaultBarConfig returns a config for resource monitoring bars.
func DefaultBarConfig(width int) BarConfig {
	return BarConfig{
		Width:       width,
		Brackets:    true,
		ColorFunc:   ProgressColorThreshold,
		ShowPercent: true,
	}
}

// ProgressBarConfig returns a config for progress-style bars.
func ProgressBarConfig(width int) BarConfig {
	return BarConfig{
		Width:       width,
		Brackets:    true,
		ColorFunc:   ProgressColorProgress,
		ShowPercent: false,
	}
}

// ClampPercent clamps a percentage to the 0-100 range.
func ClampPercent(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

// BuildBarString builds the raw bar string (without styling) from filled/empty counts.
// If brackets is true, wraps in [ ].
func BuildBarString(filledCount, emptyCount int, brackets bool) string {
	var sb strings.Builder
	capacity := filledCount + emptyCount
	if brackets {
		capacity += 2
	}
	sb.Grow(capacity)

	if brackets {
		sb.WriteRune('[')
	}

	for i := 0; i < filledCount; i++ {
		sb.WriteRune(BarFilled)
	}
	for i := 0; i < emptyCount; i++ {
		sb.WriteRune(BarEmpty)
	}

	if brackets {
		sb.WriteRune(']')
	}

	return sb.String()
}

// CalculateBarCounts returns the number of filled and empty characters for a bar.
// Percent should be 0-100, width is the total bar width.
func CalculateBarCounts(percent float64, width int) (filled, empty int) {
	filled = int((percent / 100.0) * float64(width))
	empty = width - filled
	return
}

// CalculateBarCountsNormalized returns counts for a normalized (0-1) percentage.
func CalculateBarCountsNormalized(percent float64, width int) (filled, empty int) {
	filled = int(percent * float64(width))
	if filled > width {
		filled = width
	}
	empty = width - filled
	return
}

// RenderBar renders a progress bar with the given configuration.
// Percent should be 0-100.
func RenderBar(percent float64, config BarConfig) string {
	if config.Width <= 0 {
		return ""
	}

	percent = ClampPercent(percent)
	filled, empty := CalculateBarCounts(percent, config.Width)
	bar := BuildBarString(filled, empty, config.Brackets)

	// Apply color if a color function is provided
	if config.ColorFunc != nil {
		color := config.ColorFunc(percent)
		style := lipgloss.NewStyle().Foreground(color)
		bar = style.Render(bar)
	}

	return bar
}
