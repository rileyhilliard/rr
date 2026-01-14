package parallel

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOutputManager(t *testing.T) {
	tests := []struct {
		name         string
		mode         OutputMode
		isTTY        bool
		expectedMode OutputMode
	}{
		{
			name:         "progress mode with TTY",
			mode:         OutputProgress,
			isTTY:        true,
			expectedMode: OutputProgress,
		},
		{
			name:         "progress mode without TTY falls back to quiet",
			mode:         OutputProgress,
			isTTY:        false,
			expectedMode: OutputQuiet,
		},
		{
			name:         "stream mode with TTY",
			mode:         OutputStream,
			isTTY:        true,
			expectedMode: OutputStream,
		},
		{
			name:         "stream mode without TTY",
			mode:         OutputStream,
			isTTY:        false,
			expectedMode: OutputStream,
		},
		{
			name:         "verbose mode with TTY",
			mode:         OutputVerbose,
			isTTY:        true,
			expectedMode: OutputVerbose,
		},
		{
			name:         "verbose mode without TTY",
			mode:         OutputVerbose,
			isTTY:        false,
			expectedMode: OutputVerbose,
		},
		{
			name:         "quiet mode with TTY",
			mode:         OutputQuiet,
			isTTY:        true,
			expectedMode: OutputQuiet,
		},
		{
			name:         "quiet mode without TTY",
			mode:         OutputQuiet,
			isTTY:        false,
			expectedMode: OutputQuiet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewOutputManager(tt.mode, tt.isTTY)

			require.NotNil(t, mgr)
			assert.Equal(t, tt.expectedMode, mgr.mode)
			assert.Equal(t, tt.isTTY, mgr.isTTY)
			assert.NotNil(t, mgr.taskStatus)
			assert.NotNil(t, mgr.taskHosts)
			assert.NotNil(t, mgr.taskOutput)
		})
	}
}

func TestOutputManager_StreamMode_PrefixesLines(t *testing.T) {
	var buf bytes.Buffer
	mgr := NewOutputManager(OutputStream, true)
	mgr.SetWriter(&buf)

	// Start a task (syncing phase)
	mgr.TaskSyncing("test-task", 0, "dev-host")
	assert.Contains(t, buf.String(), "[dev-host:test-task]")
	assert.Contains(t, buf.String(), "syncing")

	buf.Reset()

	// Output some lines
	mgr.TaskOutput("test-task", 0, []byte("line 1"), false)
	mgr.TaskOutput("test-task", 0, []byte("line 2"), true) // stderr

	output := buf.String()
	assert.Contains(t, output, "[dev-host:test-task]")
	assert.Contains(t, output, "line 1")
	assert.Contains(t, output, "line 2")
}

func TestOutputManager_QuietMode_NoOutput(t *testing.T) {
	var buf bytes.Buffer
	mgr := NewOutputManager(OutputQuiet, true)
	mgr.SetWriter(&buf)

	mgr.TaskStarted("test", 0, "host1")
	mgr.TaskOutput("test", 0, []byte("some output"), false)
	mgr.TaskCompleted(TaskResult{
		TaskName:  "test",
		TaskIndex: 0,
		Host:      "host1",
		ExitCode:  0,
		Duration:  1 * time.Second,
	})

	assert.Empty(t, buf.String())
}

func TestOutputManager_TaskStatus(t *testing.T) {
	mgr := NewOutputManager(OutputQuiet, true)

	// Initially no status
	assert.Equal(t, TaskStatus(0), mgr.GetTaskStatus("unknown", 0))

	// Task syncing (assigned to host, waiting for lock/sync)
	mgr.TaskSyncing("test", 0, "host1")
	assert.Equal(t, TaskSyncing, mgr.GetTaskStatus("test", 0))

	// Task executing (command running)
	mgr.TaskExecuting("test", 0)
	assert.Equal(t, TaskRunning, mgr.GetTaskStatus("test", 0))

	// Complete successfully
	mgr.TaskCompleted(TaskResult{
		TaskName:  "test",
		TaskIndex: 0,
		Host:      "host1",
		ExitCode:  0,
	})
	assert.Equal(t, TaskPassed, mgr.GetTaskStatus("test", 0))

	// Start and fail another task
	mgr.TaskSyncing("failing", 1, "host2")
	mgr.TaskExecuting("failing", 1)
	mgr.TaskCompleted(TaskResult{
		TaskName:  "failing",
		TaskIndex: 1,
		Host:      "host2",
		ExitCode:  1,
	})
	assert.Equal(t, TaskFailed, mgr.GetTaskStatus("failing", 1))
}

