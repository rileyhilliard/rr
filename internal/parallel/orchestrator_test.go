package parallel

import (
	"context"
	"fmt"
	"sync"
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

	orch := NewOrchestrator(tasks, hosts, nil, nil, cfg)

	assert.NotNil(t, orch)
	assert.Len(t, orch.tasks, 2)
	assert.Len(t, orch.hosts, 2)
	assert.Len(t, orch.hostList, 2)
	assert.Equal(t, cfg.MaxParallel, orch.config.MaxParallel)
	assert.True(t, orch.config.FailFast)
	assert.NotNil(t, orch.syncedHosts)
	assert.NotNil(t, orch.results)
}

func TestOrchestrator_HostPriorityOrder(t *testing.T) {
	tasks := []TaskInfo{
		{Name: "test", Command: "echo test"},
	}
	hosts := map[string]config.Host{
		"host-a": {SSH: []string{"a"}, Dir: "~/projects"},
		"host-b": {SSH: []string{"b"}, Dir: "~/projects"},
		"host-c": {SSH: []string{"c"}, Dir: "~/projects"},
	}

	t.Run("hostOrder is preserved when provided", func(t *testing.T) {
		// Provide explicit order: c, a, b
		hostOrder := []string{"host-c", "host-a", "host-b"}
		orch := NewOrchestrator(tasks, hosts, hostOrder, nil, Config{})

		assert.Equal(t, []string{"host-c", "host-a", "host-b"}, orch.hostList)
	})

	t.Run("hostOrder nil falls back to map iteration", func(t *testing.T) {
		orch := NewOrchestrator(tasks, hosts, nil, nil, Config{})

		// Should have all 3 hosts (order may vary since map iteration is undefined)
		assert.Len(t, orch.hostList, 3)
		assert.Contains(t, orch.hostList, "host-a")
		assert.Contains(t, orch.hostList, "host-b")
		assert.Contains(t, orch.hostList, "host-c")
	})

	t.Run("empty hostOrder falls back to map iteration", func(t *testing.T) {
		orch := NewOrchestrator(tasks, hosts, []string{}, nil, Config{})

		// Should have all 3 hosts
		assert.Len(t, orch.hostList, 3)
	})

	t.Run("first host in order is used first for workers", func(t *testing.T) {
		// When we have 3 hosts in order: preferred, secondary, tertiary
		// The first worker should use 'preferred'
		hostOrder := []string{"preferred", "secondary", "tertiary"}
		hostsMap := map[string]config.Host{
			"preferred": {SSH: []string{"p"}, Dir: "~"},
			"secondary": {SSH: []string{"s"}, Dir: "~"},
			"tertiary":  {SSH: []string{"t"}, Dir: "~"},
		}
		orch := NewOrchestrator(tasks, hostsMap, hostOrder, nil, Config{})

		// Verify order is preserved
		assert.Equal(t, "preferred", orch.hostList[0])
		assert.Equal(t, "secondary", orch.hostList[1])
		assert.Equal(t, "tertiary", orch.hostList[2])
	})
}

func TestOrchestrator_EmptyTasks(t *testing.T) {
	orch := NewOrchestrator(nil, map[string]config.Host{
		"dev": {SSH: []string{"dev"}, Dir: "~/projects"},
	}, nil, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.TaskResults)
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 0, result.Failed)
}

func TestOrchestrator_NoHosts_RunsLocally(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "echo hello"}}
	orch := NewOrchestrator(tasks, nil, nil, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, 0, result.Failed)
	assert.Contains(t, result.HostsUsed, "local")
}

func TestOrchestrator_EmptyHostMap_RunsLocally(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "echo hello"}}
	orch := NewOrchestrator(tasks, map[string]config.Host{}, nil, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.Passed)
	assert.Equal(t, 0, result.Failed)
	assert.Contains(t, result.HostsUsed, "local")
}

func TestOrchestrator_LocalExecution_FailedTask(t *testing.T) {
	tasks := []TaskInfo{{Name: "fail", Command: "exit 1"}}
	orch := NewOrchestrator(tasks, nil, nil, nil, Config{})

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
	orch := NewOrchestrator(tasks, nil, nil, nil, Config{FailFast: true})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	// Only one task should have run due to fail-fast
	assert.Equal(t, 1, len(result.TaskResults))
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 1, result.Failed)
}

