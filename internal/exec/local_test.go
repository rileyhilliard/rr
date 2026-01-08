package exec

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteLocal_SimpleCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	exitCode, err := ExecuteLocal("echo hello", "", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "hello\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestExecuteLocal_CommandWithPipe(t *testing.T) {
	var stdout, stderr bytes.Buffer

	exitCode, err := ExecuteLocal("echo 'hello world' | tr ' ' '_'", "", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "hello_world\n", stdout.String())
}

func TestExecuteLocal_NonZeroExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer

	exitCode, err := ExecuteLocal("exit 42", "", &stdout, &stderr)

	require.NoError(t, err) // No error - command ran, just had non-zero exit
	assert.Equal(t, 42, exitCode)
}

func TestExecuteLocal_WorkingDirectory(t *testing.T) {
	// Create a temp directory
	tempDir := t.TempDir()

	var stdout, stderr bytes.Buffer

	exitCode, err := ExecuteLocal("pwd", tempDir, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	// pwd output should contain the temp dir
	assert.Contains(t, strings.TrimSpace(stdout.String()), filepath.Base(tempDir))
}

func TestExecuteLocal_StderrOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer

	exitCode, err := ExecuteLocal("echo error >&2", "", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "error\n", stderr.String())
}

func TestExecuteLocal_EnvironmentVariables(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// The command should have access to environment variables
	exitCode, err := ExecuteLocal("echo $HOME", "", &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.NotEmpty(t, stdout.String())
}

func TestExecuteLocal_CommandNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer

	exitCode, err := ExecuteLocal("this_command_does_not_exist_xyz123", "", &stdout, &stderr)

	// Command should run but exit with non-zero (command not found)
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode) // Should be 127 on most shells
}

func TestExecuteLocalCapture_Success(t *testing.T) {
	stdout, stderr, exitCode, err := ExecuteLocalCapture("echo captured", "")

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "captured\n", string(stdout))
	assert.Empty(t, stderr)
}

func TestExecuteLocalCapture_WithWorkDir(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	stdout, _, exitCode, err := ExecuteLocalCapture("ls", tempDir)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, string(stdout), "test.txt")
}

func TestExecuteLocalCapture_NonZeroExit(t *testing.T) {
	stdout, stderr, exitCode, err := ExecuteLocalCapture("exit 5", "")

	require.NoError(t, err)
	assert.Equal(t, 5, exitCode)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestExecuteLocalWithInput_Success(t *testing.T) {
	var stdout, stderr bytes.Buffer
	input := strings.NewReader("hello from stdin")

	exitCode, err := ExecuteLocalWithInput("cat", "", input, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "hello from stdin", stdout.String())
}

func TestExecuteLocalWithInput_WithWorkDir(t *testing.T) {
	tempDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	input := strings.NewReader("test input")

	exitCode, err := ExecuteLocalWithInput("pwd && cat", tempDir, input, &stdout, &stderr)

	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	// Output should contain the temp dir path followed by the input
	output := stdout.String()
	assert.Contains(t, output, filepath.Base(tempDir))
	assert.Contains(t, output, "test input")
}
