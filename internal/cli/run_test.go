package cli

import (
	"fmt"
	"os"
	osexec "os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
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
	err := runCommand([]string{}, runCmdFlags{Verb: "run"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "What should I run?")
}

func TestRunCommand_InvalidProbeTimeout(t *testing.T) {
	err := runCommand([]string{"echo hello"}, runCmdFlags{Verb: "run", ProbeTimeout: "invalid-timeout"})
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
	err = runCommand([]string{"make", "test"}, runCmdFlags{Verb: "run"})
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
	err = runCommand([]string{"echo"}, runCmdFlags{Verb: "run", ProbeTimeout: "5s"})
	require.Error(t, err)
	// Should fail on no hosts configured, not on probe timeout
	assert.NotContains(t, err.Error(), "timeout")
}

func TestExecCommand_NoArgs(t *testing.T) {
	err := runCommand([]string{}, runCmdFlags{Verb: "exec", SkipSync: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "What should I run?")
}

func TestExecCommand_InvalidProbeTimeout(t *testing.T) {
	err := runCommand([]string{"ls"}, runCmdFlags{Verb: "exec", SkipSync: true, ProbeTimeout: "bad-duration"})
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
	err = runCommand([]string{"ls", "-la"}, runCmdFlags{Verb: "exec", SkipSync: true})
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
			err := runCommand([]string{"ls"}, runCmdFlags{Verb: "exec", SkipSync: true, ProbeTimeout: tt.timeout})
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
	err := runCommand([]string{}, runCmdFlags{Verb: "run"})
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
	err = runCommand([]string{"make", "test", "-v"}, runCmdFlags{Verb: "run"})
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

	err = runCommand([]string{"echo"}, runCmdFlags{Verb: "run", Host: "myhost", Tag: "mytag"})
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

	err = runCommand([]string{"ls", "-la", "/tmp"}, runCmdFlags{Verb: "exec", SkipSync: true})
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

func TestRun_LocalAndTagConflict(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// --local and --tag should conflict
	exitCode, err := Run(RunOptions{
		Command: "echo test",
		Local:   true,
		Tag:     "gpu",
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--local and --tag cannot be used together")
}

func TestRun_LocalFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Local flag alone should be accepted (will fail on no config, not on flag)
	exitCode, err := Run(RunOptions{
		Command: "echo test",
		Local:   true,
	})
	assert.Equal(t, 1, exitCode)
	require.Error(t, err)
	// Should fail on config/hosts, not on --local flag
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestPullCommand_NoArgs(t *testing.T) {
	err := pullCommand([]string{}, "", "", "", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "What should I pull?")
}

func TestPullCommand_InvalidProbeTimeout(t *testing.T) {
	err := pullCommand([]string{"coverage.xml"}, "", "", "", "not-a-duration", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "doesn't look like a valid timeout")
}

func TestPullCommand_ValidProbeTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Valid timeout should be parsed correctly (will fail on no hosts, not timeout)
	err = pullCommand([]string{"file.txt"}, "", "", "", "5s", false)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "doesn't look like a valid timeout")
}

func TestPullOptions_Defaults(t *testing.T) {
	opts := PullOptions{}

	assert.Empty(t, opts.Patterns)
	assert.Empty(t, opts.Dest)
	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.DryRun)
}

func TestPullOptions_WithValues(t *testing.T) {
	opts := PullOptions{
		Patterns:     []string{"*.xml", "*.html"},
		Dest:         "/tmp/results",
		Host:         "remote-server",
		Tag:          "fast",
		ProbeTimeout: 10 * time.Second,
		DryRun:       true,
	}

	assert.Equal(t, []string{"*.xml", "*.html"}, opts.Patterns)
	assert.Equal(t, "/tmp/results", opts.Dest)
	assert.Equal(t, "remote-server", opts.Host)
	assert.Equal(t, "fast", opts.Tag)
	assert.Equal(t, 10*time.Second, opts.ProbeTimeout)
	assert.True(t, opts.DryRun)
}

func TestPull_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = Pull(PullOptions{
		Patterns: []string{"coverage.xml"},
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestPull_WithHostFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = Pull(PullOptions{
		Patterns: []string{"file.txt"},
		Host:     "myhost",
	})
	require.Error(t, err)
	// Should fail on no hosts configured
}

func TestPull_WithTagFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = Pull(PullOptions{
		Patterns: []string{"file.txt"},
		Tag:      "gpu",
	})
	require.Error(t, err)
}

// TestRemoteCWD_TraversalGuard verifies the path-prefix check that prevents
// --cwd from escaping the remote project root via ../ sequences.
// Mirrors the exact filepath.Join + strings.HasPrefix logic in run.go.
func TestRemoteCWD_TraversalGuard(t *testing.T) {
	tests := []struct {
		name       string
		remoteRoot string
		cwd        string
		wantEscape bool
	}{
		{"safe subdir", "/home/user/project", "backend", false},
		{"nested subdir", "/home/user/project", "backend/api", false},
		{"dotdot escapes", "/home/user/project", "../other", true},
		{"dotdot full escape", "/home/user/project", "../../etc", true},
		{"dotdot then descend into sibling", "/home/user/project", "../project2/src", true},
		{"dotdot then back to same root", "/home/user/project", "../project/backend", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := path.Join(tt.remoteRoot, tt.cwd)
			escaped := !strings.HasPrefix(resolved+"/", tt.remoteRoot+"/")
			assert.Equal(t, tt.wantEscape, escaped, "cwd=%q resolved to %q", tt.cwd, resolved)
		})
	}
}

func TestPull_DryRunMode(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = Pull(PullOptions{
		Patterns: []string{"file.txt"},
		DryRun:   true,
	})
	require.Error(t, err)
	// Should fail on config, not on dry-run flag
}

// newTestWorkflowContext builds the minimum context buildRemoteRunCommand needs.
// Conn.Client stays nil: the function assembles a command string and never
// executes it. Resolved.Project must be non-nil - Defaults.Setup is dereferenced
// unconditionally.
func newTestWorkflowContext(workDir, offset string) *WorkflowContext {
	rewrite := false
	return &WorkflowContext{
		WorkDir:      workDir,
		SubdirOffset: offset,
		Resolved: &config.ResolvedConfig{
			ProjectRoot: workDir,
			Project:     &config.Config{RewritePaths: &rewrite},
			Global:      &config.GlobalConfig{},
		},
		Conn: &host.Connection{Name: "m4-mini", Host: config.Host{Dir: "~/rr/app"}},
	}
}

// TestBuildRemoteRunCommand_AutoCWD pins the fix for relative paths breaking
// when rr is invoked from a subdirectory. rr executes at the remote project
// root, so "pytest tests/foo.py" written in backend/ looked for backend's tests
// at the root, found nothing, and reported success.
func TestBuildRemoteRunCommand_AutoCWD(t *testing.T) {
	const remoteDir = "/home/u/rr/app"

	tests := []struct {
		name        string
		offset      string
		explicitCWD string
		wantContain string
		wantAbsent  string
	}{
		{
			name:        "offset applied when no explicit cwd",
			offset:      "backend",
			wantContain: "cd '/home/u/rr/app/backend' 2>/dev/null || true;",
		},
		{
			name:        "nested offset",
			offset:      "backend/api",
			wantContain: "cd '/home/u/rr/app/backend/api' 2>/dev/null || true;",
		},
		{
			name:       "no offset at project root",
			offset:     "",
			wantAbsent: "cd '/home/u/rr/app/",
		},
		{
			name:        "explicit --cwd wins over the offset",
			offset:      "backend",
			explicitCWD: "frontend",
			wantContain: "cd '/home/u/rr/app/frontend' &&",
			wantAbsent:  "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := newTestWorkflowContext("/Users/r/app", tt.offset)
			got, err := buildRemoteRunCommand(wf, RunOptions{
				Command:   "pytest tests/foo.py",
				RemoteCWD: tt.explicitCWD,
			}, remoteDir)
			require.NoError(t, err)

			if tt.wantContain != "" {
				assert.Contains(t, got, tt.wantContain)
			}
			if tt.wantAbsent != "" {
				assert.NotContains(t, got, tt.wantAbsent)
			}
		})
	}
}