func TestOutputManager_GetAllStatuses(t *testing.T) {
	mgr := NewOutputManager(OutputQuiet, true)

	mgr.TaskStarted("task1", 0, "host1")
	mgr.TaskStarted("task2", 1, "host2")
	mgr.TaskCompleted(TaskResult{TaskName: "task1", TaskIndex: 0, ExitCode: 0})
	mgr.TaskCompleted(TaskResult{TaskName: "task2", TaskIndex: 1, ExitCode: 1})

	statuses := mgr.GetAllStatuses()

	assert.Len(t, statuses, 2)
	assert.Equal(t, TaskPassed, statuses["task1#0"])
	assert.Equal(t, TaskFailed, statuses["task2#1"])
}

func TestOutputManager_BuffersOutput(t *testing.T) {
	mgr := NewOutputManager(OutputProgress, true)

	mgr.TaskStarted("test", 0, "host1")
	mgr.TaskOutput("test", 0, []byte("first line"), false)
	mgr.TaskOutput("test", 0, []byte("second line"), false)

	// Verify output is buffered
	mgr.mu.Lock()
	buf, ok := mgr.taskOutput["test#0"]
	mgr.mu.Unlock()

	require.True(t, ok)
	assert.Contains(t, buf.String(), "first line")
	assert.Contains(t, buf.String(), "second line")
}

func TestOutputManager_VerboseMode(t *testing.T) {
	var buf bytes.Buffer
	mgr := NewOutputManager(OutputVerbose, true)
	mgr.SetWriter(&buf)

	mgr.TaskSyncing("test", 0, "dev")
	assert.Contains(t, buf.String(), "Syncing")
	assert.Contains(t, buf.String(), "test")
	assert.Contains(t, buf.String(), "dev")

	buf.Reset()

	// Add some output
	mgr.TaskOutput("test", 0, []byte("test output"), false)

	// Complete the task
	mgr.TaskCompleted(TaskResult{
		TaskName:  "test",
		TaskIndex: 0,
		Host:      "dev",
		ExitCode:  0,
		Duration:  2500 * time.Millisecond,
	})

	output := buf.String()
	assert.Contains(t, output, "test")
	assert.Contains(t, output, "dev")
	assert.Contains(t, output, "2.5s")
}

func TestOutputManager_ProgressMode(t *testing.T) {
	var buf bytes.Buffer
	mgr := NewOutputManager(OutputProgress, true)
	mgr.SetWriter(&buf)

	mgr.TaskStarted("test", 0, "dev")

	output := buf.String()
	assert.Contains(t, output, "test")
	assert.Contains(t, output, "[dev]")

	buf.Reset()

	mgr.TaskCompleted(TaskResult{
		TaskName:  "test",
		TaskIndex: 0,
		Host:      "dev",
		ExitCode:  0,
	})

	output = buf.String()
	assert.Contains(t, output, "test")
}

