package output

import (
	"io"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/ui"
)

// ConnectionAttemptResult represents the outcome of a single connection attempt.
type ConnectionAttemptResult struct {
	Alias   string
	Status  ui.ConnectionStatus
	Latency time.Duration
	Error   string
}

// Phase represents a distinct stage in the road-runner workflow.
type Phase int

const (
	PhaseConnecting Phase = iota
	PhaseSyncing
	PhaseLocking
	PhaseRunning
	PhaseDone
)

// String returns the display name for a phase.
func (p Phase) String() string {
	switch p {
	case PhaseConnecting:
		return "Connecting"
	case PhaseSyncing:
		return "Syncing"
	case PhaseLocking:
		return "Acquiring lock"
	case PhaseRunning:
		return "Running"
	case PhaseDone:
		return "Done"
	default:
		return "Unknown"
	}
}

// PhaseEvent records a phase transition.
type PhaseEvent struct {
	Phase     Phase
	StartTime time.Time
	EndTime   time.Time
	Success   bool
	Error     error
	Message   string
}

// Duration returns the phase duration.
func (e PhaseEvent) Duration() time.Duration {
	if e.EndTime.IsZero() {
		return time.Since(e.StartTime)
	}
	return e.EndTime.Sub(e.StartTime)
}

// PhaseTracker tracks execution phases and emits transitions.
type PhaseTracker struct {
	mu      sync.Mutex
	current Phase
	events  []PhaseEvent
	display *ui.PhaseDisplay
	spinner *ui.Spinner
	started time.Time
	onPhase func(PhaseEvent)

	// Connection tracking
	connDisplay    *ui.ConnectionDisplay
	connAttempts   []ConnectionAttemptResult
	connectedAlias string
	connectedHost  string
	onConnection   func(ConnectionAttemptResult)
}

// NewPhaseTracker creates a tracker that writes output to w.
func NewPhaseTracker(w io.Writer) *PhaseTracker {
	return &PhaseTracker{
		display:      ui.NewPhaseDisplay(w),
		events:       make([]PhaseEvent, 0),
		connDisplay:  ui.NewConnectionDisplay(w),
		connAttempts: make([]ConnectionAttemptResult, 0),
	}
}

// OnPhase sets a callback for phase transitions.
func (pt *PhaseTracker) OnPhase(fn func(PhaseEvent)) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.onPhase = fn
}

// Start begins a new phase with the given name.
func (pt *PhaseTracker) Start(phase Phase) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	// Stop any existing spinner
	if pt.spinner != nil {
		pt.spinner.Stop()
	}

	pt.current = phase
	pt.started = time.Now()

	// Create and start spinner for this phase
	pt.spinner = ui.NewSpinner(phase.String())
	pt.spinner.Start()

	// Record event start
	event := PhaseEvent{
		Phase:     phase,
		StartTime: pt.started,
	}
	pt.events = append(pt.events, event)
}

// Complete marks the current phase as successful.
func (pt *PhaseTracker) Complete() {
	pt.CompleteWithMessage("")
}

// CompleteWithMessage marks the current phase as successful with a custom message.
func (pt *PhaseTracker) CompleteWithMessage(message string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.spinner != nil {
		pt.spinner.Success()
		pt.spinner = nil
	}

	// Update the last event
	if len(pt.events) > 0 {
		idx := len(pt.events) - 1
		pt.events[idx].EndTime = time.Now()
		pt.events[idx].Success = true
		pt.events[idx].Message = message

		if pt.onPhase != nil {
			pt.onPhase(pt.events[idx])
		}
	}
}

// Fail marks the current phase as failed.
func (pt *PhaseTracker) Fail(err error) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.spinner != nil {
		pt.spinner.Fail()
		pt.spinner = nil
	}

	// Update the last event
	if len(pt.events) > 0 {
		idx := len(pt.events) - 1
		pt.events[idx].EndTime = time.Now()
		pt.events[idx].Success = false
		pt.events[idx].Error = err

		if pt.onPhase != nil {
			pt.onPhase(pt.events[idx])
		}
	}
}

// Skip marks the current phase as skipped.
func (pt *PhaseTracker) Skip(reason string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.spinner != nil {
		pt.spinner.Skip()
		pt.spinner = nil
	}

	// Update the last event
	if len(pt.events) > 0 {
		idx := len(pt.events) - 1
		pt.events[idx].EndTime = time.Now()
		pt.events[idx].Message = reason

		if pt.onPhase != nil {
			pt.onPhase(pt.events[idx])
		}
	}
}

// Current returns the current phase.
func (pt *PhaseTracker) Current() Phase {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.current
}

// Events returns all phase events.
func (pt *PhaseTracker) Events() []PhaseEvent {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	result := make([]PhaseEvent, len(pt.events))
	copy(result, pt.events)
	return result
}