// TestBuildRemoteRunCommand_AutoCWDIsSoft - the implicit cd must never turn a
// working command into a failure, since the subdirectory may not exist remotely
// (sync excludes node_modules/, .venv/, and friends).
func TestBuildRemoteRunCommand_AutoCWDIsSoft(t *testing.T) {
	wf := newTestWorkflowContext("/Users/r/app", "node_modules/.bin")
	got, err := buildRemoteRunCommand(wf, RunOptions{Command: "make build"}, "/home/u/rr/app")
	require.NoError(t, err)

	assert.Contains(t, got, "|| true; } &&",
		"implicit cd must tolerate a missing directory, grouped so || binds only to it")
	assert.Contains(t, got, "make build")
}

// TestAutoCWDSoftCdShellSemantics runs the emitted soft cd through a real shell.
//
// This is the regression guard for a precedence bug that a substring assertion
// cannot catch: BuildRemoteCommand joins setup commands and the mandatory
// `cd <project dir>` with &&, and `||` binds looser than `&&`, so an ungrouped
// `cd X || true` swallowed the failure of the entire chain to its left. The
// command then ran in $HOME with a half-built environment and reported success -
// the exact false-green this branch exists to prevent.
func TestAutoCWDSoftCdShellSemantics(t *testing.T) {
	if _, err := osexec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	// The soft cd as emitted, with a deliberately missing target.
	const softCd = `{ cd /nonexistent/subdir 2>/dev/null || true; }`

	t.Run("earlier failure in the && chain stays fatal", func(t *testing.T) {
		script := "false && cd /tmp && " + softCd + " && echo RAN"
		out, err := osexec.Command("bash", "-c", script).CombinedOutput()
		require.Error(t, err, "a failed setup command must abort the run")
		assert.NotContains(t, string(out), "RAN",
			"the command must not run when an earlier && step failed")
	})

	t.Run("missing subdir falls back to the parent cwd", func(t *testing.T) {
		script := "cd /tmp && " + softCd + " && pwd"
		out, err := osexec.Command("bash", "-c", script).CombinedOutput()
		require.NoError(t, err, "a missing subdirectory must not fail the run")
		assert.Contains(t, string(out), "/tmp",
			"command should run in the project dir when the offset is missing")
	})

	t.Run("existing subdir is entered", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
		script := fmt.Sprintf("cd %s && { cd %s/sub 2>/dev/null || true; } && pwd", root, root)
		out, err := osexec.Command("bash", "-c", script).CombinedOutput()
		require.NoError(t, err)
		assert.Contains(t, string(out), "sub")
	})
}

