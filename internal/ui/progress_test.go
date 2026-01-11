package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderProgressBar_ZeroWidth(t *testing.T) {
	result := RenderProgressBar(50, 0)
	assert.Empty(t, result, "zero width should return empty string")
}

func TestRenderProgressBar_NegativeWidth(t *testing.T) {
	result := RenderProgressBar(50, -5)
	assert.Empty(t, result, "negative width should return empty string")
}

func TestRenderProgressBar_ZeroPercent(t *testing.T) {
	result := RenderProgressBar(0, 10)
	stripped := stripANSI(result)

	assert.Contains(t, stripped, "[", "should have opening bracket")
	assert.Contains(t, stripped, "]", "should have closing bracket")
	assert.Contains(t, stripped, "0%", "should show 0%")
	// All empty blocks - Gen Z style
	assert.Contains(t, stripped, "▱▱▱▱▱▱▱▱▱▱", "should be all empty blocks")
}

func TestRenderProgressBar_HundredPercent(t *testing.T) {
	result := RenderProgressBar(100, 10)
	stripped := stripANSI(result)

	assert.Contains(t, stripped, "[", "should have opening bracket")
	assert.Contains(t, stripped, "]", "should have closing bracket")
	assert.Contains(t, stripped, "100%", "should show 100%")
	// All filled blocks - Gen Z style
	assert.Contains(t, stripped, "▰▰▰▰▰▰▰▰▰▰", "should be all filled blocks")
}

func TestRenderProgressBar_FiftyPercent(t *testing.T) {
	result := RenderProgressBar(50, 10)
	stripped := stripANSI(result)

	assert.Contains(t, stripped, "50%", "should show 50%")
	// Should have 5 filled and 5 empty - Gen Z style
	assert.Contains(t, stripped, "▰▰▰▰▰▱▱▱▱▱", "should be half filled")
}

func TestRenderProgressBar_NegativePercent(t *testing.T) {
	result := RenderProgressBar(-10, 10)
	stripped := stripANSI(result)

	// Should clamp to 0%
	assert.Contains(t, stripped, "0%", "negative should clamp to 0%")
	assert.Contains(t, stripped, "▱▱▱▱▱▱▱▱▱▱", "should be all empty")
}

func TestRenderProgressBar_OverHundredPercent(t *testing.T) {
	result := RenderProgressBar(150, 10)
	stripped := stripANSI(result)

	// Should clamp to 100%
	assert.Contains(t, stripped, "100%", "over 100 should clamp to 100%")
	assert.Contains(t, stripped, "▰▰▰▰▰▰▰▰▰▰", "should be all filled")
}

func TestRenderProgressBar_VariousPercentages(t *testing.T) {
	tests := []struct {
		percent       float64
		width         int
		filledBlocks  int
		percentString string
	}{
		{0, 10, 0, "0%"},
		{10, 10, 1, "10%"},
		{25, 10, 2, "25%"},
		{33, 10, 3, "33%"},
		{50, 10, 5, "50%"},
		{67, 10, 6, "67%"},
		{75, 10, 7, "75%"},
		{90, 10, 9, "90%"},
		{100, 10, 10, "100%"},
	}

	for _, tt := range tests {
		t.Run(tt.percentString, func(t *testing.T) {
			result := RenderProgressBar(tt.percent, tt.width)
			stripped := stripANSI(result)

			assert.Contains(t, stripped, tt.percentString, "should show correct percentage")

			// Count filled blocks - Gen Z style
			filled := strings.Count(stripped, "▰")
			assert.Equal(t, tt.filledBlocks, filled, "should have correct number of filled blocks")
		})
	}
}

func TestRenderProgressBar_DifferentWidths(t *testing.T) {
	tests := []struct {
		width       int
		filledAt50  int
		totalBlocks int
	}{
		{5, 2, 5},
		{10, 5, 10},
		{20, 10, 20},
		{15, 7, 15},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := RenderProgressBar(50, tt.width)
			stripped := stripANSI(result)

			// Remove brackets and percentage for block counting - Gen Z style
			barOnly := extractBarOnly(stripped)
			totalBlocks := strings.Count(barOnly, "▰") + strings.Count(barOnly, "▱")
			assert.Equal(t, tt.totalBlocks, totalBlocks, "should have correct total blocks")
		})
	}
}

func TestRenderProgressBar_ColorThresholds(t *testing.T) {
	// Verify progress bars at different thresholds produce output
	// Note: lipgloss may not emit ANSI codes in non-TTY test environments,
	// so we verify the output is correct structurally rather than checking colors
	tests := []struct {
		name    string
		percent float64
	}{
		{"green for low values", 30},
		{"yellow for medium values", 70},
		{"red for high values", 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderProgressBar(tt.percent, 10)
			assert.NotEmpty(t, result, "should produce output")

			// Verify the output contains the expected structure
			stripped := stripANSI(result)
			assert.Contains(t, stripped, "[", "should have opening bracket")
			assert.Contains(t, stripped, "]", "should have closing bracket")
			assert.Contains(t, stripped, "%", "should show percentage")
		})
	}
}

func TestRenderProgressBarSimple_ZeroWidth(t *testing.T) {
	result := RenderProgressBarSimple(50, 0)
	assert.Empty(t, result, "zero width should return empty string")
}

func TestRenderProgressBarSimple_NoBrackets(t *testing.T) {
	result := RenderProgressBarSimple(50, 10)
	stripped := stripANSI(result)

	assert.NotContains(t, stripped, "[", "should not have opening bracket")
	assert.NotContains(t, stripped, "]", "should not have closing bracket")
	assert.Contains(t, stripped, "50%", "should show percentage")
}

func TestRenderProgressBarSimple_FiftyPercent(t *testing.T) {
	result := RenderProgressBarSimple(50, 10)
	stripped := stripANSI(result)

	assert.Contains(t, stripped, "▰▰▰▰▰▱▱▱▱▱", "should have 5 filled and 5 empty")
	assert.Contains(t, stripped, "50%", "should show 50%")
}

func TestRenderProgressBarSimple_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		width   int
	}{
		{"zero percent", 0, 10},
		{"hundred percent", 100, 10},
		{"negative clamped", -50, 10},
		{"over hundred clamped", 200, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderProgressBarSimple(tt.percent, tt.width)
			assert.NotEmpty(t, result, "should produce output")
		})
	}
}

func TestProgressConstants(t *testing.T) {
	assert.Equal(t, '▰', progressFilled, "filled block should be solid square")
	assert.Equal(t, '▱', progressEmpty, "empty block should be empty square")
}

func TestRenderProgressBar_FormatAlignment(t *testing.T) {
	// Test that percentage is right-aligned (3 characters for number)
	tests := []struct {
		percent float64
		suffix  string
	}{
		{0, "   0%"},   // single digit, space padded
		{10, "  10%"},  // two digits, space padded
		{100, " 100%"}, // three digits, space padded
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := RenderProgressBar(tt.percent, 10)
			stripped := stripANSI(result)
			assert.True(t, strings.HasSuffix(stripped, tt.suffix),
				"expected suffix %q, got %q", tt.suffix, stripped)
		})
	}
}

// Helper function to extract just the bar (without brackets and percentage)
func extractBarOnly(s string) string {
	// Find content between brackets
	start := strings.Index(s, "[")
	end := strings.Index(s, "]")
	if start >= 0 && end > start {
		return s[start+1 : end]
	}
	// For simple bar (no brackets), extract up to the space before percentage
	idx := strings.LastIndex(s, " ")
	if idx > 0 {
		return s[:idx]
	}
	return s
}
