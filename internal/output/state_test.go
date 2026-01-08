package output

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/ui"
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

// Connection tracking tests

func TestPhaseTrackerConnectionDisplay(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)

	cd := pt.ConnectionDisplay()
	assert.NotNil(t, cd)
}

func TestPhaseTrackerAddConnectionAttempt(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)
	pt.SetConnectionQuiet(true) // Suppress output for cleaner test

	pt.AddConnectionAttempt("mini-local", ui.StatusTimeout, 2*time.Second, "timeout")
	pt.AddConnectionAttempt("mini", ui.StatusSuccess, 300*time.Millisecond, "")

	attempts := pt.ConnectionAttempts()
	require.Len(t, attempts, 2)

	assert.Equal(t, "mini-local", attempts[0].Alias)
	assert.Equal(t, ui.StatusTimeout, attempts[0].Status)
	assert.Equal(t, 2*time.Second, attempts[0].Latency)

	assert.Equal(t, "mini", attempts[1].Alias)
	assert.Equal(t, ui.StatusSuccess, attempts[1].Status)
}

func TestPhaseTrackerCompleteConnection(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)
	pt.SetConnectionQuiet(true)

	pt.StartConnection()
	pt.AddConnectionAttempt("mini", ui.StatusSuccess, 300*time.Millisecond, "")
	pt.CompleteConnection("gpu-box", "mini")

	assert.Equal(t, "gpu-box", pt.ConnectedHost())
	assert.Equal(t, "mini", pt.ConnectedAlias())

	// Should have recorded the connection phase event
	events := pt.Events()
	require.Len(t, events, 1)
	assert.Equal(t, PhaseConnecting, events[0].Phase)
	assert.True(t, events[0].Success)
	assert.Contains(t, events[0].Message, "gpu-box")
	assert.Contains(t, events[0].Message, "mini")
}

func TestPhaseTrackerCompleteConnectionLocal(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)
	pt.SetConnectionQuiet(true)

	pt.StartConnection()
	pt.AddConnectionAttempt("remote-host", ui.StatusTimeout, 5*time.Second, "")
	pt.CompleteConnectionLocal()

	assert.Equal(t, "local", pt.ConnectedHost())
	assert.Equal(t, "local", pt.ConnectedAlias())

	events := pt.Events()
	require.Len(t, events, 1)
	assert.True(t, events[0].Success)
	assert.Contains(t, events[0].Message, "locally")
}

func TestPhaseTrackerFailConnection(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)
	pt.SetConnectionQuiet(true)

	pt.StartConnection()
	pt.AddConnectionAttempt("host1", ui.StatusTimeout, 5*time.Second, "")
	pt.AddConnectionAttempt("host2", ui.StatusRefused, 0, "")
	pt.FailConnection("all hosts unreachable")

	events := pt.Events()
	require.Len(t, events, 1)
	assert.False(t, events[0].Success)
}

func TestPhaseTrackerOnConnection(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)
	pt.SetConnectionQuiet(true)

	var receivedResults []ConnectionAttemptResult
	pt.OnConnection(func(result ConnectionAttemptResult) {
		receivedResults = append(receivedResults, result)
	})

	pt.AddConnectionAttempt("host1", ui.StatusTimeout, 2*time.Second, "")
	pt.AddConnectionAttempt("host2", ui.StatusSuccess, 300*time.Millisecond, "")

	require.Len(t, receivedResults, 2)
	assert.Equal(t, "host1", receivedResults[0].Alias)
	assert.Equal(t, ui.StatusTimeout, receivedResults[0].Status)
	assert.Equal(t, "host2", receivedResults[1].Alias)
	assert.Equal(t, ui.StatusSuccess, receivedResults[1].Status)
}

func TestPhaseTrackerConnectionAttemptsCopy(t *testing.T) {
	var buf bytes.Buffer
	pt := NewPhaseTracker(&buf)
	pt.SetConnectionQuiet(true)

	pt.AddConnectionAttempt("host1", ui.StatusSuccess, 100*time.Millisecond, "")

	attempts1 := pt.ConnectionAttempts()
	attempts2 := pt.ConnectionAttempts()

	// Modify one copy
	attempts1[0].Alias = "modified"

	// Other copy should be unchanged
	assert.NotEqual(t, attempts1[0].Alias, attempts2[0].Alias)
}

func TestConnectionAttemptResultFields(t *testing.T) {
	result := ConnectionAttemptResult{
		Alias:   "test-host",
		Status:  ui.StatusTimeout,
		Latency: 5 * time.Second,
		Error:   "connection timed out",
	}

	assert.Equal(t, "test-host", result.Alias)
	assert.Equal(t, ui.StatusTimeout, result.Status)
	assert.Equal(t, 5*time.Second, result.Latency)
	assert.Equal(t, "connection timed out", result.Error)
}
