package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ParallelProgress displays animated progress for multiple concurrent tasks.
// Uses in-place terminal updates so running tasks animate and completed
// tasks transition smoothly without printing new lines.
type ParallelProgress struct {
	mu sync.Mutex

	tasks     []taskEntry // Ordered list of tasks
	lineCount int         // Number of lines currently rendered
	frame     int         // Current animation frame

	running  bool
	stopChan chan struct{}
	doneChan chan struct{}
	output   io.Writer
	isTTY    bool

	// Styles
	successStyle lipgloss.Style
	errorStyle   lipgloss.Style
	mutedStyle   lipgloss.Style
	hostStyle    lipgloss.Style
}

type taskEntry struct {
	Name      string
	Index     int // Position in task list (for duplicate name handling)
	Host      string
	Status    TaskStatus
	StartTime time.Time
}

// TaskInit holds initialization info for a task.
// Used to pass task info from parallel package without circular imports.
type TaskInit struct {
	Name  string
	Index int
}

// TaskStatus values for parallel progress (matches parallel.TaskStatus)
type TaskStatus int

const (
	TaskStatusPending TaskStatus = iota
	TaskStatusSyncing            // Assigned to host, connecting/syncing/waiting for lock
	TaskStatusRunning            // Actually executing the command
	TaskStatusPassed
	TaskStatusFailed
)

// NewParallelProgress creates a new parallel progress display.
func NewParallelProgress(isTTY bool) *ParallelProgress {
	return &ParallelProgress{
		tasks:    make([]taskEntry, 0),
		output:   os.Stdout,
		isTTY:    isTTY,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),

		successStyle: lipgloss.NewStyle().Foreground(ColorSuccess),
		errorStyle:   lipgloss.NewStyle().Foreground(ColorError),
		mutedStyle:   lipgloss.NewStyle().Foreground(ColorMuted),
		hostStyle:    lipgloss.NewStyle().Foreground(ColorSecondary),
	}
}

// SetWriter sets the output writer (useful for testing).
func (p *ParallelProgress) SetWriter(w io.Writer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.output = w
}

// Start begins the animation loop.
func (p *ParallelProgress) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.stopChan = make(chan struct{})
	p.doneChan = make(chan struct{})
	p.mu.Unlock()

	go p.animate()
}

// Stop halts the animation and renders final state.
func (p *ParallelProgress) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	close(p.stopChan)
	p.mu.Unlock()

	<-p.doneChan
}

// InitTasks initializes all tasks as pending. Call this before starting workers
// to show all tasks upfront in the UI. Accepts any type with Name and Index fields.
func (p *ParallelProgress) InitTasks(tasks interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Handle different input types to avoid circular imports
	switch t := tasks.(type) {
	case []TaskInit:
		for _, task := range t {
			p.tasks = append(p.tasks, taskEntry{
				Name:   task.Name,
				Index:  task.Index,
				Host:   "",
				Status: TaskStatusPending,
			})
		}
	case []string:
		// Legacy support for string slice
		for i, name := range t {
			p.tasks = append(p.tasks, taskEntry{
				Name:   name,
				Index:  i,
				Host:   "",
				Status: TaskStatusPending,
			})
		}
	}

	// Render initial state
	p.renderLocked()
}

// TaskSyncing updates a task to syncing state (connecting/syncing/waiting for lock).
func (p *ParallelProgress) TaskSyncing(name string, index int, host string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find task by index (unique identifier for duplicate names)
	for i := range p.tasks {
		if p.tasks[i].Index == index {
			p.tasks[i].Status = TaskStatusSyncing
			p.tasks[i].Host = host
			p.tasks[i].StartTime = time.Now()
			p.renderLocked()
			return
		}
	}

	// Task not found in InitTasks, add it (shouldn't normally happen)
	p.tasks = append(p.tasks, taskEntry{
		Name:      name,
		Index:     index,
		Host:      host,
		Status:    TaskStatusSyncing,
		StartTime: time.Now(),
	})

	// Immediate render for responsiveness
	p.renderLocked()
}

