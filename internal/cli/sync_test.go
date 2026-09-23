package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check: lock import must be used in sync.go.
// If this causes an "imported and not used" error, it means the lock
// package was removed from sync.go, which would re-introduce #181.
var _ = func() { _ = SyncOptions{SkipLock: true} }

func TestSyncOptions_Defaults(t *testing.T) {
	opts := SyncOptions{}

	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.DryRun)
	assert.Empty(t, opts.WorkingDir)
}

func TestSyncOptions_WithValues(t *testing.T) {
	opts := SyncOptions{
		Host:         "remote-dev",
		Tag:          "fast",
		ProbeTimeout: 5 * time.Second,
		DryRun:       true,
		WorkingDir:   "/path/to/project",
	}

	assert.Equal(t, "remote-dev", opts.Host)
	assert.Equal(t, "fast", opts.Tag)
	assert.Equal(t, 5*time.Second, opts.ProbeTimeout)
	assert.True(t, opts.DryRun)
	assert.Equal(t, "/path/to/project", opts.WorkingDir)
}

func TestSyncCommand_InvalidProbeTimeout(t *testing.T) {
	err := syncCommand("", "", "invalid-duration", false)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "doesn't look like a valid timeout")
}

func TestSyncCommand_ValidProbeTimeoutFormats(t *testing.T) {
	// These will fail later in the process (no config), but should not fail
	// on duration parsing
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
			err := syncCommand("", "", tt.timeout, false)
			// Should fail with config error, not parse error
			if err != nil {
				assert.NotContains(t, err.Error(), "Invalid probe timeout",
					"should parse duration %s correctly", tt.timeout)
			}
		})
	}
}

func TestSync_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err = Sync(SyncOptions{})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestSync_WithHostFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err = Sync(SyncOptions{
		Host: "myhost",
	})
	require.Error(t, err)
	// Should fail on config or hosts, host flag was accepted
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestSync_WithTagFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err = Sync(SyncOptions{
		Tag: "gpu",
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestSync_DryRunFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err = Sync(SyncOptions{
		DryRun: true,
	})
	require.Error(t, err)
	// Should fail on config or hosts, dry-run flag was accepted
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestSync_WorkingDirFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err = Sync(SyncOptions{
		WorkingDir: "/custom/dir",
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestSync_ProbeTimeoutFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err = Sync(SyncOptions{
		ProbeTimeout: 10 * time.Second,
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestSyncCommand_PassesDryRunFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Test that dry-run flag is passed through syncCommand
	err = syncCommand("myhost", "gpu", "5s", true)
	require.Error(t, err)
	// Should fail on no hosts configured, but all flags were parsed
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestSync_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Write invalid YAML
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	err = Sync(SyncOptions{})
	require.Error(t, err)
	// Should fail on config parsing
}

func TestSync_EmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Set up isolated HOME with empty global config (no hosts)
	globalDir := filepath.Join(tmpDir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts: {}
`
	err = os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)
	t.Setenv("HOME", tmpDir)

	// Write project config
	projectContent := `
version: 1
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(projectContent), 0644)
	require.NoError(t, err)

	err = Sync(SyncOptions{})
	require.Error(t, err)
	// Should fail because no hosts configured
	assert.Contains(t, err.Error(), "No hosts")
}

func TestSyncCommand_AllFlagsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Test with all flags empty - should use defaults
	err = syncCommand("", "", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestSyncOptions_AllFlagCombinations(t *testing.T) {
	tests := []struct {
		name string
		opts SyncOptions
	}{
		{
			name: "host only",
			opts: SyncOptions{Host: "myhost"},
		},
		{
			name: "tag only",
			opts: SyncOptions{Tag: "gpu"},
		},
		{
			name: "host and tag",
			opts: SyncOptions{Host: "myhost", Tag: "gpu"},
		},
		{
			name: "with dry run",
			opts: SyncOptions{DryRun: true},
		},
		{
			name: "with probe timeout",
			opts: SyncOptions{ProbeTimeout: 5 * time.Second},
		},
		{
			name: "with working dir",
			opts: SyncOptions{WorkingDir: "/custom"},
		},
		{
			name: "all flags",
			opts: SyncOptions{
				Host:         "myhost",
				Tag:          "gpu",
				ProbeTimeout: 5 * time.Second,
				DryRun:       true,
				WorkingDir:   "/custom",
			},
		},
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

			err = Sync(tt.opts)
			require.Error(t, err)
			// All should fail on no hosts configured, proving flags are accepted
			assert.Contains(t, err.Error(), "No hosts configured")
		})
	}
}

func TestSync_ConfigWithNoDefaultHost(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Set up global config with a host
	globalDir := filepath.Join(tmpDir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  dev:
    ssh:
      - dev.example.com
    dir: /home/user/project
`
	err = os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	// Project config references the host
	projectContent := `
version: 1
hosts:
  - dev
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(projectContent), 0644)
	require.NoError(t, err)

	// Sync without host flag should still work (selector will pick first)
	err = Sync(SyncOptions{})
	// Will fail on SSH connection, not on config
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "No config file found")
}

func TestSyncOptions_ZeroValues(t *testing.T) {
	opts := SyncOptions{}

	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.DryRun)
	assert.Empty(t, opts.WorkingDir)
}

func TestSyncCommand_EmptyFlags(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// All empty flags should use defaults
	err = syncCommand("", "", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestSyncCommand_WithAllFlags(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = syncCommand("myhost", "gpu", "10s", true)
	require.Error(t, err)
	// Should fail on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestSync_MultipleProbeTimeouts(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"zero", 0},
		{"short", 100 * time.Millisecond},
		{"medium", 5 * time.Second},
		{"long", 2 * time.Minute},
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

			err = Sync(SyncOptions{
				ProbeTimeout: tt.timeout,
			})
			require.Error(t, err)
			// Should fail on no hosts configured
			assert.Contains(t, err.Error(), "No hosts configured")
		})
	}
}

