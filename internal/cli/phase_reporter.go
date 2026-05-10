package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/rileyhilliard/rr/internal/ui"
)

// PhaseReporter abstracts output for workflow phases. PrettyReporter wraps
// the existing spinner/phase display; StructuredReporter emits JSON events
// to stderr while leaving stdout clean for command output.
type PhaseReporter interface {
	PhaseStart(phase string)
	PhaseComplete(phase, host string, duration time.Duration)
	PhaseFailed(phase string, err error)
	PhaseSkipped(phase, reason string)
	Divider()
	ThinDivider()
	CommandPrompt(command string)
	CommandComplete(exitCode int, host string, totalDuration, execDuration time.Duration)
}

// NewPhaseReporter returns a StructuredReporter (default) or PrettyReporter
// based on the current output mode.
func NewPhaseReporter(pd *ui.PhaseDisplay) PhaseReporter {
	if PrettyMode() {
		return &PrettyReporter{pd: pd}
	}
	return &StructuredReporter{}
}

// PrettyReporter wraps the existing PhaseDisplay, spinners, and lipgloss
// rendering. This is what you get with --pretty.
type PrettyReporter struct {
	pd *ui.PhaseDisplay
}

func (r *PrettyReporter) PhaseStart(phase string) {
	r.pd.RenderProgress(phase)
}

func (r *PrettyReporter) PhaseComplete(phase, host string, duration time.Duration) {
	msg := phase
	if host != "" {
		msg = fmt.Sprintf("%s (%s)", phase, host)
	}
	r.pd.RenderSuccess(msg, duration)
}

func (r *PrettyReporter) PhaseFailed(phase string, err error) {
	r.pd.RenderFailed(phase, 0, err)
}

func (r *PrettyReporter) PhaseSkipped(phase, reason string) {
	r.pd.RenderSkipped(phase, reason)
}

func (r *PrettyReporter) Divider() {
	r.pd.Divider()
}

func (r *PrettyReporter) ThinDivider() {
	r.pd.ThinDivider()
}

func (r *PrettyReporter) CommandPrompt(command string) {
	r.pd.CommandPrompt(command)
}

func (r *PrettyReporter) CommandComplete(exitCode int, host string, totalDuration, execDuration time.Duration) {
	renderFinalStatus(r.pd, exitCode, totalDuration, execDuration, host)
}

// StructuredReporter emits JSON events to stderr. stdout is left clean
// for raw command output.
type StructuredReporter struct{}

func (r *StructuredReporter) PhaseStart(phase string) {
	WritePhaseEvent(PhaseEvent{
		Type:   "phase",
		Phase:  phase,
		Status: "started",
	})
}

func (r *StructuredReporter) PhaseComplete(phase, host string, duration time.Duration) {
	WritePhaseEvent(PhaseEvent{
		Type:     "phase",
		Phase:    phase,
		Status:   "complete",
		Host:     host,
		Duration: duration.Seconds(),
	})
}

func (r *StructuredReporter) PhaseFailed(phase string, err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	WritePhaseEvent(PhaseEvent{
		Type:   "phase",
		Phase:  phase,
		Status: "failed",
		Error:  errMsg,
	})
}

func (r *StructuredReporter) PhaseSkipped(phase, reason string) {
	details := map[string]interface{}{}
	if reason != "" {
		details["reason"] = reason
	}
	WritePhaseEvent(PhaseEvent{
		Type:    "phase",
		Phase:   phase,
		Status:  "skipped",
		Details: details,
	})
}

func (r *StructuredReporter) Divider() {}

func (r *StructuredReporter) ThinDivider() {}

func (r *StructuredReporter) CommandPrompt(command string) {
	WritePhaseEvent(PhaseEvent{
		Type:   "phase",
		Phase:  "exec",
		Status: "started",
		Details: map[string]interface{}{
			"command": command,
		},
	})
}

func (r *StructuredReporter) CommandComplete(exitCode int, host string, totalDuration, execDuration time.Duration) {
	status := "success"
	if exitCode != 0 {
		status = "failed"
	}
	WritePhaseEvent(PhaseEvent{
		Type:     "result",
		Status:   status,
		ExitCode: &exitCode,
		Host:     host,
		Duration: totalDuration.Seconds(),
		Details: map[string]interface{}{
			"exec_duration_s": execDuration.Seconds(),
		},
	})
}

func emitStructuredError(err error) {
	if PrettyMode() {
		return
	}
	_ = WriteJSONFromError(os.Stderr, err)
}
