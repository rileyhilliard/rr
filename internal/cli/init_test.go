package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractHostname(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "user@hostname",
			input: "user@example.com",
			want:  "example.com",
		},
		{
			name:  "root@ip",
			input: "root@192.168.1.1",
			want:  "192.168.1.1",
		},
		{
			name:  "hostname only",
			input: "localhost",
			want:  "localhost",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "multiple @ signs",
			input: "user@email.com@host.com",
			want:  "host.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHostname(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetInitDefaults(t *testing.T) {
	// Save original env and restore after test
	origHost := os.Getenv("RR_HOST")
	origName := os.Getenv("RR_HOST_NAME")
	origDir := os.Getenv("RR_REMOTE_DIR")
	origNonInteractive := os.Getenv("RR_NON_INTERACTIVE")
	origCI := os.Getenv("CI")

	defer func() {
		os.Setenv("RR_HOST", origHost)
		os.Setenv("RR_HOST_NAME", origName)
		os.Setenv("RR_REMOTE_DIR", origDir)
		os.Setenv("RR_NON_INTERACTIVE", origNonInteractive)
		os.Setenv("CI", origCI)
	}()

	t.Run("env vars populated", func(t *testing.T) {
		os.Setenv("RR_HOST", "user@env-host.com")
		os.Setenv("RR_HOST_NAME", "env-name")
		os.Setenv("RR_REMOTE_DIR", "/env/dir")
		os.Setenv("RR_NON_INTERACTIVE", "true")
		os.Unsetenv("CI")

		defaults := getInitDefaults()
		assert.Equal(t, "user@env-host.com", defaults.Host)
		assert.Equal(t, "env-name", defaults.Name)
		assert.Equal(t, "/env/dir", defaults.Dir)
		assert.True(t, defaults.NonInteractive)
	})

	t.Run("CI env triggers non-interactive", func(t *testing.T) {
		os.Unsetenv("RR_HOST")
		os.Unsetenv("RR_HOST_NAME")
		os.Unsetenv("RR_REMOTE_DIR")
		os.Unsetenv("RR_NON_INTERACTIVE")
		os.Setenv("CI", "true")

		defaults := getInitDefaults()
		assert.True(t, defaults.NonInteractive)
	})

	t.Run("empty env vars", func(t *testing.T) {
		os.Unsetenv("RR_HOST")
		os.Unsetenv("RR_HOST_NAME")
		os.Unsetenv("RR_REMOTE_DIR")
		os.Unsetenv("RR_NON_INTERACTIVE")
		os.Unsetenv("CI")

		defaults := getInitDefaults()
		assert.Empty(t, defaults.Host)
		assert.Empty(t, defaults.Name)
		assert.Empty(t, defaults.Dir)
		assert.False(t, defaults.NonInteractive)
	})
}

func TestMergeInitOptions(t *testing.T) {
	// Save original env and restore after test
	origHost := os.Getenv("RR_HOST")
	origName := os.Getenv("RR_HOST_NAME")
	origDir := os.Getenv("RR_REMOTE_DIR")
	origNonInteractive := os.Getenv("RR_NON_INTERACTIVE")
	origCI := os.Getenv("CI")

	defer func() {
		os.Setenv("RR_HOST", origHost)
		os.Setenv("RR_HOST_NAME", origName)
		os.Setenv("RR_REMOTE_DIR", origDir)
		os.Setenv("RR_NON_INTERACTIVE", origNonInteractive)
		os.Setenv("CI", origCI)
	}()

	t.Run("flags override env vars", func(t *testing.T) {
		os.Setenv("RR_HOST", "env-host")
		os.Setenv("RR_HOST_NAME", "env-name")
		os.Setenv("RR_REMOTE_DIR", "/env/dir")
		os.Unsetenv("RR_NON_INTERACTIVE")
		os.Unsetenv("CI")

		opts := InitOptions{
			Host: "flag-host",
			Name: "flag-name",
			Dir:  "/flag/dir",
		}

		merged := mergeInitOptions(opts)
		assert.Equal(t, "flag-host", merged.Host)
		assert.Equal(t, "flag-name", merged.Name)
		assert.Equal(t, "/flag/dir", merged.Dir)
	})

	t.Run("env vars fill in empty flags", func(t *testing.T) {
		os.Setenv("RR_HOST", "env-host")
		os.Setenv("RR_HOST_NAME", "env-name")
		os.Setenv("RR_REMOTE_DIR", "/env/dir")
		os.Unsetenv("RR_NON_INTERACTIVE")
		os.Unsetenv("CI")

		opts := InitOptions{} // All empty

		merged := mergeInitOptions(opts)
		assert.Equal(t, "env-host", merged.Host)
		assert.Equal(t, "env-name", merged.Name)
		assert.Equal(t, "/env/dir", merged.Dir)
	})

	t.Run("CI env sets non-interactive", func(t *testing.T) {
		os.Unsetenv("RR_HOST")
		os.Unsetenv("RR_HOST_NAME")
		os.Unsetenv("RR_REMOTE_DIR")
		os.Unsetenv("RR_NON_INTERACTIVE")
		os.Setenv("CI", "true")

		opts := InitOptions{
			NonInteractive: false, // Flag says false
		}

		merged := mergeInitOptions(opts)
		assert.True(t, merged.NonInteractive) // CI overrides
	})
}

