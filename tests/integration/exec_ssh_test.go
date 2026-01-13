package integration

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/lock"
	rsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecSimpleCommand tests executing a simple command on the remote host.
func TestExecSimpleCommand(t *testing.T) {
	conn := GetSSHConnection(t)

	// Execute a simple echo command using ExecStream
	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("echo hello", &stdout, &stderr)

	require.NoError(t, err, "Command should execute without error")
	assert.Equal(t, 0, exitCode, "Exit code should be 0")
	assert.Contains(t, stdout.String(), "hello")
}

// TestExecCommandWithArgs tests command with multiple arguments.
func TestExecCommandWithArgs(t *testing.T) {
	conn := GetSSHConnection(t)

	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("echo one two three", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "one two three")
}

// TestExecCommandWithExitCode tests that non-zero exit codes are preserved.
func TestExecCommandWithExitCode(t *testing.T) {
	conn := GetSSHConnection(t)

	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("exit 42", &stdout, &stderr)

	require.NoError(t, err, "Command execution itself should succeed")
	assert.Equal(t, 42, exitCode, "Exit code should be preserved")
}

// TestExecCommandWithStderr tests that stderr is captured.
func TestExecCommandWithStderr(t *testing.T) {
	conn := GetSSHConnection(t)

	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("echo error message >&2", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stderr.String(), "error message")
}

// TestExecCommandWithEnv tests command execution with environment variables.
func TestExecCommandWithEnv(t *testing.T) {
	conn := GetSSHConnection(t)

	// Test that we can use environment variables in commands
	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("export TEST_VAR=hello && echo $TEST_VAR", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "hello")
}

// TestExecCommandWithPipe tests piped commands.
func TestExecCommandWithPipe(t *testing.T) {
	conn := GetSSHConnection(t)

	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("echo -e 'line1\\nline2\\nline3' | wc -l", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	// wc -l should output 3 (or close to it depending on implementation)
	output := strings.TrimSpace(stdout.String())
	assert.Contains(t, output, "3")
}

// TestExecCommandInDirectory tests running a command in a specific directory.
func TestExecCommandInDirectory(t *testing.T) {
	conn := GetSSHConnection(t)
	defer CleanupRemoteDir(t, conn, conn.Host.Dir)

	// Create a test directory with a file
	CreateRemoteFile(t, conn, fmt.Sprintf("%s/testfile.txt", conn.Host.Dir), "content")

	// Run command that lists files in the directory
	var stdout, stderr bytes.Buffer
	cmd := fmt.Sprintf("cd %s && ls -1", conn.Host.Dir)
	exitCode, err := conn.Client.ExecStream(cmd, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "testfile.txt")
}

// TestExecLongRunningCommand tests a command that takes some time.
func TestExecLongRunningCommand(t *testing.T) {
	conn := GetSSHConnection(t)

	start := time.Now()
	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("sleep 1 && echo done", &stdout, &stderr)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "done")
	assert.GreaterOrEqual(t, elapsed, 1*time.Second)
}

