package logs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/parallel"
)

// LogWriter writes task output to log files.
// Each parallel execution run gets its own directory with individual task logs
// and a summary.json file.
type LogWriter struct {
	dir     string // Base log directory (~/.rr/logs)
	taskDir string // Run-specific directory (~/.rr/logs/<task>-<timestamp>/)
	closed  bool
}

// SummaryJSON is the structure written to summary.json.
type SummaryJSON struct {
	TaskName    string           `json:"task_name"`
	StartTime   time.Time        `json:"start_time"`
	EndTime     time.Time        `json:"end_time"`
	Duration    string           `json:"duration"`
	Passed      int              `json:"passed"`
	Failed      int              `json:"failed"`
	Total       int              `json:"total"`
	HostsUsed   []string         `json:"hosts_used"`
	TaskResults []TaskResultJSON `json:"task_results"`
}

// TaskResultJSON is the per-task result in summary.json.
type TaskResultJSON struct {
	TaskName  string    `json:"task_name"`
	Host      string    `json:"host"`
	ExitCode  int       `json:"exit_code"`
	Duration  string    `json:"duration"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	LogFile   string    `json:"log_file"`
	Error     string    `json:"error,omitempty"`
}

// NewLogWriter creates a log directory for a parallel execution run.
// The directory is created immediately so tasks can write to it.
func NewLogWriter(baseDir, taskName string) (*LogWriter, error) {
	// Expand ~ in baseDir
	if len(baseDir) > 0 && baseDir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrConfig,
				"Can't determine home directory",
				"Check your environment configuration.")
		}
		baseDir = filepath.Join(home, baseDir[1:])
	}

	// Create timestamp-based directory name
	timestamp := time.Now().Format("20060102-150405")
	taskDir := filepath.Join(baseDir, fmt.Sprintf("%s-%s", taskName, timestamp))

	// Create the directory
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Can't create log directory "+taskDir,
			"Check your permissions for "+baseDir+".")
	}

	return &LogWriter{
		dir:     baseDir,
		taskDir: taskDir,
	}, nil
}

// WriteTask writes a task's output to <taskname>.log.
func (w *LogWriter) WriteTask(taskName string, output []byte) error {
	if w.closed {
		return errors.New(errors.ErrExec,
			"Log writer is closed",
			"This is unexpected - create a new LogWriter.")
	}

	// Sanitize task name for filename (replace / with -)
	safeTaskName := sanitizeFilename(taskName)
	logPath := filepath.Join(w.taskDir, safeTaskName+".log")

	if err := os.WriteFile(logPath, output, 0644); err != nil {
		return errors.WrapWithCode(err, errors.ErrExec,
			"Can't write task log "+logPath,
			"Check your permissions.")
	}

	return nil
}

// WriteSummary writes summary.json with all results.
func (w *LogWriter) WriteSummary(result *parallel.Result, taskName string) error {
	if w.closed {
		return errors.New(errors.ErrExec,
			"Log writer is closed",
			"This is unexpected - create a new LogWriter.")
	}

	if result == nil {
		return nil
	}

	// Build summary structure
	summary := SummaryJSON{
		TaskName:    taskName,
		StartTime:   time.Now().Add(-result.Duration),
		EndTime:     time.Now(),
		Duration:    result.Duration.String(),
		Passed:      result.Passed,
		Failed:      result.Failed,
		Total:       len(result.TaskResults),
		HostsUsed:   result.HostsUsed,
		TaskResults: make([]TaskResultJSON, len(result.TaskResults)),
	}

	for i := range result.TaskResults {
		tr := &result.TaskResults[i]
		errStr := ""
		if tr.Error != nil {
			errStr = tr.Error.Error()
		}

		summary.TaskResults[i] = TaskResultJSON{
			TaskName:  tr.TaskName,
			Host:      tr.Host,
			ExitCode:  tr.ExitCode,
			Duration:  tr.Duration.String(),
			StartTime: tr.StartTime,
			EndTime:   tr.EndTime,
			LogFile:   sanitizeFilename(tr.TaskName) + ".log",
			Error:     errStr,
		}
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrExec,
			"Can't encode summary JSON",
			"This is unexpected - check the result data.")
	}

	summaryPath := filepath.Join(w.taskDir, "summary.json")
	if err := os.WriteFile(summaryPath, data, 0644); err != nil {
		return errors.WrapWithCode(err, errors.ErrExec,
			"Can't write summary file "+summaryPath,
			"Check your permissions.")
	}

	return nil
}

// Dir returns the log directory path for this run.
func (w *LogWriter) Dir() string {
	return w.taskDir
}

// BaseDir returns the base log directory.
func (w *LogWriter) BaseDir() string {
	return w.dir
}

// Close finalizes logging.
func (w *LogWriter) Close() error {
	w.closed = true
	return nil
}

// sanitizeFilename replaces characters that aren't safe for filenames.
func sanitizeFilename(name string) string {
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
