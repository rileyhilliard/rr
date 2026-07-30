package monitor

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// surfaceStyleCache caches graph cell styles keyed by foreground color.
// The color set is small and fixed (gradient palette, threshold colors, and
// custom ColorFunc outputs), so caching avoids re-building a lipgloss.Style
// and its ANSI sequences for every rendered cell. sync.Map keeps reads
// lock-free on the hot path.
var surfaceStyleCache sync.Map // lipgloss.Color -> cachedSurfaceStyle

// cachedSurfaceStyle holds a graph cell style plus its pre-rendered ANSI
// escape prefix/suffix, so styling a run of characters is plain string
// concatenation instead of a full lipgloss render.
type cachedSurfaceStyle struct {
	style     lipgloss.Style
	prefix    string
	suffix    string
	sequenced bool // prefix/suffix extraction succeeded
}

// styleProbe is a marker that never appears in graph output, used to split a
// rendered string into its ANSI prefix and suffix.
const styleProbe = "\x00"

// surfaceStyle returns the cached style entry for the given foreground color
// on the standard graph surface background.
func surfaceStyle(color lipgloss.Color) cachedSurfaceStyle {
	if cached, ok := surfaceStyleCache.Load(color); ok {
		return cached.(cachedSurfaceStyle)
	}
	style := lipgloss.NewStyle().Foreground(color).Background(ColorSurfaceBg)
	entry := cachedSurfaceStyle{style: style}
	rendered := style.Render(styleProbe)
	if idx := strings.Index(rendered, styleProbe); idx >= 0 {
		entry.prefix = rendered[:idx]
		entry.suffix = rendered[idx+len(styleProbe):]
		entry.sequenced = true
	}
	surfaceStyleCache.Store(color, entry)
	return entry
}

// render writes s styled with the cached ANSI sequences to b, falling back to
// a full lipgloss render if sequence extraction failed.
func (c cachedSurfaceStyle) render(b *strings.Builder, s string) {
	if c.sequenced {
		b.WriteString(c.prefix)
		b.WriteString(s)
		b.WriteString(c.suffix)
		return
	}
	b.WriteString(c.style.Render(s))
}

// renderColorRuns writes chars to b, merging consecutive characters that share
// the same color into a single styled segment. This collapses hundreds of
// per-character style renders (and ANSI escape pairs) into one per run.
// chars and colors must be the same length.
func renderColorRuns(b *strings.Builder, chars []rune, colors []lipgloss.Color) {
	runStart := 0
	for i := 1; i <= len(chars); i++ {
		if i == len(chars) || colors[i] != colors[runStart] {
			surfaceStyle(colors[runStart]).render(b, string(chars[runStart:i]))
			runStart = i
		}
	}
}

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

// ColorFunc is a function that returns a color based on a data value.
// Used for custom coloring in sparklines (e.g., latency thresholds).
type ColorFunc func(value float64) lipgloss.Color

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
	return RenderBrailleSparklineWithColorFunc(data, width, height, baseColor, nil)
}

// RenderBrailleSparklineWithColorFunc renders a sparkline with custom per-column coloring.
// If colorFunc is provided, it's called with each column's max value to determine color.
// If colorFunc is nil, falls back to default behavior (MetricColor for percentages, baseColor otherwise).
func RenderBrailleSparklineWithColorFunc(data []float64, width, height int, baseColor lipgloss.Color, colorFunc ColorFunc) string {
	return RenderBrailleSparklineWithOptions(data, width, height, baseColor, colorFunc, false)
}

// RenderBrailleSparklineWithOptions renders a sparkline with full control over coloring and scaling.
// forceZeroMin: when true, forces the Y-axis to start at 0 instead of the data's minimum.
// This is important for metrics like latency where 0 is a meaningful baseline.
func RenderBrailleSparklineWithOptions(data []float64, width, height int, baseColor lipgloss.Color, colorFunc ColorFunc, forceZeroMin bool) string {
	if len(data) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	minVal, maxVal, isPercentage := findMinMax(data)

	// Force zero baseline for metrics where 0 is meaningful (like latency)
	if forceZeroMin && !isPercentage {
		minVal = 0
	}
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

	// Determine the color for each character column once (colors depend only
	// on column data, so they are identical across rows)
	colColors := make([]lipgloss.Color, width)
	for colIdx := range colColors {
		switch {
		case colorFunc != nil:
			// Custom color function provided
			colColors[colIdx] = colorFunc(colMaxValues[colIdx])
		case isPercentage:
			// Default: use metric gradient for percentage data
			colColors[colIdx] = MetricColor(colMaxValues[colIdx])
		default:
			// Default: use base color for non-percentage data
			colColors[colIdx] = baseColor
		}
	}

	// Convert grid to string, merging same-colored runs into single segments
	var lines []string
	for _, row := range grid {
		var lineBuilder strings.Builder
		renderColorRuns(&lineBuilder, row, colColors)
		lines = append(lines, lineBuilder.String())
	}

	return strings.Join(lines, "\n")
}

// RenderGradientBar renders a horizontal bar with gradient fill.
// Colors transition from green to yellow to red based on position.
func RenderGradientBar(width int, percent float64, _ lipgloss.Color) string {
	return RenderGradientBarWithColorFunc(width, percent, nil)
}

