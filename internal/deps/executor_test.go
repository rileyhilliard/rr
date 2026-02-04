package deps

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutionResult_Success(t *testing.T) {
	tests := []struct {
		name        string
		failedStage int
		want        bool
	}{
		{
			name:        "no failures",
			failedStage: -1,
			want:        true,
		},
		{
			name:        "first stage failed",
			failedStage: 0,
			want:        false,
		},
		{
			name:        "later stage failed",
			failedStage: 2,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ExecutionResult{FailedStage: tt.failedStage}
			assert.Equal(t, tt.want, result.Success())
		})
	}
}

func TestExecutionResult_ExitCode(t *testing.T) {
	tests := []struct {
		name         string
		stageResults []*StageResult
		want         int
	}{
		{
			name:         "empty results",
			stageResults: []*StageResult{},
			want:         0,
		},
		{
			name: "all passed",
			stageResults: []*StageResult{
				{ExitCode: 0},
				{ExitCode: 0},
			},
			want: 0,
		},
		{
			name: "first failed",
			stageResults: []*StageResult{
				{ExitCode: 1},
				{ExitCode: 0},
			},
			want: 1,
		},
		{
			name: "second failed",
			stageResults: []*StageResult{
				{ExitCode: 0},
				{ExitCode: 2},
			},
			want: 2,
		},
		{
			name: "multiple failures returns first",
			stageResults: []*StageResult{
				{ExitCode: 0},
				{ExitCode: 3},
				{ExitCode: 5},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ExecutionResult{StageResults: tt.stageResults}
			assert.Equal(t, tt.want, result.ExitCode())
		})
	}
}

func TestStageResult_Success(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		want     bool
	}{
		{name: "zero exit code", exitCode: 0, want: true},
		{name: "non-zero exit code", exitCode: 1, want: false},
		{name: "high exit code", exitCode: 127, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &StageResult{ExitCode: tt.exitCode}
			assert.Equal(t, tt.want, result.Success())
		})
	}
}

func TestNewExecutor(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{
				"test": {Run: "echo test"},
			},
		},
	}
	conn := &host.Connection{IsLocal: true}

	t.Run("defaults stdout and stderr", func(t *testing.T) {
		executor := NewExecutor(resolved, conn, ExecutorOptions{})
		require.NotNil(t, executor)
		assert.NotNil(t, executor.opts.Stdout)
		assert.NotNil(t, executor.opts.Stderr)
	})

	t.Run("uses custom writers", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		executor := NewExecutor(resolved, conn, ExecutorOptions{
			Stdout: &stdout,
			Stderr: &stderr,
		})
		require.NotNil(t, executor)
		assert.Equal(t, &stdout, executor.opts.Stdout)
		assert.Equal(t, &stderr, executor.opts.Stderr)
	})

	t.Run("preserves options", func(t *testing.T) {
		opts := ExecutorOptions{
			FailFast:      true,
			Quiet:         true,
			SetupCommands: []string{"cd /tmp"},
			WorkDir:       "/home/user/project",
		}
		executor := NewExecutor(resolved, conn, opts)
		assert.True(t, executor.opts.FailFast)
		assert.True(t, executor.opts.Quiet)
		assert.Equal(t, []string{"cd /tmp"}, executor.opts.SetupCommands)
		assert.Equal(t, "/home/user/project", executor.opts.WorkDir)
	})
}

func TestExecutor_Execute_LocalSimple(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{
				"echo-test": {Run: "echo hello"},
			},
		},
		Global: &config.GlobalConfig{},
	}
	conn := &host.Connection{IsLocal: true}

	var stdout, stderr bytes.Buffer
	executor := NewExecutor(resolved, conn, ExecutorOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	plan := &ExecutionPlan{
		TargetTask: "echo-test",
		Stages: []Stage{
			{Tasks: []string{"echo-test"}, Parallel: false},
		},
	}

	result, err := executor.Execute(context.Background(), plan)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success())
	assert.Equal(t, -1, result.FailedStage)
	assert.Equal(t, 0, result.ExitCode())
	assert.Len(t, result.StageResults, 1)
	assert.Contains(t, stdout.String(), "hello")
}

func TestExecutor_Execute_LocalMultiStage(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{
				"task1": {Run: "echo stage1"},
				"task2": {Run: "echo stage2"},
			},
		},
		Global: &config.GlobalConfig{},
	}
	conn := &host.Connection{IsLocal: true}

	var stdout, stderr bytes.Buffer
	executor := NewExecutor(resolved, conn, ExecutorOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	plan := &ExecutionPlan{
		TargetTask: "task2",
		Stages: []Stage{
			{Tasks: []string{"task1"}, Parallel: false},
			{Tasks: []string{"task2"}, Parallel: false},
		},
	}

	result, err := executor.Execute(context.Background(), plan)
	require.NoError(t, err)

	assert.True(t, result.Success())
	assert.Len(t, result.StageResults, 2)
	assert.Contains(t, stdout.String(), "stage1")
	assert.Contains(t, stdout.String(), "stage2")
}

