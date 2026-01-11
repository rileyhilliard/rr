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

func TestProgressBar(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		percent float64
	}{
		{"zero percent", 10, 0.0},
		{"50 percent", 10, 50.0},
		{"100 percent", 10, 100.0},
		{"negative clamped", 10, -10.0},
		{"over 100 clamped", 10, 150.0},
		{"small width", 3, 50.0},
		{"very small width", 2, 50.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProgressBar(tt.width, tt.percent)
			assert.NotEmpty(t, result)
			// Should contain brackets
			assert.Contains(t, result, "[")
			assert.Contains(t, result, "]")
		})
	}
}

func TestCompactProgressBar(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		percent float64
	}{
		{"zero percent", 10, 0.0},
		{"50 percent", 10, 50.0},
		{"100 percent", 10, 100.0},
		{"negative clamped", 10, -10.0},
		{"over 100 clamped", 10, 150.0},
		{"width 1", 1, 50.0},
		{"width 0", 0, 50.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompactProgressBar(tt.width, tt.percent)
			assert.NotEmpty(t, result)
			// Should NOT contain decorative brackets (but may have ANSI escape codes)
			assert.NotContains(t, result, "[█")
			assert.NotContains(t, result, "[░")
			assert.NotContains(t, result, "]")
		})
	}
}

func TestCompactProgressBarWithThresholds(t *testing.T) {
	result := CompactProgressBarWithThresholds(10, 60.0, 50, 80)
	assert.NotEmpty(t, result)
}

func TestThinProgressBar(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		percent float64
	}{
		{"zero percent", 10, 0.0},
		{"50 percent", 10, 50.0},
		{"100 percent", 10, 100.0},
		{"small width", 1, 50.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ThinProgressBar(tt.width, tt.percent)
			assert.NotEmpty(t, result)
		})
	}
}

func TestThinProgressBarWithThresholds(t *testing.T) {
	result := ThinProgressBarWithThresholds(10, 60.0, 50, 80)
	assert.NotEmpty(t, result)
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
			// Should contain corners
			assert.Contains(t, result, "┌")
			assert.Contains(t, result, "┐")
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
			// Should contain corners
			assert.Contains(t, result, "└")
			assert.Contains(t, result, "┘")
		})
	}
}

func TestSectionBorder(t *testing.T) {
	result := SectionBorder()
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "│")
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
	assert.Equal(t, "●", StatusConnected)
	assert.Equal(t, "○", StatusUnreachable)
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
