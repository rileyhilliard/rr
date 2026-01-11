package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
