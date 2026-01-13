package parallel

import "time"

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
)

// Config holds configuration for parallel execution.
type Config struct {
	MaxParallel int           // Max concurrent tasks (0 = unlimited)
	FailFast    bool          // Stop on first failure
	Timeout     time.Duration // Per-task timeout (0 = no timeout)
	OutputMode  OutputMode    // How to display output
	SaveLogs    bool          // Write output to log files
	LogDir      string        // Directory for log files
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
	Host      string
	ExitCode  int
	Duration  time.Duration
	Error     error
	Output    []byte // Captured stdout+stderr for summary
	StartTime time.Time
	EndTime   time.Time
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
	Command string            // Command to execute
	Env     map[string]string // Environment variables
	WorkDir string            // Working directory on remote
}
