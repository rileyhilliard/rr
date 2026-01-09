package exec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCommandNotFound(t *testing.T) {
	tests := []struct {
		name      string
		stderr    string
		exitCode  int
		wantCmd   string
		wantFound bool
	}{
		{
			name:      "bash command not found",
			stderr:    "bash: go: command not found",
			exitCode:  127,
			wantCmd:   "go",
			wantFound: true,
		},
		{
			name:      "zsh command not found",
			stderr:    "zsh: command not found: python",
			exitCode:  127,
			wantCmd:   "python",
			wantFound: true,
		},
		{
			name:      "sh not found",
			stderr:    "sh: 1: node: not found",
			exitCode:  127,
			wantCmd:   "node",
			wantFound: true,
		},
		{
			name:      "-bash no such file",
			stderr:    "-bash: mycommand: No such file or directory",
			exitCode:  127,
			wantCmd:   "mycommand",
			wantFound: true,
		},
		{
			name:      "generic not found",
			stderr:    "rustc: not found",
			exitCode:  127,
			wantCmd:   "rustc",
			wantFound: true,
		},
		{
			name:      "exit code 127 no pattern match",
			stderr:    "some other error message",
			exitCode:  127,
			wantCmd:   "",
			wantFound: true, // Still detected by exit code
		},
		{
			name:      "normal error not command not found",
			stderr:    "Error: file not found",
			exitCode:  1,
			wantCmd:   "",
			wantFound: false,
		},
		{
			name:      "success exit code",
			stderr:    "",
			exitCode:  0,
			wantCmd:   "",
			wantFound: false,
		},
		{
			name:      "permission denied not command not found",
			stderr:    "permission denied",
			exitCode:  126,
			wantCmd:   "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, found := IsCommandNotFound(tt.stderr, tt.exitCode)
			assert.Equal(t, tt.wantFound, found, "found mismatch")
			if tt.wantFound && tt.wantCmd != "" {
				assert.Equal(t, tt.wantCmd, cmd, "command name mismatch")
			}
		})
	}
}

func TestHandleExecError_CommandNotFound(t *testing.T) {
	err := HandleExecError("go test ./...", "bash: go: command not found", 127)

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "'go' not found in PATH on remote")
	assert.Contains(t, err.Error(), "Install 'go' on the remote")
	assert.Contains(t, err.Error(), "setup_commands")
}

func TestHandleExecError_NotCommandNotFound(t *testing.T) {
	// Non-127 exit code with unrelated error should return nil
	err := HandleExecError("go test ./...", "tests failed", 1)
	assert.Nil(t, err)
}

func TestIsDependencyNotFound(t *testing.T) {
	tests := []struct {
		name      string
		stderr    string
		wantCmd   string
		wantFound bool
	}{
		{
			name:      "make can't find go",
			stderr:    "make: go: No such file or directory\nmake: *** [test] Error 1",
			wantCmd:   "go",
			wantFound: true,
		},
		{
			name:      "make can't find python",
			stderr:    "make: python3: No such file or directory",
			wantCmd:   "python3",
			wantFound: true,
		},
		{
			name:      "env shebang can't find node",
			stderr:    "env: node: No such file or directory",
			wantCmd:   "node",
			wantFound: true,
		},
		{
			name:      "/bin/sh can't find rustc",
			stderr:    "/bin/sh: rustc: not found",
			wantCmd:   "rustc",
			wantFound: true,
		},
		{
			name:      "windows style not recognized",
			stderr:    "'cargo' is not recognized as an internal or external command",
			wantCmd:   "cargo",
			wantFound: true,
		},
		{
			name:      "unrelated make error",
			stderr:    "make: *** No rule to make target 'foo'. Stop.",
			wantCmd:   "",
			wantFound: false,
		},
		{
			name:      "normal test failure",
			stderr:    "FAIL: TestSomething",
			wantCmd:   "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, found := IsDependencyNotFound(tt.stderr)
			assert.Equal(t, tt.wantFound, found, "found mismatch")
			if tt.wantFound {
				assert.Equal(t, tt.wantCmd, cmd, "command name mismatch")
			}
		})
	}
}

func TestHandleExecError_DependencyNotFound(t *testing.T) {
	// make failing because go isn't in PATH (exit code 2, not 127)
	err := HandleExecError("make test", "make: go: No such file or directory\nmake: *** [test] Error 1", 2)

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "'go' not found in PATH on remote")
	assert.Contains(t, err.Error(), "Install 'go' on the remote")
	assert.Contains(t, err.Error(), "setup_commands")
}

func TestHandleExecError_ExtractsCommandFromInput(t *testing.T) {
	// When stderr doesn't match patterns, extract from command
	err := HandleExecError("rustup show", "some error", 127)

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "'rustup' not found in PATH on remote")
}
