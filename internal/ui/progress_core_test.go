package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClampPercent(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"zero stays zero", 0, 0},
		{"fifty stays fifty", 50, 50},
		{"hundred stays hundred", 100, 100},
		{"negative becomes zero", -10, 0},
		{"over hundred becomes hundred", 150, 100},
		{"fractional values work", 33.33, 33.33},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClampPercent(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateBarCounts(t *testing.T) {
	tests := []struct {
		name       string
		percent    float64
		width      int
		wantFilled int
		wantEmpty  int
	}{
		{"zero percent", 0, 10, 0, 10},
		{"fifty percent", 50, 10, 5, 5},
		{"hundred percent", 100, 10, 10, 0},
		{"33 percent rounds down", 33, 10, 3, 7},
		{"different width", 50, 20, 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filled, empty := CalculateBarCounts(tt.percent, tt.width)
			assert.Equal(t, tt.wantFilled, filled, "filled count")
			assert.Equal(t, tt.wantEmpty, empty, "empty count")
		})
	}
}

func TestCalculateBarCountsNormalized(t *testing.T) {
	tests := []struct {
		name       string
		percent    float64 // 0.0 to 1.0
		width      int
		wantFilled int
		wantEmpty  int
	}{
		{"zero", 0.0, 10, 0, 10},
		{"fifty", 0.5, 10, 5, 5},
		{"hundred", 1.0, 10, 10, 0},
		{"over hundred clamped", 1.5, 10, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filled, empty := CalculateBarCountsNormalized(tt.percent, tt.width)
			assert.Equal(t, tt.wantFilled, filled, "filled count")
			assert.Equal(t, tt.wantEmpty, empty, "empty count")
		})
	}
}

func TestBuildBarString(t *testing.T) {
	tests := []struct {
		name     string
		filled   int
		empty    int
		brackets bool
		expected string
	}{
		{"all empty with brackets", 0, 5, true, "[▱▱▱▱▱]"},
		{"all filled with brackets", 5, 0, true, "[▰▰▰▰▰]"},
		{"mixed with brackets", 3, 2, true, "[▰▰▰▱▱]"},
		{"all empty no brackets", 0, 5, false, "▱▱▱▱▱"},
		{"all filled no brackets", 5, 0, false, "▰▰▰▰▰"},
		{"mixed no brackets", 3, 2, false, "▰▰▰▱▱"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildBarString(tt.filled, tt.empty, tt.brackets)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProgressColorThreshold(t *testing.T) {
	tests := []struct {
		percent  float64
		expected string
	}{
		{0, string(ColorSuccess)},    // Green for low
		{30, string(ColorSuccess)},   // Green
		{59.9, string(ColorSuccess)}, // Green at boundary
		{60, string(ColorWarning)},   // Yellow at 60
		{70, string(ColorWarning)},   // Yellow
		{79.9, string(ColorWarning)}, // Yellow at boundary
		{80, string(ColorError)},     // Red at 80
		{100, string(ColorError)},    // Red
	}

	for _, tt := range tests {
		result := ProgressColorThreshold(tt.percent)
		assert.Equal(t, tt.expected, string(result), "percent %v", tt.percent)
	}
}

func TestProgressColorProgress(t *testing.T) {
	tests := []struct {
		percent  float64
		expected string
	}{
		{0, string(ColorNeonPink)},      // Pink for low
		{20, string(ColorNeonPink)},     // Pink
		{24.9, string(ColorNeonPink)},   // Pink at boundary
		{25, string(ColorNeonPink)},     // Pink at 25
		{50, string(ColorNeonPurple)},   // Purple at 50
		{74.9, string(ColorNeonPurple)}, // Purple at boundary
		{75, string(ColorNeonCyan)},     // Cyan at 75
		{100, string(ColorNeonCyan)},    // Cyan
	}

	for _, tt := range tests {
		result := ProgressColorProgress(tt.percent)
		assert.Equal(t, tt.expected, string(result), "percent %v", tt.percent)
	}
}

func TestBarConstants(t *testing.T) {
	assert.Equal(t, '▰', BarFilled, "filled block constant")
	assert.Equal(t, '▱', BarEmpty, "empty block constant")
}

func TestDefaultBarConfig(t *testing.T) {
	config := DefaultBarConfig(20)
	assert.Equal(t, 20, config.Width)
	assert.False(t, config.Brackets) // Gen Z style: no brackets
	assert.True(t, config.ShowPercent)
	assert.NotNil(t, config.ColorFunc)
}

func TestProgressBarConfig(t *testing.T) {
	config := ProgressBarConfig(30)
	assert.Equal(t, 30, config.Width)
	assert.False(t, config.Brackets) // Gen Z style: no brackets
	assert.False(t, config.ShowPercent)
	assert.NotNil(t, config.ColorFunc)
}

func TestRenderBar(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		width   int
		wantLen int // Expected character length of bar (without ANSI codes)
	}{
		{"zero width returns empty", 50, 0, 0},
		{"normal bar", 50, 10, 12}, // 10 chars + 2 brackets
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := BarConfig{
				Width:    tt.width,
				Brackets: true,
			}
			result := RenderBar(tt.percent, config)
			if tt.width <= 0 {
				assert.Empty(t, result)
			} else {
				assert.NotEmpty(t, result)
			}
		})
	}
}
