package parallel

import (
	"fmt"
	"time"
)

// OutputMode controls how parallel task output is displayed.
type OutputMode string

const (
	// OutputProgress shows live status updates (default).
	OutputProgress OutputMode = "progress"
	// OutputStream shows real-time interleaved output with prefixes.
	OutputStream OutputMode = "stream"
	// OutputVerbose shows full output per task on completion.
	OutputVerbose OutputMode = "verbose"
	// OutputQuiet shows summary only.
	OutputQuiet OutputMode = "quiet"
	// OutputDashboard shows an interactive TUI dashboard.
	OutputDashboard OutputMode = "dashboard"
)

// Config holds configuration for parallel execution.
type Config struct {
	MaxParallel int           // Max concurrent tasks (0 = unlimited)
	FailFast    bool          // Stop on first failure
	Timeout     time.Duration // Per-task timeout (0 = no timeout)
	OutputMode  OutputMode    // How to display output
	SaveLogs    bool          // Write output to log files
	LogDir      string        // Directory for log files
	Setup       string        // Command to run once per host before subtasks
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxParallel: 0, // Unlimited (one per host)
		FailFast:    false,
		Timeout:     0, // No timeout
		OutputMode:  OutputProgress,
		SaveLogs:    false,
		LogDir:      "",
	}
}

// Result holds the aggregate result of parallel execution.
type Result struct {
	TaskResults []TaskResult  // Results for each task
	Duration    time.Duration // Total wall-clock time
	HostsUsed   []string      // Hosts that executed tasks
	Passed      int           // Count of passed tasks
	Failed      int           // Count of failed tasks
}

// Success returns true if all tasks passed.
func (r *Result) Success() bool {
	return r.Failed == 0
}

// TaskResult holds the result of a single task execution.
type TaskResult struct {
	TaskName  string
	TaskIndex int    // Position in task list (for duplicate name handling)
	Command   string // Original command (for formatter detection)
	Host      string
	ExitCode  int
	Duration  time.Duration
	Error     error
	Output    []byte // Captured stdout+stderr for summary
	StartTime time.Time
	EndTime   time.Time
}

// ID returns a unique identifier for this task result.
func (r *TaskResult) ID() string {
	return taskID(r.TaskName, r.TaskIndex)
}

// Success returns true if the task passed (exit code 0).
func (r *TaskResult) Success() bool {
	return r.ExitCode == 0 && r.Error == nil
}

// TaskStatus represents the current state of a task.
type TaskStatus int

const (
	TaskPending TaskStatus = iota
	TaskSyncing            // Assigned to host, connecting/syncing/waiting for lock
	TaskRunning            // Actually executing the command
	TaskPassed
	TaskFailed
)

// String returns the string representation of the task status.
func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskSyncing:
		return "syncing"
	case TaskRunning:
		return "running"
	case TaskPassed:
		return "passed"
	case TaskFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// TaskInfo represents a task to be executed with its resolved configuration.
type TaskInfo struct {
	Name    string            // Task name from config
	Index   int               // Position in task list (for duplicate name handling)
	Command string            // Command to execute
	Env     map[string]string // Environment variables
	WorkDir string            // Working directory on remote
}

// ID returns a unique identifier for this task.
func (t TaskInfo) ID() string {
	return taskID(t.Name, t.Index)
}

// taskID creates a unique task identifier from name and index.
func taskID(name string, index int) string {
	return fmt.Sprintf("%s#%d", name, index)
}

// DashboardBridge is the interface that the orchestrator uses to send events to a dashboard.
// This allows the dashboard TUI to receive real-time updates about task execution.
type DashboardBridge interface {
	InitTasks(tasks []TaskInit)
	TaskSyncing(name string, index int, host string)
	TaskExecuting(name string, index int)
	TaskCompleted(name string, index int, success bool, duration time.Duration)
	TaskRequeued(name string, index int, unavailableHost string)
	AllCompleted(passed, failed int, duration time.Duration)
	OrchestratorDone(err error)
}

// TaskInit holds initialization info for a task.
// Used by the dashboard bridge to initialize task entries.
type TaskInit struct {
	Name  string
	Index int
}
