package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Sparkline block characters representing 8 vertical levels (lowest to highest).
const sparklineBlocks = "▁▂▃▄▅▆▇█"

// sparklineBlockRunes provides indexed access to block characters.
var sparklineBlockRunes = []rune(sparklineBlocks)

// RenderSparkline creates a sparkline visualization from a slice of float64 values.
// The width parameter determines how many of the most recent data points to display.
// Values are mapped to 8 vertical levels based on the min/max range.
// Colors are applied based on the last value's threshold:
//   - 0-60%: green (success)
//   - 60-80%: yellow/amber (warning)
//   - 80-100%: red (error)
func RenderSparkline(data []float64, width int) string {
	if len(data) == 0 || width <= 0 {
		return ""
	}

	// Use only the most recent 'width' data points
	if len(data) > width {
		data = data[len(data)-width:]
	}

	// Find min and max values
	minVal, maxVal := data[0], data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// Build the sparkline string
	var sb strings.Builder
	sb.Grow(len(data) * 4) // UTF-8 block chars are up to 3 bytes + some buffer

	numLevels := len(sparklineBlockRunes)
	valueRange := maxVal - minVal

	for _, v := range data {
		var level int
		if valueRange == 0 {
			// All values are the same, use middle level
			level = numLevels / 2
		} else {
			// Map value to level (0 to numLevels-1)
			normalized := (v - minVal) / valueRange
			level = int(normalized * float64(numLevels-1))
			// Clamp to valid range
			if level < 0 {
				level = 0
			} else if level >= numLevels {
				level = numLevels - 1
			}
		}
		sb.WriteRune(sparklineBlockRunes[level])
	}

	sparkline := sb.String()

	// Determine color based on the last (current) value
	lastValue := data[len(data)-1]
	color := getThresholdColor(lastValue)

	style := lipgloss.NewStyle().Foreground(color)
	return style.Render(sparkline)
}

// getThresholdColor returns a color based on percentage thresholds.
//   - 0-60%: green (success)
//   - 60-80%: yellow/amber (warning)
//   - 80-100%: red (error)
func getThresholdColor(percent float64) lipgloss.Color {
	switch {
	case percent >= 80:
		return ColorError // Red
	case percent >= 60:
		return ColorWarning // Yellow
	default:
		return ColorSuccess // Green
	}
}
