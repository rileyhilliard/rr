package parallel

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/ui"
	"golang.org/x/term"
)

// maxOutputBufferSize limits memory usage for buffered task output (1MB per task)
const maxOutputBufferSize = 1 << 20

// OutputManager handles output display for parallel task execution.
// It supports different output modes: progress, stream, verbose, and quiet.
type OutputManager struct {
	mode  OutputMode
	isTTY bool
	w     io.Writer

	mu sync.Mutex

	// Task state tracking
	taskStatus    map[string]TaskStatus
	taskHosts     map[string]string
	taskOutput    map[string]*bytes.Buffer
	taskTruncated map[string]bool

	// Animated progress display (for progress mode with TTY)
	progress *ui.ParallelProgress

	// Styles
	successStyle lipgloss.Style
	errorStyle   lipgloss.Style
	mutedStyle   lipgloss.Style
	hostStyle    lipgloss.Style
}

// NewOutputManager creates a new output manager with the specified mode.
//
// Output mode selection logic:
//   - progress: Live updating display with spinners (requires TTY)
//   - stream: Real-time interleaved output with [host:task] prefixes
//   - verbose: Full output per task shown on completion
//   - quiet: Summary only, no per-task output
//
// TTY detection fallback: If progress mode is requested but stdout isn't a TTY
// (e.g., piped to a file or CI environment), we fall back to quiet mode since
// progress updates would be meaningless without terminal control.
func NewOutputManager(mode OutputMode, isTTY bool) *OutputManager {
	// Fall back to simple output for non-TTY in progress mode.
	// Progress mode uses terminal control sequences that don't work in pipes.
	effectiveMode := mode
	if !isTTY && mode == OutputProgress {
		effectiveMode = OutputQuiet
	}

	m := &OutputManager{
		mode:          effectiveMode,
		isTTY:         isTTY,
		w:             os.Stdout,
		taskStatus:    make(map[string]TaskStatus),
		taskHosts:     make(map[string]string),
		taskOutput:    make(map[string]*bytes.Buffer),
		taskTruncated: make(map[string]bool),

		successStyle: lipgloss.NewStyle().Foreground(ui.ColorSuccess),
		errorStyle:   lipgloss.NewStyle().Foreground(ui.ColorError),
		mutedStyle:   lipgloss.NewStyle().Foreground(ui.ColorMuted),
		hostStyle:    lipgloss.NewStyle().Foreground(ui.ColorSecondary),
	}

	// Create animated progress display for TTY progress mode
	if effectiveMode == OutputProgress && isTTY {
		m.progress = ui.NewParallelProgress(isTTY)
		m.progress.Start()
	}

	return m
}

// SetWriter sets the output writer for testing.
func (m *OutputManager) SetWriter(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.w = w
	// Also update the parallel progress writer if it exists
	if m.progress != nil {
		m.progress.SetWriter(w)
	}
}

// InitTasks initializes all tasks as pending. Call this before starting workers
// to show all tasks upfront in the UI.
func (m *OutputManager) InitTasks(taskNames []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, name := range taskNames {
		m.taskStatus[name] = TaskPending
		m.taskOutput[name] = &bytes.Buffer{}
	}

	if m.progress != nil {
		m.progress.InitTasks(taskNames)
	}
}

// TaskSyncing is called when a worker picks up a task and begins syncing.
// The task is assigned to a host but not yet executing (connecting, syncing, waiting for lock).
func (m *OutputManager) TaskSyncing(taskName, host string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.taskStatus[taskName] = TaskSyncing
	m.taskHosts[taskName] = host
	m.taskOutput[taskName] = &bytes.Buffer{}

	switch m.mode {
	case OutputProgress:
		if m.progress != nil {
			m.progress.TaskSyncing(taskName, host)
		}
	case OutputStream:
		prefix := m.formatPrefix(host, taskName)
		fmt.Fprintf(m.w, "%s syncing\n", prefix)
	case OutputVerbose:
		fmt.Fprintf(m.w, "%s Syncing %s to %s...\n",
			m.mutedStyle.Render(ui.SymbolSyncing),
			taskName,
			host)
	case OutputQuiet:
		// No output for quiet mode
	}
}

// TaskExecuting is called when a task's command actually starts running.
func (m *OutputManager) TaskExecuting(taskName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.taskStatus[taskName] = TaskRunning

	switch m.mode {
	case OutputProgress:
		if m.progress != nil {
			m.progress.TaskExecuting(taskName)
		}
	case OutputStream:
		host := m.taskHosts[taskName]
		prefix := m.formatPrefix(host, taskName)
		fmt.Fprintf(m.w, "%s running\n", prefix)
	case OutputVerbose:
		host := m.taskHosts[taskName]
		fmt.Fprintf(m.w, "%s Running %s on %s...\n",
			m.mutedStyle.Render(ui.SymbolProgress),
			taskName,
			host)
	case OutputQuiet:
		// No output for quiet mode
	}
}

