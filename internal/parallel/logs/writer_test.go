package logs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogWriter(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := NewLogWriter(tmpDir, "test-task")
	require.NoError(t, err)
	require.NotNil(t, writer)

	// Verify directory was created
	assert.DirExists(t, writer.Dir())
	assert.Contains(t, writer.Dir(), "test-task-")

	// Verify it's under the base directory
	assert.True(t, strings.HasPrefix(writer.Dir(), tmpDir))
}

func TestNewLogWriter_TildeExpansion(t *testing.T) {
	// Skip if HOME not set
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set")
	}

	// Use a subdirectory under home to test tilde expansion
	writer, err := NewLogWriter("~/.rr-test-logs", "tilde-test")
	require.NoError(t, err)
	require.NotNil(t, writer)

	// Verify directory was created under home
	assert.Contains(t, writer.Dir(), filepath.Join(home, ".rr-test-logs"))

	// Clean up
	os.RemoveAll(filepath.Join(home, ".rr-test-logs"))
}

func TestLogWriter_WriteTask(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := NewLogWriter(tmpDir, "test-task")
	require.NoError(t, err)

	// Write task output
	output := []byte("hello stdout\nhello stderr\n")
	err = writer.WriteTask("my-task", 0, output)
	require.NoError(t, err)

	// Verify file was created
	expectedFile := filepath.Join(writer.Dir(), "my-task_0.log")
	assert.FileExists(t, expectedFile)

	// Read and verify content
	content, err := os.ReadFile(expectedFile)
	require.NoError(t, err)
	assert.Equal(t, output, content)
}

func TestLogWriter_WriteTask_DifferentIndices(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := NewLogWriter(tmpDir, "test-task")
	require.NoError(t, err)

	// Write multiple tasks with different indices
	for i := 0; i < 3; i++ {
		output := []byte("output for task " + string(rune('0'+i)))
		err = writer.WriteTask("task", i, output)
		require.NoError(t, err)
	}

	// Verify all files were created
	assert.FileExists(t, filepath.Join(writer.Dir(), "task_0.log"))
	assert.FileExists(t, filepath.Join(writer.Dir(), "task_1.log"))
	assert.FileExists(t, filepath.Join(writer.Dir(), "task_2.log"))
}

func TestLogWriter_WriteSummary(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := NewLogWriter(tmpDir, "summary-test")
	require.NoError(t, err)

	// Create a mock result
	result := &parallel.Result{
		Passed:    2,
		Failed:    1,
		Duration:  5 * time.Second,
		HostsUsed: []string{"host1", "host2"},
		TaskResults: []parallel.TaskResult{
			{TaskName: "task1", TaskIndex: 0, ExitCode: 0, Duration: time.Second, StartTime: time.Now().Add(-3 * time.Second)},
			{TaskName: "task2", TaskIndex: 1, ExitCode: 0, Duration: 2 * time.Second, StartTime: time.Now().Add(-2 * time.Second)},
			{TaskName: "task3", TaskIndex: 2, ExitCode: 1, Duration: 500 * time.Millisecond, StartTime: time.Now().Add(-time.Second)},
		},
	}

	// Write summary
	err = writer.WriteSummary(result, "summary-test")
	require.NoError(t, err)

	// Verify summary.json was created
	summaryPath := filepath.Join(writer.Dir(), "summary.json")
	assert.FileExists(t, summaryPath)

	// Read and verify it's valid JSON
	content, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "summary-test")
	assert.Contains(t, string(content), "passed")
	assert.Contains(t, string(content), "\"total\": 3") // Note: pretty-printed JSON has space
}

func TestLogWriter_WriteSummary_NilResult(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := NewLogWriter(tmpDir, "nil-test")
	require.NoError(t, err)

	// Nil result should not error
	err = writer.WriteSummary(nil, "nil-test")
	require.NoError(t, err)
}

func TestLogWriter_WriteSummary_EmptyTaskResults(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := NewLogWriter(tmpDir, "empty-test")
	require.NoError(t, err)

	// Empty result
	result := &parallel.Result{
		Passed:      0,
		Failed:      0,
		Duration:    time.Second,
		TaskResults: []parallel.TaskResult{},
	}

	err = writer.WriteSummary(result, "empty-test")
	require.NoError(t, err)

	// Verify summary.json was created
	summaryPath := filepath.Join(writer.Dir(), "summary.json")
	assert.FileExists(t, summaryPath)
}

func TestLogWriter_Dir(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := NewLogWriter(tmpDir, "dir-test")
	require.NoError(t, err)

	dir := writer.Dir()
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, "dir-test-")
}

func TestTaskLogFilename(t *testing.T) {
	tests := []struct {
		name     string
		taskName string
		index    int
		expected string
	}{
		{
			name:     "simple task",
			taskName: "test",
			index:    0,
			expected: "test_0.log",
		},
		{
			name:     "task with higher index",
			taskName: "build",
			index:    5,
			expected: "build_5.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := taskLogFilename(tt.taskName, tt.index)
			assert.Equal(t, tt.expected, result)
		})
	}
}
