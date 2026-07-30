package monitor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetricColor(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		expect  string // Color name for readability
	}{
		{"healthy low", 0.0, "healthy"},
		{"healthy mid", 50.0, "healthy"},
		{"healthy near threshold", 69.9, "healthy"},
		{"warning at threshold", 70.0, "warning"},
		{"warning mid", 80.0, "warning"},
		{"warning near critical", 89.9, "warning"},
		{"critical at threshold", 90.0, "critical"},
		{"critical high", 95.0, "critical"},
		{"critical max", 100.0, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MetricColor(tt.percent)
			switch tt.expect {
			case "healthy":
				assert.Equal(t, ColorHealthy, result)
			case "warning":
				assert.Equal(t, ColorWarning, result)
			case "critical":
				assert.Equal(t, ColorCritical, result)
			}
		})
	}
}

func TestMetricColorWithThresholds(t *testing.T) {
	tests := []struct {
		name     string
		percent  float64
		warning  int
		critical int
		expect   string
	}{
		{"custom thresholds - healthy", 40.0, 50, 80, "healthy"},
		{"custom thresholds - warning", 60.0, 50, 80, "warning"},
		{"custom thresholds - critical", 85.0, 50, 80, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MetricColorWithThresholds(tt.percent, tt.warning, tt.critical)
			switch tt.expect {
			case "healthy":
				assert.Equal(t, ColorHealthy, result)
			case "warning":
				assert.Equal(t, ColorWarning, result)
			case "critical":
				assert.Equal(t, ColorCritical, result)
			}
		})
	}
}

func TestMetricStyle(t *testing.T) {
	style := MetricStyle(50.0)
	assert.NotNil(t, style)
	// Style should have foreground set
}

func TestMetricStyleWithThresholds(t *testing.T) {
	style := MetricStyleWithThresholds(50.0, 40, 80)
	assert.NotNil(t, style)
}

func TestSectionHeader(t *testing.T) {
	tests := []struct {
		name  string
		title string
		value string
		width int
	}{
		{"normal width", "CPU", "75%", 50},
		{"narrow width", "RAM", "50%", 15},
		{"very narrow", "X", "Y", 10},
		{"minimum width", "A", "B", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SectionHeader(tt.title, tt.value, tt.width)
			assert.NotEmpty(t, result)
			// Should contain rounded corners - Gen Z style
			assert.Contains(t, result, "╭")
			assert.Contains(t, result, "╮")
		})
	}
}

func TestSectionFooter(t *testing.T) {
	tests := []struct {
		name  string
		width int
	}{
		{"normal width", 50},
		{"narrow width", 10},
		{"minimum width", 2},
		{"below minimum", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SectionFooter(tt.width)
			assert.NotEmpty(t, result)
			// Should contain rounded corners - Gen Z style
			assert.Contains(t, result, "╰")
			assert.Contains(t, result, "╯")
		})
	}
}

func TestSectionContentLine(t *testing.T) {
	tests := []struct {
		name    string
		content string
		width   int
	}{
		{"normal content", "Hello World", 40},
		{"empty content", "", 20},
		{"narrow width", "Test", 10},
		{"minimum width", "X", 4},
		{"below minimum", "Y", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SectionContentLine(tt.content, tt.width)
			assert.NotEmpty(t, result)
			// Should contain borders on both sides
			assert.True(t, strings.Contains(result, "│"))
		})
	}
}

func TestThresholdConstants(t *testing.T) {
	assert.Equal(t, 70.0, WarningThreshold)
	assert.Equal(t, 90.0, CriticalThreshold)
}

func TestStatusIndicatorConstants(t *testing.T) {
	assert.Equal(t, "◉", StatusIdle)        // Filled target - ready for work
	assert.Equal(t, "⣿", StatusRunning)     // Braille full - active
	assert.Equal(t, "◌", StatusUnreachable) // Cyber glyph
}

func TestColorConstants(t *testing.T) {
	// Verify color constants are defined
	assert.NotEmpty(t, string(ColorDarkBg))
	assert.NotEmpty(t, string(ColorSurfaceBg))
	assert.NotEmpty(t, string(ColorBorder))
	assert.NotEmpty(t, string(ColorHealthy))
	assert.NotEmpty(t, string(ColorWarning))
	assert.NotEmpty(t, string(ColorCritical))
	assert.NotEmpty(t, string(ColorTextPrimary))
	assert.NotEmpty(t, string(ColorTextSecondary))
	assert.NotEmpty(t, string(ColorTextMuted))
	assert.NotEmpty(t, string(ColorAccent))
	assert.NotEmpty(t, string(ColorAccentDim))
	assert.NotEmpty(t, string(ColorGraph))
}

func TestRunningSpinnerFrames(t *testing.T) {
	// Verify running spinner frames are defined
	assert.Len(t, RunningSpinnerFrames, 8)
	// Verify they're braille characters
	for _, frame := range RunningSpinnerFrames {
		assert.NotEmpty(t, frame)
	}
}

func TestSpinnerColorFrames(t *testing.T) {
	// Verify spinner color frames are defined
	assert.Len(t, SpinnerColorFrames, 8)
	for _, color := range SpinnerColorFrames {
		assert.NotEmpty(t, string(color))
	}
}

func TestGetSpinnerColor(t *testing.T) {
	// Verify color cycling works
	for i := 0; i < 16; i++ {
		color := GetSpinnerColor(i)
		assert.NotEmpty(t, string(color))
	}
	// Verify it wraps around
	assert.Equal(t, GetSpinnerColor(0), GetSpinnerColor(8))
	assert.Equal(t, GetSpinnerColor(1), GetSpinnerColor(9))
}

func TestGetRunningSpinner(t *testing.T) {
	// Test each frame
	for i := 0; i < 8; i++ {
		char, style := GetRunningSpinner(i)
		assert.Equal(t, RunningSpinnerFrames[i], char)
		assert.NotNil(t, style)
	}

	// Test wrapping
	char0, _ := GetRunningSpinner(0)
	char8, _ := GetRunningSpinner(8)
	assert.Equal(t, char0, char8)
}
