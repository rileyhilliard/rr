package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostAddOptions_Defaults(t *testing.T) {
	opts := HostAddOptions{}

	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Name)
	assert.Empty(t, opts.Dir)
	assert.False(t, opts.SkipProbe)
}

func TestHostAddOptions_WithValues(t *testing.T) {
	opts := HostAddOptions{
		Host:      "user@example.com",
		Name:      "myhost",
		Dir:       "/remote/dir",
		SkipProbe: true,
	}

	assert.Equal(t, "user@example.com", opts.Host)
	assert.Equal(t, "myhost", opts.Name)
	assert.Equal(t, "/remote/dir", opts.Dir)
	assert.True(t, opts.SkipProbe)
}

func TestLoadGlobalConfig_NoConfig(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg, path, err := loadGlobalConfig()
	// Should succeed with empty hosts (global config is created if not exists)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotEmpty(t, path)
	assert.Empty(t, cfg.Hosts)
}

func TestLoadGlobalConfig_ValidConfig(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	// Write valid global config
	configContent := `
hosts:
  dev:
    ssh:
      - dev.example.com
    dir: /home/user/project
defaults:
  host: dev
`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, path, err := loadGlobalConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotEmpty(t, path)
	assert.Contains(t, cfg.Hosts, "dev")
	assert.Equal(t, "dev", cfg.Defaults.Host)
}

func TestLoadGlobalConfig_InvalidYAML(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	// Write invalid YAML
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	cfg, path, err := loadGlobalConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, path)
}

func TestSaveGlobalConfig_ValidConfig(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create initial global config
	cfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{
			"test": testHost(),
		},
		Defaults: config.GlobalDefaults{
			Host: "test",
		},
	}

	err := saveGlobalConfig(cfg)
	require.NoError(t, err)

	// Verify file exists
	configPath := filepath.Join(tmpDir, ".rr", "config.yaml")
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	// Read and verify content
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test.example.com")
}

func TestHostList_NoConfig(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// hostList now uses global config which auto-creates
	err := hostList()
	// Should not error, just print "No hosts configured"
	require.NoError(t, err)
}

func TestHostList_EmptyHosts(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and empty config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	configContent := `
hosts: {}
`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	// Should not error, just print "No hosts configured"
	err = hostList()
	require.NoError(t, err)
}

func TestHostList_WithHosts(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	configContent := `
hosts:
  dev:
    ssh:
      - dev.example.com
    dir: /home/user/project
  prod:
    ssh:
      - prod.example.com
    dir: /home/user/project
defaults:
  host: dev
`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostList()
	require.NoError(t, err)
}

func TestHostRemove_EmptyHosts(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory with empty hosts
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	configContent := `hosts: {}`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostRemove("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No hosts")
}