// TotalDuration returns the total time from first phase to last completed phase.
func (pt *PhaseTracker) TotalDuration() time.Duration {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if len(pt.events) == 0 {
		return 0
	}

	start := pt.events[0].StartTime
	end := time.Now()

	for i := len(pt.events) - 1; i >= 0; i-- {
		if !pt.events[i].EndTime.IsZero() {
			end = pt.events[i].EndTime
			break
		}
	}

	return end.Sub(start)
}

// Divider renders a divider line using the display.
func (pt *PhaseTracker) Divider() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.display.Divider()
}

// Display returns the underlying PhaseDisplay for custom rendering.
func (pt *PhaseTracker) Display() *ui.PhaseDisplay {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.display
}

// Summary prints a summary of all phases.
func (pt *PhaseTracker) Summary() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	for _, e := range pt.events {
		name := e.Phase.String()
		if e.Message != "" {
			name = e.Message
		}

		if e.Error != nil {
			pt.display.RenderFailed(name, e.Duration(), e.Error)
		} else if e.Success {
			pt.display.RenderSuccess(name, e.Duration())
		} else {
			pt.display.RenderSkipped(name, "")
		}
	}
}

// Connection tracking methods

// OnConnection sets a callback for connection attempt events.
func (pt *PhaseTracker) OnConnection(fn func(ConnectionAttemptResult)) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.onConnection = fn
}

// SetConnectionQuiet enables or disables quiet mode for connection display.
// In quiet mode, individual attempts are not shown, only the final result.
func (pt *PhaseTracker) SetConnectionQuiet(quiet bool) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.connDisplay.SetQuiet(quiet)
}

// StartConnection begins the connection phase with animated output.
func (pt *PhaseTracker) StartConnection() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.current = PhaseConnecting
	pt.started = time.Now()
	pt.connAttempts = make([]ConnectionAttemptResult, 0)
	pt.connDisplay.Start()

	// Record event start
	event := PhaseEvent{
		Phase:     PhaseConnecting,
		StartTime: pt.started,
	}
	pt.events = append(pt.events, event)
}

// AddConnectionAttempt records a connection attempt and updates the display.
func (pt *PhaseTracker) AddConnectionAttempt(alias string, status ui.ConnectionStatus, latency time.Duration, errMsg string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	result := ConnectionAttemptResult{
		Alias:   alias,
		Status:  status,
		Latency: latency,
		Error:   errMsg,
	}
	pt.connAttempts = append(pt.connAttempts, result)

	// Update display
	pt.connDisplay.AddAttempt(alias, status, latency, errMsg)

	// Emit callback
	if pt.onConnection != nil {
		pt.onConnection(result)
	}
}

// CompleteConnection marks the connection phase as successful.
func (pt *PhaseTracker) CompleteConnection(hostName, alias string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.connectedHost = hostName
	pt.connectedAlias = alias
	pt.connDisplay.Success(hostName, alias)

	// Update the connection phase event
	if len(pt.events) > 0 {
		idx := len(pt.events) - 1
		pt.events[idx].EndTime = time.Now()
		pt.events[idx].Success = true
		pt.events[idx].Message = "Connected to " + hostName + " via " + alias

		if pt.onPhase != nil {
			pt.onPhase(pt.events[idx])
		}
	}
}

// CompleteConnectionLocal marks the connection phase as using local fallback.
func (pt *PhaseTracker) CompleteConnectionLocal() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.connectedHost = "local"
	pt.connectedAlias = "local"
	pt.connDisplay.SuccessLocal()

	// Update the connection phase event
	if len(pt.events) > 0 {
		idx := len(pt.events) - 1
		pt.events[idx].EndTime = time.Now()
		pt.events[idx].Success = true
		pt.events[idx].Message = "Running locally (all remote hosts unreachable)"

		if pt.onPhase != nil {
			pt.onPhase(pt.events[idx])
		}
	}
}

// FailConnection marks the connection phase as failed.
func (pt *PhaseTracker) FailConnection(errMsg string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.connDisplay.Fail(errMsg)

	// Update the connection phase event
	if len(pt.events) > 0 {
		idx := len(pt.events) - 1
		pt.events[idx].EndTime = time.Now()
		pt.events[idx].Success = false

		if pt.onPhase != nil {
			pt.onPhase(pt.events[idx])
		}
	}
}

// ConnectionAttempts returns all recorded connection attempts.
func (pt *PhaseTracker) ConnectionAttempts() []ConnectionAttemptResult {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	result := make([]ConnectionAttemptResult, len(pt.connAttempts))
	copy(result, pt.connAttempts)
	return result
}

// ConnectedAlias returns the alias that successfully connected.
func (pt *PhaseTracker) ConnectedAlias() string {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.connectedAlias
}

// ConnectedHost returns the host name that was connected to.
func (pt *PhaseTracker) ConnectedHost() string {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.connectedHost
}

// ConnectionDisplay returns the underlying ConnectionDisplay.
func (pt *PhaseTracker) ConnectionDisplay() *ui.ConnectionDisplay {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.connDisplay
}
