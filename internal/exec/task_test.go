package exec

import (
	"bytes"
	"context"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	rrerrors "github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
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
			expected: "{ echo hello\n}",
		},
		{
			name:     "tilde workdir keeps tilde expandable",
			cmd:      "make test",
			workDir:  "~/rr projects/app",
			expected: "cd ~/'rr projects/app' && { make test\n}",
		},
		{
			name:          "cd, setup, env, then command",
			cmd:           "make build",
			env:           map[string]string{"CC": "gcc", "AR": "ar"},
			workDir:       "/app",
			setupCommands: []string{"source .venv/bin/activate"},
			expected:      "cd '/app' && { source .venv/bin/activate\n} && export AR=\"ar\" && export CC=\"gcc\" && { make build\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, BuildCommand(tt.cmd, tt.env, tt.workDir, tt.setupCommands))
		})
	}
}

// taskShells are the shells a built command meets: sh and $SHELL locally,
// the user's login shell remotely. Tests run under each one installed.
func taskShells(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, name := range []string{"sh", "bash", "dash", "zsh"} {
		if path, err := osexec.LookPath(name); err == nil {
			found = append(found, path)
		}
	}
	require.NotEmpty(t, found, "no POSIX shell found")
	return found
}

// runInShell runs cmd the way a local task does, with shell as $SHELL.
func runInShell(t *testing.T, shell, cmd string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv("SHELL", shell)
	var out, errOut bytes.Buffer
	code, err := ExecuteLocal(cmd, "", &out, &errOut)
	require.NoError(t, err)
	return out.String(), errOut.String(), code
}

