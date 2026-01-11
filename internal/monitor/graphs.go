package monitor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Braille character rendering for high-resolution terminal graphs.
//
// Braille patterns use a 2x4 dot matrix per character:
//
//	  Col 0  Col 1
//	Row 0:   ⠁      ⠈     (dots 1, 4)
//	Row 1:   ⠂      ⠐     (dots 2, 5)
//	Row 2:   ⠄      ⠠     (dots 3, 6)
//	Row 3:   ⡀      ⢀     (dots 7, 8)
//
// Unicode braille starts at U+2800 (empty) and uses bit patterns:
// bit 0 = dot 1, bit 1 = dot 2, bit 2 = dot 3, bit 3 = dot 4,
// bit 4 = dot 5, bit 5 = dot 6, bit 6 = dot 7, bit 7 = dot 8

const brailleBase = '\u2800'

// sparklineBlocks are block characters for 8-level vertical resolution (lowest to highest).
var sparklineBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// findMinMax returns the minimum and maximum values in a slice.
// For percentage data (all values 0-100), returns fixed range 0-100.
func findMinMax(data []float64) (minVal, maxVal float64, isPercentage bool) {
	if len(data) == 0 {
		return 0, 100, true
	}

	minVal, maxVal = data[0], data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// For percentage data (0-100), use fixed range for consistent scaling
	isPercentage = maxVal <= 100 && minVal >= 0
	if isPercentage {
		minVal = 0
		maxVal = 100
	}

	return minVal, maxVal, isPercentage
}

// normalizeValue converts a value to 0-1 range given min/max bounds.
func normalizeValue(val, minVal, maxVal float64) float64 {
	if maxVal > minVal {
		return (val - minVal) / (maxVal - minVal)
	}
	return 0.5
}