// TestExecMultilineOutput tests command with multiple lines of output.
func TestExecMultilineOutput(t *testing.T) {
	conn := GetSSHConnection(t)

	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("printf 'line1\\nline2\\nline3\\n'", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	assert.Len(t, lines, 3)
}

// TestExecCommandNotFound tests running a non-existent command.
func TestExecCommandNotFound(t *testing.T) {
	conn := GetSSHConnection(t)

	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("this_command_does_not_exist_12345", &stdout, &stderr)

	require.NoError(t, err, "Execution should succeed even if command fails")
	assert.NotEqual(t, 0, exitCode, "Exit code should be non-zero for missing command")
}

// TestExecWithQuotes tests command with quoted arguments.
func TestExecWithQuotes(t *testing.T) {
	conn := GetSSHConnection(t)

	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream(`echo "hello world" 'single quotes'`, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "hello world")
	assert.Contains(t, stdout.String(), "single quotes")
}

// TestFullWorkflowSyncLockExec tests the complete sync -> lock -> exec workflow.
func TestFullWorkflowSyncLockExec(t *testing.T) {
	conn := GetSSHConnection(t)
	RequireRemoteRsync(t, conn)
	defer CleanupRemoteDir(t, conn, conn.Host.Dir)

	// 1. Create local files to sync
	localDir := TempSyncDirWithFiles(t, map[string]string{
		"script.sh": "#!/bin/bash\necho 'Hello from synced script'",
	})

	// 2. Sync files to remote
	syncCfg := config.SyncConfig{}
	err := rsync.Sync(conn, localDir, syncCfg, nil)
	require.NoError(t, err, "Sync should succeed")

	// 3. Acquire lock
	lockCfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}
	projectHash := fmt.Sprintf("workflow-%d", time.Now().UnixNano())
	lck, err := lock.TryAcquire(conn, lockCfg, projectHash)
	require.NoError(t, err, "Lock acquisition should succeed")
	defer lck.Release()

	// 4. Execute the synced script
	var stdout, stderr bytes.Buffer
	cmd := fmt.Sprintf("cd %s && bash script.sh", conn.Host.Dir)
	exitCode, err := conn.Client.ExecStream(cmd, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "Hello from synced script")

	// 5. Release lock (handled by defer)
}

// TestExecWithWorkingDir tests execution in a specific directory.
func TestExecWithWorkingDir(t *testing.T) {
	conn := GetSSHConnection(t)
	defer CleanupRemoteDir(t, conn, conn.Host.Dir)

	// Create remote directory structure
	CreateRemoteFile(t, conn, fmt.Sprintf("%s/subdir/file.txt", conn.Host.Dir), "test")

	// Execute ls in the directory
	var stdout, stderr bytes.Buffer
	cmd := fmt.Sprintf("cd %s/subdir && pwd && ls", conn.Host.Dir)
	exitCode, err := conn.Client.ExecStream(cmd, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "subdir")
	assert.Contains(t, stdout.String(), "file.txt")
}

// TestExecStreamingOutput tests that output is streamed.
func TestExecStreamingOutput(t *testing.T) {
	conn := GetSSHConnection(t)

	// This command outputs incrementally
	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("for i in 1 2 3; do echo $i; done", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "1")
	assert.Contains(t, stdout.String(), "2")
	assert.Contains(t, stdout.String(), "3")
}

// TestExecSpecialCharacters tests commands with special characters.
func TestExecSpecialCharacters(t *testing.T) {
	conn := GetSSHConnection(t)

	var stdout, stderr bytes.Buffer
	// Test various special characters
	exitCode, err := conn.Client.ExecStream(`echo "tab:	end"`, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "tab:")
}

// TestExecuteTask tests the exec.ExecuteTask function with a real SSH connection.
func TestExecuteTask(t *testing.T) {
	conn := GetSSHConnection(t)
	defer CleanupRemoteDir(t, conn, conn.Host.Dir)
	EnsureRemoteDir(t, conn, conn.Host.Dir)

	task := &config.TaskConfig{
		Run: "echo 'task executed'",
	}

	var stdout, stderr bytes.Buffer
	result, err := exec.ExecuteTask(conn, task, nil, nil, conn.Host.Dir, &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "task executed")
}

// TestExecuteTaskWithArgs tests task execution with extra arguments.
func TestExecuteTaskWithArgs(t *testing.T) {
	conn := GetSSHConnection(t)
	defer CleanupRemoteDir(t, conn, conn.Host.Dir)
	EnsureRemoteDir(t, conn, conn.Host.Dir)

	task := &config.TaskConfig{
		Run: "echo",
	}

	var stdout, stderr bytes.Buffer
	result, err := exec.ExecuteTask(conn, task, []string{"arg1", "arg2"}, nil, conn.Host.Dir, &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "arg1")
	assert.Contains(t, stdout.String(), "arg2")
}

