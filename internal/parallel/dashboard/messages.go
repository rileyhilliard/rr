package dashboard

import (
	"time"

	"github.com/rileyhilliard/rr/internal/parallel"
)

// InitTasksMsg signals that tasks have been initialized.
type InitTasksMsg struct {
	Tasks []parallel.TaskInit
}

// TaskSyncingMsg signals a task has started syncing to a host.
type TaskSyncingMsg struct {
	Name  string
	Index int
	Host  string
}

// TaskExecutingMsg signals a task command has started executing.
type TaskExecutingMsg struct {
	Name  string
	Index int
}

// TaskCompletedMsg signals a task has finished.
type TaskCompletedMsg struct {
	Name     string
	Index    int
	Success  bool
	Duration time.Duration
}

// TaskRequeuedMsg signals a task was re-queued due to host unavailability.
type TaskRequeuedMsg struct {
	Name            string
	Index           int
	UnavailableHost string
}

// AllCompletedMsg signals all tasks have finished.
type AllCompletedMsg struct {
	Passed   int
	Failed   int
	Duration time.Duration
}

// tickMsg signals a periodic refresh for spinner animation.
type tickMsg time.Time

// orchestratorDoneMsg signals the orchestrator has finished.
type orchestratorDoneMsg struct {
	err error
}
