package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Progress bar block characters.
const (
	progressFilled = '█'
	progressEmpty  = '░'
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

	// Clamp percent to 0-100 range
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
		percent = 100
	}

	// Calculate filled and empty portions
	filledCount := int((percent / 100.0) * float64(width))
	emptyCount := width - filledCount

	// Build the bar
	var sb strings.Builder
	sb.Grow(width + 10) // bar + brackets + percentage

	sb.WriteRune('[')
	for i := 0; i < filledCount; i++ {
		sb.WriteRune(progressFilled)
	}
	for i := 0; i < emptyCount; i++ {
		sb.WriteRune(progressEmpty)
	}
	sb.WriteRune(']')

	bar := sb.String()

	// Get the color based on percentage threshold
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

	// Clamp percent to 0-100 range
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
		percent = 100
	}

	// Calculate filled and empty portions
	filledCount := int((percent / 100.0) * float64(width))
	emptyCount := width - filledCount

	// Build the bar
	var sb strings.Builder
	sb.Grow(width + 6) // bar + percentage

	for i := 0; i < filledCount; i++ {
		sb.WriteRune(progressFilled)
	}
	for i := 0; i < emptyCount; i++ {
		sb.WriteRune(progressEmpty)
	}

	bar := sb.String()

	// Get the color based on percentage threshold
	color := getThresholdColor(percent)
	style := lipgloss.NewStyle().Foreground(color)

	// Format percentage (whole number)
	percentStr := fmt.Sprintf(" %3.0f%%", percent)

	return style.Render(bar) + percentStr
}