// TestBuildCommand_ShellEvaluation runs built commands through real shells:
// $ expands, while quotes, backticks, and backslashes arrive intact.
func TestBuildCommand_ShellEvaluation(t *testing.T) {
	t.Setenv("HOME", "/home/rr-test")
	dir := filepath.Join(t.TempDir(), `it's a "dir"`)
	require.NoError(t, os.Mkdir(dir, 0o755))
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"expands $HOME", "$HOME/.local/bin", "/home/rr-test/.local/bin"},
		{"escaped dollar", `pa\$\$word`, "pa$$word"},
		{"double quote", `say "hi"`, `say "hi"`},
		{"backtick", "`echo pwned`", "`echo pwned`"},
		{"backslash", `a\b\`, `a\b\`},
		{"single quote", "it's", "it's"},
		{"substitution runs as written", `$(echo "a:b" | tr ':' '\n')`, "a\nb"},
		{"quotes inside a substitution are real", `$(echo "a  b")`, "a  b"},
		{"nested substitution", "$(echo $(echo x))", "x"},
		{"backtick inside a substitution", "$(echo `echo hi`)", "hi"},
		{"escaping resumes after a substitution", `$(echo x) "y"`, `x "y"`},
	}

	for _, shell := range taskShells(t) {
		for _, tt := range tests {
			t.Run(filepath.Base(shell)+"/"+tt.name, func(t *testing.T) {
				cmd := BuildCommand(`printf '%s|' "$RR_VAL"; pwd -P`, map[string]string{"RR_VAL": tt.value}, dir, nil)
				stdout, stderr, code := runInShell(t, shell, cmd)
				require.Equal(t, 0, code, stderr)

				got, pwd, _ := strings.Cut(strings.TrimSpace(stdout), "|")
				assert.Equal(t, tt.want, got)
				assert.Equal(t, resolved, pwd)
			})
		}
	}
}

// TestBuildCommand_ChainStopsOnFailure checks that each part of the chain
// gates the next, and that operators inside setup commands or the task
// command can't escape the chain.
func TestBuildCommand_ChainStopsOnFailure(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")

	tests := []struct {
		name     string
		cmd      string
		env      map[string]string
		workDir  string
		setup    []string
		want     string
		wantFail bool
	}{
		{name: "missing workdir stops the command", cmd: "echo RAN", workDir: missing, wantFail: true},
		{name: "missing workdir stops an || command", cmd: "false || echo RAN", workDir: missing, wantFail: true},
		{name: "missing workdir stops the command after env", cmd: "echo RAN", workDir: missing, env: map[string]string{"A": "1"}, wantFail: true},
		{name: "failing setup stops the command", cmd: "echo RAN", workDir: dir, setup: []string{"false"}, wantFail: true},
		{name: "failing setup stops later setup", cmd: "echo RAN", setup: []string{"false", "echo SETUP"}, wantFail: true},
		{name: "setup with || recovers", cmd: "echo RAN", setup: []string{"false || true"}, want: "RAN"},
		{name: "setup with || stays inside its group", cmd: "echo RAN", workDir: missing, setup: []string{"false || true"}, wantFail: true},
		{name: "command with || runs as written", cmd: "false || echo ok", want: "ok"},
		{name: "command exit code propagates", cmd: "exit 3", wantFail: true},
		{name: "command ending in &", cmd: "echo RAN &", want: "RAN"},
		{name: "command with a trailing comment", cmd: "echo RAN # done", want: "RAN"},
		{name: "setup with a trailing comment", cmd: "echo RAN", setup: []string{"true # ok"}, want: "RAN"},
		{name: "env sees setup and earlier keys", cmd: `echo "$B"`, setup: []string{"export S=s"}, env: map[string]string{"A": "a", "B": "$S-$A"}, want: "s-a"},
	}

	for _, shell := range taskShells(t) {
		for _, tt := range tests {
			t.Run(filepath.Base(shell)+"/"+tt.name, func(t *testing.T) {
				stdout, stderr, code := runInShell(t, shell, BuildCommand(tt.cmd, tt.env, tt.workDir, tt.setup))
				if tt.wantFail {
					assert.NotEqual(t, 0, code)
					assert.Empty(t, stdout, "nothing after the failure should run")
					return
				}
				require.Equal(t, 0, code, stderr)
				assert.Equal(t, tt.want, strings.TrimSpace(stdout))
			})
		}
	}
}

// TestBuildRemoteCommand_ChainStopsOnFailure runs `rr run` commands through
// real shells: a ; or || in the command or a setup command can't run part of
// the command after a failed setup or cd.
func TestBuildRemoteCommand_ChainStopsOnFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no rc files to source
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")

	tests := []struct {
		name     string
		cmd      string
		dir      string
		setup    []string
		want     string
		wantFail bool
	}{
		{name: "missing dir stops a ; command", cmd: "echo A; echo B", dir: missing, wantFail: true},
		{name: "failing setup stops a ; command", cmd: "echo A; echo B", dir: dir, setup: []string{"false"}, wantFail: true},
		{name: "|| in a setup command stays inside it", cmd: "echo RAN", dir: dir, setup: []string{"false", "true || true"}, wantFail: true},
		{name: "; command runs in the dir", cmd: "echo A; pwd -P", dir: dir, want: "A\n" + evalSymlinks(t, dir)},
	}

	for _, shell := range taskShells(t) {
		for _, tt := range tests {
			t.Run(filepath.Base(shell)+"/"+tt.name, func(t *testing.T) {
				host := &config.Host{Dir: tt.dir, SetupCommands: tt.setup, Shell: shell + " -c"}
				stdout, stderr, code := runInShell(t, shell, BuildRemoteCommand(tt.cmd, host))
				if tt.wantFail {
					assert.NotEqual(t, 0, code)
					assert.Empty(t, stdout, "nothing after the failure should run")
					return
				}
				require.Equal(t, 0, code, stderr)
				assert.Equal(t, tt.want, strings.TrimSpace(stdout))
			})
		}
	}
}

func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return resolved
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

// A step cut off by a dropped connection still comes back with the result so
// far, so callers can report which step it was instead of only the error.
func TestExecuteTask_MultiStepConnectionLost(t *testing.T) {
	client := sshtesting.NewMockClient("test-host")
	client.SetCommandResponse("echo two", sshtesting.CommandResponse{
		Error: rrerrors.WrapWithCode(&ssh.ExitMissingError{}, rrerrors.ErrSSH, "Lost the connection", ""),
	})
	conn := &host.Connection{Name: "test-host", Client: client}
	task := &config.TaskConfig{
		Steps: []config.TaskStep{
			{Name: "one", Run: "echo one"},
			{Name: "two", Run: "echo two"},
			{Name: "three", Run: "echo three"},
		},
	}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(context.Background(), conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.Error(t, err)
	assert.True(t, sshutil.IsConnectionLost(err))
	require.NotNil(t, result)
	assert.Equal(t, -1, result.ExitCode)
	assert.Equal(t, 1, result.FailedStep)
	require.Len(t, result.StepResults, 2)
	assert.Equal(t, -1, result.StepResults[1].ExitCode)
}
