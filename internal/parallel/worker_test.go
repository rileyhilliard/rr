package parallel

import (
	"context"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFullCommand(t *testing.T) {
	tests := []struct {
		name          string
		cmd           string
		env           map[string]string
		workDir       string
		setupCommands []string
		expected      string
	}{
		{
			name:     "simple command no extras",
			cmd:      "echo hello",
			expected: "echo hello",
		},
		{
			name:     "command with workdir",
			cmd:      "make test",
			workDir:  "/home/user/project",
			expected: "cd '/home/user/project' && make test",
		},
		{
			name:     "workdir with spaces is quoted",
			cmd:      "make test",
			workDir:  "/home/user/my project",
			expected: "cd '/home/user/my project' && make test",
		},
		{
			name:     "tilde workdir keeps tilde expandable",
			cmd:      "make test",
			workDir:  "~/rr projects/app",
			expected: "cd ~/'rr projects/app' && make test",
		},
		{
			name:     "workdir with shell metacharacters is inert",
			cmd:      "make test",
			workDir:  "/tmp/x; rm -rf ~",
			expected: "cd '/tmp/x; rm -rf ~' && make test",
		},
		{
			name: "command with env vars",
			cmd:  "go test",
			env: map[string]string{
				"GOOS": "linux",
			},
			expected: "export GOOS='linux'; go test",
		},
		{
			name:          "command with setup",
			cmd:           "pytest",
			setupCommands: []string{"source venv/bin/activate"},
			expected:      "source venv/bin/activate && pytest",
		},
		{
			name:          "command with multiple setup commands",
			cmd:           "npm test",
			setupCommands: []string{"nvm use 18", "npm ci"},
			expected:      "nvm use 18 && npm ci && npm test",
		},
		{
			name:          "command with all options",
			cmd:           "make build",
			env:           map[string]string{"CC": "gcc"},
			workDir:       "/app",
			setupCommands: []string{"module load gcc"},
			// cd runs first so relative setup (source .venv/bin/activate)
			// resolves in the project dir, matching single tasks.
			expected: "cd '/app' && module load gcc && export CC='gcc'; make build",
		},
		{
			name:     "empty command",
			cmd:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFullCommand(tt.cmd, tt.env, tt.workDir, tt.setupCommands)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "hello",
			expected: "'hello'",
		},
		{
			name:     "string with spaces",
			input:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "string with single quote",
			input:    "it's working",
			expected: "'it'\"'\"'s working'",
		},
		{
			name:     "string with multiple single quotes",
			input:    "can't won't don't",
			expected: "'can'\"'\"'t won'\"'\"'t don'\"'\"'t'",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "string with special chars",
			input:    "$HOME; rm -rf /",
			expected: "'$HOME; rm -rf /'",
		},
		{
			name:     "string with double quotes",
			input:    `say "hello"`,
			expected: `'say "hello"'`,
		},
		{
			name:     "string with backticks",
			input:    "`whoami`",
			expected: "'`whoami`'",
		},
		{
			name:     "string with newlines",
			input:    "line1\nline2",
			expected: "'line1\nline2'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shellQuote(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

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
			want:     "cd '/srv/app' && source ~/.profile && source .venv/bin/activate && export FROM_DEFAULTS='d'; export FROM_HOST='h'; export FROM_TASK='t'; make test",
		},
		{
			name:     "setup step with no task env still gets host and defaults",
			resolved: &config.ResolvedConfig{Project: project},
			want:     "cd '/srv/app' && source ~/.profile && source .venv/bin/activate && export FROM_DEFAULTS='d'; export FROM_HOST='h'; make test",
		},
		{
			name: "no project config",
			want: "cd '/srv/app' && source ~/.profile && export FROM_HOST='h'; make test",
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
	assert.Equal(t, "export K='task'; run", w.fullCommand("run", map[string]string{"K": "task"}, ""))
	assert.Equal(t, "export K='defaults'; run", w.fullCommand("run", nil, ""))
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
