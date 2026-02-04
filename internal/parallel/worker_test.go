package parallel

import (
	"context"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
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
			expected: "cd /home/user/project && make test",
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
			expected:      "module load gcc && cd /app && export CC='gcc'; make build",
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