// TestBuildRemoteRunCommand_ExplicitCWDStillHardFails - an explicit --cwd is a
// direct request, so traversal outside the project root is still an error. This
// exercises the real guard rather than mirroring its logic.
func TestBuildRemoteRunCommand_ExplicitCWDStillHardFails(t *testing.T) {
	wf := newTestWorkflowContext("/Users/r/app", "")
	_, err := buildRemoteRunCommand(wf, RunOptions{
		Command:   "ls",
		RemoteCWD: "../../etc",
	}, "/home/u/rr/app")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes the remote project root")
}

func TestLocalRunDir(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "backend"), 0o755))

	t.Run("applies offset so local matches remote", func(t *testing.T) {
		wf := newTestWorkflowContext(root, "backend")
		assert.Equal(t, filepath.Join(root, "backend"), localRunDir(wf, RunOptions{}))
	})

	t.Run("falls back to root when offset is missing locally", func(t *testing.T) {
		wf := newTestWorkflowContext(root, "nonexistent")
		assert.Equal(t, root, localRunDir(wf, RunOptions{}))
	})

	t.Run("no offset yields project root", func(t *testing.T) {
		wf := newTestWorkflowContext(root, "")
		assert.Equal(t, root, localRunDir(wf, RunOptions{}))
	})

	t.Run("explicit cwd wins", func(t *testing.T) {
		wf := newTestWorkflowContext(root, "nonexistent")
		assert.Equal(t, filepath.Join(root, "backend"),
			localRunDir(wf, RunOptions{RemoteCWD: "backend"}))
	})
}

// TestValidatedRunOffset_TraversalGuard - an escaping --cwd must be rejected on
// the local path too. buildRemoteRunCommand has always rejected it, but the
// local/local_fallback path applied opts.RemoteCWD with only an existence
// check, so `rr run --local --cwd ../ "cat outside.txt"` read a file outside the
// project and exited 0. Identical invocations must not be an error remotely and
// a silent escape locally.
func TestValidatedRunOffset_TraversalGuard(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))

	wf := newTestWorkflowContext(root, "")
	wf.WorkDir = root

	tests := []struct {
		name    string
		cwd     string
		offset  string
		want    string
		wantErr bool
	}{
		{name: "explicit cwd inside root", cwd: "sub", want: "sub"},
		{name: "explicit cwd escapes root", cwd: "../", wantErr: true},
		{name: "explicit cwd escapes deeper", cwd: "../../etc", wantErr: true},
		// "." is passed through rather than normalized away; it joins to the
		// root either way, and normalizeOffset reduces it for display.
		{name: "explicit cwd is the root", cwd: ".", want: "."},
		{name: "no cwd, no offset", want: ""},
		{name: "auto offset inside root", offset: "sub", want: "sub"},
		// An auto offset can't escape (subdirOffset filters ".."), and it's
		// implicit, so it falls back to the root rather than erroring.
		{name: "auto offset escaping falls back quietly", offset: "../oops", want: ""},
		{name: "auto offset missing on disk", offset: "gone", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf.SubdirOffset = tt.offset
			got, err := validatedRunOffset(wf, RunOptions{RemoteCWD: tt.cwd})
			if tt.wantErr {
				require.Error(t, err, "escaping --cwd must be rejected")
				assert.Contains(t, err.Error(), "escapes the project root")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
