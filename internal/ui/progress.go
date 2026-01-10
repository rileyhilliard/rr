package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Keep these for backward compatibility with tests that reference them.
const (
	progressFilled = BarFilled
	progressEmpty  = BarEmpty
)

// RenderProgressBar creates a progress bar visualization.
// The percent parameter should be 0-100 (values outside this range are clamped).
// The width parameter determines the character width of the bar itself (excluding brackets and percentage).
// Output format: [████████░░░░] 67%
// Colors are applied based on percentage threshold:
//   - 0-60%: green (success)
//   - 60-80%: yellow/amber (warning)
//   - 80-100%: red (error)
func RenderProgressBar(percent float64, width int) string {
	if width <= 0 {
		return ""
	}

	percent = ClampPercent(percent)
	filled, empty := CalculateBarCounts(percent, width)
	bar := BuildBarString(filled, empty, true) // with brackets

	// Apply threshold-based coloring
	color := getThresholdColor(percent)
	style := lipgloss.NewStyle().Foreground(color)

	// Format percentage (whole number, right-aligned)
	percentStr := fmt.Sprintf(" %3.0f%%", percent)

	return style.Render(bar) + percentStr
}

// RenderProgressBarSimple creates a simpler progress bar without brackets.
// Output format: ████████░░░░ 67%
func RenderProgressBarSimple(percent float64, width int) string {
	if width <= 0 {
		return ""
	}

	percent = ClampPercent(percent)
	filled, empty := CalculateBarCounts(percent, width)
	bar := BuildBarString(filled, empty, false) // no brackets

	// Apply threshold-based coloring
	color := getThresholdColor(percent)
	style := lipgloss.NewStyle().Foreground(color)

	// Format percentage (whole number)
	percentStr := fmt.Sprintf(" %3.0f%%", percent)

	return style.Render(bar) + percentStr
}
