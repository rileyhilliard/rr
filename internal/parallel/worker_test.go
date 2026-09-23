package parallel

import (
	"context"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalWorker_ExecuteTask_Success(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "echo hello"}}
	hosts := map[string]config.Host{}
	resolved := &config.ResolvedConfig{
		Project: &config.Config{},
		Global:  &config.GlobalConfig{},
	}

	orchestrator := NewOrchestrator(tasks, hosts, nil, resolved, Config{})
	worker := &localWorker{orchestrator: orchestrator}

	task := TaskInfo{
		Name:    "test-task",
		Index:   0,
		Command: "echo hello",
	}

	result := worker.executeTask(context.Background(), task)

	assert.Equal(t, "test-task", result.TaskName)
	assert.Equal(t, 0, result.TaskIndex)
	assert.Equal(t, "local", result.Host)
	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Error)
	assert.Contains(t, string(result.Output), "hello")
	assert.True(t, result.Duration > 0)
}

func TestLocalWorker_ExecuteTask_Failure(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "exit 42"}}
	hosts := map[string]config.Host{}
	resolved := &config.ResolvedConfig{
		Project: &config.Config{},
		Global:  &config.GlobalConfig{},
	}

	orchestrator := NewOrchestrator(tasks, hosts, nil, resolved, Config{})
	worker := &localWorker{orchestrator: orchestrator}

	task := TaskInfo{
		Name:    "failing-task",
		Index:   1,
		Command: "exit 42",
	}

	result := worker.executeTask(context.Background(), task)

	assert.Equal(t, "failing-task", result.TaskName)
	assert.Equal(t, 1, result.TaskIndex)
	assert.Equal(t, "local", result.Host)
	assert.Equal(t, 42, result.ExitCode)
	assert.True(t, result.Duration > 0)
}

func TestLocalWorker_ExecuteTask_WithEnv(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "echo $MY_TEST_VAR"}}
	hosts := map[string]config.Host{}
	resolved := &config.ResolvedConfig{
		Project: &config.Config{},
		Global:  &config.GlobalConfig{},
	}

	orchestrator := NewOrchestrator(tasks, hosts, nil, resolved, Config{})
	worker := &localWorker{orchestrator: orchestrator}

	task := TaskInfo{
		Name:    "env-task",
		Index:   0,
		Command: "echo $MY_TEST_VAR",
		Env: map[string]string{
			"MY_TEST_VAR": "test_value_123",
		},
	}

	result := worker.executeTask(context.Background(), task)

	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, string(result.Output), "test_value_123")
}

func TestLocalWorker_ExecuteTask_WithWorkDir(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "pwd"}}
	hosts := map[string]config.Host{}
	resolved := &config.ResolvedConfig{
		Project: &config.Config{},
		Global:  &config.GlobalConfig{},
	}

	orchestrator := NewOrchestrator(tasks, hosts, nil, resolved, Config{})
	worker := &localWorker{orchestrator: orchestrator}

	task := TaskInfo{
		Name:    "workdir-task",
		Index:   0,
		Command: "pwd",
		WorkDir: "/tmp",
	}

	result := worker.executeTask(context.Background(), task)

	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, string(result.Output), "/tmp")
}

func TestLocalWorker_ExecuteTask_WithSetup(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "echo done"}}
	hosts := map[string]config.Host{}
	resolved := &config.ResolvedConfig{
		Project: &config.Config{},
		Global:  &config.GlobalConfig{},
	}

	orchestrator := NewOrchestrator(tasks, hosts, nil, resolved, Config{
		Setup: "export SETUP_RAN=true",
	})
	worker := &localWorker{orchestrator: orchestrator}

	task := TaskInfo{
		Name:    "setup-task",
		Index:   0,
		Command: "echo done",
	}

	result := worker.executeTask(context.Background(), task)

	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, string(result.Output), "done")
}

func TestLocalWorker_ExecuteTask_SetupFailure(t *testing.T) {
	tasks := []TaskInfo{{Name: "test", Command: "echo should not run"}}
	hosts := map[string]config.Host{}
	resolved := &config.ResolvedConfig{
		Project: &config.Config{},
		Global:  &config.GlobalConfig{},
	}

	orchestrator := NewOrchestrator(tasks, hosts, nil, resolved, Config{
		Setup: "exit 1",
	})
	worker := &localWorker{orchestrator: orchestrator}

	task := TaskInfo{
		Name:    "setup-fail-task",
		Index:   0,
		Command: "echo should not run",
	}

	result := worker.executeTask(context.Background(), task)

	assert.NotEqual(t, 0, result.ExitCode)
	assert.NotNil(t, result.Error)
	assert.NotContains(t, string(result.Output), "should not run")
}

