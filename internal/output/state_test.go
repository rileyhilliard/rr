package output

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPhaseString(t *testing.T) {
	tests := []struct {
		phase Phase
		want  string
	}{
		{PhaseConnecting, "Connecting"},
		{PhaseSyncing, "Syncing"},
		{PhaseLocking, "Acquiring lock"},
		{PhaseRunning, "Running"},
		{PhaseDone, "Done"},
		{Phase(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.phase.String())
		})
	}
}

func TestPhaseEventDuration(t *testing.T) {
	start := time.Now()
	e := PhaseEvent{
		Phase:     PhaseConnecting,
		StartTime: start,
		EndTime:   start.Add(500 * time.Millisecond),
	}

	assert.Equal(t, 500*time.Millisecond, e.Duration())
}

func TestPhaseEventDurationInProgress(t *testing.T) {
	e := PhaseEvent{
		Phase:     PhaseConnecting,
		StartTime: time.Now().Add(-100 * time.Millisecond),
	}

	d := e.Duration()
	assert.GreaterOrEqual(t, d, 90*time.Millisecond)
}

func TestNewPhaseTracker(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	assert.NotNil(t, pt)
	assert.Empty(t, pt.Events())
}

func TestPhaseTrackerStart(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Start(PhaseConnecting)

	assert.Equal(t, PhaseConnecting, pt.Current())
	events := pt.Events()
	require.Len(t, events, 1)
	assert.Equal(t, PhaseConnecting, events[0].Phase)
	assert.False(t, events[0].StartTime.IsZero())
}

func TestPhaseTrackerComplete(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Start(PhaseConnecting)
	time.Sleep(20 * time.Millisecond)
	pt.Complete()

	events := pt.Events()
	require.Len(t, events, 1)
	assert.True(t, events[0].Success)
	assert.False(t, events[0].EndTime.IsZero())
}

func TestPhaseTrackerCompleteWithMessage(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Start(PhaseConnecting)
	pt.CompleteWithMessage("Connected to mini via SSH")

	events := pt.Events()
	require.Len(t, events, 1)
	assert.Equal(t, "Connected to mini via SSH", events[0].Message)
}

func TestPhaseTrackerFail(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	testErr := errors.New("connection timeout")
	pt.Start(PhaseConnecting)
	pt.Fail(testErr)

	events := pt.Events()
	require.Len(t, events, 1)
	assert.False(t, events[0].Success)
	assert.Equal(t, testErr, events[0].Error)
}

func TestPhaseTrackerSkip(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Start(PhaseSyncing)
	pt.Skip("no changes")

	events := pt.Events()
	require.Len(t, events, 1)
	assert.Equal(t, "no changes", events[0].Message)
}

func TestPhaseTrackerMultiplePhases(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Start(PhaseConnecting)
	pt.Complete()

	pt.Start(PhaseSyncing)
	pt.Complete()

	pt.Start(PhaseLocking)
	pt.Complete()

	events := pt.Events()
	require.Len(t, events, 3)

	assert.Equal(t, PhaseConnecting, events[0].Phase)
	assert.Equal(t, PhaseSyncing, events[1].Phase)
	assert.Equal(t, PhaseLocking, events[2].Phase)
}

func TestPhaseTrackerOnPhase(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	var receivedEvents []PhaseEvent
	pt.OnPhase(func(e PhaseEvent) {
		receivedEvents = append(receivedEvents, e)
	})

	pt.Start(PhaseConnecting)
	pt.Complete()

	pt.Start(PhaseSyncing)
	pt.Fail(errors.New("sync failed"))

	require.Len(t, receivedEvents, 2)
	assert.True(t, receivedEvents[0].Success)
	assert.False(t, receivedEvents[1].Success)
}

func TestPhaseTrackerTotalDuration(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Start(PhaseConnecting)
	time.Sleep(50 * time.Millisecond)
	pt.Complete()

	pt.Start(PhaseSyncing)
	time.Sleep(50 * time.Millisecond)
	pt.Complete()

	d := pt.TotalDuration()
	assert.GreaterOrEqual(t, d, 90*time.Millisecond)
}

func TestPhaseTrackerTotalDurationEmpty(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	assert.Equal(t, time.Duration(0), pt.TotalDuration())
}

func TestPhaseTrackerDivider(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Divider()

	assert.Contains(t, buf.String(), "━")
}

func TestPhaseTrackerDisplay(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	d := pt.Display()
	assert.NotNil(t, d)
}

func TestPhaseTrackerSummary(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Start(PhaseConnecting)
	pt.Complete()

	pt.Start(PhaseSyncing)
	pt.Fail(errors.New("failed"))

	buf.Reset()
	pt.Summary()

	output := buf.String()
	// Should contain both phases
	assert.NotEmpty(t, output)
}

func TestPhaseEventsAreCopied(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	pt.Start(PhaseConnecting)
	pt.Complete()

	events1 := pt.Events()
	events2 := pt.Events()

	// Modifying one shouldn't affect the other
	events1[0].Message = "modified"
	assert.NotEqual(t, events1[0].Message, events2[0].Message)
}
