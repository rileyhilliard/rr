package exec

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createLocalConn creates a local connection for testing.
func createLocalConn() *host.Connection {
	return &host.Connection{
		Name:    "local",
		Alias:   "local",
		IsLocal: true,
	}
}

func TestExecuteTask_SingleCommand(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Run: "echo hello",
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, -1, result.FailedStep)
	assert.Nil(t, result.StepResults)
	assert.Contains(t, stdout.String(), "hello")
}

func TestExecuteTask_SingleCommandWithArgs(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Run: "echo hello",
	}
	args := []string{"world", "foo"}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, args, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	// Args should be appended: "echo hello world foo"
	assert.Contains(t, stdout.String(), "hello world foo")
}

func TestExecuteTask_SingleCommandWithEnv(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Run: "echo $MY_VAR",
	}
	env := map[string]string{"MY_VAR": "test_value"}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, env, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "test_value")
}

func TestExecuteTask_SingleCommandFailure(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Run: "exit 42",
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err) // No error - command ran but returned non-zero
	assert.Equal(t, 42, result.ExitCode)
	assert.Equal(t, -1, result.FailedStep) // Single command, no steps
}

func TestExecuteTask_MultiStepAllPassing(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Steps: []config.TaskStep{
			{Name: "step1", Run: "echo step1"},
			{Name: "step2", Run: "echo step2"},
			{Name: "step3", Run: "echo step3"},
		},
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, -1, result.FailedStep)
	require.Len(t, result.StepResults, 3)

	for i, sr := range result.StepResults {
		assert.Equal(t, 0, sr.ExitCode, "step %d should pass", i)
	}

	output := stdout.String()
	assert.Contains(t, output, "step1")
	assert.Contains(t, output, "step2")
	assert.Contains(t, output, "step3")
}

func TestExecuteTask_MultiStepFailureWithStop(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Steps: []config.TaskStep{
			{Name: "step1", Run: "echo step1"},
			{Name: "step2", Run: "exit 1", OnFail: "stop"}, // Default is stop anyway
			{Name: "step3", Run: "echo step3"},             // Should not run
		},
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, 1, result.FailedStep) // step2 (index 1) failed

	// Should have 2 step results - stopped after step2
	require.Len(t, result.StepResults, 2)
	assert.Equal(t, 0, result.StepResults[0].ExitCode)
	assert.Equal(t, 1, result.StepResults[1].ExitCode)

	output := stdout.String()
	assert.Contains(t, output, "step1")
	assert.NotContains(t, output, "step3") // step3 should not run
}

func TestExecuteTask_MultiStepFailureWithContinue(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Steps: []config.TaskStep{
			{Name: "step1", Run: "echo step1"},
			{Name: "step2", Run: "exit 1", OnFail: "continue"}, // Continue despite failure
			{Name: "step3", Run: "echo step3"},                 // Should still run
		},
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.Equal(t, 1, result.ExitCode)   // Final exit code is from failed step
	assert.Equal(t, 1, result.FailedStep) // step2 (index 1) was first failure

	// Should have all 3 step results - continued after failure
	require.Len(t, result.StepResults, 3)
	assert.Equal(t, 0, result.StepResults[0].ExitCode)
	assert.Equal(t, 1, result.StepResults[1].ExitCode)
	assert.Equal(t, 0, result.StepResults[2].ExitCode)

	output := stdout.String()
	assert.Contains(t, output, "step1")
	assert.Contains(t, output, "step3") // step3 ran despite step2 failing
}

func TestExecuteTask_MultiStepMixedOnFail(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Steps: []config.TaskStep{
			{Name: "step1", Run: "exit 1", OnFail: "continue"}, // Fail but continue
			{Name: "step2", Run: "echo step2"},                 // Should run
			{Name: "step3", Run: "exit 2", OnFail: "stop"},     // Fail and stop
			{Name: "step4", Run: "echo step4"},                 // Should not run
		},
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.Equal(t, 2, result.ExitCode)   // Exit code from step3
	assert.Equal(t, 0, result.FailedStep) // step1 (index 0) was first failure

	// Should have 3 step results - stopped at step3
	require.Len(t, result.StepResults, 3)
	assert.Equal(t, 1, result.StepResults[0].ExitCode)
	assert.Equal(t, 0, result.StepResults[1].ExitCode)
	assert.Equal(t, 2, result.StepResults[2].ExitCode)

	output := stdout.String()
	assert.Contains(t, output, "step2")
	assert.NotContains(t, output, "step4")
}