func TestOutputManager_Close(t *testing.T) {
	mgr := NewOutputManager(OutputQuiet, true)
	// Close should not panic
	mgr.Close()
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{50 * time.Millisecond, "0.05s"},
		{500 * time.Millisecond, "0.5s"},
		{1 * time.Second, "1.0s"},
		{5500 * time.Millisecond, "5.5s"},
		{59 * time.Second, "59.0s"},
		{60 * time.Second, "1m0.0s"},
		{90 * time.Second, "1m30.0s"},
		{125 * time.Second, "2m5.0s"},
	}

	for _, tt := range tests {
		t.Run(tt.duration.String(), func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestOutputMode_Constants(t *testing.T) {
	assert.Equal(t, OutputMode("progress"), OutputProgress)
	assert.Equal(t, OutputMode("stream"), OutputStream)
	assert.Equal(t, OutputMode("verbose"), OutputVerbose)
	assert.Equal(t, OutputMode("quiet"), OutputQuiet)
}

func TestOutputManager_SetWriter(t *testing.T) {
	mgr := NewOutputManager(OutputQuiet, true)

	var buf bytes.Buffer
	mgr.SetWriter(&buf)

	mgr.mu.Lock()
	assert.Equal(t, &buf, mgr.w)
	mgr.mu.Unlock()
}

func TestOutputManager_RenderProgress_NonTTY(t *testing.T) {
	mgr := NewOutputManager(OutputProgress, false)
	// Should be safe to call even for non-TTY
	mgr.RenderProgress()
}

func TestOutputManager_TaskHostTracking(t *testing.T) {
	mgr := NewOutputManager(OutputQuiet, true)

	mgr.TaskStarted("task1", 0, "host-a")
	mgr.TaskStarted("task2", 1, "host-b")

	mgr.mu.Lock()
	assert.Equal(t, "host-a", mgr.taskHosts["task1#0"])
	assert.Equal(t, "host-b", mgr.taskHosts["task2#1"])
	mgr.mu.Unlock()
}

// TestOutputManager_DuplicateTaskNames verifies that tasks with the same name
// but different indices are tracked separately (regression test for the bug
// where running the same task multiple times in parallel caused state confusion).
func TestOutputManager_DuplicateTaskNames(t *testing.T) {
	mgr := NewOutputManager(OutputQuiet, true)

	// Start three tasks with the same name but different indices
	mgr.TaskSyncing("test-opendata", 0, "host1")
	mgr.TaskSyncing("test-opendata", 1, "host2")
	mgr.TaskSyncing("test-opendata", 2, "host3")

	// Verify each is tracked separately
	assert.Equal(t, TaskSyncing, mgr.GetTaskStatus("test-opendata", 0))
	assert.Equal(t, TaskSyncing, mgr.GetTaskStatus("test-opendata", 1))
	assert.Equal(t, TaskSyncing, mgr.GetTaskStatus("test-opendata", 2))

	// Transition to executing
	mgr.TaskExecuting("test-opendata", 0)
	mgr.TaskExecuting("test-opendata", 1)
	mgr.TaskExecuting("test-opendata", 2)

	assert.Equal(t, TaskRunning, mgr.GetTaskStatus("test-opendata", 0))
	assert.Equal(t, TaskRunning, mgr.GetTaskStatus("test-opendata", 1))
	assert.Equal(t, TaskRunning, mgr.GetTaskStatus("test-opendata", 2))

	// Complete in different order with different results
	mgr.TaskCompleted(TaskResult{TaskName: "test-opendata", TaskIndex: 1, ExitCode: 0})
	mgr.TaskCompleted(TaskResult{TaskName: "test-opendata", TaskIndex: 2, ExitCode: 1})
	mgr.TaskCompleted(TaskResult{TaskName: "test-opendata", TaskIndex: 0, ExitCode: 0})

	// Verify each has correct final status
	assert.Equal(t, TaskPassed, mgr.GetTaskStatus("test-opendata", 0))
	assert.Equal(t, TaskPassed, mgr.GetTaskStatus("test-opendata", 1))
	assert.Equal(t, TaskFailed, mgr.GetTaskStatus("test-opendata", 2))

	// Verify all three are in the status map
	statuses := mgr.GetAllStatuses()
	assert.Len(t, statuses, 3)
	assert.Equal(t, TaskPassed, statuses["test-opendata#0"])
	assert.Equal(t, TaskPassed, statuses["test-opendata#1"])
	assert.Equal(t, TaskFailed, statuses["test-opendata#2"])
}
