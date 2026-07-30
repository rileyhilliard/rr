package cli

import (
	"fmt"
	"os"
	"strings"
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
	// CommandComplete emits the final result. extra carries additional
	// result details (fallback, log_file, summary, ...): the structured
	// reporter merges them into the envelope's details map, the pretty
	// reporter re-prints the fallback warning from them. Pass nil when
	// there is nothing extra.
	CommandComplete(exitCode int, host string, totalDuration, execDuration time.Duration, extra map[string]interface{})
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

func (r *PrettyReporter) CommandComplete(exitCode int, host string, totalDuration, execDuration time.Duration, extra map[string]interface{}) {
	renderFinalStatus(r.pd, exitCode, totalDuration, execDuration, host)
	repeatFallbackWarning(extra)
	warnNoTests(extra)
}

// warnNoTests prints a warning when the test runner executed nothing. Exit
// codes stay untouched, so without this a zero-test run is visually
// indistinguishable from a clean suite.
func warnNoTests(details map[string]interface{}) {
	if noTests, ok := details["no_tests"].(bool); !ok || !noTests {
		return
	}
	// Deliberately says nothing about where relative paths resolve: that
	// depends on the offset and on --cwd, and details.remote_cwd already
	// reports the answer. Stale directory advice is worse than none.
	msg := "No tests ran - the runner collected zero tests. " +
		"Check the path or filter you passed."
	if tasks, ok := details["no_tests_tasks"].([]string); ok && len(tasks) > 0 {
		msg = fmt.Sprintf("No tests ran in: %s. Check the path or filter passed to those subtasks.",
			strings.Join(tasks, ", "))
	}
	// A pipe is why the exit code can't be trusted here: the shell reports the
	// last stage's status, so a runner that failed upstream still exits 0.
	if piped, ok := details["piped_exit_code"].(bool); ok && piped {
		msg += " The command pipes its output, so the exit code came from the last stage, not the runner - " +
			"set shell: \"bash -o pipefail -c\" on the host to propagate it."
	}
	ui.PrintWarning(msg)
}

// repeatFallbackWarning re-prints the local-fallback warning after the final
// status line - readers of the output tail must not mistake a local run for
// a remote one. No-op when the run didn't fall back.
func repeatFallbackWarning(details map[string]interface{}) {
	fb, ok := details["fallback"].(fallbackDetail)
	if !ok {
		return
	}
	msg := "Ran LOCALLY - all remote hosts were locked"
	if len(fb.Holders) > 0 {
		msg += " (" + describeHolders(fb.Holders) + ")"
	}
	ui.PrintWarning(msg)
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

func (r *StructuredReporter) CommandComplete(exitCode int, host string, totalDuration, execDuration time.Duration, extra map[string]interface{}) {
	status := "success"
	if exitCode != 0 {
		status = "failed"
	}
	details := map[string]interface{}{
		"exec_duration_s": execDuration.Seconds(),
	}
	for k, v := range extra {
		details[k] = v
	}
	WritePhaseEvent(PhaseEvent{
		Type:     "result",
		Status:   status,
		ExitCode: &exitCode,
		Host:     host,
		Duration: totalDuration.Seconds(),
		Details:  details,
	})
}

func emitStructuredError(err error) {
	if PrettyMode() {
		return
	}
	_ = WriteJSONFromError(os.Stderr, err)
}