func TestExecuteTask_StepNamesDefault(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Steps: []config.TaskStep{
			{Run: "echo a"},           // No name
			{Name: "", Run: "echo b"}, // Empty name
			{Name: "named", Run: "echo c"},
		},
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	require.Len(t, result.StepResults, 3)
	assert.Equal(t, "step 1", result.StepResults[0].Name)
	assert.Equal(t, "step 2", result.StepResults[1].Name)
	assert.Equal(t, "named", result.StepResults[2].Name)
}

func TestExecuteTask_OnFailDefaults(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Steps: []config.TaskStep{
			{Name: "step1", Run: "echo a"},             // No on_fail
			{Name: "step2", Run: "echo b", OnFail: ""}, // Empty on_fail
			{Name: "step3", Run: "echo c", OnFail: "stop"},
			{Name: "step4", Run: "echo d", OnFail: "continue"},
		},
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err)
	require.Len(t, result.StepResults, 4)

	// Default and empty should be "stop"
	assert.Equal(t, "stop", result.StepResults[0].OnFail)
	assert.Equal(t, "stop", result.StepResults[1].OnFail)
	assert.Equal(t, "stop", result.StepResults[2].OnFail)
	assert.Equal(t, "continue", result.StepResults[3].OnFail)
}

func TestExecuteTask_NilTask(t *testing.T) {
	conn := createLocalConn()

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, nil, nil, nil, "", &stdout, &stderr, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "No task provided")
}

func TestExecuteTask_EmptyTask(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "doesn't have anything to run")
}

func TestBuildCommand(t *testing.T) {
	tests := []struct {
		name          string
		cmd           string
		env           map[string]string
		workDir       string
		setupCommands []string
		expected      string
	}{
		{
			name:     "bare command",
			cmd:      "echo hello",
			expected: "echo hello",
		},
		{
			name:     "workdir is single-quoted",
			cmd:      "make test",
			workDir:  "/home/user/project",
			expected: "cd '/home/user/project' && make test",
		},
		{
			name:     "workdir with spaces and a quote",
			cmd:      "make test",
			workDir:  "/srv/it's my dir",
			expected: `cd '/srv/it'\''s my dir' && make test`,
		},
		{
			name:     "tilde workdir keeps tilde expandable",
			cmd:      "make test",
			workDir:  "~/rr projects/app",
			expected: "cd ~/'rr projects/app' && make test",
		},
		{
			name:     "env keys are sorted",
			cmd:      "go test",
			env:      map[string]string{"GOOS": "linux", "CGO_ENABLED": "0"},
			expected: `export CGO_ENABLED="0"; export GOOS="linux"; go test`,
		},
		{
			name:     "env value leaves $ for the shell to expand",
			cmd:      "go test",
			env:      map[string]string{"PATH": "$HOME/.local/bin:$PATH"},
			expected: `export PATH="$HOME/.local/bin:$PATH"; go test`,
		},
		{
			name:     "env value with a double quote",
			cmd:      "run",
			env:      map[string]string{"MSG": `say "hi"`},
			expected: `export MSG="say \"hi\""; run`,
		},
		{
			name:     "env value with a backtick stays literal",
			cmd:      "run",
			env:      map[string]string{"MSG": "`whoami`"},
			expected: "export MSG=\"\\`whoami\\`\"; run",
		},
		{
			name:          "setup commands run after cd and before env",
			cmd:           "make build",
			env:           map[string]string{"CC": "gcc"},
			workDir:       "/app",
			setupCommands: []string{"source .venv/bin/activate", "export PATH=/opt/bin:$PATH"},
			expected:      `cd '/app' && source .venv/bin/activate && export PATH=/opt/bin:$PATH && export CC="gcc"; make build`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, BuildCommand(tt.cmd, tt.env, tt.workDir, tt.setupCommands))
		})
	}
}

