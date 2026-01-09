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
		p.percent = tt.percent
		bar := p.renderBar()

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