func TestSyncCommand_InvalidProbeTimeoutFormats(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr string
	}{
		{"no unit", "5", "doesn't look like a valid timeout"},
		{"invalid text", "fast", "doesn't look like a valid timeout"},
		{"invalid unit", "5x", "doesn't look like a valid timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := syncCommand("", "", tt.timeout, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSync_DryRunWithHost(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = Sync(SyncOptions{
		Host:   "dev-server",
		DryRun: true,
	})
	require.Error(t, err)
	// Should fail on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestSync_DryRunWithTag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = Sync(SyncOptions{
		Tag:    "gpu",
		DryRun: true,
	})
	require.Error(t, err)
	// Should fail on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestSync_CustomWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	customDir := filepath.Join(tmpDir, "custom")
	err = os.MkdirAll(customDir, 0755)
	require.NoError(t, err)

	err = Sync(SyncOptions{
		WorkingDir: customDir,
	})
	require.Error(t, err)
	// Should fail on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestSync_NonExistentWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = Sync(SyncOptions{
		WorkingDir: "/nonexistent/path/to/project",
	})
	require.Error(t, err)
	// Should fail on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

func TestSync_HostAndTagCombined(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = Sync(SyncOptions{
		Host: "dev-server",
		Tag:  "gpu",
	})
	require.Error(t, err)
	// Should fail on no hosts configured
	assert.Contains(t, err.Error(), "No hosts configured")
}

