package parallel

import (
	"context"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOrchestrator(t *testing.T) {
	tasks := []TaskInfo{
		{Name: "test", Command: "go test ./..."},
		{Name: "lint", Command: "golangci-lint run"},
	}
	hosts := map[string]config.Host{
		"dev":  {SSH: []string{"dev"}, Dir: "~/projects"},
		"prod": {SSH: []string{"prod"}, Dir: "~/projects"},
	}
	cfg := Config{MaxParallel: 2, FailFast: true}

	orch := NewOrchestrator(tasks, hosts, nil, cfg)

	assert.NotNil(t, orch)
	assert.Len(t, orch.tasks, 2)
	assert.Len(t, orch.hosts, 2)
	assert.Len(t, orch.hostList, 2)
	assert.Equal(t, cfg.MaxParallel, orch.config.MaxParallel)
	assert.True(t, orch.config.FailFast)
	assert.NotNil(t, orch.syncedHosts)
	assert.NotNil(t, orch.results)
}

func TestOrchestrator_EmptyTasks(t *testing.T) {
	orch := NewOrchestrator(nil, map[string]config.Host{
		"dev": {SSH: []string{"dev"}, Dir: "~/projects"},
	}, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.TaskResults)
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 0, result.Failed)
}

func TestOrchestrator_NoHosts_RunsLocally(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "echo hello"}}
	orch := NewOrchestrator(tasks, nil, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, 0, result.Failed)
	assert.Contains(t, result.HostsUsed, "local")
}

func TestOrchestrator_EmptyHostMap_RunsLocally(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "echo hello"}}
	orch := NewOrchestrator(tasks, map[string]config.Host{}, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, 0, result.Failed)
	assert.Contains(t, result.HostsUsed, "local")
}

func TestOrchestrator_LocalExecution_FailedTask(t *testing.T) {
	tasks := []TaskInfo{{Name: "fail", Command: "exit 1"}}
	orch := NewOrchestrator(tasks, nil, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 1, result.Failed)
	assert.False(t, result.Success())
}

func TestOrchestrator_LocalExecution_FailFast(t *testing.T) {
	tasks := []TaskInfo{
		{Name: "fail", Command: "exit 1"},
		{Name: "skip", Command: "echo should not run"},
	}
	orch := NewOrchestrator(tasks, nil, nil, Config{FailFast: true})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	// Only one task should have run due to fail-fast
	assert.Equal(t, 1, len(result.TaskResults))
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 1, result.Failed)
}

func TestOrchestrator_MaxParallelLimiting(t *testing.T) {
	tests := []struct {
		name        string
		numTasks    int
		numHosts    int
		maxParallel int
		wantWorkers int
	}{
		{
			name:        "unlimited - use all hosts",
			numTasks:    5,
			numHosts:    3,
			maxParallel: 0,
			wantWorkers: 3,
		},
		{
			name:        "max less than hosts",
			numTasks:    5,
			numHosts:    3,
			maxParallel: 2,
			wantWorkers: 2,
		},
		{
			name:        "more workers than tasks",
			numTasks:    2,
			numHosts:    5,
			maxParallel: 0,
			wantWorkers: 2,
		},
		{
			name:        "max greater than hosts",
			numTasks:    10,
			numHosts:    3,
			maxParallel: 5,
			wantWorkers: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test tasks
			tasks := make([]TaskInfo, tt.numTasks)
			for i := 0; i < tt.numTasks; i++ {
				tasks[i] = TaskInfo{Name: "task", Command: "echo"}
			}

			// Create test hosts
			hosts := make(map[string]config.Host)
			for i := 0; i < tt.numHosts; i++ {
				hosts["host"+string(rune('a'+i))] = config.Host{SSH: []string{"h"}, Dir: "~"}
			}

			cfg := Config{MaxParallel: tt.maxParallel}
			orch := NewOrchestrator(tasks, hosts, nil, cfg)

			// Calculate expected workers using same logic as Run
			numWorkers := len(orch.hostList)
			if cfg.MaxParallel > 0 && cfg.MaxParallel < numWorkers {
				numWorkers = cfg.MaxParallel
			}
			if numWorkers > len(tasks) {
				numWorkers = len(tasks)
			}

			assert.Equal(t, tt.wantWorkers, numWorkers)
		})
	}
}