// clampInt clamps an integer to a range [0, maxVal].
func clampInt(val, maxVal int) int {
	if val < 0 {
		return 0
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

// brailleDots maps row/column to the bit offset for braille pattern
// [row][col] where row is 0-3 (top to bottom) and col is 0-1 (left to right)
var brailleDots = [4][2]uint8{
	{0, 3}, // Row 0: dots 1 and 4
	{1, 4}, // Row 1: dots 2 and 5
	{2, 5}, // Row 2: dots 3 and 6
	{6, 7}, // Row 3: dots 7 and 8
}

// RenderBrailleSparkline renders a sparkline graph using braille characters.
// Each character represents 2 horizontal data points with 4 vertical levels.
// This gives much higher resolution than standard block characters.
// Colors transition from green to yellow to red based on value (btop-style gradient).
//
// Parameters:
//   - data: values to plot (will be normalized to 0-100 range if not already)
//   - width: number of braille characters (each represents 2 data points)
//   - height: number of rows (each row represents 4 vertical levels)
//   - baseColor: fallback color (used for non-percentage data)
func RenderBrailleSparkline(data []float64, width, height int, baseColor lipgloss.Color) string {
	if len(data) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	minVal, maxVal, isPercentage := findMinMax(data)
	totalDots := height * 4
	targetPoints := width * 2

	// Only downsample if we have more data than display width.
	// If we have less data, use it directly (graph fills from right).
	resampled := data
	if len(data) > targetPoints {
		resampled = resampleData(data, targetPoints)
	}

	// Create the braille grid
	grid := make([][]rune, height)
	for i := range grid {
		grid[i] = make([]rune, width)
		for j := range grid[i] {
			grid[i][j] = brailleBase
		}
	}

	// Track the max value for each character column (for coloring)
	colMaxValues := make([]float64, width)

	// Right-align data when we have less than full width
	horizOffset := targetPoints - len(resampled)
	if horizOffset < 0 {
		horizOffset = 0
	}

	// Plot each data point
	for i, val := range resampled {
		normalized := normalizeValue(val, minVal, maxVal)
		dotHeight := clampInt(int(normalized*float64(totalDots)), totalDots)

		// Which character column (apply offset to right-align)
		charCol := (i + horizOffset) / 2
		if charCol >= width {
			continue
		}

		// Track max value for this column
		if val > colMaxValues[charCol] {
			colMaxValues[charCol] = val
		}

		// Which sub-column within the braille char (0 or 1)
		subCol := (i + horizOffset) % 2

		// Fill dots from bottom up
		for dot := 0; dot < dotHeight; dot++ {
			row := height - 1 - (dot / 4)
			if row < 0 {
				continue
			}
			subRow := 3 - (dot % 4)
			bitOffset := brailleDots[subRow][subCol]
			grid[row][charCol] |= rune(1 << bitOffset)
		}
	}

	// Convert grid to string with per-column coloring based on data values
	var lines []string
	for _, row := range grid {
		var lineBuilder strings.Builder
		for colIdx, char := range row {
			// Determine color based on max value at this column
			var color lipgloss.Color
			if isPercentage {
				color = MetricColor(colMaxValues[colIdx])
			} else {
				color = baseColor
			}

			// Apply both foreground and background color
			style := lipgloss.NewStyle().Foreground(color).Background(ColorSurfaceBg)
			lineBuilder.WriteString(style.Render(string(char)))
		}
		lines = append(lines, lineBuilder.String())
	}

	return strings.Join(lines, "\n")
}

// RenderMiniSparkline renders a single-row sparkline using block characters.
// This is more compact than braille and good for inline display in cards.
//
// Parameters:
//   - data: values to plot
//   - width: number of characters
func RenderMiniSparkline(data []float64, width int) string {
	if len(data) == 0 || width <= 0 {
		return ""
	}

	minVal, maxVal, _ := findMinMax(data)
	resampled := resampleData(data, width)

	var result strings.Builder
	for _, val := range resampled {
		normalized := normalizeValue(val, minVal, maxVal)
		idx := clampInt(int(normalized*float64(len(sparklineBlocks)-1)), len(sparklineBlocks)-1)
		result.WriteRune(sparklineBlocks[idx])
	}

	return result.String()
}

// RenderColoredMiniSparkline renders a sparkline with threshold-based coloring.
func RenderColoredMiniSparkline(data []float64, width int) string {
	sparkline := RenderMiniSparkline(data, width)
	if len(data) == 0 {
		return sparkline
	}

	// Color based on the most recent value
	lastVal := data[len(data)-1]
	color := MetricColor(lastVal)
	return lipgloss.NewStyle().Foreground(color).Render(sparkline)
}

// RenderCleanSparkline renders a single-row sparkline with a consistent accent color.
// This provides a cleaner, less noisy visualization than multi-row braille graphs.
// Each character represents one data point using block characters (▁▂▃▄▅▆▇█).
func RenderCleanSparkline(data []float64, width int, color lipgloss.Color) string {
	if len(data) == 0 || width <= 0 {
		return ""
	}

	// Always use percentage range for clean sparklines
	minVal, maxVal := 0.0, 100.0
	resampled := resampleData(data, width)

	var result strings.Builder
	for _, val := range resampled {
		normalized := normalizeValue(val, minVal, maxVal)
		idx := clampInt(int(normalized*float64(len(sparklineBlocks)-1)), len(sparklineBlocks)-1)
		result.WriteRune(sparklineBlocks[idx])
	}

	return lipgloss.NewStyle().Foreground(color).Render(result.String())
}

// RenderTimeSeriesGraph renders a multi-row time series graph showing historical data.
// Each column represents one time point, rendered vertically with block characters.
// Height is the number of rows (typically 3-5 for good visibility).
func RenderTimeSeriesGraph(data []float64, width, height int, color lipgloss.Color) string {
	if len(data) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	// Always use percentage range
	minVal, maxVal := 0.0, 100.0
	resampled := resampleData(data, width)

	rows := make([]strings.Builder, height)
	fillChars := []rune{'█', '▓', '▒', '░'}

	for col, val := range resampled {
		normalized := normalizeValue(val, minVal, maxVal)
		if normalized > 1 {
			normalized = 1
		}
		if normalized < 0 {
			normalized = 0
		}

		// Calculate how many rows should be filled (from bottom up)
		filledRows := int(normalized * float64(height))

		// For each row (0 = top, height-1 = bottom)
		for row := 0; row < height; row++ {
			// Row from bottom: height-1-row
			rowFromBottom := height - 1 - row

			if rowFromBottom < filledRows {
				// This row is filled
				// Use gradient: bottom rows are brighter
				charIdx := 0
				if filledRows > 0 {
					// Gradient based on position within filled area
					gradientPos := float64(rowFromBottom) / float64(filledRows)
					charIdx = int(gradientPos * float64(len(fillChars)-1))
					if charIdx >= len(fillChars) {
						charIdx = len(fillChars) - 1
					}
				}
				rows[row].WriteRune(fillChars[charIdx])
			} else if rowFromBottom == filledRows && col > 0 {
				// Partial fill at the top - use lighter char
				rows[row].WriteRune('░')
			} else {
				// Empty
				rows[row].WriteRune(' ')
			}
		}
	}

	// Convert rows to styled strings
	var lines []string
	style := lipgloss.NewStyle().Foreground(color)
	for _, row := range rows {
		lines = append(lines, style.Render(row.String()))
	}

	return strings.Join(lines, "\n")
}

// RenderGradientBar renders a horizontal bar with gradient fill.
// Colors transition from green to yellow to red based on position (btop-style).
func RenderGradientBar(width int, percent float64, _ lipgloss.Color) string {
	if width < 1 {
		width = 1
	}

	// Clamp percentage
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

	var result strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			// Color based on position in the bar (gradient effect)
			posPercent := float64(i+1) / float64(width) * 100
			color := MetricColor(posPercent)
			style := lipgloss.NewStyle().Foreground(color).Background(ColorSurfaceBg)
			result.WriteString(style.Render("█"))
		} else {
			// Empty portion - use muted color
			style := lipgloss.NewStyle().Foreground(ColorTextMuted).Background(ColorSurfaceBg)
			result.WriteString(style.Render("░"))
		}
	}

	return result.String()
}

