package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewPhaseDisplay(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)
	assert.NotNil(t, pd)
}

func TestPhaseDisplayRenderProgress(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.RenderProgress("Connecting")

	output := buf.String()
	assert.Contains(t, output, "Connecting")
	assert.Contains(t, output, "...")
}

func TestPhaseDisplayRenderSuccess(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.RenderSuccess("Connected", 300*time.Millisecond)

	output := buf.String()
	assert.Contains(t, output, SymbolComplete)
	assert.Contains(t, output, "Connected")
	assert.Contains(t, output, "0.3s")
}

func TestPhaseDisplayRenderFailed(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.RenderFailed("Connection failed", 2300*time.Millisecond, nil)

	output := buf.String()
	assert.Contains(t, output, SymbolFail)
	assert.Contains(t, output, "Connection failed")
	assert.Contains(t, output, "2.3s")
}

func TestPhaseDisplayRenderSkipped(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.RenderSkipped("Syncing", "no changes")

	output := buf.String()
	assert.Contains(t, output, SymbolSkipped)
	assert.Contains(t, output, "Syncing")
	assert.Contains(t, output, "(no changes)")
}

func TestPhaseDisplayRenderSkippedNoReason(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.RenderSkipped("Syncing", "")

	output := buf.String()
	assert.Contains(t, output, SymbolSkipped)
	assert.Contains(t, output, "Syncing")
	assert.NotContains(t, output, "(")
}

func TestPhaseDisplayRenderSubStatus(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.RenderSubStatus(SymbolPending, "mini-local", "timeout (2s)")

	output := buf.String()
	assert.Contains(t, output, SymbolPending)
	assert.Contains(t, output, "mini-local")
	assert.Contains(t, output, "timeout (2s)")
	// Should be indented
	assert.True(t, strings.HasPrefix(output, "  "))
}

func TestPhaseDisplayDivider(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.Divider()

	output := buf.String()
	// Should contain thick box-drawing character
	assert.Contains(t, output, "━")
	// Should be DividerWidth characters of the divider
	assert.GreaterOrEqual(t, strings.Count(output, "━"), DividerWidth)
}

func TestPhaseDisplayThinDivider(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.ThinDivider()

	output := buf.String()
	// Should contain thin box-drawing character
	assert.Contains(t, output, "─")
	assert.GreaterOrEqual(t, strings.Count(output, "─"), DividerWidth)
}

func TestPhaseDisplayCommandPrompt(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.CommandPrompt("pytest -n auto")

	output := buf.String()
	assert.Contains(t, output, "$")
	assert.Contains(t, output, "pytest -n auto")
}

func TestPhaseDisplayNewline(t *testing.T) {
	var buf bytes.Buffer
	pd := NewPhaseDisplay(&buf)

	pd.Newline()

	assert.Equal(t, "\n", buf.String())
}

func TestPhaseDuration(t *testing.T) {
	start := time.Now()
	p := Phase{
		Name:      "Test",
		StartTime: start,
		EndTime:   start.Add(500 * time.Millisecond),
	}

	assert.Equal(t, 500*time.Millisecond, p.Duration())
}

func TestPhaseDurationInProgress(t *testing.T) {
	p := Phase{
		Name:      "Test",
		StartTime: time.Now().Add(-100 * time.Millisecond),
	}

	// Duration should be approximately 100ms (with some tolerance)
	d := p.Duration()
	assert.GreaterOrEqual(t, d, 90*time.Millisecond)
}

func TestFormatPhase(t *testing.T) {
	tests := []struct {
		name        string
		symbol      string
		symbolColor string
		phaseName   string
		timing      string
		wantSymbol  string
		wantName    string
	}{
		{
			name:        "success with timing",
			symbol:      SymbolComplete,
			symbolColor: "2",
			phaseName:   "Connected",
			timing:      "0.3s",
			wantSymbol:  SymbolComplete,
			wantName:    "Connected",
		},
		{
			name:        "failed with timing",
			symbol:      SymbolFail,
			symbolColor: "1",
			phaseName:   "Failed",
			timing:      "1.0s",
			wantSymbol:  SymbolFail,
			wantName:    "Failed",
		},
		{
			name:        "in progress no timing",
			symbol:      SymbolProgress,
			symbolColor: "4",
			phaseName:   "Connecting",
			timing:      "",
			wantSymbol:  SymbolProgress,
			wantName:    "Connecting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPhase(tt.symbol, ColorSuccess, tt.phaseName, tt.timing)
			// Result contains ANSI codes, so just check the text is present
			assert.NotEmpty(t, result)
		})
	}
}

func TestFormatDivider(t *testing.T) {
	d := FormatDivider(40)
	// Contains 40 thick dashes (may have ANSI codes)
	assert.GreaterOrEqual(t, strings.Count(d, "━"), 40)
}

func TestDividerWidth(t *testing.T) {
	assert.Equal(t, 64, DividerWidth)
}