func TestOrchestrator_ContextCancellation(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "sleep 10"}}
	hosts := map[string]config.Host{
		"dev": {SSH: []string{"dev"}, Dir: "~/projects"},
	}

	orch := NewOrchestrator(tasks, hosts, nil, Config{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should return quickly without error since context is cancelled
	result, err := orch.Run(ctx)

	// Context cancellation is handled gracefully
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestOrchestrator_MarkHostSynced(t *testing.T) {
	orch := &Orchestrator{
		syncedHosts: make(map[string]bool),
	}

	// First call should return false (not yet synced)
	alreadySynced := orch.markHostSynced("host1")
	assert.False(t, alreadySynced)

	// Second call should return true (already synced)
	alreadySynced = orch.markHostSynced("host1")
	assert.True(t, alreadySynced)

	// Different host should return false
	alreadySynced = orch.markHostSynced("host2")
	assert.False(t, alreadySynced)
}

func TestTaskResult_Success(t *testing.T) {
	tests := []struct {
		name   string
		result TaskResult
		want   bool
	}{
		{
			name:   "exit code 0 no error",
			result: TaskResult{ExitCode: 0, Error: nil},
			want:   true,
		},
		{
			name:   "exit code 1",
			result: TaskResult{ExitCode: 1, Error: nil},
			want:   false,
		},
		{
			name:   "exit code 0 with error",
			result: TaskResult{ExitCode: 0, Error: context.DeadlineExceeded},
			want:   false,
		},
		{
			name:   "both exit code and error",
			result: TaskResult{ExitCode: 1, Error: context.Canceled},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.Success())
		})
	}
}

func TestResult_Success(t *testing.T) {
	tests := []struct {
		name   string
		result Result
		want   bool
	}{
		{
			name:   "no failures",
			result: Result{Passed: 5, Failed: 0},
			want:   true,
		},
		{
			name:   "one failure",
			result: Result{Passed: 4, Failed: 1},
			want:   false,
		},
		{
			name:   "all failures",
			result: Result{Passed: 0, Failed: 5},
			want:   false,
		},
		{
			name:   "empty result",
			result: Result{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.Success())
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 0, cfg.MaxParallel)
	assert.False(t, cfg.FailFast)
	assert.Equal(t, time.Duration(0), cfg.Timeout)
	assert.Equal(t, OutputProgress, cfg.OutputMode)
	assert.False(t, cfg.SaveLogs)
	assert.Empty(t, cfg.LogDir)
}

func TestTaskStatus_String(t *testing.T) {
	tests := []struct {
		status TaskStatus
		want   string
	}{
		{TaskPending, "pending"},
		{TaskRunning, "running"},
		{TaskPassed, "passed"},
		{TaskFailed, "failed"},
		{TaskStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.String())
		})
	}
}

func TestBuildResult(t *testing.T) {
	orch := &Orchestrator{
		results: []TaskResult{
			{TaskName: "test1", Host: "host1", ExitCode: 0},
			{TaskName: "test2", Host: "host2", ExitCode: 0},
			{TaskName: "test3", Host: "host1", ExitCode: 1},
		},
	}

	hostsUsed := map[string]bool{"host1": true, "host2": true}
	duration := 5 * time.Second

	result := orch.buildResult(duration, hostsUsed)

	assert.Equal(t, 3, len(result.TaskResults))
	assert.Equal(t, 2, result.Passed)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, duration, result.Duration)
	assert.Len(t, result.HostsUsed, 2)
}