func TestTaskInfo_ID(t *testing.T) {
	info := TaskInfo{
		Name:  "test-task",
		Index: 5,
	}

	assert.Equal(t, "test-task#5", info.ID())
}

// TestResolveWorkDir tests the resolveWorkDir helper function that determines
// the sync directory. This is the core fix for issue #163.
func TestResolveWorkDir(t *testing.T) {
	tests := []struct {
		name     string
		resolved *config.ResolvedConfig
		expected string
	}{
		{
			name: "uses ProjectRoot when set",
			resolved: &config.ResolvedConfig{
				ProjectRoot: "/home/user/myproject",
				Project:     &config.Config{},
				Global:      &config.GlobalConfig{},
			},
			expected: "/home/user/myproject",
		},
		{
			name: "returns empty when ProjectRoot is empty",
			resolved: &config.ResolvedConfig{
				ProjectRoot: "",
				Project:     &config.Config{},
				Global:      &config.GlobalConfig{},
			},
			expected: "",
		},
		{
			name:     "returns empty when resolved is nil",
			resolved: nil,
			expected: "",
		},
		{
			name: "handles absolute path",
			resolved: &config.ResolvedConfig{
				ProjectRoot: "/var/lib/app",
			},
			expected: "/var/lib/app",
		},
		{
			name: "handles path with spaces",
			resolved: &config.ResolvedConfig{
				ProjectRoot: "/home/user/my project",
			},
			expected: "/home/user/my project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveWorkDir(tt.resolved)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHostWorker_EnsureSync_FallsBackToCwd(t *testing.T) {
	// When ProjectRoot is not set, ensureSync should fall back to os.Getwd()

	resolved := &config.ResolvedConfig{
		ProjectRoot: "", // Empty - should trigger fallback
		Project:     &config.Config{},
		Global:      &config.GlobalConfig{},
	}

	orchestrator := &Orchestrator{
		resolved:    resolved,
		syncedHosts: make(map[string]bool),
	}

	worker := &hostWorker{
		orchestrator: orchestrator,
		hostName:     "test-host",
	}

	// Mark host as synced so ensureSync returns early
	orchestrator.syncedHosts["test-host"] = true

	// Should not panic or error when ProjectRoot is empty
	err := worker.ensureSync(context.Background())
	assert.NoError(t, err)
}

func TestHostWorker_EnsureSync_NilResolved(t *testing.T) {
	// When resolved config is nil, ensureSync should fall back to os.Getwd()

	orchestrator := &Orchestrator{
		resolved:    nil, // Nil config
		syncedHosts: make(map[string]bool),
	}

	worker := &hostWorker{
		orchestrator: orchestrator,
		hostName:     "test-host",
	}

	// Mark host as synced so ensureSync returns early
	orchestrator.syncedHosts["test-host"] = true

	// Should not panic or error when resolved is nil
	err := worker.ensureSync(context.Background())
	assert.NoError(t, err)
}

func TestHostWorker_ExecuteTaskWithRequeue_ContextCancellation(t *testing.T) {
	// When context is cancelled, the task should NOT be re-queued
	// (context cancellation is intentional, not a host availability issue)

	orchestrator := &Orchestrator{
		syncedHosts:      make(map[string]bool),
		unavailableHosts: make(map[string]bool),
	}

	worker := &hostWorker{
		orchestrator: orchestrator,
		hostName:     "test-host",
		host:         config.Host{SSH: []string{"nonexistent-host"}},
	}

	task := TaskInfo{
		Name:    "test-task",
		Index:   0,
		Command: "echo hello",
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, shouldRequeue := worker.executeTaskWithRequeue(ctx, task)

	// Should NOT re-queue when context is cancelled
	assert.False(t, shouldRequeue, "should not re-queue when context is cancelled")
	assert.NotNil(t, result.Error, "should have an error")
	assert.Equal(t, 1, result.ExitCode, "exit code should be 1")
}

func TestHostWorker_ExecuteTaskWithRequeue_ContextTimeout(t *testing.T) {
	// When context times out, the task should NOT be re-queued

	orchestrator := &Orchestrator{
		syncedHosts:      make(map[string]bool),
		unavailableHosts: make(map[string]bool),
	}

	worker := &hostWorker{
		orchestrator: orchestrator,
		hostName:     "test-host",
		host:         config.Host{SSH: []string{"nonexistent-host"}},
	}

	task := TaskInfo{
		Name:    "test-task",
		Index:   0,
		Command: "echo hello",
	}

	// Create an already-expired context
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	result, shouldRequeue := worker.executeTaskWithRequeue(ctx, task)

	// Should NOT re-queue when context times out
	assert.False(t, shouldRequeue, "should not re-queue when context times out")
	assert.NotNil(t, result.Error, "should have an error")
	assert.Equal(t, 1, result.ExitCode, "exit code should be 1")
}

// TestHostWorker_DeadConnectionRequeues pins the fix for a laptop going to
// sleep mid-run: the worker kept its dead connection, and every task still
// queued for that host failed in microseconds instead of moving to another
// host.
func TestHostWorker_DeadConnectionRequeues(t *testing.T) {
	client := sshtesting.NewMockClient("test-host")
	worker := &hostWorker{
		orchestrator: &Orchestrator{
			syncedHosts:      make(map[string]bool),
			unavailableHosts: make(map[string]bool),
		},
		hostName: "test-host",
		conn:     &host.Connection{Name: "test-host", Client: client},
	}

	require.NoError(t, worker.ensureConnection(context.Background()), "a live connection is reused")

	require.NoError(t, client.Close())
	result, requeue := worker.executeTaskWithRequeue(context.Background(), TaskInfo{Name: "test-task", Command: "echo hello"})
	assert.True(t, requeue, "a task for a dead connection goes back to the queue")
	assert.True(t, sshutil.IsConnectionLost(result.Error), "the re-queue notice gets the cause")
}

// TestHostWorker_FullCommand_MergesEnvAndSetup checks that parallel subtasks
// get the same env and setup merge as single tasks: host env < defaults env
// < task env, and host setup_commands then defaults.setup.
func TestHostWorker_FullCommand_MergesEnvAndSetup(t *testing.T) {
	project := &config.Config{Defaults: config.ProjectDefaults{
		Env:   map[string]string{"FROM_DEFAULTS": "d"},
		Setup: []string{"source .venv/bin/activate"},
	}}

	tests := []struct {
		name     string
		resolved *config.ResolvedConfig
		taskEnv  map[string]string
		want     string
	}{
		{
			name:     "host, defaults and task layers",
			resolved: &config.ResolvedConfig{Project: project},
			taskEnv:  map[string]string{"FROM_TASK": "t"},
			want:     "cd '/srv/app' && { source ~/.profile\n} && { source .venv/bin/activate\n} && export FROM_DEFAULTS=\"d\" && export FROM_HOST=\"h\" && export FROM_TASK=\"t\" && { make test\n}",
		},
		{
			name:     "setup step with no task env still gets host and defaults",
			resolved: &config.ResolvedConfig{Project: project},
			want:     "cd '/srv/app' && { source ~/.profile\n} && { source .venv/bin/activate\n} && export FROM_DEFAULTS=\"d\" && export FROM_HOST=\"h\" && { make test\n}",
		},
		{
			name: "no project config",
			want: "cd '/srv/app' && { source ~/.profile\n} && export FROM_HOST=\"h\" && { make test\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &hostWorker{
				orchestrator: &Orchestrator{resolved: tt.resolved},
				hostName:     "box",
				host: config.Host{
					Env:           map[string]string{"FROM_HOST": "h"},
					SetupCommands: []string{"source ~/.profile"},
				},
			}
			assert.Equal(t, tt.want, w.fullCommand("make test", tt.taskEnv, "/srv/app"))
		})
	}
}

// TestHostWorker_FullCommand_TaskEnvWins checks precedence when all three
// layers set the same key.
func TestHostWorker_FullCommand_TaskEnvWins(t *testing.T) {
	w := &hostWorker{
		orchestrator: &Orchestrator{resolved: &config.ResolvedConfig{Project: &config.Config{
			Defaults: config.ProjectDefaults{Env: map[string]string{"K": "defaults"}},
		}}},
		host: config.Host{Env: map[string]string{"K": "host"}},
	}
	assert.Contains(t, w.fullCommand("run", map[string]string{"K": "task"}, ""), `export K="task" &&`)
	assert.Contains(t, w.fullCommand("run", nil, ""), `export K="defaults" &&`)
}

// TestLocalWorker_DefaultsEnvAndSetup checks local parallel runs get
// defaults.env and defaults.setup like local single tasks do.
func TestLocalWorker_DefaultsEnvAndSetup(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{Defaults: config.ProjectDefaults{
			Env:   map[string]string{"RR_DEF": "from-defaults", "RR_OVERRIDE": "defaults"},
			Setup: []string{"export RR_SETUP=from-setup"},
		}},
		Global: &config.GlobalConfig{},
	}
	orchestrator := NewOrchestrator(nil, map[string]config.Host{}, nil, resolved, Config{})
	worker := &localWorker{orchestrator: orchestrator}

	result := worker.executeTask(context.Background(), TaskInfo{
		Name:    "t",
		Command: `echo "$RR_DEF $RR_SETUP $RR_OVERRIDE"`,
		Env:     map[string]string{"RR_OVERRIDE": "task"},
	})

	require.Equal(t, 0, result.ExitCode, string(result.Output))
	assert.Contains(t, string(result.Output), "from-defaults from-setup task")
}