func TestInit_NonInteractive_NoHostCreatesEmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	opts := InitOptions{
		NonInteractive: true,
		// Host is empty - in the new model, this creates a project config without a host reference
	}

	err = Init(opts)
	require.NoError(t, err)

	// Verify project config was created
	configPath := filepath.Join(tmpDir, ".rr.yaml")
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	// Project config should exist but not have a host reference
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "version: 1")
}

func TestInit_NonInteractive_Success(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	opts := InitOptions{
		NonInteractive: true,
		Host:           "user@example.com",
		Dir:            "/tmp/test",
		SkipProbe:      true, // Skip connection test
	}

	err = Init(opts)
	require.NoError(t, err)

	// Verify project config was created
	configPath := filepath.Join(tmpDir, ".rr.yaml")
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	// Verify project config contains host reference (hostname extracted from user@example.com)
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "hosts:")
	assert.Contains(t, string(content), "- example.com")

	// Verify global config was created with host details
	globalConfigPath := filepath.Join(tmpDir, ".rr", "config.yaml")
	_, err = os.Stat(globalConfigPath)
	require.NoError(t, err)

	globalContent, err := os.ReadFile(globalConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(globalContent), "user@example.com")
	assert.Contains(t, string(globalContent), "/tmp/test")
}

func TestInit_NonInteractive_ExtractsHostname(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	opts := InitOptions{
		NonInteractive: true,
		Host:           "root@192.168.1.100",
		SkipProbe:      true,
		// Name not specified - should extract from host
	}

	err = Init(opts)
	require.NoError(t, err)

	// Verify project config was created with extracted hostname as host reference
	content, err := os.ReadFile(filepath.Join(tmpDir, ".rr.yaml"))
	require.NoError(t, err)
	// The hostname "192.168.1.100" should be used as the host reference
	assert.Contains(t, string(content), "hosts:")
	assert.Contains(t, string(content), "- 192.168.1.100")
}

func TestInit_NonInteractive_ConfigExists(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	// Create existing config
	configPath := filepath.Join(tmpDir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte("existing: config"), 0644)
	require.NoError(t, err)

	opts := InitOptions{
		NonInteractive: true,
		Host:           "user@example.com",
		SkipProbe:      true,
		Overwrite:      false, // Don't overwrite
	}

	err = Init(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already a config file")
}

func TestInit_NonInteractive_ForceOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	// Create existing config
	configPath := filepath.Join(tmpDir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte("existing: config"), 0644)
	require.NoError(t, err)

	opts := InitOptions{
		NonInteractive: true,
		Host:           "user@example.com",
		SkipProbe:      true,
		Overwrite:      true, // Force overwrite
	}

	err = Init(opts)
	require.NoError(t, err)

	// Verify project config was overwritten with host reference
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "hosts:")
	assert.Contains(t, string(content), "- example.com")
	assert.NotContains(t, string(content), "existing: config")

	// Verify global config has SSH details
	globalContent, err := os.ReadFile(filepath.Join(tmpDir, ".rr", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(globalContent), "user@example.com")
}

func TestInit_NonInteractive_DefaultRemoteDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Isolate from real user config
	t.Setenv("HOME", tmpDir)

	opts := InitOptions{
		NonInteractive: true,
		Host:           "user@example.com",
		SkipProbe:      true,
		// Dir not specified - should use default
	}

	err = Init(opts)
	require.NoError(t, err)

	// Default remote dir should be in global config
	globalContent, err := os.ReadFile(filepath.Join(tmpDir, ".rr", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(globalContent), "~/rr/${PROJECT}")
}

func TestInitOptions_Defaults(t *testing.T) {
	opts := InitOptions{}

	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Name)
	assert.Empty(t, opts.Dir)
	assert.False(t, opts.Overwrite)
	assert.False(t, opts.NonInteractive)
	assert.False(t, opts.SkipProbe)
}

func TestInitOptions_WithValues(t *testing.T) {
	opts := InitOptions{
		Host:           "user@example.com",
		Name:           "myhost",
		Dir:            "/remote/dir",
		Overwrite:      true,
		NonInteractive: true,
		SkipProbe:      true,
	}

	assert.Equal(t, "user@example.com", opts.Host)
	assert.Equal(t, "myhost", opts.Name)
	assert.Equal(t, "/remote/dir", opts.Dir)
	assert.True(t, opts.Overwrite)
	assert.True(t, opts.NonInteractive)
	assert.True(t, opts.SkipProbe)
}

