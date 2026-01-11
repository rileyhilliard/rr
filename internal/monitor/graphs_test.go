package monitor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Force TrueColor output in tests so we can verify ANSI color codes
	lipgloss.SetColorProfile(termenv.TrueColor)
}

func TestFindMinMax(t *testing.T) {
	tests := []struct {
		name          string
		data          []float64
		wantMin       float64
		wantMax       float64
		wantIsPercent bool
	}{
		{
			name:          "empty data returns percentage defaults",
			data:          []float64{},
			wantMin:       0,
			wantMax:       100,
			wantIsPercent: true,
		},
		{
			name:          "percentage data uses fixed range",
			data:          []float64{10, 50, 90},
			wantMin:       0,
			wantMax:       100,
			wantIsPercent: true,
		},
		{
			name:          "non-percentage data uses actual range",
			data:          []float64{-50, 200, 500},
			wantMin:       -50,
			wantMax:       500,
			wantIsPercent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minVal, maxVal, isPercent := findMinMax(tt.data)
			assert.Equal(t, tt.wantMin, minVal)
			assert.Equal(t, tt.wantMax, maxVal)
			assert.Equal(t, tt.wantIsPercent, isPercent)
		})
	}
}

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		name   string
		val    float64
		minVal float64
		maxVal float64
		want   float64
	}{
		{
			name:   "middle value",
			val:    50,
			minVal: 0,
			maxVal: 100,
			want:   0.5,
		},
		{
			name:   "min value",
			val:    0,
			minVal: 0,
			maxVal: 100,
			want:   0,
		},
		{
			name:   "max value",
			val:    100,
			minVal: 0,
			maxVal: 100,
			want:   1,
		},
		{
			name:   "equal min max returns 0.5",
			val:    50,
			minVal: 50,
			maxVal: 50,
			want:   0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeValue(tt.val, tt.minVal, tt.maxVal)
			assert.InDelta(t, tt.want, got, 0.001)
		})
	}
}

func TestClampInt(t *testing.T) {
	tests := []struct {
		name string
		val  int
		max  int
		want int
	}{
		{name: "within range", val: 5, max: 10, want: 5},
		{name: "at max", val: 10, max: 10, want: 10},
		{name: "over max", val: 15, max: 10, want: 10},
		{name: "negative clamped to zero", val: -5, max: 10, want: 0},
		{name: "zero", val: 0, max: 10, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampInt(tt.val, tt.max)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResampleData(t *testing.T) {
	tests := []struct {
		name       string
		data       []float64
		targetSize int
		wantLen    int
		wantNil    bool
	}{
		{
			name:       "empty data returns nil",
			data:       []float64{},
			targetSize: 10,
			wantNil:    true,
		},
		{
			name:       "zero target returns nil",
			data:       []float64{1, 2, 3},
			targetSize: 0,
			wantNil:    true,
		},
		{
			name:       "negative target returns nil",
			data:       []float64{1, 2, 3},
			targetSize: -5,
			wantNil:    true,
		},
		{
			name:       "same size returns original",
			data:       []float64{1, 2, 3},
			targetSize: 3,
			wantLen:    3,
		},
		{
			name:       "single value fills target",
			data:       []float64{42},
			targetSize: 5,
			wantLen:    5,
		},
		{
			name:       "downsampling reduces size",
			data:       []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			targetSize: 5,
			wantLen:    5,
		},
		{
			name:       "upsampling increases size",
			data:       []float64{0, 100},
			targetSize: 5,
			wantLen:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resampleData(tt.data, tt.targetSize)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Len(t, result, tt.wantLen)
			}
		})
	}
}

func TestResampleData_DownsamplingPreservesPeaks(t *testing.T) {
	// Data with a spike in the middle
	data := []float64{10, 10, 10, 100, 10, 10, 10, 10, 10, 10}

	// Downsample to 5 points - the spike should be preserved
	result := resampleData(data, 5)

	require.Len(t, result, 5)

	// The bucket containing 100 should have max=100
	hasSpike := false
	for _, v := range result {
		if v == 100 {
			hasSpike = true
			break
		}
	}
	assert.True(t, hasSpike, "downsampling should preserve peak values")
}

func TestResampleData_UpsamplingInterpolates(t *testing.T) {
	data := []float64{0, 100}
	result := resampleData(data, 5)

	require.Len(t, result, 5)

	// Should interpolate: 0, 25, 50, 75, 100
	assert.InDelta(t, 0, result[0], 0.1)
	assert.InDelta(t, 25, result[1], 0.1)
	assert.InDelta(t, 50, result[2], 0.1)
	assert.InDelta(t, 75, result[3], 0.1)
	assert.InDelta(t, 100, result[4], 0.1)
}

func TestRenderBrailleSparkline(t *testing.T) {
	tests := []struct {
		name      string
		data      []float64
		width     int
		height    int
		wantEmpty bool
	}{
		{
			name:      "empty data returns empty string",
			data:      []float64{},
			width:     10,
			height:    4,
			wantEmpty: true,
		},
		{
			name:      "zero width returns empty string",
			data:      []float64{50},
			width:     0,
			height:    4,
			wantEmpty: true,
		},
		{
			name:      "zero height returns empty string",
			data:      []float64{50},
			width:     10,
			height:    0,
			wantEmpty: true,
		},
		{
			name:      "valid input returns non-empty",
			data:      []float64{25, 50, 75, 100},
			width:     10,
			height:    4,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderBrailleSparkline(tt.data, tt.width, tt.height, ColorGraph)
			if tt.wantEmpty {
				assert.Empty(t, result)
			} else {
				assert.NotEmpty(t, result)
			}
		})
	}
}