// TaskStarted is called when a task begins execution.
//
// Deprecated: Use TaskSyncing followed by TaskExecuting for clearer state tracking.
func (m *OutputManager) TaskStarted(taskName, host string) {
	m.TaskSyncing(taskName, host)
}

// TaskOutput is called when a task produces output.
func (m *OutputManager) TaskOutput(taskName string, line []byte, isStderr bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Buffer output for later display (with size limit to prevent unbounded growth)
	if buf, ok := m.taskOutput[taskName]; ok {
		if buf.Len() < maxOutputBufferSize {
			buf.Write(line)
			buf.WriteByte('\n')
		} else if !m.taskTruncated[taskName] {
			m.taskTruncated[taskName] = true
			buf.WriteString("\n... output truncated (exceeded 1MB) ...\n")
		}
	}

	switch m.mode {
	case OutputStream:
		host := m.taskHosts[taskName]
		prefix := m.formatPrefix(host, taskName)
		fmt.Fprintf(m.w, "%s %s\n", prefix, string(line))
	case OutputProgress, OutputVerbose, OutputQuiet:
		// Buffered, no immediate output
	}
}

// TaskCompleted is called when a task finishes execution.
func (m *OutputManager) TaskCompleted(taskName string, result TaskResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := TaskPassed
	if !result.Success() {
		status = TaskFailed
	}
	m.taskStatus[taskName] = status

	switch m.mode {
	case OutputProgress:
		// Use animated progress display
		if m.progress != nil {
			m.progress.TaskCompleted(taskName, result.Success())
		}
	case OutputStream:
		prefix := m.formatPrefix(result.Host, taskName)
		symbol := ui.SymbolSuccess
		style := m.successStyle
		if status == TaskFailed {
			symbol = ui.SymbolFail
			style = m.errorStyle
		}
		fmt.Fprintf(m.w, "%s %s %s\n", prefix, style.Render(symbol), formatDuration(result.Duration))
	case OutputVerbose:
		m.renderVerboseCompletion(taskName, result, status)
	case OutputQuiet:
		// No output for quiet mode
	}
}

// RenderProgress renders the current progress state.
// This is called periodically for live updates in progress mode.
func (m *OutputManager) RenderProgress() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.mode != OutputProgress || !m.isTTY {
		return
	}

	// In a real implementation, this would update an in-place display
	// For now, we render on state changes in TaskStarted/TaskCompleted
}

// Close finalizes the output manager and performs any cleanup.
func (m *OutputManager) Close() {
	m.mu.Lock()
	progress := m.progress
	m.mu.Unlock()

	// Stop the animated progress display
	if progress != nil {
		progress.Stop()
	}
}

// renderVerboseCompletion renders verbose output for a completed task.
func (m *OutputManager) renderVerboseCompletion(taskName string, result TaskResult, status TaskStatus) {
	symbol := ui.SymbolSuccess
	style := m.successStyle
	if status == TaskFailed {
		symbol = ui.SymbolFail
		style = m.errorStyle
	}

	// Header line
	fmt.Fprintf(m.w, "\n%s %s on %s %s\n",
		style.Render(symbol),
		taskName,
		result.Host,
		m.mutedStyle.Render(formatDuration(result.Duration)))

	// Output
	if buf, ok := m.taskOutput[taskName]; ok && buf.Len() > 0 {
		fmt.Fprintf(m.w, "%s\n", m.mutedStyle.Render(strings.Repeat("-", 40)))
		fmt.Fprintf(m.w, "%s\n", buf.String())
	}
}

// formatPrefix creates the output prefix for stream mode.
func (m *OutputManager) formatPrefix(host, taskName string) string {
	return m.mutedStyle.Render(fmt.Sprintf("[%s:%s]", host, taskName))
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	secs := d.Seconds()
	if secs < 0.1 {
		return fmt.Sprintf("%.2fs", secs)
	}
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs / 60)
	remainingSecs := secs - float64(mins)*60
	return fmt.Sprintf("%dm%.1fs", mins, remainingSecs)
}

// isTerminal checks if stdout is a terminal.
func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// GetTaskStatus returns the current status of a task.
func (m *OutputManager) GetTaskStatus(taskName string) TaskStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.taskStatus[taskName]
}

// GetAllStatuses returns a copy of all task statuses.
func (m *OutputManager) GetAllStatuses() map[string]TaskStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]TaskStatus, len(m.taskStatus))
	for k, v := range m.taskStatus {
		result[k] = v
	}
	return result
}
