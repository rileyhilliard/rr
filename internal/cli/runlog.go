package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/output/formatters"
	"github.com/rileyhilliard/rr/internal/parallel/logs"
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

// attachRunOutcome reads the run log back and records test summary and
// failure details in the result envelope. Best-effort.
func attachRunOutcome(wf *WorkflowContext, command, logPath string, exitCode int) {
	if logPath == "" {
		return
	}
	data := readFileTail(logPath, maxLogReadBytes)
	if len(data) == 0 {
		return
	}

	if summary, ok := formatters.ExtractTestSummary(command, data); ok {
		wf.AddResultDetail("summary", summary)
		if summary.NoTests {
			// A run that collected nothing looks identical to a clean suite if
			// we only report counts, so call it out explicitly. Reported, not
			// fatal: the exit code stays whatever the runner returned.
			wf.AddResultDetail("no_tests", true)
			// Note when a pipe is why exitCode can't be trusted: the shell
			// reports the last stage's status, so a runner that failed upstream
			// still exits 0. rr won't rewrite the command's semantics (a
			// deliberate `cmd | grep -q` tolerates upstream failure), but the
			// caveat belongs in the report.
			if util.HasPipe(command) {
				wf.AddResultDetail("piped_exit_code", true)
			}
		}
	}

	if exitCode != 0 {
		if failures := formatters.ExtractFailures(command, data); len(failures) > 0 {
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
			wf.AddResultDetail("failures", entries)
		}
	}
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
