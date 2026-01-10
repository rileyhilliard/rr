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

func TestLoadExistingConfig_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	cfg, path, err := loadExistingConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, path)
	assert.Contains(t, err.Error(), "No config file found")
}

func TestLoadExistingConfig_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Write valid config
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

	cfg, path, err := loadExistingConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotEmpty(t, path)
	assert.Contains(t, cfg.Hosts, "dev")
}

func TestLoadExistingConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	// Write invalid YAML
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	cfg, path, err := loadExistingConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Empty(t, path)
}

func TestSaveConfig_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".rr.yaml")

	cfg, err := loadExistingConfigForTest(t, tmpDir)
	require.NoError(t, err)

	// Add a host
	cfg.Hosts["test"] = testHost()
	cfg.Default = "test"

	err = saveConfig(configPath, cfg)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	// Read and verify content
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test.example.com")
	assert.Contains(t, string(content), "Road Runner configuration")
}

func TestSaveConfig_WritesHeader(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".rr.yaml")

	cfg, err := loadExistingConfigForTest(t, tmpDir)
	require.NoError(t, err)

	err = saveConfig(configPath, cfg)
	require.NoError(t, err)

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "# Road Runner configuration")
	assert.Contains(t, string(content), "rr run <command>")
}

func TestHostList_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = hostList()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No config file found")
}

func TestHostList_EmptyHosts(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	configContent := `
version: 1
hosts: {}
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	// Should not error, just print "No hosts configured"
	err = hostList()
	require.NoError(t, err)
}

func TestHostList_WithHosts(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	configContent := `
version: 1
hosts:
  dev:
    ssh:
      - dev.example.com
    dir: /home/user/project
  prod:
    ssh:
      - prod.example.com
    dir: /home/user/project
default: dev
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostList()
	require.NoError(t, err)
}

func TestHostRemove_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = hostRemove("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No config file found")
}

func TestHostRemove_HostNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

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

	// Try to remove a host that doesn't exist
	// Note: This will prompt for confirmation in interactive mode,
	// so we test the error case for a non-existent host
	err = hostRemove("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestHostAdd_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	err = hostAdd(HostAddOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No config file found")
}

func TestHostList_MultipleSSHAliases(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	configContent := `
version: 1
hosts:
  dev:
    ssh:
      - dev.example.com
      - dev-lan.local
      - dev-vpn.example.com
    dir: /home/user/project
default: dev
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostList()
	require.NoError(t, err)
}

func TestHostList_NoDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	configContent := `
version: 1
hosts:
  dev:
    ssh:
      - dev.example.com
default: dev
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostList()
	require.NoError(t, err)
}

func TestHostList_SortedOutput(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	configContent := `
version: 1
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
default: alpha
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	err = hostList()
	require.NoError(t, err)
}

func TestSaveConfig_OverwritesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".rr.yaml")

	// Write initial config
	initialContent := "version: 1\nhosts: {}\n"
	err := os.WriteFile(configPath, []byte(initialContent), 0644)
	require.NoError(t, err)

	// Load and modify
	cfg := &config.Config{
		Hosts: map[string]config.Host{
			"new": {SSH: []string{"new.example.com"}, Dir: "/new/dir"},
		},
		Default: "new",
	}

	err = saveConfig(configPath, cfg)
	require.NoError(t, err)

	// Verify overwritten content
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "new.example.com")
	assert.Contains(t, string(content), "new")
}

func TestHostRemove_EmptyHostsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	configContent := `
version: 1
hosts: {}
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	// Try to remove with no hosts configured
	err = hostRemove("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No hosts")
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

func TestLoadExistingConfig_MultipleHosts(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	configContent := `
version: 1
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
default: dev
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, path, err := loadExistingConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotEmpty(t, path)
	assert.Len(t, cfg.Hosts, 3)
	assert.Contains(t, cfg.Hosts, "dev")
	assert.Contains(t, cfg.Hosts, "staging")
	assert.Contains(t, cfg.Hosts, "prod")
}

func TestSaveConfig_PreservesAllHosts(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".rr.yaml")

	cfg := &config.Config{
		Hosts: map[string]config.Host{
			"host1": {SSH: []string{"h1.example.com"}, Dir: "/dir1"},
			"host2": {SSH: []string{"h2.example.com"}, Dir: "/dir2"},
			"host3": {SSH: []string{"h3.example.com"}, Dir: "/dir3"},
		},
		Default: "host1",
	}

	err := saveConfig(configPath, cfg)
	require.NoError(t, err)

	content, err := os.ReadFile(configPath)
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

func loadExistingConfigForTest(t *testing.T, tmpDir string) (*config.Config, error) {
	t.Helper()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	if err != nil {
		return nil, err
	}

	configContent := `
version: 1
hosts: {}
`
	err = os.WriteFile(filepath.Join(tmpDir, ".rr.yaml"), []byte(configContent), 0644)
	if err != nil {
		return nil, err
	}

	cfg, _, err := loadExistingConfig()
	return cfg, err
}