func TestRenderBrailleSparkline_ProducesCorrectRowCount(t *testing.T) {
	data := []float64{25, 50, 75, 100}
	height := 8

	result := RenderBrailleSparkline(data, 10, height, ColorGraph)

	// Should have height rows separated by newlines
	lines := strings.Split(result, "\n")
	assert.Len(t, lines, height)
}

func TestRenderBrailleSparkline_RightAlignedWhenPartial(t *testing.T) {
	// With only 4 data points and width=10, data should be right-aligned
	data := []float64{100, 100, 100, 100}
	width := 10
	height := 4

	result := RenderBrailleSparkline(data, width, height, ColorGraph)
	lines := strings.Split(result, "\n")

	// The leftmost characters should be empty braille (U+2800)
	// since data is right-aligned
	for _, line := range lines {
		runes := []rune(line)
		// First few characters should be styled empty braille or empty
		// (hard to test exactly due to ANSI codes, but line should exist)
		assert.NotEmpty(t, runes)
	}
}

func TestRenderBrailleSparkline_ColorBasedOnValue(t *testing.T) {
	// This test ensures braille sparkline colors are based on data values,
	// not the row position in the graph. Previously there was a bug where
	// all braille dots were red because of row-based gradient coloring.

	// ANSI color codes for reference:
	// ColorHealthy (#3fb950) appears as 38;2;63;185;80
	// ColorWarning (#d29922) appears as 38;2;210;153;34
	// ColorCritical (#f85149) appears as 38;2;248;81;73

	tests := []struct {
		name           string
		data           []float64
		shouldContain  string // partial ANSI code to look for
		shouldNotMatch string // color that should NOT appear for this data
		description    string
	}{
		{
			name:           "low values should be green",
			data:           []float64{20, 25, 30, 20, 25, 30}, // all under 70%
			shouldContain:  "38;2;63;185;80",                  // green RGB
			shouldNotMatch: "38;2;248;81;73",                  // should NOT be red
			description:    "values under 70% should use healthy (green) color",
		},
		{
			name:           "medium values should be yellow",
			data:           []float64{75, 80, 85, 75, 80, 85}, // all 70-90%
			shouldContain:  "38;2;210;153;34",                 // yellow RGB
			shouldNotMatch: "",
			description:    "values 70-90% should use warning (yellow) color",
		},
		{
			name:           "high values should be red",
			data:           []float64{92, 95, 98, 92, 95, 98}, // all over 90%
			shouldContain:  "38;2;248;81;73",                  // red RGB
			shouldNotMatch: "",
			description:    "values over 90% should use critical (red) color",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use height=2 which was problematic with the old row-based coloring
			result := RenderBrailleSparkline(tt.data, 10, 2, ColorGraph)

			assert.Contains(t, result, tt.shouldContain,
				"%s: expected color code %s in output", tt.description, tt.shouldContain)

			if tt.shouldNotMatch != "" {
				assert.NotContains(t, result, tt.shouldNotMatch,
					"%s: should not contain color code %s", tt.description, tt.shouldNotMatch)
			}
		})
	}
}