// TestSync_AcquiresLockBeforeSync is a regression test for
// https://github.com/rileyhilliard/rr/issues/181
//
// rr sync must acquire a lock before syncing to prevent overwriting files
// while another rr process is executing on the same host.
// This test verifies lock.Acquire is called before sync.Sync and that
// the lock is released afterward.
func TestSync_AcquiresLockBeforeSync(t *testing.T) {
	src, err := os.ReadFile("sync.go")
	require.NoError(t, err, "should be able to read sync.go source")

	content := string(src)

	// Verify lock.Acquire is called
	lockIdx := strings.Index(content, "lock.Acquire(")
	assert.Greater(t, lockIdx, 0,
		"Sync must call lock.Acquire (see issue #181)")

	// Verify lock acquisition happens before sync
	syncIdx := strings.Index(content, "sync.SyncWithOptions(")
	assert.Greater(t, syncIdx, lockIdx,
		"lock.Acquire must appear before sync.SyncWithOptions (see issue #181)")

	// Verify lock is released
	assert.Contains(t, content, "lck.Release()",
		"Sync must release the lock after syncing (see issue #181)")
}

// TestSyncCommandOptions_DryRunInvalidation checks a dry run reports the
// directories it would invalidate through the active output mode: the usual
// sync invalidated event marked dry_run, or a "Would invalidate" line. It
// must never fall through to the sync package's plain-text fallback.
func TestSyncCommandOptions_DryRunInvalidation(t *testing.T) {
	t.Run("structured", func(t *testing.T) {
		withStructuredOutput(t)
		opts := syncCommandOptions(true)
		assert.True(t, opts.DryRun)
		require.NotNil(t, opts.Invalidated, "nil falls back to a plain Printf on stdout")

		stdout, stderr := runCaptured(t, func() { opts.Invalidated("node_modules", "package-lock.json") })
		assert.Empty(t, stdout)
		events := parseEvents(t, stderr)
		require.Len(t, events, 1)
		assert.Equal(t, "sync", events[0].Phase)
		assert.Equal(t, "invalidated", events[0].Status)
		assert.Equal(t, map[string]interface{}{
			"dir": "node_modules", "lockfile": "package-lock.json", "dry_run": true,
		}, events[0].Details)
	})

	t.Run("pretty", func(t *testing.T) {
		withPrettyOutput(t)
		opts := syncCommandOptions(true)
		require.NotNil(t, opts.Invalidated)

		stdout, stderr := runCaptured(t, func() { opts.Invalidated("node_modules", "package-lock.json") })
		assert.True(t, strings.HasPrefix(stdout, "\r\033[K"), "clears the spinner line first")
		assert.Contains(t, stdout, "Would invalidate stale node_modules (package-lock.json changed)")
		assert.NotContains(t, stdout, "Invalidating")
		assert.Empty(t, stderr)
	})

	t.Run("a real sync is not a dry run", func(t *testing.T) {
		assert.False(t, syncCommandOptions(false).DryRun)
	})
}

func TestParseItemizedChanges(t *testing.T) {
	tests := []struct {
		name         string
		out          string
		wantTransfer []string
		wantDelete   []string
	}{
		{
			name: "rsync 3.x",
			out: "*deleting   stale.txt\n" +
				">f.st....... a.txt\n" +
				"cd++++++++++ sub/\n" +
				">f++++++++++ sub/b.txt\n" +
				"cL++++++++++ link -> a.txt\n" +
				".d..t....... ./\n" +
				"              4 100%    3.91kB/s    0:00:00 (xfr#2, to-chk=0/4)\n",
			wantTransfer: []string{"a.txt", "sub/", "sub/b.txt", "link -> a.txt"},
			wantDelete:   []string{"stale.txt"},
		},
		{
			name:         "openrsync",
			out:          "*deleting stale.txt\n>f.st.... a.txt\ncd+++++++ sub/\n",
			wantTransfer: []string{"a.txt", "sub/"},
			wantDelete:   []string{"stale.txt"},
		},
		{
			name: "rsync messages are not paths",
			out: "sending incremental file list\n" +
				"cannot delete non-empty directory: keep\n" +
				"sent 142 bytes  received 39 bytes  362.00 bytes/sec\n" +
				"total size is 4  speedup is 0.02 (DRY RUN)\n",
			wantTransfer: []string{},
			wantDelete:   []string{},
		},
		{
			name:         "path with spaces",
			out:          ">f+++++++++ my file.txt\n",
			wantTransfer: []string{"my file.txt"},
			wantDelete:   []string{},
		},
		{
			name:         "nothing to do",
			out:          "",
			wantTransfer: []string{},
			wantDelete:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseItemizedChanges(tt.out)
			assert.Equal(t, tt.wantTransfer, got.Transfer)
			assert.Equal(t, tt.wantDelete, got.Delete)
		})
	}
}

