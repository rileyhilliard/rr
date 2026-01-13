package parallel

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

			// Show output snippet for failures
			if len(tr.Output) > 0 && cfg.MaxOutputLines > 0 {
				outputStr := string(tr.Output)
				lines := strings.Split(strings.TrimSpace(outputStr), "\n")

				// Take last N lines (most relevant for errors)
				start := 0
				if len(lines) > cfg.MaxOutputLines {
					start = len(lines) - cfg.MaxOutputLines
					fmt.Fprintf(w, "    %s\n", mutedStyle.Render(fmt.Sprintf("... (%d lines omitted)", start)))
				}

				for _, line := range lines[start:] {
					if line != "" {
						fmt.Fprintf(w, "    %s\n", mutedStyle.Render(line))
					}
				}
			}
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

	// Hosts used
	if len(result.HostsUsed) > 0 {
		sort.Strings(result.HostsUsed)
		fmt.Fprintf(w, "  %s %s\n",
			mutedStyle.Render("Hosts:"),
			mutedStyle.Render(strings.Join(result.HostsUsed, ", ")),
		)
	}

	// Log directory
	if cfg.ShowLogs && cfg.LogDir != "" {
		fmt.Fprintf(w, "  %s %s\n",
			mutedStyle.Render("Logs:"),
			mutedStyle.Render(cfg.LogDir),
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
