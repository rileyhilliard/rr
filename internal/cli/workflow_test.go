package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowOptions_Defaults(t *testing.T) {
	opts := WorkflowOptions{}

	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.SkipSync)
	assert.False(t, opts.SkipLock)
	assert.Empty(t, opts.WorkingDir)
	assert.False(t, opts.Quiet)
}

func TestWorkflowOptions_WithValues(t *testing.T) {
	opts := WorkflowOptions{
		Host:         "dev-server",
		Tag:          "fast",
		ProbeTimeout: 10 * time.Second,
		SkipSync:     true,
		SkipLock:     true,
		WorkingDir:   "/project",
		Quiet:        true,
	}

	assert.Equal(t, "dev-server", opts.Host)
	assert.Equal(t, "fast", opts.Tag)
	assert.Equal(t, 10*time.Second, opts.ProbeTimeout)
	assert.True(t, opts.SkipSync)
	assert.True(t, opts.SkipLock)
	assert.Equal(t, "/project", opts.WorkingDir)
	assert.True(t, opts.Quiet)
}

func TestWorkflowContext_Close_NilLock(t *testing.T) {
	ctx := &WorkflowContext{
		Lock:     nil,
		selector: nil,
	}

	// Should not panic
	ctx.Close()
}

func TestWorkflowContext_Close_NilSelector(t *testing.T) {
	ctx := &WorkflowContext{
		Lock:     nil,
		selector: nil,
	}

	// Should not panic
	ctx.Close()
}

func TestWorkflowContext_ZeroValues(t *testing.T) {
	ctx := &WorkflowContext{}

	// Zero values should be safe
	assert.Nil(t, ctx.Resolved)
	assert.Nil(t, ctx.Conn)
	assert.Nil(t, ctx.Lock)
	assert.Empty(t, ctx.WorkDir)
	assert.Nil(t, ctx.PhaseDisplay)
	assert.True(t, ctx.StartTime.IsZero())
}

func TestWorkflowContext_WithValues(t *testing.T) {
	now := time.Now()
	ctx := &WorkflowContext{
		WorkDir:   "/test/dir",
		StartTime: now,
	}

	assert.Equal(t, "/test/dir", ctx.WorkDir)
	assert.Equal(t, now, ctx.StartTime)
}

func TestSetupWorkDir_WithExplicitPath(t *testing.T) {
	ctx := &WorkflowContext{}
	opts := WorkflowOptions{
		WorkingDir: "/explicit/path",
	}

	err := setupWorkDir(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "/explicit/path", ctx.WorkDir)
}

func TestSetupWorkDir_DefaultsToCwd(t *testing.T) {
	ctx := &WorkflowContext{}
	opts := WorkflowOptions{} // WorkingDir is empty

	err := setupWorkDir(ctx, opts)
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	assert.Equal(t, cwd, ctx.WorkDir)
}

func TestSetupHostSelector_WithConfig(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev": {SSH: []string{"dev.example.com"}},
				},
				Defaults: config.GlobalDefaults{
					LocalFallback: true,
					ProbeTimeout:  5 * time.Second,
				},
			},
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	assert.Equal(t, 1, ctx.selector.HostCount())
	ctx.selector.Close()
}

func TestSetupHostSelector_ProbeTimeoutOverride(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev": {SSH: []string{"dev.example.com"}},
				},
				Defaults: config.GlobalDefaults{
					ProbeTimeout: 2 * time.Second,
				},
			},
		},
	}

	opts := WorkflowOptions{
		ProbeTimeout: 10 * time.Second, // Override config value
	}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	// The timeout is set internally, we're just verifying no panic
	ctx.selector.Close()
}

func TestSetupHostSelector_NoHosts(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{},
			},
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	assert.Equal(t, 0, ctx.selector.HostCount())
	ctx.selector.Close()
}

func TestSelectHostInteractively_WithPreferredHost(t *testing.T) {
	ctx := &WorkflowContext{
		selector: host.NewSelector(map[string]config.Host{
			"dev":  {SSH: []string{"dev.example.com"}},
			"prod": {SSH: []string{"prod.example.com"}},
		}),
		Resolved: &config.ResolvedConfig{
			Global:  &config.GlobalConfig{},
			Project: &config.Config{},
		},
	}
	defer ctx.selector.Close()

	selected, err := selectHostInteractively(ctx, "dev", false)
	require.NoError(t, err)
	assert.Equal(t, "dev", selected)
}

func TestSelectHostInteractively_SingleHost(t *testing.T) {
	ctx := &WorkflowContext{
		selector: host.NewSelector(map[string]config.Host{
			"dev": {SSH: []string{"dev.example.com"}},
		}),
		Resolved: &config.ResolvedConfig{
			Global:  &config.GlobalConfig{},
			Project: &config.Config{},
		},
	}
	defer ctx.selector.Close()

	// With only one host, it should return empty (let selector choose)
	selected, err := selectHostInteractively(ctx, "", false)
	require.NoError(t, err)
	assert.Empty(t, selected)
}