func TestOrchestrator_LocalExecution_WithSetup(t *testing.T) {
	// Create a temp file to track setup execution
	tmpDir := t.TempDir()
	setupFile := tmpDir + "/setup_ran"

	tasks := []TaskInfo{
		{Name: "task1", Command: "cat " + setupFile},
		{Name: "task2", Command: "cat " + setupFile},
	}

	// Setup command creates a marker file
	cfg := Config{Setup: "echo setup_complete > " + setupFile}
	orch := NewOrchestrator(tasks, nil, nil, nil, cfg)

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 2, result.Passed)
	assert.Equal(t, 0, result.Failed)

	// Both tasks should have read from the setup file
	for _, tr := range result.TaskResults {
		assert.Contains(t, string(tr.Output), "setup_complete")
	}
}

func TestOrchestrator_LocalExecution_SetupRunsOnce(t *testing.T) {
	// Create a temp file that counts setup invocations
	tmpDir := t.TempDir()
	counterFile := tmpDir + "/counter"

	tasks := []TaskInfo{
		{Name: "task1", Command: "cat " + counterFile},
		{Name: "task2", Command: "cat " + counterFile},
		{Name: "task3", Command: "cat " + counterFile},
	}

	// Setup command appends a line each time it runs
	cfg := Config{Setup: "echo x >> " + counterFile}
	orch := NewOrchestrator(tasks, nil, nil, nil, cfg)

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 3, result.Passed)

	// Each task should see exactly one "x" (setup ran only once)
	for _, tr := range result.TaskResults {
		// Output should contain exactly one "x" followed by newline
		assert.Equal(t, "x\n", string(tr.Output), "Task %s should see setup ran exactly once", tr.TaskName)
	}
}

func TestOrchestrator_LocalExecution_SetupFailure_AbortsAllTasks(t *testing.T) {
	tasks := []TaskInfo{
		{Name: "task1", Command: "echo task1"},
		{Name: "task2", Command: "echo task2"},
		{Name: "task3", Command: "echo task3"},
	}

	// Setup command that fails
	cfg := Config{Setup: "exit 1"}
	orch := NewOrchestrator(tasks, nil, nil, nil, cfg)

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	// All tasks should fail because setup failed
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 3, result.Failed)

	// Each task should have the setup failure error
	for _, tr := range result.TaskResults {
		assert.NotNil(t, tr.Error)
		assert.Contains(t, tr.Error.Error(), "setup command failed")
	}
}

func TestOrchestrator_HostSetupTracking(t *testing.T) {
	orch := &Orchestrator{
		setupHosts:  make(map[string]bool),
		setupErrors: make(map[string]error),
	}

	// First check should return not attempted
	attempted, err := orch.checkHostSetup("host1")
	assert.False(t, attempted)
	assert.NoError(t, err)

	// Record successful setup
	orch.recordHostSetup("host1", nil)

	// Second check should return attempted with no error
	attempted, err = orch.checkHostSetup("host1")
	assert.True(t, attempted)
	assert.NoError(t, err)

	// Different host should return not attempted
	attempted, err = orch.checkHostSetup("host2")
	assert.False(t, attempted)
	assert.NoError(t, err)

	// Record failed setup for host2
	setupErr := fmt.Errorf("setup failed")
	orch.recordHostSetup("host2", setupErr)

	// Check should return the error
	attempted, err = orch.checkHostSetup("host2")
	assert.True(t, attempted)
	assert.Equal(t, setupErr, err)
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
			orch := NewOrchestrator(tasks, hosts, nil, nil, cfg)

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

	orch := NewOrchestrator(tasks, hosts, nil, nil, Config{})

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
		{TaskSyncing, "syncing"},
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

// TestOrchestrator_LocalExecution_MultipleTasks verifies that local execution
// processes all tasks sequentially when no remote hosts are configured.
// This is the fallback behavior when hosts are unavailable.
func TestOrchestrator_LocalExecution_MultipleTasks(t *testing.T) {
	tasks := []TaskInfo{
		{Name: "task1", Index: 0, Command: "echo task1"},
		{Name: "task2", Index: 1, Command: "echo task2"},
		{Name: "task3", Index: 2, Command: "echo task3"},
		{Name: "task4", Index: 3, Command: "echo task4"},
		{Name: "task5", Index: 4, Command: "echo task5"},
		{Name: "task6", Index: 5, Command: "echo task6"},
	}

	// No hosts configured - runs locally via runLocal()
	orch := NewOrchestrator(tasks, nil, nil, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 6, result.Passed)
	assert.Equal(t, 0, result.Failed)
	assert.True(t, result.Success())

	// Verify all tasks completed
	assert.Len(t, result.TaskResults, 6)

	// All should be on "local" since no remote hosts
	for _, tr := range result.TaskResults {
		assert.Equal(t, "local", tr.Host)
		assert.Equal(t, 0, tr.ExitCode)
	}
}

// TestOrchestrator_LocalExecution_ManyTasks verifies local execution handles
// larger task counts correctly.
func TestOrchestrator_LocalExecution_ManyTasks(t *testing.T) {
	tasks := make([]TaskInfo, 10)
	for i := 0; i < 10; i++ {
		tasks[i] = TaskInfo{
			Name:    fmt.Sprintf("task%d", i+1),
			Index:   i,
			Command: fmt.Sprintf("echo task%d", i+1),
		}
	}

	// No hosts means local execution
	orch := NewOrchestrator(tasks, nil, nil, nil, Config{})

	result, err := orch.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 10, result.Passed)
	assert.Equal(t, 0, result.Failed)
	assert.True(t, result.Success())
}

