package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rileyhilliard/rr/internal/parallel"
)

// Bridge implements the callback interface for OutputManager and forwards
// events to the Bubble Tea program via program.Send(). This is goroutine-safe.
type Bridge struct {
	program *tea.Program
}

// NewBridge creates a new bridge that forwards events to the given program.
func NewBridge(program *tea.Program) *Bridge {
	return &Bridge{program: program}
}

// InitTasks forwards task initialization to the TUI.
func (b *Bridge) InitTasks(tasks []parallel.TaskInit) {
	b.program.Send(InitTasksMsg{Tasks: tasks})
}

// TaskSyncing forwards task syncing state to the TUI.
func (b *Bridge) TaskSyncing(name string, index int, host string) {
	b.program.Send(TaskSyncingMsg{
		Name:  name,
		Index: index,
		Host:  host,
	})
}

// TaskExecuting forwards task executing state to the TUI.
func (b *Bridge) TaskExecuting(name string, index int) {
	b.program.Send(TaskExecutingMsg{
		Name:  name,
		Index: index,
	})
}

// TaskCompleted forwards task completion to the TUI.
func (b *Bridge) TaskCompleted(name string, index int, success bool, duration time.Duration) {
	b.program.Send(TaskCompletedMsg{
		Name:     name,
		Index:    index,
		Success:  success,
		Duration: duration,
	})
}

// TaskRequeued forwards task re-queue notification to the TUI.
func (b *Bridge) TaskRequeued(name string, index int, unavailableHost string) {
	b.program.Send(TaskRequeuedMsg{
		Name:            name,
		Index:           index,
		UnavailableHost: unavailableHost,
	})
}

// AllCompleted forwards completion of all tasks to the TUI.
func (b *Bridge) AllCompleted(passed, failed int, duration time.Duration) {
	b.program.Send(AllCompletedMsg{
		Passed:   passed,
		Failed:   failed,
		Duration: duration,
	})
}

// OrchestratorDone signals that the orchestrator has finished.
func (b *Bridge) OrchestratorDone(err error) {
	b.program.Send(orchestratorDoneMsg{err: err})
}
