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
	assert.Contains(t, err.Error(), "'go' isn't available on the remote")
	assert.Contains(t, err.Error(), "Possible fixes")
	assert.Contains(t, err.Error(), "source ~/.zshrc")
}

func TestHandleExecError_NotCommandNotFound(t *testing.T) {
	// Non-127 exit code should return nil
	err := HandleExecError("go test ./...", "tests failed", 1)
	assert.Nil(t, err)
}

func TestHandleExecError_ExtractsCommandFromInput(t *testing.T) {
	// When stderr doesn't match patterns, extract from command
	err := HandleExecError("rustup show", "some error", 127)

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "'rustup' isn't available on the remote")
}