// TestBuildCommand_ShellEvaluation runs built commands through a real shell:
// $ expands, while quotes, backticks, and backslashes arrive intact.
func TestBuildCommand_ShellEvaluation(t *testing.T) {
	t.Setenv("HOME", "/home/rr-test")
	dir := filepath.Join(t.TempDir(), `it's a "dir"`)
	require.NoError(t, os.Mkdir(dir, 0o755))

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"expands $HOME", "$HOME/.local/bin", "/home/rr-test/.local/bin"},
		{"double quote", `say "hi"`, `say "hi"`},
		{"backtick", "`echo pwned`", "`echo pwned`"},
		{"backslash", `a\b\`, `a\b\`},
		{"single quote", "it's", "it's"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := BuildCommand(`printf '%s|' "$RR_VAL"; pwd -P`, map[string]string{"RR_VAL": tt.value}, dir, nil)
			var stdout, stderr bytes.Buffer
			code, err := ExecuteLocal(cmd, "", &stdout, &stderr)
			require.NoError(t, err)
			require.Equal(t, 0, code, stderr.String())

			got, pwd, _ := strings.Cut(strings.TrimSpace(stdout.String()), "|")
			assert.Equal(t, tt.want, got)
			resolved, err := filepath.EvalSymlinks(dir)
			require.NoError(t, err)
			assert.Equal(t, resolved, pwd)
		})
	}
}

func TestBuildRemoteCommand_DefaultShell(t *testing.T) {
	host := &config.Host{
		Dir: "/home/user/project",
	}
	result := BuildRemoteCommand("make test", host)

	// Should use user's default shell
	assert.Contains(t, result, "${SHELL:-/bin/bash} -c")
	// Should source rc files for PATH setup
	assert.Contains(t, result, "[ -f ~/.bashrc ]")
	assert.Contains(t, result, "[ -f ~/.zshrc ]")
	assert.Contains(t, result, "make test")
}

func TestBuildRemoteCommand_CustomShell(t *testing.T) {
	host := &config.Host{
		Dir:   "/home/user/project",
		Shell: "zsh -l -c",
	}
	result := BuildRemoteCommand("make test", host)

	// Should use custom shell
	assert.Contains(t, result, "zsh -l -c")
	assert.Contains(t, result, "make test")
}

func TestBuildRemoteCommand_SetupCommands(t *testing.T) {
	host := &config.Host{
		Dir:           "/home/user/project",
		SetupCommands: []string{"export PATH=/opt/go/bin:$PATH"},
	}
	result := BuildRemoteCommand("go test", host)

	// Should include setup command before main command ($ is escaped to prevent outer shell expansion)
	assert.Contains(t, result, "export PATH=/opt/go/bin:\\$PATH")
	assert.Contains(t, result, "go test")
}

func TestApplyTaskArgs(t *testing.T) {
	tests := []struct {
		name     string
		run      string
		args     []string
		expected string
		wantErr  bool
	}{
		{
			name:     "no args returns run unchanged",
			run:      "pytest | grep -v PASS",
			args:     nil,
			expected: "pytest | grep -v PASS",
		},
		{
			name:     "simple command appends quoted",
			run:      "pytest tests/",
			args:     []string{"-k", "a b"},
			expected: "pytest tests/ '-k' 'a b'",
		},
		{
			name:     "placeholder in pipeline",
			run:      "pytest {args:-.} -n 4 | grep -v PASS",
			args:     []string{"tests/foo.py"},
			expected: "pytest 'tests/foo.py' -n 4 | grep -v PASS",
		},
		{
			name:     "placeholder default without args",
			run:      "pytest {args:-.} -n 4 | grep -v PASS",
			args:     nil,
			expected: "pytest . -n 4 | grep -v PASS",
		},
		{
			name:    "compound without placeholder errors",
			run:     "pytest | grep -v PASS",
			args:    []string{"tests/foo.py"},
			wantErr: true,
		},
		{
			name:    "redirection without placeholder errors",
			run:     "pytest --tb=short 2>&1",
			args:    []string{"tests/foo.py"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyTaskArgs(tt.run, tt.args)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "{args}")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExecuteTask_ArgsPlaceholder(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Run: "echo start {args:-default} end",
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, []string{"middle"}, nil, "", &stdout, &stderr, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "start middle end")

	stdout.Reset()
	result, err = ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "start default end")
}

func TestExecuteTask_CompoundCommandRejectsArgs(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{
		Run: "echo a | grep a",
	}

	var stdout, stderr bytes.Buffer
	_, err := ExecuteTask(context.Background(), conn, task, []string{"extra"}, nil, "", &stdout, &stderr, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compound command")
}
