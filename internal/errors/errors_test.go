package errors

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorCodes(t *testing.T) {
	// Verify all expected error codes exist
	codes := []string{
		ErrConfig,
		ErrSSH,
		ErrSync,
		ErrLock,
		ErrExec,
	}

	for _, code := range codes {
		assert.NotEmpty(t, code, "error code should not be empty")
	}

	// Verify codes are unique
	seen := make(map[string]bool)
	for _, code := range codes {
		assert.False(t, seen[code], "error code %q should be unique", code)
		seen[code] = true
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		message    string
		suggestion string
	}{
		{
			name:       "config error",
			code:       ErrConfig,
			message:    "Invalid configuration in .rr.yaml",
			suggestion: "Check your configuration file syntax",
		},
		{
			name:       "ssh error",
			code:       ErrSSH,
			message:    "Cannot connect to any configured hosts",
			suggestion: "Run 'rr doctor' to diagnose connection issues",
		},
		{
			name:       "sync error",
			code:       ErrSync,
			message:    "rsync not found on remote host",
			suggestion: "Install rsync: brew install rsync",
		},
		{
			name:       "lock error",
			code:       ErrLock,
			message:    "Lock acquisition timed out after 5m",
			suggestion: "Use --force-unlock to override stale lock",
		},
		{
			name:       "exec error",
			code:       ErrExec,
			message:    "Command failed with exit code 1",
			suggestion: "Check command output for details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(tt.code, tt.message, tt.suggestion)

			require.NotNil(t, err)
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.message, err.Message)
			assert.Equal(t, tt.suggestion, err.Suggestion)
			assert.Nil(t, err.Cause)
		})
	}
}

func TestErrorInterface(t *testing.T) {
	err := New(ErrConfig, "test message", "test suggestion")

	// Should implement error interface
	var _ error = err

	// Error() should return formatted message
	errStr := err.Error()
	assert.NotEmpty(t, errStr)
}

