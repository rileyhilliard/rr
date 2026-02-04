// Package dashboard provides an interactive Bubble Tea-based TUI for parallel task execution.
// It displays a task list view with real-time status updates, keyboard navigation,
// and a summary on completion.
package dashboard

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rileyhilliard/rr/internal/parallel"
	"golang.org/x/term"
)

// RunOptions configures the dashboard execution.
type RunOptions struct {
	// SaveLogs indicates whether to save output to log files.
	SaveLogs bool
	// LogDir is the directory for log files.
	LogDir string
}

// orchestratorResult holds the result from the orchestrator goroutine.
type orchestratorResult struct {
	result *parallel.Result
	err    error
}

// Run starts the dashboard TUI and orchestrator.
// The orchestrator runs in a background goroutine while the TUI runs in the main thread.
// Returns the parallel execution result and any error.
func Run(ctx context.Context, orchestrator *parallel.Orchestrator, tasks []parallel.TaskInfo) (*parallel.Result, error) {
	// Check if stdout is a TTY
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		// Non-TTY: fall back to running without dashboard
		return orchestrator.Run(ctx)
	}

	// Create cancellable context for the orchestrator
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create the model with cancel function
	model := NewModel(len(tasks), cancel)

	// Create the program with alt screen for full-screen TUI
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // Enable mouse support for scrolling
	)

	// Create the bridge that forwards events to the TUI
	bridge := NewBridge(program)

	// Set up the dashboard bridge on the orchestrator
	orchestrator.SetDashboardBridge(bridge)

	// Channel to receive orchestrator result
	resultChan := make(chan orchestratorResult, 1)

	// Run orchestrator in background goroutine
	go func() {
		result, err := orchestrator.Run(ctx)
		resultChan <- orchestratorResult{result: result, err: err}

		// Notify TUI that orchestrator is done
		if result != nil {
			bridge.AllCompleted(result.Passed, result.Failed, result.Duration)
		}
		bridge.OrchestratorDone(err)
	}()

	// Run the TUI (blocks until user quits)
	if _, err := program.Run(); err != nil {
		cancel() // Cancel orchestrator if TUI fails
		return nil, err
	}

	// Wait for orchestrator result
	r := <-resultChan
	return r.result, r.err
}
