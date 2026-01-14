package parallel

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/output/formatters"
	"github.com/rileyhilliard/rr/internal/ui"
)

// SummaryConfig holds configuration for rendering the summary.
type SummaryConfig struct {
	// ShowLogs indicates whether to show the log directory path.
	ShowLogs bool
	// LogDir is the path to the log directory.
	LogDir string
	// MaxOutputLines limits the number of output lines shown per failed task.
	MaxOutputLines int
}

// DefaultSummaryConfig returns default summary configuration.
func DefaultSummaryConfig() SummaryConfig {
	return SummaryConfig{
		ShowLogs:       false,
		LogDir:         "",
		MaxOutputLines: 10,
	}
}

// RenderSummary prints a formatted summary of parallel execution results.
func RenderSummary(result *Result, logDir string) {
	RenderSummaryTo(os.Stdout, result, SummaryConfig{
		ShowLogs:       logDir != "",
		LogDir:         logDir,
		MaxOutputLines: 10,
	})
}

// RenderSummaryTo prints a formatted summary to the specified writer.
func RenderSummaryTo(w io.Writer, result *Result, cfg SummaryConfig) {
	if result == nil {
		return
	}

	// Styles
	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	headerStyle := lipgloss.NewStyle().Foreground(ui.ColorSecondary).Bold(true)

	// Divider
	divider := mutedStyle.Render(strings.Repeat("─", 60))

	fmt.Fprintln(w)
	fmt.Fprintln(w, divider)
	fmt.Fprintln(w)

	// Header
	fmt.Fprintln(w, headerStyle.Render("Parallel Execution Summary"))
	fmt.Fprintln(w)

	// Sort results by task name for consistent output
	sortedResults := make([]TaskResult, len(result.TaskResults))
	copy(sortedResults, result.TaskResults)
	sort.Slice(sortedResults, func(i, j int) bool {
		return sortedResults[i].TaskName < sortedResults[j].TaskName
	})

	// Per-task results
	for i := range sortedResults {
		tr := &sortedResults[i]
		symbol := ui.SymbolSuccess
		style := successStyle
		statusText := "passed"

		if !tr.Success() {
			symbol = ui.SymbolFail
			style = errorStyle
			statusText = "failed"
			if tr.Error != nil {
				statusText = fmt.Sprintf("failed: %s", tr.Error.Error())
			}
		}

		// Task line: [symbol] task-name on host (duration)
		fmt.Fprintf(w, "  %s %s on %s %s\n",
			style.Render(symbol),
			tr.TaskName,
			mutedStyle.Render(tr.Host),
			mutedStyle.Render(fmt.Sprintf("(%s)", formatDuration(tr.Duration))),
		)

		// Show error details for failed tasks
		if !tr.Success() {
			fmt.Fprintf(w, "    %s\n", mutedStyle.Render(statusText))
			renderTaskFailures(w, tr, cfg.MaxOutputLines, errorStyle, mutedStyle)
		}
	}

	fmt.Fprintln(w)

	// Aggregate stats
	total := len(result.TaskResults)
	passedStyle := successStyle
	failedStyle := mutedStyle
	if result.Failed > 0 {
		failedStyle = errorStyle
	}

	fmt.Fprintf(w, "  %s %d passed  %s %d failed  %s %d total  %s\n",
		passedStyle.Render(ui.SymbolSuccess),
		result.Passed,
		failedStyle.Render(ui.SymbolFail),
		result.Failed,
		mutedStyle.Render(ui.SymbolComplete),
		total,
		mutedStyle.Render(fmt.Sprintf("(%s)", formatDuration(result.Duration))),
	)

	// Hosts used (copy to avoid mutating the original slice)
	if len(result.HostsUsed) > 0 {
		hosts := make([]string, len(result.HostsUsed))
		copy(hosts, result.HostsUsed)
		sort.Strings(hosts)
		fmt.Fprintf(w, "  %s %s\n",
			mutedStyle.Render("Hosts:"),
			mutedStyle.Render(strings.Join(hosts, ", ")),
		)
	}

	// Log directory - point to specific file if there's only one failure
	if cfg.ShowLogs && cfg.LogDir != "" {
		logPath := cfg.LogDir

		// If there's exactly one failed task, point directly to its log file
		if result.Failed == 1 {
			for i := range sortedResults {
				if !sortedResults[i].Success() {
					// Sanitize the task name the same way logs/writer.go does
					taskLogName := sanitizeTaskName(sortedResults[i].TaskName) + ".log"
					logPath = cfg.LogDir + "/" + taskLogName
					break
				}
			}
		}

		fmt.Fprintf(w, "  %s %s\n",
			mutedStyle.Render("Logs:"),
			mutedStyle.Render(logPath),
		)
	}

	fmt.Fprintln(w)

	// Actionable suggestions for failures
	if result.Failed > 0 {
		fmt.Fprintln(w, headerStyle.Render("Retry Failed Tasks:"))
		fmt.Fprintln(w)

		for i := range sortedResults {
			if !sortedResults[i].Success() {
				fmt.Fprintf(w, "  %s rr %s\n",
					mutedStyle.Render("$"),
					sortedResults[i].TaskName,
				)
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, divider)
}

// FormatBriefSummary returns a one-line summary string.
func FormatBriefSummary(result *Result) string {
	if result == nil {
		return "No results"
	}

	total := len(result.TaskResults)
	if result.Failed == 0 {
		return fmt.Sprintf("%d/%d tasks passed (%s)",
			result.Passed, total, formatDuration(result.Duration))
	}

	return fmt.Sprintf("%d passed, %d failed of %d tasks (%s)",
		result.Passed, result.Failed, total, formatDuration(result.Duration))
}

// sanitizeTaskName converts a task name to a safe filename.
// Replaces characters that are invalid in filenames with dashes.
func sanitizeTaskName(name string) string {
	result := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '/' || c == '\\' || c == ':' || c == '*' || c == '?' || c == '"' || c == '<' || c == '>' || c == '|' {
			result[i] = '-'
		} else {
			result[i] = c
		}
	}
	return string(result)
}

// renderTaskFailures displays failure details for a failed task.
func renderTaskFailures(w io.Writer, tr *TaskResult, maxLines int, errorStyle, mutedStyle lipgloss.Style) {
	if len(tr.Output) == 0 {
		return
	}

	failures := formatters.ExtractFailures(tr.Command, tr.Output)
	if len(failures) > 0 {
		renderStructuredFailures(w, failures, maxLines, errorStyle, mutedStyle)
		return
	}

	// Fallback: show last N lines if no structured failures found
	if maxLines > 0 {
		renderFallbackOutput(w, tr.Output, maxLines, mutedStyle)
	}
}

// renderStructuredFailures displays parsed test failures.
func renderStructuredFailures(w io.Writer, failures []output.TestFailure, maxLines int, errorStyle, mutedStyle lipgloss.Style) {
	maxFailures := maxLines
	if maxFailures <= 0 {
		maxFailures = 5
	}
	showCount := len(failures)
	if showCount > maxFailures {
		showCount = maxFailures
	}

	for i := 0; i < showCount; i++ {
		f := failures[i]
		location := formatLocation(f.File, f.Line)

		if location != "" {
			fmt.Fprintf(w, "    %s %s\n", errorStyle.Render(f.TestName), mutedStyle.Render("("+location+")"))
		} else {
			fmt.Fprintf(w, "    %s\n", errorStyle.Render(f.TestName))
		}

		if f.Message != "" {
			msg := strings.Split(f.Message, "\n")[0]
			if len(msg) > 80 {
				msg = msg[:77] + "..."
			}
			fmt.Fprintf(w, "      %s\n", mutedStyle.Render(msg))
		}
	}

	if len(failures) > showCount {
		fmt.Fprintf(w, "    %s\n", mutedStyle.Render(fmt.Sprintf("... and %d more failures", len(failures)-showCount)))
	}
}

// formatLocation formats file:line location string.
func formatLocation(file string, line int) string {
	if file == "" {
		return ""
	}
	if line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}

// renderFallbackOutput shows last N lines of raw output.
func renderFallbackOutput(w io.Writer, rawOutput []byte, maxLines int, mutedStyle lipgloss.Style) {
	lines := strings.Split(strings.TrimSpace(string(rawOutput)), "\n")

	start := 0
	if len(lines) > maxLines {
		start = len(lines) - maxLines
		fmt.Fprintf(w, "    %s\n", mutedStyle.Render(fmt.Sprintf("... (%d lines omitted)", start)))
	}

	for _, line := range lines[start:] {
		if line != "" {
			fmt.Fprintf(w, "    %s\n", mutedStyle.Render(line))
		}
	}
}