func TestHostRemove_HostNotFound(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	configContent := `
hosts:
  dev:
    ssh:
      - dev.example.com
    dir: /home/user/project
defaults:
  host: dev
`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	// Try to remove a host that doesn't exist
	err = hostRemove("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestHostList_MultipleSSHAliases(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	configContent := `
hosts:
  dev:
    ssh:
      - dev.example.com
      - dev-lan.local
      - dev-vpn.example.com
    dir: /home/user/project
defaults:
  host: dev
`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostList()
	require.NoError(t, err)
}

func TestHostList_NoDir(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	configContent := `
hosts:
  dev:
    ssh:
      - dev.example.com
defaults:
  host: dev
`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostList()
	require.NoError(t, err)
}

func TestHostList_SortedOutput(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	configContent := `
hosts:
  zebra:
    ssh:
      - zebra.example.com
  alpha:
    ssh:
      - alpha.example.com
  middle:
    ssh:
      - middle.example.com
defaults:
  host: alpha
`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostList()
	require.NoError(t, err)
}

func TestSaveGlobalConfig_OverwritesExisting(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and initial config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	initialContent := "hosts: {}\n"
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(initialContent), 0644)
	require.NoError(t, err)

	// Create new config
	cfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{
			"new": {SSH: []string{"new.example.com"}, Dir: "/new/dir"},
		},
		Defaults: config.GlobalDefaults{
			Host: "new",
		},
	}

	err = saveGlobalConfig(cfg)
	require.NoError(t, err)

	// Verify overwritten content
	content, err := os.ReadFile(filepath.Join(rrDir, "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "new.example.com")
	assert.Contains(t, string(content), "new")
}

func TestHostAddOptions_AllFieldsCombined(t *testing.T) {
	opts := HostAddOptions{
		Host:      "admin@server.example.com",
		Name:      "prod-server",
		Dir:       "/var/www/app",
		SkipProbe: true,
	}

	assert.Equal(t, "admin@server.example.com", opts.Host)
	assert.Equal(t, "prod-server", opts.Name)
	assert.Equal(t, "/var/www/app", opts.Dir)
	assert.True(t, opts.SkipProbe)
}

func TestLoadGlobalConfig_MultipleHosts(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create .rr directory and config
	rrDir := filepath.Join(tmpDir, ".rr")
	err := os.MkdirAll(rrDir, 0755)
	require.NoError(t, err)

	configContent := `
hosts:
  dev:
    ssh:
      - dev.example.com
    dir: /home/user/dev
  staging:
    ssh:
      - staging.example.com
    dir: /home/user/staging
  prod:
    ssh:
      - prod.example.com
    dir: /home/user/prod
defaults:
  host: dev
`
	err = os.WriteFile(filepath.Join(rrDir, "config.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, path, err := loadGlobalConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotEmpty(t, path)
	assert.Len(t, cfg.Hosts, 3)
	assert.Contains(t, cfg.Hosts, "dev")
	assert.Contains(t, cfg.Hosts, "staging")
	assert.Contains(t, cfg.Hosts, "prod")
}

func TestSaveGlobalConfig_PreservesAllHosts(t *testing.T) {
	// Save and restore home directory
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{
			"host1": {SSH: []string{"h1.example.com"}, Dir: "/dir1"},
			"host2": {SSH: []string{"h2.example.com"}, Dir: "/dir2"},
			"host3": {SSH: []string{"h3.example.com"}, Dir: "/dir3"},
		},
		Defaults: config.GlobalDefaults{
			Host: "host1",
		},
	}

	err := saveGlobalConfig(cfg)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, ".rr", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "h1.example.com")
	assert.Contains(t, string(content), "h2.example.com")
	assert.Contains(t, string(content), "h3.example.com")
}

// Helper functions for tests

func testHost() config.Host {
	return config.Host{
		SSH: []string{"test.example.com"},
		Dir: "/home/user/project",
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple string", "hello", "'hello'"},
		{"string with spaces", "hello world", "'hello world'"},
		{"string with single quote", "it's", "'it'\\''s'"},
		{"empty string", "", "''"},
		{"path", "/home/user/project", "'/home/user/project'"},
		{"path with spaces", "/home/user/my project", "'/home/user/my project'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShellQuotePreserveTilde(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"tilde path", "~/project", "~/'project'"},
		{"tilde nested path", "~/rr/myproject", "~/'rr/myproject'"},
		{"standalone tilde", "~", "~"},
		{"absolute path", "/home/user/project", "'/home/user/project'"},
		{"no tilde", "project", "'project'"},
		{"tilde in middle", "some/~/path", "'some/~/path'"},
		{"path with spaces after tilde", "~/my project", "~/'my project'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuotePreserveTilde(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCleanupRemoteArtifacts_NoSSHAliases(t *testing.T) {
	// Should return early without error if no SSH aliases configured
	hostConfig := config.Host{
		SSH: []string{},
		Dir: "/some/dir",
	}

	// This should not panic or error - it just returns early
	cleanupRemoteArtifacts("test", hostConfig)
}

func TestCleanupRemoteArtifacts_NoDir(t *testing.T) {
	// Should return early without error if no Dir configured
	hostConfig := config.Host{
		SSH: []string{"example.com"},
		Dir: "",
	}

	// This should not panic or error - it just returns early
	cleanupRemoteArtifacts("test", hostConfig)
}
