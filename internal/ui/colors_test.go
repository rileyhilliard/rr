package ui

import (
	"bytes"
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestColorConstants(t *testing.T) {
	// Test that color constants are valid hex colors
	colors := []lipgloss.Color{
		ColorNeonPink,
		ColorNeonCyan,
		ColorNeonPurple,
		ColorNeonGreen,
		ColorNeonOrange,
		ColorNeonAmber,
		ColorDeepVoid,
		ColorDarkSurface,
		ColorGlassBorder,
		ColorSuccess,
		ColorError,
		ColorWarning,
		ColorInfo,
		ColorPrimary,
		ColorSecondary,
		ColorMuted,
	}

	for _, color := range colors {
		// Color should be a non-empty string starting with #
		colorStr := string(color)
		assert.NotEmpty(t, colorStr, "color should not be empty")
		assert.True(t, colorStr[0] == '#', "color should start with #: %s", colorStr)
		assert.Len(t, colorStr, 7, "color should be 7 chars (#RRGGBB): %s", colorStr)
	}
}

func TestGradientColors(t *testing.T) {
	assert.NotEmpty(t, GradientColors)
	assert.Len(t, GradientColors, 4)

	for i, color := range GradientColors {
		colorStr := string(color)
		assert.NotEmpty(t, colorStr, "gradient color %d should not be empty", i)
		assert.True(t, colorStr[0] == '#', "gradient color should start with #")
	}
}

func TestSuccessStyle(t *testing.T) {
	style := SuccessStyle()
	rendered := style.Render("success")
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "success")
}

func TestErrorStyle(t *testing.T) {
	style := ErrorStyle()
	rendered := style.Render("error")
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "error")
}

func TestWarningStyle(t *testing.T) {
	style := WarningStyle()
	rendered := style.Render("warning")
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "warning")
}

func TestInfoStyle(t *testing.T) {
	style := InfoStyle()
	rendered := style.Render("info")
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "info")
}

func TestMutedStyle(t *testing.T) {
	style := MutedStyle()
	rendered := style.Render("muted")
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "muted")
}

func TestSymbolWarning(t *testing.T) {
	assert.NotEmpty(t, SymbolWarning)
	assert.Equal(t, "⚠", SymbolWarning)
}

func TestPrintWarning(t *testing.T) {
	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	PrintWarning("test warning message")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	assert.Contains(t, output, "test warning message")
	assert.Contains(t, output, SymbolWarning)
}

func TestDisableColors(t *testing.T) {
	// This test verifies DisableColors doesn't panic
	// We can't easily verify the color profile change in tests
	assert.NotPanics(t, func() {
		DisableColors()
	})

	// After DisableColors, styles should still work but produce plain text
	style := SuccessStyle()
	rendered := style.Render("test")
	assert.Contains(t, rendered, "test")
}

func TestStylesAreFunctional(t *testing.T) {
	// Test that all styles can render text without panicking
	styles := []struct {
		name  string
		style lipgloss.Style
	}{
		{"Success", SuccessStyle()},
		{"Error", ErrorStyle()},
		{"Warning", WarningStyle()},
		{"Info", InfoStyle()},
		{"Muted", MutedStyle()},
	}

	for _, tt := range styles {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				result := tt.style.Render("test text")
				assert.NotEmpty(t, result)
			})
		})
	}
}