func TestSelectHostInteractively_QuietMode(t *testing.T) {
	ctx := &WorkflowContext{
		selector: host.NewSelector(map[string]config.Host{
			"dev":  {SSH: []string{"dev.example.com"}},
			"prod": {SSH: []string{"prod.example.com"}},
		}),
		Resolved: &config.ResolvedConfig{
			Global:  &config.GlobalConfig{},
			Project: &config.Config{},
		},
	}
	defer ctx.selector.Close()

	// In quiet mode, no interactive picker
	selected, err := selectHostInteractively(ctx, "", true)
	require.NoError(t, err)
	assert.Empty(t, selected)
}

func TestLoadAndValidateConfig_NoConfig(t *testing.T) {
	// Create temp dir without config
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.Error(t, err)
	// Should fail because no config file and no hosts in global config
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestLoadAndValidateConfig_ValidConfig(t *testing.T) {
	// Create temp dir with valid project config and global config
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Set up global config with hosts
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
	t.Setenv("HOME", tmpDir)

	// Write a valid project config
	projectContent := `
version: 1
host: dev
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(projectContent), 0644)
	require.NoError(t, err)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, ctx.Resolved)
	require.NotNil(t, ctx.Resolved.Project)
	assert.Equal(t, "dev", ctx.Resolved.Project.Host)
}

func TestSyncPhase_LocalConnection(t *testing.T) {
	ctx := &WorkflowContext{
		Conn: &host.Connection{
			IsLocal: true,
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
	}
	opts := WorkflowOptions{}

	// Local connection should skip sync
	err := syncPhase(ctx, opts)
	require.NoError(t, err)
}

func TestSyncPhase_SkipSync(t *testing.T) {
	ctx := &WorkflowContext{
		Conn: &host.Connection{
			IsLocal: false,
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
	}
	opts := WorkflowOptions{
		SkipSync: true,
	}

	// Should skip sync when SkipSync is true
	err := syncPhase(ctx, opts)
	require.NoError(t, err)
}

func TestLockPhase_Disabled(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Lock: config.LockConfig{
					Enabled: false,
				},
			},
		},
		Conn: &host.Connection{
			IsLocal: false,
		},
	}
	opts := WorkflowOptions{}

	// Lock disabled in config, should skip
	err := lockPhase(ctx, opts)
	require.NoError(t, err)
	assert.Nil(t, ctx.Lock)
}

func TestLockPhase_SkipLock(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Lock: config.LockConfig{
					Enabled: true,
				},
			},
		},
		Conn: &host.Connection{
			IsLocal: false,
		},
	}
	opts := WorkflowOptions{
		SkipLock: true,
	}

	// SkipLock flag should skip
	err := lockPhase(ctx, opts)
	require.NoError(t, err)
	assert.Nil(t, ctx.Lock)
}

func TestLockPhase_LocalConnection(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Lock: config.LockConfig{
					Enabled: true,
				},
			},
		},
		Conn: &host.Connection{
			IsLocal: true,
		},
	}
	opts := WorkflowOptions{}

	// Local connection should skip lock
	err := lockPhase(ctx, opts)
	require.NoError(t, err)
	assert.Nil(t, ctx.Lock)
}

func TestSetupWorkflow_NoConfig(t *testing.T) {
	// Create temp dir without config
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	_, err = SetupWorkflow(WorkflowOptions{})
	require.Error(t, err)
	// Should fail because no config file and no hosts in global config
	assert.True(t, strings.Contains(err.Error(), "No config file found") ||
		strings.Contains(err.Error(), "No hosts"),
		"Expected error about missing config or hosts, got: %s", err.Error())
}

func TestSetupWorkflow_LocalAndTagConflict(t *testing.T) {
	// This test verifies that --local and --tag flags are mutually exclusive.
	// The validation happens before config loading, so no setup needed.
	_, err := SetupWorkflow(WorkflowOptions{
		Local: true,
		Tag:   "gpu",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--local and --tag cannot be used together")
}

func TestLoadAndValidateConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Write invalid YAML
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.Error(t, err)
}

func TestLoadAndValidateConfig_MissingHosts(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Set up global config with no hosts
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
	projectContent := `version: 1`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(projectContent), 0644)
	require.NoError(t, err)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No hosts")
}

func TestSetupHostSelector_ZeroProbeTimeout(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev": {SSH: []string{"dev.example.com"}},
				},
				Defaults: config.GlobalDefaults{
					ProbeTimeout: 0, // No default timeout
				},
			},
		},
	}

	opts := WorkflowOptions{
		ProbeTimeout: 0, // No override
	}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	assert.Equal(t, 1, ctx.selector.HostCount())
	ctx.selector.Close()
}

func TestSetupHostSelector_LocalFallbackEnabled(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev": {SSH: []string{"dev.example.com"}},
				},
				Defaults: config.GlobalDefaults{
					LocalFallback: true,
				},
			},
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	ctx.selector.Close()
}

func TestSetupHostSelector_LocalFallbackDisabled(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev": {SSH: []string{"dev.example.com"}},
				},
				Defaults: config.GlobalDefaults{
					LocalFallback: false,
				},
			},
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	ctx.selector.Close()
}

func TestSetupHostSelector_MultipleHosts(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev":     {SSH: []string{"dev.example.com"}},
					"staging": {SSH: []string{"staging.example.com"}},
					"prod":    {SSH: []string{"prod.example.com"}},
				},
			},
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	assert.Equal(t, 3, ctx.selector.HostCount())
	ctx.selector.Close()
}

func TestSelectHostInteractively_NoHosts(t *testing.T) {
	ctx := &WorkflowContext{
		selector: host.NewSelector(map[string]config.Host{}),
		Resolved: &config.ResolvedConfig{
			Global:  &config.GlobalConfig{},
			Project: &config.Config{},
		},
	}
	defer ctx.selector.Close()

	selected, err := selectHostInteractively(ctx, "", false)
	require.NoError(t, err)
	assert.Empty(t, selected)
}

func TestSelectHostInteractively_NonExistentPreferredHost(t *testing.T) {
	ctx := &WorkflowContext{
		selector: host.NewSelector(map[string]config.Host{
			"dev": {SSH: []string{"dev.example.com"}},
		}),
		Resolved: &config.ResolvedConfig{
			Global:  &config.GlobalConfig{},
			Project: &config.Config{},
		},
	}
	defer ctx.selector.Close()

	// Non-existent host should still be returned - selector handles validation
	selected, err := selectHostInteractively(ctx, "nonexistent", false)
	require.NoError(t, err)
	assert.Equal(t, "nonexistent", selected)
}

func TestWorkflowContext_Close_WithLock(t *testing.T) {
	// Create a mock lock-like structure to test Close behavior
	// We can't easily create a real lock without SSH, but we can test nil handling
	ctx := &WorkflowContext{
		Lock:     nil, // Lock release on nil should not panic
		selector: nil,
	}

	// Should not panic when both are nil
	ctx.Close()
}

func TestWorkflowContext_Close_MultipleTimes(t *testing.T) {
	ctx := &WorkflowContext{
		Lock:     nil,
		selector: nil,
	}

	// Multiple closes should not panic
	ctx.Close()
	ctx.Close()
	ctx.Close()
}

func TestSyncPhase_BothLocalAndSkipSync(t *testing.T) {
	ctx := &WorkflowContext{
		Conn: &host.Connection{
			IsLocal: true,
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
	}
	opts := WorkflowOptions{
		SkipSync: true, // Both flags set
	}

	// IsLocal takes precedence, should skip with "local" message
	err := syncPhase(ctx, opts)
	require.NoError(t, err)
}

func TestLockPhase_AllDisableConditions(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		skipLock bool
		isLocal  bool
		wantLock bool
	}{
		{
			name:     "lock disabled in config",
			enabled:  false,
			skipLock: false,
			isLocal:  false,
			wantLock: false,
		},
		{
			name:     "skip lock flag set",
			enabled:  true,
			skipLock: true,
			isLocal:  false,
			wantLock: false,
		},
		{
			name:     "local connection",
			enabled:  true,
			skipLock: false,
			isLocal:  true,
			wantLock: false,
		},
		{
			name:     "all disable conditions",
			enabled:  false,
			skipLock: true,
			isLocal:  true,
			wantLock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &WorkflowContext{
				Resolved: &config.ResolvedConfig{
					Global: &config.GlobalConfig{},
					Project: &config.Config{
						Lock: config.LockConfig{
							Enabled: tt.enabled,
						},
					},
				},
				Conn: &host.Connection{
					IsLocal: tt.isLocal,
				},
			}
			opts := WorkflowOptions{
				SkipLock: tt.skipLock,
			}

			err := lockPhase(ctx, opts)
			require.NoError(t, err)
			assert.Nil(t, ctx.Lock)
		})
	}
}

func TestWorkflowOptions_AllFieldsCombined(t *testing.T) {
	opts := WorkflowOptions{
		Host:         "prod-server",
		Tag:          "gpu",
		ProbeTimeout: 30 * time.Second,
		SkipSync:     true,
		SkipLock:     true,
		WorkingDir:   "/custom/path",
		Quiet:        true,
	}

	// Verify all fields are set correctly
	assert.Equal(t, "prod-server", opts.Host)
	assert.Equal(t, "gpu", opts.Tag)
	assert.Equal(t, 30*time.Second, opts.ProbeTimeout)
	assert.True(t, opts.SkipSync)
	assert.True(t, opts.SkipLock)
	assert.Equal(t, "/custom/path", opts.WorkingDir)
	assert.True(t, opts.Quiet)
}

func TestWorkflowContext_StartTimeSet(t *testing.T) {
	before := time.Now()
	ctx := &WorkflowContext{
		StartTime: time.Now(),
	}
	after := time.Now()

	assert.False(t, ctx.StartTime.Before(before))
	assert.False(t, ctx.StartTime.After(after))
}

// ============================================================================
// Tests for Close() with real selector cleanup
// ============================================================================

func TestWorkflowContext_Close_WithSelector(t *testing.T) {
	// Create a real selector with some hosts
	selector := host.NewSelector(map[string]config.Host{
		"dev": {SSH: []string{"dev.example.com"}},
	})

	ctx := &WorkflowContext{
		Lock:     nil,
		selector: selector,
	}

	// Close should clean up the selector without panic
	ctx.Close()

	// Verify selector is still accessible (it doesn't nil out the reference)
	// but internal resources should be cleaned up
	assert.NotNil(t, ctx.selector)
}

func TestWorkflowContext_Close_WithSelectorAndConfig(t *testing.T) {
	// Set up a full context as would happen in normal workflow
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"test": {SSH: []string{"test.example.com"}},
				},
			},
			Project: &config.Config{},
		},
		WorkDir:   "/test/dir",
		StartTime: time.Now(),
	}

	// Set up selector
	setupHostSelector(ctx, WorkflowOptions{})

	// Close should clean up properly
	ctx.Close()

	// Context should still have its data
	assert.NotNil(t, ctx.Resolved)
	assert.Equal(t, "/test/dir", ctx.WorkDir)
}

func TestWorkflowContext_Close_IdempotentSelector(t *testing.T) {
	selector := host.NewSelector(map[string]config.Host{
		"dev": {SSH: []string{"dev.example.com"}},
	})

	ctx := &WorkflowContext{
		selector: selector,
	}

	// Multiple closes should not panic
	ctx.Close()
	ctx.Close()
	ctx.Close()
}

// ============================================================================
// Tests for setupHostSelector edge cases
// ============================================================================

func TestSetupHostSelector_ConfigProbeTimeoutApplied(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev": {SSH: []string{"dev.example.com"}},
				},
				Defaults: config.GlobalDefaults{
					ProbeTimeout: 15 * time.Second,
				},
			},
		},
	}

	opts := WorkflowOptions{
		ProbeTimeout: 0, // No override, should use config
	}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	ctx.selector.Close()
}

func TestSetupHostSelector_OptsOverrideConfigTimeout(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev": {SSH: []string{"dev.example.com"}},
				},
				Defaults: config.GlobalDefaults{
					ProbeTimeout: 5 * time.Second, // Config says 5s
				},
			},
		},
	}

	opts := WorkflowOptions{
		ProbeTimeout: 30 * time.Second, // Override to 30s
	}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	// We can't directly check the timeout, but we verify no panic
	ctx.selector.Close()
}

func TestSetupHostSelector_HostsWithMultipleAliases(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"dev": {
						SSH:  []string{"dev-local", "dev-vpn", "dev-public"},
						Dir:  "/home/user/project",
						Tags: []string{"fast", "gpu"},
					},
				},
			},
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	assert.Equal(t, 1, ctx.selector.HostCount())
	ctx.selector.Close()
}

func TestSetupHostSelector_HostsWithTags(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"gpu-box": {
						SSH:  []string{"gpu.example.com"},
						Tags: []string{"gpu", "fast"},
					},
					"cpu-box": {
						SSH:  []string{"cpu.example.com"},
						Tags: []string{"cpu"},
					},
				},
			},
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	assert.Equal(t, 2, ctx.selector.HostCount())
	ctx.selector.Close()
}

// ============================================================================
// Tests for syncPhase with various configurations
// ============================================================================

func TestSyncPhase_RemoteConnectionNotSkipped(t *testing.T) {
	// This tests that a remote connection with SkipSync=false would
	// actually attempt sync (though it will fail without real rsync)
	ctx := &WorkflowContext{
		Conn: &host.Connection{
			IsLocal: false,
			Name:    "remote",
			Alias:   "remote.example.com",
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Sync: config.SyncConfig{
					Exclude: []string{".git", "node_modules"},
				},
			},
		},
		WorkDir: "/tmp/test-project",
	}
	opts := WorkflowOptions{
		SkipSync: false,
		Quiet:    false,
	}

	// This will fail because there's no actual SSH connection,
	// but it exercises the non-skip path
	err := syncPhase(ctx, opts)
	// We expect an error since we don't have a real connection
	assert.Error(t, err)
}

func TestSyncPhase_QuietModeWithLocalConnection(t *testing.T) {
	ctx := &WorkflowContext{
		Conn: &host.Connection{
			IsLocal: true,
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
	}
	opts := WorkflowOptions{
		Quiet: true, // Quiet mode should still skip for local
	}

	err := syncPhase(ctx, opts)
	require.NoError(t, err)
}

func TestSyncPhase_LocalTakesPrecedenceOverSkip(t *testing.T) {
	// When both IsLocal and SkipSync are true, IsLocal should
	// be the reason for skipping (renders "local" not "skipped")
	ctx := &WorkflowContext{
		Conn: &host.Connection{
			IsLocal: true,
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
	}
	opts := WorkflowOptions{
		SkipSync: true,
	}

	err := syncPhase(ctx, opts)
	require.NoError(t, err)
}

// ============================================================================
// Tests for lockPhase edge cases
// ============================================================================

func TestLockPhase_EnabledButLocalConnection(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Lock: config.LockConfig{
					Enabled: true,
					Timeout: 5 * time.Second,
					Stale:   10 * time.Minute,
				},
			},
		},
		Conn: &host.Connection{
			IsLocal: true, // Local connections skip locking
		},
	}
	opts := WorkflowOptions{}

	err := lockPhase(ctx, opts)
	require.NoError(t, err)
	assert.Nil(t, ctx.Lock)
}

func TestLockPhase_DisabledWithRemoteConnection(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Lock: config.LockConfig{
					Enabled: false, // Explicitly disabled
				},
			},
		},
		Conn: &host.Connection{
			IsLocal: false,
			Name:    "remote",
		},
	}
	opts := WorkflowOptions{}

	err := lockPhase(ctx, opts)
	require.NoError(t, err)
	assert.Nil(t, ctx.Lock)
}

func TestLockPhase_SkipFlagWithEnabledConfig(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Lock: config.LockConfig{
					Enabled: true, // Enabled in config
					Timeout: 30 * time.Second,
				},
			},
		},
		Conn: &host.Connection{
			IsLocal: false,
			Name:    "remote",
		},
	}
	opts := WorkflowOptions{
		SkipLock: true, // But skip flag is set
	}

	err := lockPhase(ctx, opts)
	require.NoError(t, err)
	assert.Nil(t, ctx.Lock)
}

// ============================================================================
// Table-driven tests for lockPhase conditions
// ============================================================================

func TestLockPhase_SkipConditions(t *testing.T) {
	tests := []struct {
		name        string
		lockEnabled bool
		skipLock    bool
		isLocal     bool
		expectSkip  bool
	}{
		{
			name:        "all conditions met for locking",
			lockEnabled: true,
			skipLock:    false,
			isLocal:     false,
			expectSkip:  false, // Would try to lock (and fail without connection)
		},
		{
			name:        "lock disabled",
			lockEnabled: false,
			skipLock:    false,
			isLocal:     false,
			expectSkip:  true,
		},
		{
			name:        "skip flag set",
			lockEnabled: true,
			skipLock:    true,
			isLocal:     false,
			expectSkip:  true,
		},
		{
			name:        "local connection",
			lockEnabled: true,
			skipLock:    false,
			isLocal:     true,
			expectSkip:  true,
		},
		{
			name:        "local with skip flag",
			lockEnabled: true,
			skipLock:    true,
			isLocal:     true,
			expectSkip:  true,
		},
		{
			name:        "disabled with local",
			lockEnabled: false,
			skipLock:    false,
			isLocal:     true,
			expectSkip:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &WorkflowContext{
				Resolved: &config.ResolvedConfig{
					Global: &config.GlobalConfig{},
					Project: &config.Config{
						Lock: config.LockConfig{
							Enabled: tt.lockEnabled,
							Timeout: time.Second,
						},
					},
				},
				Conn: &host.Connection{
					IsLocal: tt.isLocal,
				},
				PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
				WorkDir:      "/tmp/test",
			}
			opts := WorkflowOptions{
				SkipLock: tt.skipLock,
			}

			err := lockPhase(ctx, opts)

			if tt.expectSkip {
				// Skip conditions should result in no error and no lock
				require.NoError(t, err)
				assert.Nil(t, ctx.Lock)
			}
			// For non-skip conditions, we'd get an error (no SSH connection)
			// so we don't assert on that case here
		})
	}
}

// ============================================================================
// Tests for selectHostInteractively edge cases
// ============================================================================

func TestSelectHostInteractively_PreferredHostReturned(t *testing.T) {
	ctx := &WorkflowContext{
		selector: host.NewSelector(map[string]config.Host{
			"dev":  {SSH: []string{"dev.example.com"}},
			"prod": {SSH: []string{"prod.example.com"}},
		}),
		Resolved: &config.ResolvedConfig{
			Global:  &config.GlobalConfig{},
			Project: &config.Config{},
		},
	}
	defer ctx.selector.Close()

	// When preferred is set, it should be returned regardless of host count
	selected, err := selectHostInteractively(ctx, "dev", false)
	require.NoError(t, err)
	assert.Equal(t, "dev", selected)
}

func TestSelectHostInteractively_EmptyPreferredWithOneHost(t *testing.T) {
	ctx := &WorkflowContext{
		selector: host.NewSelector(map[string]config.Host{
			"only": {SSH: []string{"only.example.com"}},
		}),
		Resolved: &config.ResolvedConfig{
			Global:  &config.GlobalConfig{},
			Project: &config.Config{},
		},
	}
	defer ctx.selector.Close()

	// With only one host, should return empty (no interactive selection)
	selected, err := selectHostInteractively(ctx, "", false)
	require.NoError(t, err)
	assert.Empty(t, selected)
}

// ============================================================================
// Tests for setupWorkDir
// ============================================================================

func TestSetupWorkDir_ExplicitPathUsed(t *testing.T) {
	ctx := &WorkflowContext{}
	opts := WorkflowOptions{
		WorkingDir: "/custom/project/path",
	}

	err := setupWorkDir(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "/custom/project/path", ctx.WorkDir)
}

func TestSetupWorkDir_EmptyUsesCurrentDir(t *testing.T) {
	// When no ProjectRoot is set, should fall back to cwd
	ctx := &WorkflowContext{}
	opts := WorkflowOptions{
		WorkingDir: "",
	}

	err := setupWorkDir(ctx, opts)
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	assert.Equal(t, cwd, ctx.WorkDir)
}

func TestSetupWorkDir_UsesProjectRoot(t *testing.T) {
	// When ProjectRoot is set in Resolved config, use that as working directory
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			ProjectRoot: "/project/root",
		},
	}
	opts := WorkflowOptions{
		WorkingDir: "",
	}

	err := setupWorkDir(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "/project/root", ctx.WorkDir)
}

func TestSetupWorkDir_ExplicitOverridesProjectRoot(t *testing.T) {
	// Explicit WorkingDir should override ProjectRoot
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			ProjectRoot: "/project/root",
		},
	}
	opts := WorkflowOptions{
		WorkingDir: "/explicit/path",
	}

	err := setupWorkDir(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "/explicit/path", ctx.WorkDir)
}

func TestSetupWorkDir_PreservesExistingWorkDir(t *testing.T) {
	// If WorkingDir is set in opts, it should override any existing value
	ctx := &WorkflowContext{
		WorkDir: "/previous/path",
	}
	opts := WorkflowOptions{
		WorkingDir: "/new/path",
	}

	err := setupWorkDir(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "/new/path", ctx.WorkDir)
}

// ============================================================================
// Integration-style tests for workflow setup scenarios
// ============================================================================

func TestSetupHostSelector_FullConfiguration(t *testing.T) {
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"primary": {
						SSH:  []string{"primary-lan", "primary-vpn"},
						Dir:  "/home/user/project",
						Tags: []string{"fast"},
					},
					"secondary": {
						SSH:  []string{"secondary.example.com"},
						Dir:  "/opt/project",
						Tags: []string{"backup"},
					},
				},
				Defaults: config.GlobalDefaults{
					LocalFallback: true,
					ProbeTimeout:  10 * time.Second,
				},
			},
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	assert.Equal(t, 2, ctx.selector.HostCount())

	// Verify host info can be retrieved
	hostInfos := ctx.selector.HostInfo()
	assert.Len(t, hostInfos, 2)

	// Find primary and verify tags
	for _, h := range hostInfos {
		if h.Name == "primary" {
			assert.Equal(t, []string{"fast"}, h.Tags)
		}
	}

	ctx.selector.Close()
}

func TestWorkflowContext_FullLifecycle(t *testing.T) {
	// Test a complete workflow context lifecycle
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"test": {SSH: []string{"test.example.com"}},
				},
				Defaults: config.GlobalDefaults{
					LocalFallback: true,
				},
			},
			Project: &config.Config{},
		},
		WorkDir:      "/tmp/project",
		StartTime:    time.Now(),
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
	}

	// Set up selector
	setupHostSelector(ctx, WorkflowOptions{})
	require.NotNil(t, ctx.selector)

	// Set up a local connection (simulating fallback)
	ctx.Conn = &host.Connection{
		Name:    "local",
		IsLocal: true,
	}

	// Test sync phase with local connection
	err := syncPhase(ctx, WorkflowOptions{})
	require.NoError(t, err)

	// Test lock phase with local connection
	ctx.Resolved.Project.Lock = config.LockConfig{Enabled: true}
	err = lockPhase(ctx, WorkflowOptions{})
	require.NoError(t, err)

	// Close should clean up
	ctx.Close()
}

// ============================================================================
// Tests for Close() with mock lock
// ============================================================================

func TestWorkflowContext_Close_WithMockLock(t *testing.T) {
	// Create a mock lock using the lock package
	mockLock := &lock.Lock{
		Dir:  "/tmp/rr-test.lock",
		Info: &lock.LockInfo{},
		// Note: conn is nil, so Release() will just return nil
	}

	ctx := &WorkflowContext{
		Lock:     mockLock,
		selector: nil,
	}

	// Close should call Lock.Release() without panic
	ctx.Close()
}

func TestWorkflowContext_Close_WithLockAndSelector(t *testing.T) {
	// Create both a lock and selector
	mockLock := &lock.Lock{
		Dir:  "/tmp/rr-test.lock",
		Info: &lock.LockInfo{},
	}

	selector := host.NewSelector(map[string]config.Host{
		"dev": {SSH: []string{"dev.example.com"}},
	})

	ctx := &WorkflowContext{
		Lock:     mockLock,
		selector: selector,
	}

	// Close should clean up both without panic
	ctx.Close()
}

// ============================================================================
// Tests for syncPhase quiet mode path
// ============================================================================

func TestSyncPhase_QuietModeRemoteAttempt(t *testing.T) {
	// Test the quiet mode path with a remote connection
	// This will fail because there's no actual SSH connection,
	// but it exercises the syncQuiet code path
	ctx := &WorkflowContext{
		Conn: &host.Connection{
			IsLocal: false,
			Name:    "remote",
			Alias:   "remote.example.com",
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Sync: config.SyncConfig{
					Exclude: []string{".git"},
				},
			},
		},
		WorkDir: "/tmp/test-project",
	}
	opts := WorkflowOptions{
		SkipSync: false,
		Quiet:    true, // This triggers syncQuiet path
	}

	// This will fail at rsync, but exercises the quiet sync path
	err := syncPhase(ctx, opts)
	assert.Error(t, err) // Expected to fail without real connection
}

// ============================================================================
// Tests for lockPhase with real lock attempt
// ============================================================================

func TestLockPhase_AttemptLockWithoutConnection(t *testing.T) {
	// This tests the path where locking is enabled but will fail
	// because there's no SSH client
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Lock: config.LockConfig{
					Enabled: true,
					Timeout: 100 * time.Millisecond, // Short timeout
					Stale:   10 * time.Minute,
					Dir:     "/tmp",
				},
			},
		},
		Conn: &host.Connection{
			IsLocal: false,
			Name:    "remote",
			Client:  nil, // No client - will fail
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
		WorkDir:      "/tmp/test",
	}
	opts := WorkflowOptions{
		SkipLock: false,
	}

	// This should fail because there's no SSH client
	err := lockPhase(ctx, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Can't grab the lock")
}

// ============================================================================
// Tests for setupWorkDir edge cases
// ============================================================================

func TestSetupWorkDir_CwdPreference(t *testing.T) {
	// When opts.WorkingDir is empty, should use os.Getwd()
	ctx := &WorkflowContext{}
	opts := WorkflowOptions{WorkingDir: ""}

	cwd, err := os.Getwd()
	require.NoError(t, err)

	err = setupWorkDir(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, cwd, ctx.WorkDir)
}

// ============================================================================
// Tests for loadAndValidateConfig edge cases
// ============================================================================

func TestLoadAndValidateConfig_WithDefaultHost(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Set up global config with hosts
	globalDir := filepath.Join(tmpDir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  prod:
    ssh:
      - prod.example.com
    dir: /home/user/project
  staging:
    ssh:
      - staging.example.com
    dir: /home/user/staging
`
	err = os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)
	t.Setenv("HOME", tmpDir)

	// Write a project config with host reference
	projectContent := `
version: 1
host: prod
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(projectContent), 0644)
	require.NoError(t, err)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, ctx.Resolved)
	require.NotNil(t, ctx.Resolved.Project)
	assert.Equal(t, "prod", ctx.Resolved.Project.Host)
	assert.Len(t, ctx.Resolved.Global.Hosts, 2)
}

func TestLoadAndValidateConfig_WithSyncExcludes(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Set up global config with hosts
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
	t.Setenv("HOME", tmpDir)

	// Write project config with sync excludes
	projectContent := `
version: 1
host: dev
sync:
  exclude:
    - .git
    - node_modules
    - "*.pyc"
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(projectContent), 0644)
	require.NoError(t, err)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, ctx.Resolved)
	require.NotNil(t, ctx.Resolved.Project)
	assert.Contains(t, ctx.Resolved.Project.Sync.Exclude, ".git")
	assert.Contains(t, ctx.Resolved.Project.Sync.Exclude, "node_modules")
}

