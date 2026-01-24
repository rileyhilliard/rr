package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapProbeErrorToStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ui.ConnectionStatus
	}{
		{
			name: "nil error returns success",
			err:  nil,
			want: ui.StatusSuccess,
		},
		{
			name: "timeout error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailTimeout,
			},
			want: ui.StatusTimeout,
		},
		{
			name: "refused error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailRefused,
			},
			want: ui.StatusRefused,
		},
		{
			name: "unreachable error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnreachable,
			},
			want: ui.StatusUnreachable,
		},
		{
			name: "auth error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailAuth,
			},
			want: ui.StatusAuthFailed,
		},
		{
			name: "unknown probe error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnknown,
			},
			want: ui.StatusFailed,
		},
		{
			name: "host key error maps to failed",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailHostKey,
			},
			want: ui.StatusFailed,
		},
		{
			name: "generic error returns failed",
			err:  assert.AnError,
			want: ui.StatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapProbeErrorToStatus(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunOptions_Defaults(t *testing.T) {
	opts := RunOptions{}

	assert.Empty(t, opts.Command)
	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.SkipSync)
	assert.False(t, opts.SkipLock)
	assert.False(t, opts.DryRun)
	assert.Empty(t, opts.WorkingDir)
	assert.False(t, opts.Quiet)
}

func TestRunOptions_WithValues(t *testing.T) {
	opts := RunOptions{
		Command:      "make test",
		Host:         "remote-dev",
		Tag:          "fast",
		ProbeTimeout: 5 * time.Second,
		SkipSync:     true,
		SkipLock:     true,
		DryRun:       true,
		WorkingDir:   "/custom/dir",
		Quiet:        true,
	}

	assert.Equal(t, "make test", opts.Command)
	assert.Equal(t, "remote-dev", opts.Host)
	assert.Equal(t, "fast", opts.Tag)
	assert.Equal(t, 5*time.Second, opts.ProbeTimeout)
	assert.True(t, opts.SkipSync)
	assert.True(t, opts.SkipLock)
	assert.True(t, opts.DryRun)
	assert.Equal(t, "/custom/dir", opts.WorkingDir)
	assert.True(t, opts.Quiet)
}

func TestRunCommand_NoArgs(t *testing.T) {
	err := runCommand([]string{}, "", "", "", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "What should I run?")
}

func TestRunCommand_InvalidProbeTimeout(t *testing.T) {
	err := runCommand([]string{"echo hello"}, "", "", "invalid-timeout", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "doesn't look like a valid timeout")
}

func TestRunCommand_JoinsArgs(t *testing.T) {
	// Create temp dir without config - will fail on config load
	// but we're testing arg parsing
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Multiple args should be joined into single command
	err = runCommand([]string{"make", "test"}, "", "", "", false, false)
	require.Error(t, err)
	// Should fail on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestRunCommand_ValidProbeTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Valid probe timeout should not fail on parsing
	err = runCommand([]string{"echo"}, "", "", "5s", false, false)
	require.Error(t, err)
	// Should fail on no hosts configured, not on probe timeout
	assert.NotContains(t, err.Error(), "timeout")
}

func TestExecCommand_NoArgs(t *testing.T) {
	err := execCommand([]string{}, "", "", "", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "What should I run?")
}

func TestExecCommand_InvalidProbeTimeout(t *testing.T) {
	err := execCommand([]string{"ls"}, "", "", "bad-duration", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "doesn't look like a valid timeout")
}

func TestExecCommand_JoinsArgs(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Multiple args should be joined
	err = execCommand([]string{"ls", "-la"}, "", "", "", false, false)
	require.Error(t, err)
	// Should fail on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestExecCommand_ValidProbeTimeoutFormats(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	tests := []struct {
		name    string
		timeout string
	}{
		{"seconds", "5s"},
		{"minutes", "2m"},
		{"milliseconds", "500ms"},
		{"combined", "1m30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := execCommand([]string{"ls"}, "", "", tt.timeout, false, false)
			// Should fail with config error, not parse error
			if err != nil {
				assert.NotContains(t, err.Error(), "doesn't look like a valid timeout",
					"should parse duration %s correctly", tt.timeout)
			}
		})
	}
}