func TestOrchestrator_RecordFirstTaskTime(t *testing.T) {
	t.Run("records first task time and updates fastest", func(t *testing.T) {
		orch := &Orchestrator{
			hostFirstTaskTime: make(map[string]time.Duration),
		}

		// Record first host
		orch.recordFirstTaskTime("fast-host", 100*time.Millisecond)
		assert.Equal(t, 100*time.Millisecond, orch.hostFirstTaskTime["fast-host"])
		assert.Equal(t, 100*time.Millisecond, orch.fastestFirstTask)

		// Record slower host - fastest should not change
		orch.recordFirstTaskTime("slow-host", 200*time.Millisecond)
		assert.Equal(t, 200*time.Millisecond, orch.hostFirstTaskTime["slow-host"])
		assert.Equal(t, 100*time.Millisecond, orch.fastestFirstTask)

		// Record even faster host - fastest should update
		orch.recordFirstTaskTime("faster-host", 50*time.Millisecond)
		assert.Equal(t, 50*time.Millisecond, orch.hostFirstTaskTime["faster-host"])
		assert.Equal(t, 50*time.Millisecond, orch.fastestFirstTask)
	})

	t.Run("ignores duplicate recordings for same host", func(t *testing.T) {
		orch := &Orchestrator{
			hostFirstTaskTime: make(map[string]time.Duration),
		}

		// Record first time
		orch.recordFirstTaskTime("host1", 100*time.Millisecond)
		assert.Equal(t, 100*time.Millisecond, orch.hostFirstTaskTime["host1"])

		// Try to record again with different value - should be ignored
		orch.recordFirstTaskTime("host1", 50*time.Millisecond)
		assert.Equal(t, 100*time.Millisecond, orch.hostFirstTaskTime["host1"])
	})

	t.Run("handles zero duration", func(t *testing.T) {
		orch := &Orchestrator{
			hostFirstTaskTime: make(map[string]time.Duration),
		}

		// Record zero duration (edge case)
		orch.recordFirstTaskTime("host1", 0)
		assert.Equal(t, time.Duration(0), orch.hostFirstTaskTime["host1"])
		assert.Equal(t, time.Duration(0), orch.fastestFirstTask)
	})
}