// TaskExecuting transitions a task from syncing to running (command execution started).
func (p *ParallelProgress) TaskExecuting(name string, index int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.tasks {
		if p.tasks[i].Index == index {
			p.tasks[i].Status = TaskStatusRunning
			p.renderLocked()
			return
		}
	}
}

// TaskStarted adds a new task in syncing state.
//
// Deprecated: Use TaskSyncing followed by TaskExecuting for clearer state tracking.
func (p *ParallelProgress) TaskStarted(name string, index int, host string) {
	p.TaskSyncing(name, index, host)
}

// TaskCompleted updates a task's status to passed or failed.
func (p *ParallelProgress) TaskCompleted(name string, index int, success bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.tasks {
		if p.tasks[i].Index == index {
			if success {
				p.tasks[i].Status = TaskStatusPassed
			} else {
				p.tasks[i].Status = TaskStatusFailed
			}
			break
		}
	}

	// Immediate render to show completion
	p.renderLocked()
}

// HasRunningTasks returns true if any tasks are still running or syncing.
func (p *ParallelProgress) HasRunningTasks() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, t := range p.tasks {
		if t.Status == TaskStatusRunning || t.Status == TaskStatusSyncing {
			return true
		}
	}
	return false
}

func (p *ParallelProgress) animate() {
	ticker := time.NewTicker(80 * time.Millisecond) // Smooth animation
	defer ticker.Stop()
	defer close(p.doneChan)

	for {
		select {
		case <-p.stopChan:
			// Final render with all tasks in their final state
			p.mu.Lock()
			p.renderLocked()
			p.mu.Unlock()
			return
		case <-ticker.C:
			p.mu.Lock()
			p.frame = (p.frame + 1) % len(spinnerFrames)
			p.renderLocked()
			p.mu.Unlock()
		}
	}
}

// renderLocked renders all task lines in-place. Must be called with lock held.
func (p *ParallelProgress) renderLocked() {
	if !p.isTTY || len(p.tasks) == 0 {
		return
	}

	var sb strings.Builder

	// Move cursor up to overwrite previous lines
	if p.lineCount > 0 {
		sb.WriteString(fmt.Sprintf("\x1b[%dA", p.lineCount))
	}

	// Render each task line
	for _, task := range p.tasks {
		line := p.renderTaskLine(task)
		// Clear line and write new content
		sb.WriteString("\x1b[K") // Clear to end of line
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	fmt.Fprint(p.output, sb.String())
	p.lineCount = len(p.tasks)
}

// renderTaskLine renders a single task with appropriate symbol and style.
func (p *ParallelProgress) renderTaskLine(task taskEntry) string {
	var symbol string
	var style lipgloss.Style

	switch task.Status {
	case TaskStatusPending:
		symbol = SymbolPending
		style = p.mutedStyle
	case TaskStatusSyncing:
		// Slow pulsing animation for syncing (waiting for lock/sync)
		symbol = SymbolSyncing
		// Slower color pulse to differentiate from running
		colorIdx := (p.frame / 4) % len(GradientColors)
		style = lipgloss.NewStyle().Foreground(GradientColors[colorIdx])
	case TaskStatusRunning:
		// Animated spinner with color cycling
		symbol = spinnerFrames[p.frame]
		colorIdx := (p.frame / 2) % len(GradientColors)
		style = lipgloss.NewStyle().Foreground(GradientColors[colorIdx])
	case TaskStatusPassed:
		symbol = SymbolSuccess
		style = p.successStyle
	case TaskStatusFailed:
		symbol = SymbolFail
		style = p.errorStyle
	}

	// Only show host if assigned
	if task.Host != "" {
		hostStr := p.mutedStyle.Render(fmt.Sprintf("[%s]", task.Host))
		return fmt.Sprintf("%s %s %s", style.Render(symbol), task.Name, hostStr)
	}
	return fmt.Sprintf("%s %s", style.Render(symbol), task.Name)
}

// Finalize ensures proper cursor positioning after progress display.
// Call this after Stop() and before printing additional output.
func (p *ParallelProgress) Finalize() {
	// Nothing extra needed - cursor is already at correct position
}
