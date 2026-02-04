package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/rileyhilliard/rr/internal/ui"
)

// TaskEntry holds the state of a single task in the dashboard.
type TaskEntry struct {
	Name      string
	Index     int
	Host      string
	Status    parallel.TaskStatus
	StartTime time.Time
	Duration  time.Duration
}

// Model is the Bubble Tea model for the dashboard.
type Model struct {
	tasks        []TaskEntry
	selected     int
	width        int
	height       int
	spinnerFrame int
	completed    bool
	passed       int
	failed       int
	totalTime    time.Duration
	cancelFunc   context.CancelFunc
	quitting     bool
	warnings     []string // Re-queue warnings to display
	startTime    time.Time
}

// Spinner frames for running tasks
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Animation interval for spinner
const spinnerInterval = 80 * time.Millisecond

// NewModel creates a new dashboard model.
func NewModel(taskCount int, cancelFunc context.CancelFunc) Model {
	return Model{
		tasks:      make([]TaskEntry, 0, taskCount),
		selected:   0,
		cancelFunc: cancelFunc,
		startTime:  time.Now(),
	}
}

// Init returns the initial command for the model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
	)
}

// Update handles messages and updates the model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, tickCmd()

	case InitTasksMsg:
		m.tasks = make([]TaskEntry, len(msg.Tasks))
		for i, t := range msg.Tasks {
			m.tasks[i] = TaskEntry{
				Name:   t.Name,
				Index:  t.Index,
				Status: parallel.TaskPending,
			}
		}
		return m, nil

	case TaskSyncingMsg:
		for i := range m.tasks {
			if m.tasks[i].Index == msg.Index {
				m.tasks[i].Status = parallel.TaskSyncing
				m.tasks[i].Host = msg.Host
				m.tasks[i].StartTime = time.Now()
				break
			}
		}
		return m, nil

	case TaskExecutingMsg:
		for i := range m.tasks {
			if m.tasks[i].Index == msg.Index {
				m.tasks[i].Status = parallel.TaskRunning
				break
			}
		}
		return m, nil

	case TaskCompletedMsg:
		for i := range m.tasks {
			if m.tasks[i].Index == msg.Index {
				if msg.Success {
					m.tasks[i].Status = parallel.TaskPassed
					m.passed++
				} else {
					m.tasks[i].Status = parallel.TaskFailed
					m.failed++
				}
				m.tasks[i].Duration = msg.Duration
				break
			}
		}
		return m, nil

	case TaskRequeuedMsg:
		for i := range m.tasks {
			if m.tasks[i].Index == msg.Index {
				m.tasks[i].Status = parallel.TaskPending
				m.tasks[i].Host = ""
				m.tasks[i].StartTime = time.Time{}
				break
			}
		}
		m.warnings = append(m.warnings, fmt.Sprintf("%s unavailable, re-queuing %s",
			msg.UnavailableHost, msg.Name))
		return m, nil

	case AllCompletedMsg:
		m.completed = true
		m.passed = msg.Passed
		m.failed = msg.Failed
		m.totalTime = msg.Duration
		return m, nil

	case orchestratorDoneMsg:
		m.completed = true
		m.totalTime = time.Since(m.startTime)
		// Don't quit immediately - let user see results
		return m, nil
	}

	return m, nil
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.selected < len(m.tasks)-1 {
			m.selected++
		}
		return m, nil

	case "k", "up":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil

	case "g", "home":
		m.selected = 0
		return m, nil

	case "G", "end":
		if len(m.tasks) > 0 {
			m.selected = len(m.tasks) - 1
		}
		return m, nil

	case "q", "ctrl+c":
		if m.cancelFunc != nil {
			m.cancelFunc()
		}
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

// View renders the dashboard.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var sb strings.Builder

	// Render header
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")

	// Render warnings (if any)
	for _, warning := range m.warnings {
		sb.WriteString(warningStyle.Render(ui.SymbolWarning + " " + warning))
		sb.WriteString("\n")
	}

	// Render task list
	sb.WriteString(m.renderTaskList())

	// Render footer
	if ShowFooter(m.height) {
		sb.WriteString("\n")
		sb.WriteString(m.renderFooter())
	}

	return sb.String()
}

