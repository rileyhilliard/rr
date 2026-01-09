package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewInlineProgress(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Syncing", &buf)

	assert.Equal(t, "Syncing", p.label)
	assert.Equal(t, 30, p.width)
	assert.False(t, p.running)
}

func TestInlineProgressSetWidth(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Test", &buf)

	p.SetWidth(50)

	assert.Equal(t, 50, p.width)
}

func TestInlineProgressStartStop(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Test", &buf)

	p.Start()
	assert.True(t, p.running)

	time.Sleep(50 * time.Millisecond) // Let it render once

	p.Stop()
	assert.False(t, p.running)
}

func TestInlineProgressUpdate(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Test", &buf)

	p.Update(0.5, "10MB/s", "0:00:30", 1024*1024)

	assert.Equal(t, 0.5, p.percent)
	assert.Equal(t, "10MB/s", p.speed)
	assert.Equal(t, "0:00:30", p.eta)
	assert.Equal(t, int64(1024*1024), p.bytes)
}

func TestInlineProgressSuccess(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Syncing files", &buf)

	p.Start()
	time.Sleep(20 * time.Millisecond)
	p.Update(1.0, "", "", 5*1024*1024)
	p.Success()

	output := buf.String()
	assert.Contains(t, output, SymbolComplete)
	assert.Contains(t, output, "Syncing files")
}

func TestInlineProgressFail(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Syncing files", &buf)

	p.Start()
	time.Sleep(20 * time.Millisecond)
	p.Fail()

	output := buf.String()
	assert.Contains(t, output, SymbolFail)
	assert.Contains(t, output, "Syncing files")
}

func TestInlineProgressRenderBar(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Test", &buf)
	p.width = 10

	tests := []struct {
		percent     float64
		filledCount int
	}{
		{0.0, 0},
		{0.5, 5},
		{1.0, 10},
		{0.33, 3},
	}

	for _, tt := range tests {
		bar := p.renderBarWithPercent(tt.percent)

		// Count filled characters (█)
		filledCount := strings.Count(bar, "█")
		assert.Equal(t, tt.filledCount, filledCount, "For percent %v", tt.percent)
	}
}

func TestStripAnsi(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"plain text", "plain text"},
		{"\x1b[32mgreen\x1b[0m", "green"},
		{"\x1b[1;31mbold red\x1b[0m", "bold red"},
		{"no \x1b[34mblue\x1b[0m codes", "no blue codes"},
	}

	for _, tt := range tests {
		result := stripAnsi(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestProgressWriter(t *testing.T) {
	var buf bytes.Buffer
	var passthrough bytes.Buffer

	p := NewInlineProgress("Sync", &buf)
	w := NewProgressWriter(p, &passthrough)

	// Write rsync progress data
	input := "    1,000,000  50%    5.00MB/s    0:00:30\n"
	n, err := w.Write([]byte(input))

	assert.NoError(t, err)
	assert.Equal(t, len(input), n)

	// Check passthrough
	assert.Equal(t, input, passthrough.String())

	// Check progress was updated
	assert.InDelta(t, 0.5, p.percent, 0.01)
	assert.Equal(t, "5.00MB/s", p.speed)
	assert.Equal(t, int64(1000000), p.bytes)
}

func TestProgressWriterNoPassthrough(t *testing.T) {
	var buf bytes.Buffer

	p := NewInlineProgress("Sync", &buf)
	w := NewProgressWriter(p, nil) // No passthrough

	input := "    500,000  25%    2.50MB/s    0:01:00\n"
	n, err := w.Write([]byte(input))

	assert.NoError(t, err)
	assert.Equal(t, len(input), n)

	// Progress should still be updated
	assert.InDelta(t, 0.25, p.percent, 0.01)
}

func TestEaseOutQuad(t *testing.T) {
	// At t=0, should be 0
	assert.Equal(t, 0.0, easeOutQuad(0.0))

	// At t=1, should be 1
	assert.Equal(t, 1.0, easeOutQuad(1.0))

	// At t=0.5, should be 0.75 (ease-out decelerates toward end)
	assert.Equal(t, 0.75, easeOutQuad(0.5))

	// Verify it's monotonically increasing
	prev := 0.0
	for i := 0.0; i <= 1.0; i += 0.1 {
		val := easeOutQuad(i)
		assert.GreaterOrEqual(t, val, prev, "easeOutQuad should be monotonically increasing")
		prev = val
	}
}

func TestFakeProgressCapsAt99(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Test", &buf)
	p.startTime = time.Now().Add(-15 * time.Second) // 15 seconds ago

	fake := p.calculateFakeProgress()

	// After 10+ seconds, should cap at 99%
	assert.Equal(t, 0.99, fake)
}

func TestEffectiveProgressUsesMaxOfRealAndFake(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Test", &buf)
	p.useFake = true
	p.startTime = time.Now().Add(-5 * time.Second) // 5 seconds ago

	// Real progress is 0, fake should be > 0
	p.percent = 0
	p.mu.Lock()
	effective := p.effectiveProgressLocked()
	p.mu.Unlock()
	assert.Greater(t, effective, 0.0, "With real=0, should use fake progress")

	// Real progress is higher than fake, should use real
	p.percent = 0.95
	p.mu.Lock()
	effective = p.effectiveProgressLocked()
	p.mu.Unlock()
	assert.Equal(t, 0.95, effective, "Should use real progress when it's higher")
}

func TestFakeProgressDisabled(t *testing.T) {
	var buf bytes.Buffer
	p := NewInlineProgress("Test", &buf)
	p.SetUseFakeProgress(false)
	p.startTime = time.Now().Add(-5 * time.Second)

	p.percent = 0.1
	p.mu.Lock()
	effective := p.effectiveProgressLocked()
	p.mu.Unlock()

	assert.Equal(t, 0.1, effective, "With fake disabled, should only use real progress")
}