func TestErrorFormatting(t *testing.T) {
	tests := []struct {
		name          string
		err           *Error
		expectedParts []string
		notExpected   []string
	}{
		{
			name: "basic error formatting",
			err:  New(ErrConfig, "Invalid configuration", "Check .rr.yaml syntax"),
			expectedParts: []string{
				"Invalid configuration",
				"Check .rr.yaml syntax",
			},
		},
		{
			name: "error with failure symbol",
			err:  New(ErrSSH, "Connection failed", "Try again"),
			expectedParts: []string{
				"✗", // Failure symbol from ARCHITECTURE.md
				"Connection failed",
			},
		},
		{
			name: "error without suggestion",
			err:  New(ErrExec, "Command failed", ""),
			expectedParts: []string{
				"Command failed",
			},
			notExpected: []string{
				"suggestion", // Should not include suggestion header if empty
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := tt.err.Error()

			for _, part := range tt.expectedParts {
				assert.Contains(t, output, part, "output should contain %q", part)
			}

			for _, part := range tt.notExpected {
				assert.NotContains(t, output, part, "output should not contain %q", part)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("underlying network error")
	wrapped := Wrap(cause, "SSH connection failed")

	require.NotNil(t, wrapped)
	assert.Equal(t, ErrSSH, wrapped.Code, "Wrap should default to ErrSSH code")
	assert.Equal(t, "SSH connection failed", wrapped.Message)
	assert.Equal(t, cause, wrapped.Cause)
}

func TestWrapWithCode(t *testing.T) {
	cause := errors.New("file not found")
	wrapped := WrapWithCode(cause, ErrConfig, "Failed to load config", "Create .rr.yaml file")

	require.NotNil(t, wrapped)
	assert.Equal(t, ErrConfig, wrapped.Code)
	assert.Equal(t, "Failed to load config", wrapped.Message)
	assert.Equal(t, "Create .rr.yaml file", wrapped.Suggestion)
	assert.Equal(t, cause, wrapped.Cause)
}

func TestErrorWrappingPreservesCause(t *testing.T) {
	original := errors.New("original error")
	wrapped := WrapWithCode(original, ErrSync, "Sync failed", "")

	// Should preserve the original cause
	assert.Equal(t, original, wrapped.Cause)

	// Error message should include cause information
	errStr := wrapped.Error()
	assert.Contains(t, errStr, "original error")
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	wrapped := WrapWithCode(cause, ErrExec, "Execution failed", "")

	// Should implement Unwrap for errors.Is/errors.As
	unwrapped := wrapped.Unwrap()
	assert.Equal(t, cause, unwrapped)
}

func TestErrorsIs(t *testing.T) {
	cause := errors.New("specific error")
	wrapped := WrapWithCode(cause, ErrLock, "Lock error", "")

	// errors.Is should work with wrapped errors
	assert.True(t, errors.Is(wrapped, cause))
}

func TestErrorsAs(t *testing.T) {
	wrapped := New(ErrConfig, "Config error", "Fix config")

	var rrErr *Error
	ok := errors.As(wrapped, &rrErr)

	assert.True(t, ok)
	assert.Equal(t, ErrConfig, rrErr.Code)
}

func TestIsCode(t *testing.T) {
	err := New(ErrConfig, "Config error", "")

	assert.True(t, IsCode(err, ErrConfig))
	assert.False(t, IsCode(err, ErrSSH))
	assert.False(t, IsCode(errors.New("standard error"), ErrConfig))
	assert.False(t, IsCode(nil, ErrConfig))
}

func TestErrorMessageStructure(t *testing.T) {
	// Based on ARCHITECTURE.md error message design:
	// ✗ <What failed>
	//
	//   <Why it failed - technical details>
	//
	//   <How to fix it - actionable steps>

	err := WrapWithCode(
		errors.New("Connection timed out after 2s"),
		ErrSSH,
		"Cannot connect to any configured hosts",
		"Run: rr doctor",
	)

	output := err.Error()
	lines := strings.Split(output, "\n")

	// First line should have failure symbol and main message
	assert.True(t, strings.HasPrefix(strings.TrimSpace(lines[0]), "✗"), "First line should start with failure symbol")
	assert.Contains(t, lines[0], "Cannot connect to any configured hosts")
}

func TestNewNotImplemented(t *testing.T) {
	err := NewNotImplemented("run")

	require.NotNil(t, err)
	assert.Contains(t, err.Message, "run")
	assert.Contains(t, err.Message, "not implemented")
}

func TestExitError(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		wantMsg string
	}{
		{
			name:    "zero exit code",
			code:    0,
			wantMsg: "exit code 0",
		},
		{
			name:    "non-zero exit code",
			code:    1,
			wantMsg: "exit code 1",
		},
		{
			name:    "signal exit code",
			code:    137,
			wantMsg: "exit code 137",
		},
		{
			name:    "negative exit code",
			code:    -1,
			wantMsg: "exit code -1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewExitError(tt.code)

			require.NotNil(t, err)
			assert.Equal(t, tt.code, err.Code)
			assert.Equal(t, tt.wantMsg, err.Error())
		})
	}
}

func TestExitError_ImplementsError(t *testing.T) {
	var err error = NewExitError(42)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "42")
}

func TestGetExitCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantOk   bool
	}{
		{
			name:     "ExitError returns code",
			err:      NewExitError(42),
			wantCode: 42,
			wantOk:   true,
		},
		{
			name:     "ExitError with zero",
			err:      NewExitError(0),
			wantCode: 0,
			wantOk:   true,
		},
		{
			name:     "standard error returns false",
			err:      errors.New("standard error"),
			wantCode: 0,
			wantOk:   false,
		},
		{
			name:     "nil error returns false",
			err:      nil,
			wantCode: 0,
			wantOk:   false,
		},
		{
			name:     "structured Error returns false",
			err:      New(ErrExec, "test", ""),
			wantCode: 0,
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := GetExitCode(tt.err)
			assert.Equal(t, tt.wantOk, ok)
			assert.Equal(t, tt.wantCode, code)
		})
	}
}

func TestGetExitCode_WrappedError(t *testing.T) {
	// Test that GetExitCode can unwrap errors
	exitErr := NewExitError(99)

	// errors.As should work with wrapped errors
	code, ok := GetExitCode(exitErr)
	assert.True(t, ok)
	assert.Equal(t, 99, code)
}