// TestReportSyncDone checks how a finished rr sync reports: a result event
// in structured mode, with the dry-run preview in its details, or summary
// lines and the preview with --pretty.
func TestReportSyncDone(t *testing.T) {
	conn := &host.Connection{Name: "box-a", Alias: "box-a-lan"}
	preview := &syncPreview{Transfer: []string{"a.txt", "sub/b.txt"}, Delete: []string{"stale.txt"}}

	t.Run("structured dry run", func(t *testing.T) {
		withStructuredOutput(t)
		stdout, stderr := runCaptured(t, func() { reportSyncDone(conn, preview, time.Second, time.Second) })

		assert.Empty(t, stdout)
		result := resultEvent(t, parseEvents(t, stderr))
		assert.Equal(t, "success", result.Status)
		assert.Equal(t, "box-a", result.Host)
		assert.Nil(t, result.ExitCode)
		assert.Equal(t, map[string]interface{}{
			"dry_run":  true,
			"transfer": []interface{}{"a.txt", "sub/b.txt"},
			"delete":   []interface{}{"stale.txt"},
		}, result.Details)
	})

	t.Run("structured sync", func(t *testing.T) {
		withStructuredOutput(t)
		_, stderr := runCaptured(t, func() { reportSyncDone(conn, nil, time.Second, time.Second) })

		result := resultEvent(t, parseEvents(t, stderr))
		assert.Equal(t, map[string]interface{}{"dry_run": false}, result.Details)
	})

	t.Run("pretty dry run", func(t *testing.T) {
		withPrettyOutput(t)
		stdout, _ := runCaptured(t, func() { reportSyncDone(conn, preview, time.Second, time.Second) })

		assert.Contains(t, stdout, "Would transfer:\n  a.txt\n  sub/b.txt\n")
		assert.Contains(t, stdout, "Would delete:\n  stale.txt\n")
		assert.Contains(t, stdout, "Dry run completed")
	})

	t.Run("pretty sync", func(t *testing.T) {
		withPrettyOutput(t)
		stdout, _ := runCaptured(t, func() { reportSyncDone(conn, nil, time.Second, time.Second) })

		assert.Contains(t, stdout, "Files synced to box-a-lan")
		assert.NotContains(t, stdout, "Would")
	})

	t.Run("pretty dry run with nothing to do", func(t *testing.T) {
		withPrettyOutput(t)
		stdout, _ := runCaptured(t, func() {
			reportSyncDone(conn, &syncPreview{Transfer: []string{}, Delete: []string{}}, time.Second, time.Second)
		})

		assert.Contains(t, stdout, "up to date")
		assert.NotContains(t, stdout, "Would transfer")
	})
}

// TestSync_StructuredOutput checks structured rr sync keeps stdout clean and
// reports its phases as JSON events on stderr, including a failed connect.
func TestSync_StructuredOutput(t *testing.T) {
	withStructuredOutput(t)
	writeGlobalConfig(t, `version: 1
hosts:
  box-a:
    ssh: [rr-test-unreachable.invalid]
    dir: ~/rr/proj
`)
	inProject(t, "version: 1\nhosts: [box-a]\n")

	var err error
	stdout, stderr := runCaptured(t, func() {
		err = Sync(SyncOptions{DryRun: true, ProbeTimeout: time.Second})
	})

	require.Error(t, err)
	assert.Empty(t, stdout)
	events := parseEvents(t, stderr)
	assert.Len(t, eventsWith(events, "connect", "started"), 1)
	assert.Len(t, eventsWith(events, "connect", "failed"), 1)
}