// TestLocalWorker_EnvExpandsLikeSingleTasks checks a local parallel subtask
// gets env through the same shell exports as a single task, so a value like
// "$HOME/bin:$PATH" expands instead of arriving literally.
func TestLocalWorker_EnvExpandsLikeSingleTasks(t *testing.T) {
	t.Setenv("HOME", "/home/rr-test")
	resolved := &config.ResolvedConfig{
		Project: &config.Config{Defaults: config.ProjectDefaults{
			Env: map[string]string{"RR_PATH": "$HOME/bin"},
		}},
		Global: &config.GlobalConfig{},
	}
	orchestrator := NewOrchestrator(nil, map[string]config.Host{}, nil, resolved, Config{})
	worker := &localWorker{orchestrator: orchestrator}

	result := worker.executeTask(context.Background(), TaskInfo{
		Name:    "t",
		Command: `echo "$RR_PATH|$RR_MSG"`,
		Env:     map[string]string{"RR_MSG": "say \"hi\" `nope`"},
	})

	require.Equal(t, 0, result.ExitCode, string(result.Output))
	assert.Equal(t, "/home/rr-test/bin|say \"hi\" `nope`\n", string(result.Output))
}

// TestLocalWorker_SetupStepSeesDefaults checks the parallel `setup:` step runs
// after defaults.setup with defaults.env applied, locally as well as remote.
func TestLocalWorker_SetupStepSeesDefaults(t *testing.T) {
	resolved := &config.ResolvedConfig{
		Project: &config.Config{Defaults: config.ProjectDefaults{
			Env:   map[string]string{"RR_DEF": "yes"},
			Setup: []string{"export RR_SETUP=yes"},
		}},
		Global: &config.GlobalConfig{},
	}
	orchestrator := NewOrchestrator(nil, map[string]config.Host{}, nil, resolved, Config{
		Setup: `test "$RR_DEF" = yes && test "$RR_SETUP" = yes`,
	})
	worker := &localWorker{orchestrator: orchestrator}

	result := worker.executeTask(context.Background(), TaskInfo{Name: "t", Command: "true"})

	assert.Equal(t, 0, result.ExitCode)
	assert.NoError(t, result.Error)
}

// TestHostWorker_SyncOptions checks the worker asks the caller for sync
// options for its own host, so parallel syncs get the same invalidation,
// provenance and prune callbacks as single runs.
func TestHostWorker_SyncOptions(t *testing.T) {
	want := &rrsync.SyncOptions{}
	var gotHost string

	tests := []struct {
		name     string
		cfg      Config
		wantOpts *rrsync.SyncOptions
		wantHost string
	}{
		{
			name: "callback set",
			cfg: Config{SyncOptions: func(hostName string) *rrsync.SyncOptions {
				gotHost = hostName
				return want
			}},
			wantOpts: want,
			wantHost: "gpu-box",
		},
		{
			name:     "no callback",
			cfg:      Config{},
			wantOpts: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost = ""
			w := &hostWorker{orchestrator: &Orchestrator{config: tt.cfg}, hostName: "gpu-box"}
			got := w.syncOptions()
			if tt.wantOpts == nil {
				assert.Nil(t, got)
			} else {
				assert.Same(t, tt.wantOpts, got)
			}
			assert.Equal(t, tt.wantHost, gotHost)
		})
	}
}
