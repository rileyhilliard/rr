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
//
// Parameters:
//   - data: values to plot (will be normalized to 0-100 range if not already)
//   - width: number of braille characters (each represents 2 data points)
//   - height: number of rows (each row represents 4 vertical levels)
//   - color: lipgloss color for the graph
func RenderBrailleSparkline(data []float64, width, height int, color lipgloss.Color) string {
	if len(data) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	// Find min/max for scaling
	minVal, maxVal := data[0], data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// For percentage data (0-100), use fixed range
	if maxVal <= 100 && minVal >= 0 {
		minVal = 0
		maxVal = 100
	}

	// Total vertical resolution: height rows * 4 dots per row
	totalDots := height * 4

	// Resample data to fit width*2 horizontal points
	resampled := resampleData(data, width*2)

	// Create the braille grid: rows of characters
	// Each row is height braille chars tall
	grid := make([][]rune, height)
	for i := range grid {
		grid[i] = make([]rune, width)
		for j := range grid[i] {
			grid[i][j] = brailleBase // Start with empty braille
		}
	}

	// Plot each data point
	for i, val := range resampled {
		// Normalize value to 0-1
		var normalized float64
		if maxVal > minVal {
			normalized = (val - minVal) / (maxVal - minVal)
		} else {
			normalized = 0.5
		}

		// Convert to dot height (0 to totalDots)
		dotHeight := int(normalized * float64(totalDots))
		if dotHeight > totalDots {
			dotHeight = totalDots
		}
		if dotHeight < 0 {
			dotHeight = 0
		}

		// Which character column (each braille char has 2 columns)
		charCol := i / 2
		if charCol >= width {
			continue
		}

		// Which sub-column within the braille char (0 or 1)
		subCol := i % 2

		// Fill dots from bottom up
		for dot := 0; dot < dotHeight; dot++ {
			// Which row (from bottom)
			row := height - 1 - (dot / 4)
			if row < 0 {
				continue
			}

			// Which sub-row within the braille char (0-3, but inverted since we go bottom-up)
			subRow := 3 - (dot % 4)

			// Set the appropriate bit
			bitOffset := brailleDots[subRow][subCol]
			grid[row][charCol] |= rune(1 << bitOffset)
		}
	}

	// Convert grid to string
	var lines []string
	style := lipgloss.NewStyle().Foreground(color)
	for _, row := range grid {
		lines = append(lines, style.Render(string(row)))
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

	// Block characters for 8 levels
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Find min/max for scaling
	minVal, maxVal := data[0], data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// For percentage data, use fixed range
	if maxVal <= 100 && minVal >= 0 {
		minVal = 0
		maxVal = 100
	}

	// Resample data to fit width
	resampled := resampleData(data, width)

	var result strings.Builder
	for _, val := range resampled {
		var normalized float64
		if maxVal > minVal {
			normalized = (val - minVal) / (maxVal - minVal)
		} else {
			normalized = 0.5
		}

		// Map to block character (0-7)
		idx := int(normalized * float64(len(blocks)-1))
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		if idx < 0 {
			idx = 0
		}
		result.WriteRune(blocks[idx])
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

	// Block characters for 8 levels (from lowest to highest)
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// For percentage data (0-100), use fixed range
	minVal, maxVal := 0.0, 100.0

	// Resample data to fit width
	resampled := resampleData(data, width)

	var result strings.Builder
	for _, val := range resampled {
		var normalized float64
		if maxVal > minVal {
			normalized = (val - minVal) / (maxVal - minVal)
		} else {
			normalized = 0.5
		}

		// Map to block character (0-7)
		idx := int(normalized * float64(len(blocks)-1))
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		if idx < 0 {
			idx = 0
		}
		result.WriteRune(blocks[idx])
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

	// For percentage data, use fixed 0-100 range
	minVal, maxVal := 0.0, 100.0

	// Resample data to fit width
	resampled := resampleData(data, width)

	// Build the graph row by row, from top to bottom
	// Each row represents a threshold band
	rows := make([]strings.Builder, height)

	// Characters for drawing: full block for filled, space for empty
	// We use different intensities for a gradient effect
	fillChars := []rune{'█', '▓', '▒', '░'}

	for col, val := range resampled {
		// Normalize value to 0-1
		normalized := (val - minVal) / (maxVal - minVal)
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

// resampleData resamples data to the target size using linear interpolation.
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

	// Linear interpolation
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
