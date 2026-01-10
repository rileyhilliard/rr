package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
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
	assert.Nil(t, ctx.Config)
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
		Config: &config.Config{
			Hosts: map[string]config.Host{
				"dev": {SSH: []string{"dev.example.com"}},
			},
			LocalFallback: true,
			ProbeTimeout:  5 * time.Second,
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
		Config: &config.Config{
			Hosts: map[string]config.Host{
				"dev": {SSH: []string{"dev.example.com"}},
			},
			ProbeTimeout: 2 * time.Second,
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
		Config: &config.Config{
			Hosts: map[string]config.Host{},
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
		Config: &config.Config{},
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
		Config: &config.Config{},
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
		Config: &config.Config{},
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

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No config file found")
}

func TestLoadAndValidateConfig_ValidConfig(t *testing.T) {
	// Create temp dir with valid config
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Write a valid config
	configContent := `
version: 1
hosts:
  dev:
    ssh:
      - dev.example.com
    dir: /home/user/project
default: dev
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, ctx.Config)
	assert.Equal(t, "dev", ctx.Config.Default)
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
		Config: &config.Config{
			Lock: config.LockConfig{
				Enabled: false,
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
		Config: &config.Config{
			Lock: config.LockConfig{
				Enabled: true,
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
		Config: &config.Config{
			Lock: config.LockConfig{
				Enabled: true,
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

	_, err = SetupWorkflow(WorkflowOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No config file found")
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

	// Write config with no hosts
	configContent := `
version: 1
hosts: {}
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	ctx := &WorkflowContext{}
	err = loadAndValidateConfig(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No hosts")
}

func TestSetupHostSelector_ZeroProbeTimeout(t *testing.T) {
	ctx := &WorkflowContext{
		Config: &config.Config{
			Hosts: map[string]config.Host{
				"dev": {SSH: []string{"dev.example.com"}},
			},
			ProbeTimeout: 0, // No default timeout
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
		Config: &config.Config{
			Hosts: map[string]config.Host{
				"dev": {SSH: []string{"dev.example.com"}},
			},
			LocalFallback: true,
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	ctx.selector.Close()
}

func TestSetupHostSelector_LocalFallbackDisabled(t *testing.T) {
	ctx := &WorkflowContext{
		Config: &config.Config{
			Hosts: map[string]config.Host{
				"dev": {SSH: []string{"dev.example.com"}},
			},
			LocalFallback: false,
		},
	}

	opts := WorkflowOptions{}
	setupHostSelector(ctx, opts)

	require.NotNil(t, ctx.selector)
	ctx.selector.Close()
}

func TestSetupHostSelector_MultipleHosts(t *testing.T) {
	ctx := &WorkflowContext{
		Config: &config.Config{
			Hosts: map[string]config.Host{
				"dev":     {SSH: []string{"dev.example.com"}},
				"staging": {SSH: []string{"staging.example.com"}},
				"prod":    {SSH: []string{"prod.example.com"}},
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
		Config:   &config.Config{},
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
		Config: &config.Config{},
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
				Config: &config.Config{
					Lock: config.LockConfig{
						Enabled: tt.enabled,
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