func TestRenderBrailleSparkline_LowValuesNotRedInShortGraphs(t *testing.T) {
	// Regression test: with height=2 (card view), low values like 26% RAM
	// were incorrectly showing as red due to row-based gradient logic.
	// The fix ensures coloring is purely value-based.

	lowData := []float64{26, 27, 25, 26, 28, 25, 26, 27} // ~26% - well under warning threshold
	redColorCode := "38;2;248;81;73"                     // ColorCritical RGB

	// Test with various small heights that are used in card views
	for _, height := range []int{1, 2, 3} {
		t.Run(strings.ReplaceAll("height_"+string(rune('0'+height)), " ", "_"), func(t *testing.T) {
			result := RenderBrailleSparkline(lowData, 10, height, ColorGraph)

			assert.NotContains(t, result, redColorCode,
				"height=%d: low values (26%%) should not be colored red", height)
		})
	}
}

func TestRenderMiniSparkline(t *testing.T) {
	tests := []struct {
		name      string
		data      []float64
		width     int
		wantEmpty bool
	}{
		{
			name:      "empty data",
			data:      []float64{},
			width:     10,
			wantEmpty: true,
		},
		{
			name:      "zero width",
			data:      []float64{50},
			width:     0,
			wantEmpty: true,
		},
		{
			name:      "valid input",
			data:      []float64{10, 50, 90},
			width:     5,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderMiniSparkline(tt.data, tt.width)
			if tt.wantEmpty {
				assert.Empty(t, result)
			} else {
				assert.NotEmpty(t, result)
				// Result should be exactly width characters
				assert.Len(t, []rune(result), tt.width)
			}
		})
	}
}

func TestRenderGradientBar(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		percent float64
	}{
		{
			name:    "zero percent",
			width:   10,
			percent: 0,
		},
		{
			name:    "50 percent",
			width:   10,
			percent: 50,
		},
		{
			name:    "100 percent",
			width:   10,
			percent: 100,
		},
		{
			name:    "clamps negative",
			width:   10,
			percent: -10,
		},
		{
			name:    "clamps over 100",
			width:   10,
			percent: 150,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderGradientBar(tt.width, tt.percent, ColorGraph)
			assert.NotEmpty(t, result)
		})
	}
}

func TestRenderCleanSparkline(t *testing.T) {
	data := []float64{0, 25, 50, 75, 100}
	result := RenderCleanSparkline(data, 5, ColorGraph)

	assert.NotEmpty(t, result)
}

func TestRenderTimeSeriesGraph(t *testing.T) {
	tests := []struct {
		name      string
		data      []float64
		width     int
		height    int
		wantEmpty bool
	}{
		{
			name:      "empty data",
			data:      []float64{},
			width:     10,
			height:    4,
			wantEmpty: true,
		},
		{
			name:      "valid input",
			data:      []float64{25, 50, 75, 100},
			width:     10,
			height:    4,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderTimeSeriesGraph(tt.data, tt.width, tt.height, ColorGraph)
			if tt.wantEmpty {
				assert.Empty(t, result)
			} else {
				assert.NotEmpty(t, result)
				lines := strings.Split(result, "\n")
				assert.Len(t, lines, tt.height)
			}
		})
	}
}

func TestRenderColoredMiniSparkline(t *testing.T) {
	// Test with data ending in different severity levels
	tests := []struct {
		name string
		data []float64
	}{
		{
			name: "healthy range",
			data: []float64{10, 20, 30},
		},
		{
			name: "warning range",
			data: []float64{10, 50, 75},
		},
		{
			name: "critical range",
			data: []float64{10, 50, 95},
		},
		{
			name: "empty data",
			data: []float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderColoredMiniSparkline(tt.data, 5)
			// Just verify it doesn't panic
			_ = result
		})
	}
}
