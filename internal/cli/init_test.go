package cli

import (
	"os"
	"path/filepath"
	"testing"

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

func TestInit_NonInteractive_RequiresHost(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	opts := InitOptions{
		NonInteractive: true,
		// Host is empty - should fail
	}

	err = Init(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Need an SSH host")
}

func TestInit_NonInteractive_Success(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	opts := InitOptions{
		NonInteractive: true,
		Host:           "user@example.com",
		Dir:            "/tmp/test",
		SkipProbe:      true, // Skip connection test
	}

	err = Init(opts)
	require.NoError(t, err)

	// Verify config was created
	configPath := filepath.Join(tmpDir, ".rr.yaml")
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	// Verify contents
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "user@example.com")
	assert.Contains(t, string(content), "/tmp/test")
}

func TestInit_NonInteractive_ExtractsHostname(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	opts := InitOptions{
		NonInteractive: true,
		Host:           "root@192.168.1.100",
		SkipProbe:      true,
		// Name not specified - should extract from host
	}

	err = Init(opts)
	require.NoError(t, err)

	// Verify config was created with extracted hostname
	content, err := os.ReadFile(filepath.Join(tmpDir, ".rr.yaml"))
	require.NoError(t, err)
	// The hostname "192.168.1.100" should be used as the host name
	assert.Contains(t, string(content), "192.168.1.100")
}

func TestInit_NonInteractive_ConfigExists(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

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

	// Verify config was overwritten
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "user@example.com")
	assert.NotContains(t, string(content), "existing: config")
}

func TestInit_NonInteractive_DefaultRemoteDir(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	err := os.Chdir(tmpDir)
	require.NoError(t, err)

	opts := InitOptions{
		NonInteractive: true,
		Host:           "user@example.com",
		SkipProbe:      true,
		// Dir not specified - should use default
	}

	err = Init(opts)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, ".rr.yaml"))
	require.NoError(t, err)
	// Default remote dir should be used
	assert.Contains(t, string(content), "~/projects/${PROJECT}")
}