func TestIsIPAddress(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "IPv4 address",
			input: "192.168.1.1",
			want:  true,
		},
		{
			name:  "IPv6 address",
			input: "::1",
			want:  true,
		},
		{
			name:  "full IPv6",
			input: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			want:  true,
		},
		{
			name:  "hostname",
			input: "example.com",
			want:  false,
		},
		{
			name:  "localhost",
			input: "localhost",
			want:  false,
		},
		{
			name:  "just dots",
			input: "...",
			want:  true, // Edge case - technically matches pattern
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPAddress(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToHomeRelativePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Linux home path",
			input: "/home/user/bin",
			want:  "$HOME/bin",
		},
		{
			name:  "macOS home path",
			input: "/Users/john/Projects",
			want:  "$HOME/Projects",
		},
		{
			name:  "root home path",
			input: "/root/scripts",
			want:  "$HOME/scripts",
		},
		{
			name:  "non-home path",
			input: "/usr/local/bin",
			want:  "/usr/local/bin",
		},
		{
			name:  "empty path",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toHomeRelativePath(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckExistingConfig_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".rr.yaml")

	proceed, err := checkExistingConfig(configPath, InitOptions{})
	require.NoError(t, err)
	assert.True(t, proceed)
}

func TestCheckExistingConfig_WithOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".rr.yaml")

	// Create existing config
	err := os.WriteFile(configPath, []byte("existing: config"), 0644)
	require.NoError(t, err)

	proceed, err := checkExistingConfig(configPath, InitOptions{Overwrite: true})
	require.NoError(t, err)
	assert.True(t, proceed)
}

func TestCheckExistingConfig_NonInteractive_NoOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".rr.yaml")

	// Create existing config
	err := os.WriteFile(configPath, []byte("existing: config"), 0644)
	require.NoError(t, err)

	proceed, err := checkExistingConfig(configPath, InitOptions{NonInteractive: true, Overwrite: false})
	require.Error(t, err)
	assert.False(t, proceed)
	assert.Contains(t, err.Error(), "already a config file")
}

func TestCollectNonInteractiveValues_NoHostNoGlobal(t *testing.T) {
	// With no host specified and no global hosts, vals.hostRefs should be empty
	globalCfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{},
	}
	vals, err := collectNonInteractiveValues(InitOptions{}, globalCfg)
	require.NoError(t, err)
	require.NotNil(t, vals)
	assert.Empty(t, vals.hostRefs) // No host to reference
}

func TestCollectNonInteractiveValues_WithExistingGlobalHost(t *testing.T) {
	globalCfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{
			"dev": {SSH: []string{"dev.example.com"}, Dir: "/home/user/dev"},
		},
		Defaults: config.GlobalDefaults{Host: "dev"},
	}
	vals, err := collectNonInteractiveValues(InitOptions{}, globalCfg)
	require.NoError(t, err)
	require.NotNil(t, vals)
	// When no host flag is provided, hostRefs is empty = use all global hosts
	assert.Empty(t, vals.hostRefs)
}

func TestCollectNonInteractiveValues_WithHostAddsToGlobal(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	globalCfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{},
	}
	vals, err := collectNonInteractiveValues(InitOptions{
		Host:      "user@example.com",
		SkipProbe: true,
	}, globalCfg)
	require.NoError(t, err)
	require.NotNil(t, vals)
	assert.Equal(t, []string{"example.com"}, vals.hostRefs) // Uses extracted hostname as hostRef
}

func TestCollectNonInteractiveValues_WithExplicitName(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	globalCfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{},
	}
	vals, err := collectNonInteractiveValues(InitOptions{
		Host:      "user@example.com",
		Name:      "myhost",
		SkipProbe: true,
	}, globalCfg)
	require.NoError(t, err)
	require.NotNil(t, vals)
	assert.Equal(t, []string{"myhost"}, vals.hostRefs) // Uses explicit name
}

func TestInitCommand_MergesOptions(t *testing.T) {
	// Save original env and restore after test
	origHost := os.Getenv("RR_HOST")
	defer os.Setenv("RR_HOST", origHost)

	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Set env var
	os.Setenv("RR_HOST", "env@example.com")

	// Run with NonInteractive
	err = initCommand(InitOptions{
		NonInteractive: true,
		SkipProbe:      true,
	})
	require.NoError(t, err)

	// Verify project config was created
	content, err := os.ReadFile(filepath.Join(tmpDir, ".rr.yaml"))
	require.NoError(t, err)
	// Project config should reference the host, not contain the SSH details
	assert.Contains(t, string(content), "hosts:")
	assert.Contains(t, string(content), "- example.com")

	// Verify global config was created with the host
	globalContent, err := os.ReadFile(filepath.Join(tmpHome, ".rr", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(globalContent), "env@example.com")
}