// TestExecuteTaskWithEnv tests task execution with environment variables.
func TestExecuteTaskWithEnv(t *testing.T) {
	conn := GetSSHConnection(t)
	defer CleanupRemoteDir(t, conn, conn.Host.Dir)
	EnsureRemoteDir(t, conn, conn.Host.Dir)

	task := &config.TaskConfig{
		Run: "echo $MY_VAR",
	}

	env := map[string]string{
		"MY_VAR": "hello_from_env",
	}

	var stdout, stderr bytes.Buffer
	result, err := exec.ExecuteTask(conn, task, nil, env, conn.Host.Dir, &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "hello_from_env")
}

// TestExecuteMultiStepTask tests execution of a multi-step task.
func TestExecuteMultiStepTask(t *testing.T) {
	conn := GetSSHConnection(t)
	defer CleanupRemoteDir(t, conn, conn.Host.Dir)
	EnsureRemoteDir(t, conn, conn.Host.Dir)

	task := &config.TaskConfig{
		Steps: []config.TaskStep{
			{Name: "step1", Run: "echo 'step 1'"},
			{Name: "step2", Run: "echo 'step 2'"},
			{Name: "step3", Run: "echo 'step 3'"},
		},
	}

	var stdout, stderr bytes.Buffer
	result, err := exec.ExecuteTask(conn, task, nil, nil, conn.Host.Dir, &stdout, &stderr, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, -1, result.FailedStep)
	assert.Len(t, result.StepResults, 3)
	assert.Contains(t, stdout.String(), "step 1")
	assert.Contains(t, stdout.String(), "step 2")
	assert.Contains(t, stdout.String(), "step 3")
}

// TestExecuteTaskFailure tests task execution with a failing command.
func TestExecuteTaskFailure(t *testing.T) {
	conn := GetSSHConnection(t)

	task := &config.TaskConfig{
		Run: "exit 1",
	}

	var stdout, stderr bytes.Buffer
	result, err := exec.ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, nil)

	require.NoError(t, err, "ExecuteTask should not return error for command failure")
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.ExitCode)
}

// TestExecuteTaskWithSetupCommands tests task execution with setup commands.
func TestExecuteTaskWithSetupCommands(t *testing.T) {
	conn := GetSSHConnection(t)

	task := &config.TaskConfig{
		Run: "echo $SETUP_VAR",
	}

	opts := &exec.TaskExecOptions{
		SetupCommands: []string{"export SETUP_VAR=from_setup"},
	}

	var stdout, stderr bytes.Buffer
	result, err := exec.ExecuteTask(conn, task, nil, nil, "", &stdout, &stderr, opts)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "from_setup")
}

// TestBuildRemoteCommand tests the BuildRemoteCommand function.
func TestBuildRemoteCommand(t *testing.T) {
	hostCfg := &config.Host{
		Dir:           "/home/user/project",
		SetupCommands: []string{"export PATH=/usr/local/go/bin:$PATH"},
	}

	cmd := exec.BuildRemoteCommand("go test ./...", hostCfg)

	// Should contain the setup command, cd, and the actual command
	assert.Contains(t, cmd, "go test ./...")
	assert.Contains(t, cmd, "/home/user/project")
	assert.Contains(t, cmd, "PATH")
}

// TestDetectMissingTool tests the missing tool detection.
func TestDetectMissingTool(t *testing.T) {
	conn := GetSSHConnection(t)

	// Run a command that doesn't exist
	var stdout, stderr bytes.Buffer
	exitCode, err := conn.Client.ExecStream("nonexistent_command_xyz", &stdout, &stderr)
	require.NoError(t, err)

	// Detect the missing tool
	result := exec.DetectMissingTool("nonexistent_command_xyz", stderr.String(), exitCode, conn.Client, "test-host")

	if exitCode == 127 {
		// Command not found should be detected
		assert.NotNil(t, result)
		assert.Equal(t, "nonexistent_command_xyz", result.ToolName)
	}
}
