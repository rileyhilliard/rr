package ui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBubblesSpinnerFrames(t *testing.T) {
	// Verify braille scan frames are configured correctly
	assert.Equal(t, []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}, SpinnerFrames.Frames)
	assert.Equal(t, time.Second/16, SpinnerFrames.FPS)
}

func TestNewSpinnerComponent(t *testing.T) {
	sp := NewSpinnerComponent("Testing")

	assert.Equal(t, "Testing", sp.Label)
	assert.Equal(t, SpinnerComponentPending, sp.State)
	assert.True(t, sp.StartTime.IsZero())
}

func TestSpinnerComponentStart(t *testing.T) {
	sp := NewSpinnerComponent("Testing")

	cmd := sp.Start()

	assert.Equal(t, SpinnerComponentInProgress, sp.State)
	assert.False(t, sp.StartTime.IsZero())
	assert.NotNil(t, cmd, "Start should return a tick command")
}

func TestSpinnerComponentStateTransitions(t *testing.T) {
	tests := []struct {
		name     string
		action   func(*SpinnerComponent)
		expected SpinnerComponentState
	}{
		{"Success", func(s *SpinnerComponent) { s.Success() }, SpinnerComponentSuccess},
		{"Fail", func(s *SpinnerComponent) { s.Fail() }, SpinnerComponentFailed},
		{"Skip", func(s *SpinnerComponent) { s.Skip() }, SpinnerComponentSkipped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := NewSpinnerComponent("Testing")
			sp.Start()

			tt.action(&sp)

			assert.Equal(t, tt.expected, sp.State)
		})
	}
}

func TestSpinnerComponentElapsed(t *testing.T) {
	sp := NewSpinnerComponent("Testing")

	// Before start, elapsed should be 0
	assert.Equal(t, time.Duration(0), sp.Elapsed())

	sp.Start()
	time.Sleep(10 * time.Millisecond)

	elapsed := sp.Elapsed()
	assert.True(t, elapsed >= 10*time.Millisecond, "Elapsed should be at least 10ms, got %v", elapsed)
}

func TestSpinnerComponentView(t *testing.T) {
	tests := []struct {
		name     string
		state    SpinnerComponentState
		contains string
	}{
		{"Pending", SpinnerComponentPending, SymbolPending},
		{"Success", SpinnerComponentSuccess, SymbolComplete},
		{"Failed", SpinnerComponentFailed, SymbolFail},
		{"Skipped", SpinnerComponentSkipped, SymbolSkipped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := NewSpinnerComponent("Testing")
			sp.State = tt.state
			sp.StartTime = time.Now() // Set start time for duration calculation

			view := sp.View()

			assert.Contains(t, view, tt.contains, "View should contain symbol %s", tt.contains)
			assert.Contains(t, view, "Testing", "View should contain label")
		})
	}
}

func TestSpinnerComponentViewInProgress(t *testing.T) {
	sp := NewSpinnerComponent("Loading")
	sp.Start()

	view := sp.View()

	// In progress view should have the spinner frame and ellipsis
	assert.Contains(t, view, "Loading...")
	// Should contain one of the spinner frames
	hasFrame := false
	for _, frame := range SpinnerFrames.Frames {
		if containsString(view, frame) {
			hasFrame = true
			break
		}
	}
	assert.True(t, hasFrame, "View should contain a spinner frame")
}

func TestSpinnerComponentUpdate(t *testing.T) {
	sp := NewSpinnerComponent("Testing")
	sp.Start()

	// Send a tick message
	tickMsg := spinner.TickMsg{}
	updated, cmd := sp.Update(tickMsg)

	// Update should return a command (next tick)
	assert.NotNil(t, cmd, "Update should return a command when in progress")
	require.Equal(t, SpinnerComponentInProgress, updated.State)
}

func TestSpinnerComponentUpdateWhenNotInProgress(t *testing.T) {
	sp := NewSpinnerComponent("Testing")
	// Don't start - leave in pending state

	tickMsg := spinner.TickMsg{}
	updated, cmd := sp.Update(tickMsg)

	// Should not process ticks when not in progress
	assert.Nil(t, cmd, "Update should not return command when not in progress")
	assert.Equal(t, SpinnerComponentPending, updated.State)
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || containsString(s[1:], substr)))
}