func TestExecutor_Execute_FailFast(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{
				"fail":    {Run: "exit 1"},
				"succeed": {Run: "echo should not run"},
			},
		},
		Global: &config.GlobalConfig{},
	}
	conn := &host.Connection{IsLocal: true}

	var stdout, stderr bytes.Buffer
	executor := NewExecutor(resolved, conn, ExecutorOptions{
		Stdout:   &stdout,
		Stderr:   &stderr,
		FailFast: true,
	})

	plan := &ExecutionPlan{
		TargetTask: "succeed",
		Stages: []Stage{
			{Tasks: []string{"fail"}, Parallel: false},
			{Tasks: []string{"succeed"}, Parallel: false},
		},
	}

	result, err := executor.Execute(context.Background(), plan)
	require.NoError(t, err)

	assert.False(t, result.Success())
	assert.Equal(t, 0, result.FailedStage)
	assert.True(t, result.FailFast)
	assert.NotContains(t, stdout.String(), "should not run")
}

func TestExecutor_Execute_TaskNotFound(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{},
		},
		Global: &config.GlobalConfig{},
	}
	conn := &host.Connection{IsLocal: true}

	var stdout, stderr bytes.Buffer
	executor := NewExecutor(resolved, conn, ExecutorOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	plan := &ExecutionPlan{
		TargetTask: "missing",
		Stages: []Stage{
			{Tasks: []string{"missing"}, Parallel: false},
		},
	}

	result, err := executor.Execute(context.Background(), plan)
	// The executor returns an error when task is not found
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NotNil(t, result)
}

func TestExecutor_Execute_ContextCancellation(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{
				"task": {Run: "echo test"},
			},
		},
		Global: &config.GlobalConfig{},
	}
	conn := &host.Connection{IsLocal: true}

	var stdout, stderr bytes.Buffer
	executor := NewExecutor(resolved, conn, ExecutorOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	plan := &ExecutionPlan{
		TargetTask: "task",
		Stages: []Stage{
			{Tasks: []string{"task"}, Parallel: false},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := executor.Execute(ctx, plan)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.NotNil(t, result)
}

func TestExecutor_Execute_ParallelStage(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{
				"task1": {Run: "echo task1"},
				"task2": {Run: "echo task2"},
			},
		},
		Global: &config.GlobalConfig{},
	}
	conn := &host.Connection{IsLocal: true}

	// Use io.Discard for parallel tests to avoid race on shared buffer
	// The parallel executor runs tasks concurrently which would race on stdout
	executor := NewExecutor(resolved, conn, ExecutorOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})

	plan := &ExecutionPlan{
		TargetTask: "task2",
		Stages: []Stage{
			{Tasks: []string{"task1", "task2"}, Parallel: true},
		},
	}

	result, err := executor.Execute(context.Background(), plan)
	require.NoError(t, err)

	assert.True(t, result.Success())
	assert.Len(t, result.StageResults, 1)
	// Both tasks should have results
	stageResult := result.StageResults[0]
	assert.Len(t, stageResult.TaskResults, 2)
	assert.Contains(t, stageResult.TaskResults, "task1")
	assert.Contains(t, stageResult.TaskResults, "task2")
}

// MockStageHandler implements StageHandler for testing
type MockStageHandler struct {
	stageStarts    []int
	stageCompletes []int
	taskStarts     []string
	taskCompletes  []string
}

func (m *MockStageHandler) OnStageStart(stageNum, totalStages int, stage Stage) {
	m.stageStarts = append(m.stageStarts, stageNum)
}

func (m *MockStageHandler) OnStageComplete(stageNum, totalStages int, stage Stage, result *StageResult, duration time.Duration) {
	m.stageCompletes = append(m.stageCompletes, stageNum)
}

func (m *MockStageHandler) OnTaskStart(taskName string, parallel bool) {
	m.taskStarts = append(m.taskStarts, taskName)
}

func (m *MockStageHandler) OnTaskComplete(taskName string, exitCode int, duration time.Duration) {
	m.taskCompletes = append(m.taskCompletes, taskName)
}

func TestExecutor_Execute_StageHandler(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{
				"task1": {Run: "echo task1"},
				"task2": {Run: "echo task2"},
			},
		},
		Global: &config.GlobalConfig{},
	}
	conn := &host.Connection{IsLocal: true}

	handler := &MockStageHandler{}
	var stdout, stderr bytes.Buffer
	executor := NewExecutor(resolved, conn, ExecutorOptions{
		Stdout:       &stdout,
		Stderr:       &stderr,
		StageHandler: handler,
	})

	plan := &ExecutionPlan{
		TargetTask: "task2",
		Stages: []Stage{
			{Tasks: []string{"task1"}, Parallel: false},
			{Tasks: []string{"task2"}, Parallel: false},
		},
	}

	result, err := executor.Execute(context.Background(), plan)
	require.NoError(t, err)
	assert.True(t, result.Success())

	// Verify handler was called correctly
	assert.Equal(t, []int{1, 2}, handler.stageStarts)
	assert.Equal(t, []int{1, 2}, handler.stageCompletes)
	assert.Equal(t, []string{"task1", "task2"}, handler.taskStarts)
	assert.Equal(t, []string{"task1", "task2"}, handler.taskCompletes)
}

func TestExecutor_Execute_EmptyPlan(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{
			Tasks: map[string]config.TaskConfig{},
		},
		Global: &config.GlobalConfig{},
	}
	conn := &host.Connection{IsLocal: true}

	var stdout, stderr bytes.Buffer
	executor := NewExecutor(resolved, conn, ExecutorOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	plan := &ExecutionPlan{
		TargetTask: "empty",
		Stages:     []Stage{},
	}

	result, err := executor.Execute(context.Background(), plan)
	require.NoError(t, err)
	assert.True(t, result.Success())
	assert.Empty(t, result.StageResults)
}