// renderHeader renders the dashboard header.
func (m Model) renderHeader() string {
	running := 0
	syncing := 0
	pending := 0
	for _, t := range m.tasks {
		switch t.Status {
		case parallel.TaskRunning:
			running++
		case parallel.TaskSyncing:
			syncing++
		case parallel.TaskPending:
			pending++
		}
	}

	var status string
	if m.completed {
		if m.failed > 0 {
			status = summaryFailedStyle.Render(fmt.Sprintf("%d failed", m.failed)) +
				footerStyle.Render(", ") +
				summaryPassedStyle.Render(fmt.Sprintf("%d passed", m.passed))
		} else {
			status = summaryPassedStyle.Render(fmt.Sprintf("All %d passed", m.passed))
		}
		status += footerStyle.Render(fmt.Sprintf(" in %.1fs", m.totalTime.Seconds()))
	} else {
		parts := []string{}
		if running > 0 {
			parts = append(parts, runningStyle.Render(fmt.Sprintf("%d running", running)))
		}
		if syncing > 0 {
			parts = append(parts, syncingStyle.Render(fmt.Sprintf("%d syncing", syncing)))
		}
		if pending > 0 {
			parts = append(parts, pendingStyle.Render(fmt.Sprintf("%d pending", pending)))
		}
		if m.passed > 0 {
			parts = append(parts, passedStyle.Render(fmt.Sprintf("%d passed", m.passed)))
		}
		if m.failed > 0 {
			parts = append(parts, failedStyle.Render(fmt.Sprintf("%d failed", m.failed)))
		}
		status = strings.Join(parts, footerStyle.Render(" | "))
	}

	return headerStyle.Render("Tasks") + " " + status
}

// renderTaskList renders all task entries.
func (m Model) renderTaskList() string {
	var sb strings.Builder

	for i, task := range m.tasks {
		line := m.renderTaskLine(task, i == m.selected)
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderTaskLine renders a single task entry.
func (m Model) renderTaskLine(task TaskEntry, selected bool) string {
	var symbol string
	var statusStyle lipgloss.Style

	switch task.Status {
	case parallel.TaskPending:
		symbol = ui.SymbolPending
		statusStyle = pendingStyle
	case parallel.TaskSyncing:
		// Animated color for syncing
		colorIdx := (m.spinnerFrame / 4) % len(ui.GradientColors)
		symbol = ui.SymbolSyncing
		statusStyle = lipgloss.NewStyle().Foreground(ui.GradientColors[colorIdx])
	case parallel.TaskRunning:
		// Animated spinner with color cycling
		symbol = spinnerFrames[m.spinnerFrame]
		colorIdx := (m.spinnerFrame / 2) % len(ui.GradientColors)
		statusStyle = lipgloss.NewStyle().Foreground(ui.GradientColors[colorIdx])
	case parallel.TaskPassed:
		symbol = ui.SymbolSuccess
		statusStyle = passedStyle
	case parallel.TaskFailed:
		symbol = ui.SymbolFail
		statusStyle = failedStyle
	}

	// Build the line
	line := statusStyle.Render(symbol) + " " + task.Name

	// Add host if assigned
	if task.Host != "" {
		line += " " + hostStyle.Render(fmt.Sprintf("[%s]", task.Host))
	}

	// Add duration for completed tasks
	if task.Duration > 0 {
		line += " " + durationStyle.Render(formatDuration(task.Duration))
	} else if task.Status == parallel.TaskRunning || task.Status == parallel.TaskSyncing {
		// Show elapsed time for running tasks
		if !task.StartTime.IsZero() {
			elapsed := time.Since(task.StartTime)
			line += " " + durationStyle.Render(formatDuration(elapsed))
		}
	}

	// Apply selection styling
	if selected {
		line = selectedStyle.Render(line)
	} else {
		line = unselectedStyle.Render(line)
	}

	return line
}

// renderFooter renders the footer with keyboard shortcuts.
func (m Model) renderFooter() string {
	if m.completed {
		return footerStyle.Render("Press q to exit")
	}
	return footerStyle.Render("j/k: navigate | q: cancel and exit")
}

// tickCmd returns a command that sends a tick after the spinner interval.
func tickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
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