// resampleData resamples data to the target size.
// When downsampling (compressing), uses max-based sampling to preserve peaks/spikes.
// When upsampling (expanding), uses linear interpolation.
func resampleData(data []float64, targetSize int) []float64 {
	if len(data) == 0 || targetSize <= 0 {
		return nil
	}

	if len(data) == targetSize {
		return data
	}

	result := make([]float64, targetSize)

	if len(data) == 1 {
		// Single value - fill with it
		for i := range result {
			result[i] = data[0]
		}
		return result
	}

	// Downsampling: use max within each bucket to preserve peaks
	if len(data) > targetSize {
		bucketSize := float64(len(data)) / float64(targetSize)
		for i := 0; i < targetSize; i++ {
			start := int(float64(i) * bucketSize)
			end := int(float64(i+1) * bucketSize)
			if end > len(data) {
				end = len(data)
			}
			if start >= end {
				start = end - 1
			}
			if start < 0 {
				start = 0
			}

			// Find max in this bucket
			maxVal := data[start]
			for j := start + 1; j < end; j++ {
				if data[j] > maxVal {
					maxVal = data[j]
				}
			}
			result[i] = maxVal
		}
		return result
	}

	// Upsampling: linear interpolation
	scale := float64(len(data)-1) / float64(targetSize-1)
	for i := 0; i < targetSize; i++ {
		pos := float64(i) * scale
		idx := int(pos)
		frac := pos - float64(idx)

		if idx >= len(data)-1 {
			result[i] = data[len(data)-1]
		} else {
			// Linear interpolation between data[idx] and data[idx+1]
			result[i] = data[idx]*(1-frac) + data[idx+1]*frac
		}
	}

	return result
}