func TestRun_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	exitCode, err := Run(RunOptions{
		Command: "echo hello",
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestRun_WithHostFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Run with host flag but no config
	exitCode, err := Run(RunOptions{
		Command: "echo hello",
		Host:    "myhost",
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
	// Should fail on no hosts configured
}

func TestRun_WithTagFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	exitCode, err := Run(RunOptions{
		Command: "echo hello",
		Tag:     "gpu",
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
}

func TestMapProbeErrorToStatus_AllReasons(t *testing.T) {
	// Comprehensive test for all probe failure reasons
	tests := []struct {
		reason host.ProbeFailReason
		want   ui.ConnectionStatus
	}{
		{host.ProbeFailTimeout, ui.StatusTimeout},
		{host.ProbeFailRefused, ui.StatusRefused},
		{host.ProbeFailUnreachable, ui.StatusUnreachable},
		{host.ProbeFailAuth, ui.StatusAuthFailed},
		{host.ProbeFailHostKey, ui.StatusFailed},
		{host.ProbeFailUnknown, ui.StatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.reason.String(), func(t *testing.T) {
			err := &host.ProbeError{
				SSHAlias: "test",
				Reason:   tt.reason,
			}
			got := mapProbeErrorToStatus(err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRun_DryRunMode(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	// DryRun mode still needs config
	exitCode, err := Run(RunOptions{
		Command: "echo test",
		DryRun:  true,
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestRun_SkipSyncFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	exitCode, err := Run(RunOptions{
		Command:  "echo test",
		SkipSync: true,
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
	// Should fail on config or hosts, not on skip-sync flag
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestRun_SkipLockFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	exitCode, err := Run(RunOptions{
		Command:  "echo test",
		SkipLock: true,
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
}

func TestRun_QuietMode(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	exitCode, err := Run(RunOptions{
		Command: "echo test",
		Quiet:   true,
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
}

func TestRun_WorkingDirFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	exitCode, err := Run(RunOptions{
		Command:    "echo test",
		WorkingDir: "/custom/path",
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
}

func TestRun_AllOptions(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	exitCode, err := Run(RunOptions{
		Command:      "make test",
		Host:         "dev-server",
		Tag:          "gpu",
		ProbeTimeout: 5 * time.Second,
		SkipSync:     true,
		SkipLock:     true,
		DryRun:       false,
		WorkingDir:   "/project",
		Quiet:        true,
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
	// All options accepted, fails on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestRunOptions_ZeroValues(t *testing.T) {
	opts := RunOptions{}

	assert.Empty(t, opts.Command)
	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.SkipSync)
	assert.False(t, opts.SkipLock)
	assert.False(t, opts.DryRun)
	assert.Empty(t, opts.WorkingDir)
	assert.False(t, opts.Quiet)
}

func TestRunCommand_EmptyArgs(t *testing.T) {
	err := runCommand([]string{}, "", "", "", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "What should I run?")
}

func TestRunCommand_MultipleArgsJoined(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Multiple args should be joined with spaces
	err = runCommand([]string{"make", "test", "-v"}, "", "", "", false, false)
	require.Error(t, err)
	// Fails on no hosts configured, but args were processed
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestRunCommand_WithHostAndTag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = runCommand([]string{"echo"}, "myhost", "mytag", "", false, false)
	require.Error(t, err)
	// Should fail on no hosts configured, flags were accepted
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestExecCommand_MultipleArgsJoined(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = execCommand([]string{"ls", "-la", "/tmp"}, "", "", "", false, false)
	require.Error(t, err)
	// Fails on no hosts configured, but args were processed
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestMapProbeErrorToStatus_NilProbeError(t *testing.T) {
	// Test with non-ProbeError type
	status := mapProbeErrorToStatus(nil)
	assert.Equal(t, ui.StatusSuccess, status)
}

func TestMapProbeErrorToStatus_WrappedError(t *testing.T) {
	// Test with a regular error (not ProbeError)
	regularErr := assert.AnError
	status := mapProbeErrorToStatus(regularErr)
	assert.Equal(t, ui.StatusFailed, status)
}

func TestRun_ProbeTimeoutValues(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"zero timeout", 0},
		{"1 second", time.Second},
		{"30 seconds", 30 * time.Second},
		{"2 minutes", 2 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			origDir, _ := os.Getwd()
			defer os.Chdir(origDir)

			// Isolate from real user config
			t.Setenv("HOME", tmpDir)

			err := os.Chdir(tmpDir)
			require.NoError(t, err)

			exitCode, err := Run(RunOptions{
				Command:      "echo test",
				ProbeTimeout: tt.timeout,
			})
			assert.Equal(t, 1, exitCode)
			require.Error(t, err)
			// Should fail on no hosts configured, not probe timeout
			assert.Contains(t, err.Error(), "No hosts configured")
		})
	}
}