// RenderGradientBarWithColorFunc renders a horizontal gradient bar with custom coloring.
// If colorFunc is provided, it's called with each cell's position percentage to determine color.
// If colorFunc is nil, falls back to the default MetricColor thresholds.
func RenderGradientBarWithColorFunc(width int, percent float64, colorFunc ColorFunc) string {
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

	chars := make([]rune, width)
	colors := make([]lipgloss.Color, width)
	for i := 0; i < width; i++ {
		if i < filled {
			// Color based on position in the bar (gradient effect)
			posPercent := float64(i+1) / float64(width) * 100
			if colorFunc != nil {
				colors[i] = colorFunc(posPercent)
			} else {
				colors[i] = MetricColor(posPercent)
			}
			chars[i] = '▰'
		} else {
			// Empty portion - use muted color
			colors[i] = ColorTextMuted
			chars[i] = '▱'
		}
	}

	var result strings.Builder
	renderColorRuns(&result, chars, colors)
	return result.String()
}

// RenderGraphWithYAxis renders a braille sparkline with y-axis labels on the left.
// Returns the complete graph with scale markers showing max at top and 0 at bottom.
// The formatValue function converts values to display strings (e.g., "1856ms", "5.2 MB/s").
// The minLabelWidth parameter ensures consistent width across different graphs.
// The colorFunc parameter allows custom per-column coloring (nil uses default behavior).
// forceZeroMin forces the Y-axis to start at 0 (important for latency where 0 is meaningful).
func RenderGraphWithYAxis(data []float64, graphWidth, height int, baseColor lipgloss.Color, formatValue func(float64) string, minLabelWidth int, colorFunc ColorFunc, forceZeroMin bool) string {
	if len(data) == 0 || graphWidth <= 0 || height <= 0 {
		return ""
	}

	_, maxVal, _ := findMinMax(data)

	// Format the axis labels
	maxLabel := formatValue(maxVal)
	minLabel := formatValue(0)

	// Find the widest label for consistent padding
	labelWidth := len(maxLabel)
	if len(minLabel) > labelWidth {
		labelWidth = len(minLabel)
	}
	// Ensure minimum width for consistent layout
	if labelWidth < minLabelWidth {
		labelWidth = minLabelWidth
	}

	// Render the sparkline with optional custom coloring and zero baseline
	graph := RenderBrailleSparklineWithOptions(data, graphWidth, height, baseColor, colorFunc, forceZeroMin)
	graphLines := strings.Split(graph, "\n")

	// Build output with y-axis labels
	var lines []string
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)

	for i, graphLine := range graphLines {
		var label string
		if i == 0 {
			// Top row: show max value
			label = padLeft(maxLabel, labelWidth)
		} else if i == len(graphLines)-1 {
			// Bottom row: show min value (0)
			label = padLeft(minLabel, labelWidth)
		} else {
			// Middle rows: empty space
			label = strings.Repeat(" ", labelWidth)
		}

		lines = append(lines, labelStyle.Render(label)+" "+graphLine)
	}

	return strings.Join(lines, "\n")
}

// padLeft pads a string with spaces on the left to reach the target width.
func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// ResampleMode controls how data is combined when downsampling.
type ResampleMode int

const (
	// ResampleMax preserves peaks by taking the max value in each bucket.
	// Good for CPU/RAM where spikes are important to see.
	ResampleMax ResampleMode = iota
	// ResampleAvg smooths data by averaging values in each bucket.
	// Good for latency where you want to see the trend, not individual spikes.
	ResampleAvg
)

// SmoothWithMovingAverage applies a rolling average to smooth data while preserving shape.
// Each point becomes the average of itself and its neighbors within the window.
// Window size of 5 is typical - smooths noise while keeping trends visible.
func SmoothWithMovingAverage(data []float64, windowSize int) []float64 {
	if len(data) == 0 || windowSize <= 1 {
		return data
	}

	result := make([]float64, len(data))
	halfWindow := windowSize / 2

	for i := range data {
		// Calculate window bounds
		start := i - halfWindow
		if start < 0 {
			start = 0
		}
		end := i + halfWindow + 1
		if end > len(data) {
			end = len(data)
		}

		// Average values in window
		sum := 0.0
		for j := start; j < end; j++ {
			sum += data[j]
		}
		result[i] = sum / float64(end-start)
	}

	return result
}

// resampleData resamples data to the target size.
// When downsampling (compressing), uses max-based sampling to preserve peaks/spikes.
// When upsampling (expanding), uses linear interpolation.
func resampleData(data []float64, targetSize int) []float64 {
	return ResampleDataWithMode(data, targetSize, ResampleMax)
}

// ResampleDataWithMode resamples data using the specified mode for downsampling.
// Exported so callers can pre-smooth data (e.g., latency) before rendering.
func ResampleDataWithMode(data []float64, targetSize int, mode ResampleMode) []float64 {
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

	// Downsampling: combine values within each bucket
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

			switch mode {
			case ResampleAvg:
				// Average values in this bucket for smoother trend visualization
				sum := 0.0
				for j := start; j < end; j++ {
					sum += data[j]
				}
				result[i] = sum / float64(end-start)
			default: // ResampleMax
				// Find max in this bucket to preserve peaks/spikes
				maxVal := data[start]
				for j := start + 1; j < end; j++ {
					if data[j] > maxVal {
						maxVal = data[j]
					}
				}
				result[i] = maxVal
			}
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
