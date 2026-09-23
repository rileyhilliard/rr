package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/output/formatters"
	"github.com/rileyhilliard/rr/internal/parallel/logs"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/internal/util"
)

// maxLogReadBytes caps how much of a run log is read back for test-summary
// extraction.
const maxLogReadBytes = 4 << 20 // 4MB

// maxTailReadBytes caps how far back --tail reads into a run log.
const maxTailReadBytes = 1 << 20 // 1MB

// setupRunLog opens a per-run log file, tees raw command output into it,
// and records details.log_file. Best-effort: any failure disables logging
// without erroring. The returned func closes the file (safe to call
// always).
func setupRunLog(wf *WorkflowContext, name string, sh *output.StreamHandler) (string, func()) {
	logsCfg := wf.Resolved.Global.Logs
	baseDir := logsCfg.Dir
	if baseDir == "" {
		baseDir = "~/.rr/logs"
	}

	f, path, err := logs.OpenRunLog(baseDir, name)
	if err != nil {
		return "", func() {}
	}

	sh.SetTee(f)
	wf.AddResultDetail("log_file", path)

	// Apply the same retention policy as parallel runs.
	_ = logs.Cleanup(logsCfg)

	return path, func() { _ = f.Close() }
}

// attachRunOutcome parses the run log (see formatters.ParseRunOutcome),
// records the test summary and failure details in the result envelope, and
// returns the outcome for the pretty renderer. Best-effort: a missing or
// empty log yields a zero Outcome.
func attachRunOutcome(wf *WorkflowContext, command, logPath string, exitCode int) formatters.Outcome {
	if logPath == "" {
		return formatters.Outcome{}
	}
	data := readFileTail(logPath, maxLogReadBytes)
	if len(data) == 0 {
		return formatters.Outcome{}
	}

	outcome := formatters.ParseRunOutcome(command, data)
	for k, v := range outcomeDetails(outcome, exitCode) {
		wf.AddResultDetail(k, v)
	}
	return outcome
}

// outcomeDetails renders an Outcome as result-envelope details. A run that
// collected nothing looks identical to a clean suite if only counts are
// reported, so no_tests calls it out explicitly (reported, not fatal: the
// exit code stays whatever the runner returned). piped_exit_code notes when
// a pipe is why the exit code can't be trusted - rr won't rewrite the
// command's semantics (a deliberate `cmd | grep -q` tolerates upstream
// failure), but the caveat belongs in the report. Failures are listed only
// for failed runs, with messages truncated to maxFailureMessageLen.
func outcomeDetails(o formatters.Outcome, exitCode int) map[string]interface{} {
	details := map[string]interface{}{}
	if o.Summary != nil {
		details["summary"] = *o.Summary
	}
	if o.NoTests {
		details["no_tests"] = true
	}
	if o.PipedExitCode {
		details["piped_exit_code"] = true
	}

	if exitCode != 0 && len(o.Failures) > 0 {
		details["failures"] = failureEntries(o.Failures)
	}
	return details
}

// failureEntries renders parsed test failures for result details: the test
// name, file (with :line when known), and the message truncated to
// maxFailureMessageLen. Shared by single runs and parallel subtasks so both
// report failures in the same shape.
func failureEntries(failures []output.TestFailure) []map[string]string {
	entries := make([]map[string]string, 0, len(failures))
	for _, f := range failures {
		entry := map[string]string{"name": f.TestName}
		if f.File != "" {
			loc := f.File
			if f.Line > 0 {
				loc += ":" + util.Itoa(f.Line)
			}
			entry["file"] = loc
		}
		if f.Message != "" {
			msg := f.Message
			if len(msg) > maxFailureMessageLen {
				msg = msg[:maxFailureMessageLen] + "..."
			}
			entry["message"] = msg
		}
		entries = append(entries, entry)
	}
	return entries
}

// renderOutcomeFailures prints the pretty-mode failure block (counts plus
// each failed test with its location and message) for a failed run.
// Returns whether anything was printed.
func renderOutcomeFailures(o formatters.Outcome, exitCode int) bool {
	if exitCode == 0 || len(o.Failures) == 0 {
		return false
	}

	summary := &ui.TestSummary{Failures: make([]ui.TestFailure, len(o.Failures))}
	if o.Summary != nil {
		summary.Passed = o.Summary.Passed
		summary.Failed = o.Summary.Failed
		summary.Skipped = o.Summary.Skipped
		summary.Errors = o.Summary.Errors
	}
	for i, f := range o.Failures {
		summary.Failures[i] = ui.TestFailure{
			TestName: f.TestName,
			File:     f.File,
			Line:     f.Line,
			Message:  f.Message,
		}
	}

	fmt.Println()
	fmt.Print(ui.FormatDivider(ui.DividerWidth))
	fmt.Println()
	fmt.Print(ui.RenderSummary(summary, exitCode))
	return true
}

// printLogTail prints the last n lines of the run log to stdout. Used by
// --tail so consumers that lost the live stream (broken pipe, redirected
// output) still get the end of the run.
func printLogTail(logPath string, n int) {
	if logPath == "" || n <= 0 {
		return
	}
	lines := tailLines(logPath, n)
	if len(lines) == 0 {
		return
	}
	fmt.Println(strings.Join(lines, "\n"))
}

// tailLines returns the last n lines of the file at path, reading at most
// maxTailReadBytes from the end.
func tailLines(path string, n int) []string {
	data := readFileTail(path, maxTailReadBytes)
	if len(data) == 0 {
		return nil
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// readFileTail returns up to the last capBytes of the file at path without
// reading the whole file into memory. Best-effort: any error returns nil.
func readFileTail(path string, capBytes int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil
	}
	if st.Size() > capBytes {
		if _, err := f.Seek(-capBytes, io.SeekEnd); err != nil {
			return nil
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	return data
}