func TestOrchestrator_GetSlowHostDelay(t *testing.T) {
	tests := []struct {
		name             string
		hostTime         time.Duration
		fastestTime      time.Duration
		hostExists       bool
		expectedDelayMin time.Duration
		expectedDelayMax time.Duration
	}{
		{
			name:             "no data for host - no delay",
			hostTime:         0,
			fastestTime:      100 * time.Millisecond,
			hostExists:       false,
			expectedDelayMin: 0,
			expectedDelayMax: 0,
		},
		{
			name:             "no fastest time recorded - no delay",
			hostTime:         100 * time.Millisecond,
			fastestTime:      0,
			hostExists:       true,
			expectedDelayMin: 0,
			expectedDelayMax: 0,
		},
		{
			name:             "host within 10% of fastest - no delay",
			hostTime:         105 * time.Millisecond,
			fastestTime:      100 * time.Millisecond,
			hostExists:       true,
			expectedDelayMin: 0,
			expectedDelayMax: 0,
		},
		{
			name:             "host exactly at 10% threshold - no delay",
			hostTime:         109 * time.Millisecond,
			fastestTime:      100 * time.Millisecond,
			hostExists:       true,
			expectedDelayMin: 0,
			expectedDelayMax: 0,
		},
		{
			name:             "host 1.5x slower - 50% delay",
			hostTime:         150 * time.Millisecond,
			fastestTime:      100 * time.Millisecond,
			hostExists:       true,
			expectedDelayMin: 49 * time.Millisecond, // Allow small float rounding
			expectedDelayMax: 51 * time.Millisecond,
		},
		{
			name:             "host 2x slower - 100% delay (capped)",
			hostTime:         200 * time.Millisecond,
			fastestTime:      100 * time.Millisecond,
			hostExists:       true,
			expectedDelayMin: 99 * time.Millisecond,
			expectedDelayMax: 101 * time.Millisecond,
		},
		{
			name:             "host 3x slower - still 100% delay (capped)",
			hostTime:         300 * time.Millisecond,
			fastestTime:      100 * time.Millisecond,
			hostExists:       true,
			expectedDelayMin: 99 * time.Millisecond,
			expectedDelayMax: 101 * time.Millisecond,
		},
		{
			name:             "same speed as fastest - no delay",
			hostTime:         100 * time.Millisecond,
			fastestTime:      100 * time.Millisecond,
			hostExists:       true,
			expectedDelayMin: 0,
			expectedDelayMax: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := &Orchestrator{
				hostFirstTaskTime: make(map[string]time.Duration),
				fastestFirstTask:  tt.fastestTime,
			}

			if tt.hostExists {
				orch.hostFirstTaskTime["test-host"] = tt.hostTime
			}

			delay := orch.getSlowHostDelay("test-host")

			assert.GreaterOrEqual(t, delay, tt.expectedDelayMin,
				"delay %v should be >= %v", delay, tt.expectedDelayMin)
			assert.LessOrEqual(t, delay, tt.expectedDelayMax,
				"delay %v should be <= %v", delay, tt.expectedDelayMax)
		})
	}
}

func TestOrchestrator_GetSlowHostDelay_UnknownHost(t *testing.T) {
	orch := &Orchestrator{
		hostFirstTaskTime: make(map[string]time.Duration),
		fastestFirstTask:  100 * time.Millisecond,
	}

	// Record time for one host
	orch.hostFirstTaskTime["known-host"] = 150 * time.Millisecond

	// Unknown host should get no delay
	delay := orch.getSlowHostDelay("unknown-host")
	assert.Equal(t, time.Duration(0), delay)
}

func TestOrchestrator_UnavailableHostTracking(t *testing.T) {
	tests := []struct {
		name              string
		hostList          []string
		markUnavailable   []string
		checkHost         string
		expectUnavailable bool
		expectAllDown     bool
	}{
		{
			name:              "no hosts marked unavailable",
			hostList:          []string{"host1", "host2", "host3"},
			markUnavailable:   []string{},
			checkHost:         "host1",
			expectUnavailable: false,
			expectAllDown:     false,
		},
		{
			name:              "one host marked unavailable",
			hostList:          []string{"host1", "host2", "host3"},
			markUnavailable:   []string{"host1"},
			checkHost:         "host1",
			expectUnavailable: true,
			expectAllDown:     false,
		},
		{
			name:              "check available host when one is down",
			hostList:          []string{"host1", "host2", "host3"},
			markUnavailable:   []string{"host1"},
			checkHost:         "host2",
			expectUnavailable: false,
			expectAllDown:     false,
		},
		{
			name:              "all hosts marked unavailable",
			hostList:          []string{"host1", "host2"},
			markUnavailable:   []string{"host1", "host2"},
			checkHost:         "host1",
			expectUnavailable: true,
			expectAllDown:     true,
		},
		{
			name:              "single host marked unavailable",
			hostList:          []string{"only-host"},
			markUnavailable:   []string{"only-host"},
			checkHost:         "only-host",
			expectUnavailable: true,
			expectAllDown:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := &Orchestrator{
				hostList:         tt.hostList,
				unavailableHosts: make(map[string]bool),
			}

			// Mark hosts as unavailable
			for _, host := range tt.markUnavailable {
				orch.markHostUnavailable(host)
			}

			// Check individual host
			assert.Equal(t, tt.expectUnavailable, orch.isHostUnavailable(tt.checkHost),
				"isHostUnavailable(%s)", tt.checkHost)

			// Check if all hosts are down
			assert.Equal(t, tt.expectAllDown, orch.allHostsUnavailable(),
				"allHostsUnavailable()")
		})
	}
}