func TestLoadAndValidateConfig_WithLockConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Set up global config with hosts
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
	t.Setenv("HOME", tmpDir)

	// Write project config with lock settings
	projectContent := `
version: 1
host: dev
lock:
  enabled: true
  timeout: 30s
  stale: 5m
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(projectContent), 0644)
	require.NoError(t, err)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, ctx.Resolved)
	require.NotNil(t, ctx.Resolved.Project)
	assert.True(t, ctx.Resolved.Project.Lock.Enabled)
	assert.Equal(t, 30*time.Second, ctx.Resolved.Project.Lock.Timeout)
	assert.Equal(t, 5*time.Minute, ctx.Resolved.Project.Lock.Stale)
}

// ============================================================================
// Additional tests for complete behavior coverage
// ============================================================================

func TestWorkflowOptions_ZeroValueBehavior(t *testing.T) {
	// Verify that zero values have expected behavior
	opts := WorkflowOptions{}

	// All boolean flags should be false by default
	assert.False(t, opts.SkipSync)
	assert.False(t, opts.SkipLock)
	assert.False(t, opts.Quiet)

	// String fields should be empty
	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Empty(t, opts.WorkingDir)

	// Duration should be zero
	assert.Zero(t, opts.ProbeTimeout)
}

func TestWorkflowContext_ZeroValueClose(t *testing.T) {
	// A completely zero-value context should be safe to close
	ctx := &WorkflowContext{}
	ctx.Close() // Should not panic
}

func TestSyncPhase_SkipSyncFlag(t *testing.T) {
	// Test that SkipSync flag properly skips sync for remote connections
	ctx := &WorkflowContext{
		Conn: &host.Connection{
			IsLocal: false, // Remote connection
			Name:    "remote",
		},
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
	}
	opts := WorkflowOptions{
		SkipSync: true, // Skip sync flag set
	}

	err := syncPhase(ctx, opts)
	require.NoError(t, err)
}

func TestLockPhase_AllSkipConditionsCombined(t *testing.T) {
	// Test with all conditions that should skip locking
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{},
			Project: &config.Config{
				Lock: config.LockConfig{
					Enabled: false, // Disabled
				},
			},
		},
		Conn: &host.Connection{
			IsLocal: true, // Local
		},
	}
	opts := WorkflowOptions{
		SkipLock: true, // Also skip flag
	}

	err := lockPhase(ctx, opts)
	require.NoError(t, err)
	assert.Nil(t, ctx.Lock)
}

// ============================================================================
// Tests for signal handling
// ============================================================================

func TestWorkflowContext_SetupSignalHandler(t *testing.T) {
	ctx := &WorkflowContext{}

	// Setup signal handler
	ctx.setupSignalHandler()

	// Signal channel should be initialized
	assert.NotNil(t, ctx.signalChan)

	// Clean up
	ctx.Close()
}

func TestWorkflowContext_Close_StopsSignalHandler(t *testing.T) {
	ctx := &WorkflowContext{}
	ctx.setupSignalHandler()

	// Close should stop the signal handler
	ctx.Close()

	// Verify signalChan is closed (sending would panic on closed channel)
	// We can't directly test this without risking panic, but we verify
	// that Close() completes without hanging
}

func TestWorkflowContext_Close_WithSignalHandler_MultipleTimes(t *testing.T) {
	ctx := &WorkflowContext{}
	ctx.setupSignalHandler()

	// Multiple closes should be safe due to sync.Once
	ctx.Close()
	ctx.Close()
	ctx.Close()
}

func TestWorkflowContext_Close_WithAllResources(t *testing.T) {
	// Test Close with all resources initialized
	mockLock := &lock.Lock{
		Dir:  "/tmp/rr-test.lock",
		Info: &lock.LockInfo{},
	}

	selector := host.NewSelector(map[string]config.Host{
		"dev": {SSH: []string{"dev.example.com"}},
	})

	ctx := &WorkflowContext{
		Lock:     mockLock,
		selector: selector,
	}
	ctx.setupSignalHandler()

	// Close should clean up everything
	ctx.Close()
}

func TestWorkflowContext_SignalHandler_CleanupOrder(t *testing.T) {
	// Verify that Close() properly handles all resources regardless of order
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Hosts: map[string]config.Host{
					"test": {SSH: []string{"test.example.com"}},
				},
			},
			Project: &config.Config{},
		},
		WorkDir:   "/test/dir",
		StartTime: time.Now(),
	}

	// Setup resources
	setupHostSelector(ctx, WorkflowOptions{})
	ctx.setupSignalHandler()

	// Close should handle all resources
	ctx.Close()

	// Verify we can still access the context data (Close doesn't nil fields)
	assert.NotNil(t, ctx.Resolved)
	assert.Equal(t, "/test/dir", ctx.WorkDir)
}
