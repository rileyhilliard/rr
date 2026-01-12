package exec

import (
	"bytes"
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
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, args, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, nil, env, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

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
	result, err := ExecuteTask(conn, nil, nil, nil, "", &stdout, &stderr, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "No task provided")
}

func TestExecuteTask_EmptyTask(t *testing.T) {
	conn := createLocalConn()
	task := &config.TaskConfig{}

	var stdout, stderr bytes.Buffer
	result, err := ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "doesn't have anything to run")
}

func TestBuildEnvPrefix(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name:     "empty env",
			env:      nil,
			expected: "",
		},
		{
			name:     "empty map",
			env:      map[string]string{},
			expected: "",
		},
		{
			name:     "single var",
			env:      map[string]string{"FOO": "bar"},
			expected: `export FOO="bar"; `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildEnvPrefix(tt.env)
			if len(tt.env) > 0 {
				// For non-empty, just verify structure since map order is undefined
				assert.Contains(t, result, "export")
				for k, v := range tt.env {
					assert.Contains(t, result, k)
					assert.Contains(t, result, v)
				}
			} else {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestBuildCommand_Local(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		env      map[string]string
		workDir  string
		expected string
	}{
		{
			name:     "simple command",
			cmd:      "echo hello",
			env:      nil,
			workDir:  "",
			expected: "echo hello",
		},
		{
			name:    "command with env",
			cmd:     "echo $FOO",
			env:     map[string]string{"FOO": "bar"},
			workDir: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildCommand(tt.cmd, tt.env, tt.workDir, nil, true)
			if len(tt.env) == 0 {
				assert.Equal(t, tt.expected, result)
			} else {
				// Verify env prefix is present
				assert.Contains(t, result, "export")
				assert.Contains(t, result, tt.cmd)
			}
		})
	}
}

func TestBuildCommand_Remote(t *testing.T) {
	result := buildCommand("make test", nil, "/home/user/project", nil, false)
	assert.Contains(t, result, "cd")
	assert.Contains(t, result, "/home/user/project")
	assert.Contains(t, result, "make test")
}

func TestBuildCommand_WithSetupCommands(t *testing.T) {
	setup := []string{"source ~/.env", "export PATH=/opt/bin:$PATH"}
	result := buildCommand("make test", nil, "", setup, true)
	assert.Contains(t, result, "source ~/.env")
	assert.Contains(t, result, "export PATH=/opt/bin:$PATH")
	assert.Contains(t, result, "make test")
	// Should be joined with &&
	assert.Contains(t, result, " && ")
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