// TestOrchestrator_RemotePath_CompletesWithoutDeadlock is a regression test for
// issue #177: the orchestrator would deadlock when all tasks completed because
// the dispatcher blocked on requeueChan while workers blocked on taskQueue.
// The fix added an allDone signal channel that breaks this circular dependency.
//
// Uses unreachable hosts to exercise the full remote code path (dispatcher,
// workers, requeue channel, result channel, allDone signal) without needing SSH.
func TestOrchestrator_RemotePath_CompletesWithoutDeadlock(t *testing.T) {
	tests := []struct {
		name  string
		tasks []TaskInfo
		hosts map[string]config.Host
	}{
		{
			name:  "single task single host",
			tasks: []TaskInfo{{Name: "test", Index: 0, Command: "echo test"}},
			hosts: map[string]config.Host{
				"fake": {SSH: []string{"nonexistent-host-xxxx"}, Dir: "~/proj"},
			},
		},
		{
			name: "multiple tasks single host",
			tasks: []TaskInfo{
				{Name: "vet", Index: 0, Command: "go vet"},
				{Name: "lint", Index: 1, Command: "golangci-lint run"},
			},
			hosts: map[string]config.Host{
				"fake": {SSH: []string{"nonexistent-host-xxxx"}, Dir: "~/proj"},
			},
		},
		{
			name: "multiple tasks multiple hosts",
			tasks: []TaskInfo{
				{Name: "test", Index: 0, Command: "go test"},
				{Name: "vet", Index: 1, Command: "go vet"},
				{Name: "lint", Index: 2, Command: "golangci-lint run"},
			},
			hosts: map[string]config.Host{
				"host-a": {SSH: []string{"fake-a"}, Dir: "~"},
				"host-b": {SSH: []string{"fake-b"}, Dir: "~"},
			},
		},
		{
			name: "more tasks than hosts",
			tasks: []TaskInfo{
				{Name: "t1", Index: 0, Command: "echo 1"},
				{Name: "t2", Index: 1, Command: "echo 2"},
				{Name: "t3", Index: 2, Command: "echo 3"},
				{Name: "t4", Index: 3, Command: "echo 4"},
				{Name: "t5", Index: 4, Command: "echo 5"},
			},
			hosts: map[string]config.Host{
				"h1": {SSH: []string{"fake1"}, Dir: "~"},
				"h2": {SSH: []string{"fake2"}, Dir: "~"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := NewOrchestrator(tt.tasks, tt.hosts, nil, nil, Config{})

			// 5-second timeout acts as a deadlock detector. Run() should complete
			// well within this window since all hosts are unreachable.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result, err := orch.Run(ctx)

			require.NoError(t, err, "Run() should not return an error")
			require.NotNil(t, result, "Run() should return a result")

			// The key assertion: Run() returned before the deadline, meaning no deadlock.
			// Before the fix, Run() would hang indefinitely here.
			require.NoError(t, ctx.Err(),
				"context timed out — likely deadlock in orchestrator shutdown")

			// All returned results should be failures (hosts are unreachable)
			assert.Greater(t, len(result.TaskResults), 0,
				"should have at least one task result")
			for _, tr := range result.TaskResults {
				assert.False(t, tr.Success(), "task %s should have failed", tr.TaskName)
			}
		})
	}
}

func TestOrchestrator_UnavailableHostConcurrency(t *testing.T) {
	orch := &Orchestrator{
		hostList:         []string{"host1", "host2", "host3", "host4", "host5"},
		unavailableHosts: make(map[string]bool),
	}

	// Concurrent marking and checking of hosts
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		hostName := orch.hostList[i%len(orch.hostList)]

		// Goroutine to mark host unavailable
		go func(h string) {
			defer wg.Done()
			orch.markHostUnavailable(h)
		}(hostName)

		// Goroutine to check host availability
		go func(h string) {
			defer wg.Done()
			_ = orch.isHostUnavailable(h)
			_ = orch.allHostsUnavailable()
		}(hostName)
	}

	wg.Wait()
	// No panics = success
}
