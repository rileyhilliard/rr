package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, CurrentConfigVersion, cfg.Version)
	assert.Empty(t, cfg.Host) // Project config has optional host reference
	assert.True(t, cfg.Lock.Enabled)
	assert.Equal(t, 5*time.Minute, cfg.Lock.Timeout)
	assert.Equal(t, 10*time.Minute, cfg.Lock.Stale)
	assert.Equal(t, "auto", cfg.Output.Color)
	assert.Equal(t, "auto", cfg.Output.Format)
	assert.True(t, cfg.Output.Timing)
	assert.Equal(t, "normal", cfg.Output.Verbosity)

	// Monitor defaults
	assert.Equal(t, "2s", cfg.Monitor.Interval)
	assert.Equal(t, 70, cfg.Monitor.Thresholds.CPU.Warning)
	assert.Equal(t, 90, cfg.Monitor.Thresholds.CPU.Critical)
	assert.Equal(t, 70, cfg.Monitor.Thresholds.RAM.Warning)
	assert.Equal(t, 90, cfg.Monitor.Thresholds.RAM.Critical)
	assert.Equal(t, 70, cfg.Monitor.Thresholds.GPU.Warning)
	assert.Equal(t, 90, cfg.Monitor.Thresholds.GPU.Critical)
	assert.Empty(t, cfg.Monitor.Exclude)
}

func TestDefaultGlobalConfig(t *testing.T) {
	cfg := DefaultGlobalConfig()

	assert.Equal(t, CurrentGlobalConfigVersion, cfg.Version)
	assert.NotNil(t, cfg.Hosts)
	assert.Empty(t, cfg.Hosts)
	assert.Equal(t, 2*time.Second, cfg.Defaults.ProbeTimeout)
	assert.False(t, cfg.Defaults.LocalFallback)
}

func TestGlobalConfigPath(t *testing.T) {
	path, err := GlobalConfigPath()
	require.NoError(t, err)

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".rr", "config.yaml")
	assert.Equal(t, expected, path)
}

func TestEnsureGlobalConfigDir(t *testing.T) {
	// Save original home and restore after test
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Use temp dir as home
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	err := EnsureGlobalConfigDir()
	require.NoError(t, err)

	// Check directory was created
	configDir := filepath.Join(tmpHome, ".rr")
	info, err := os.Stat(configDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestLoadGlobal_NoFile(t *testing.T) {
	// Save original home and restore after test
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Use temp dir as home (no config exists)
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	cfg, err := LoadGlobal()
	require.NoError(t, err)

	// Should return defaults
	assert.Equal(t, CurrentGlobalConfigVersion, cfg.Version)
	assert.Empty(t, cfg.Hosts)
}

func TestLoadGlobal_WithFile(t *testing.T) {
	// Save original home and restore after test
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Set up temp home with config
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".rr")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	content := `
version: 1
hosts:
  dev:
    ssh:
      - dev-lan
      - dev-vpn
    dir: ~/projects
defaults:
  probe_timeout: 5s
  local_fallback: true
`
	configPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	cfg, err := LoadGlobal()
	require.NoError(t, err)

	assert.Equal(t, 1, cfg.Version)
	assert.Len(t, cfg.Hosts, 1)
	assert.Contains(t, cfg.Hosts, "dev")
	assert.Equal(t, []string{"dev-lan", "dev-vpn"}, cfg.Hosts["dev"].SSH)
	assert.Equal(t, 5*time.Second, cfg.Defaults.ProbeTimeout)
	assert.True(t, cfg.Defaults.LocalFallback)
}

func TestResolveHost(t *testing.T) {
	tests := []struct {
		name        string
		resolved    *ResolvedConfig
		preferred   string
		wantName    string
		wantErr     bool
		errContains string
	}{
		{
			name: "preferred from flag takes precedence",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Hosts: map[string]Host{
						"dev":  {SSH: []string{"dev"}, Dir: "/home/dev"},
						"prod": {SSH: []string{"prod"}, Dir: "/home/prod"},
					},
				},
				Project: &Config{Host: "prod"},
			},
			preferred: "dev",
			wantName:  "dev",
		},
		{
			name: "project host used when no flag",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Hosts: map[string]Host{
						"dev":  {SSH: []string{"dev"}, Dir: "/home/dev"},
						"prod": {SSH: []string{"prod"}, Dir: "/home/prod"},
					},
				},
				Project: &Config{Host: "prod"},
			},
			preferred: "",
			wantName:  "prod",
		},
		{
			name: "alphabetical order used when no flag or project host",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Hosts: map[string]Host{
						"dev":  {SSH: []string{"dev"}, Dir: "/home/dev"},
						"prod": {SSH: []string{"prod"}, Dir: "/home/prod"},
					},
				},
				Project: &Config{Host: ""},
			},
			preferred: "",
			wantName:  "dev", // "dev" comes before "prod" alphabetically
		},
		{
			name: "first alphabetically when no other preference",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Hosts: map[string]Host{
						"zeta":  {SSH: []string{"zeta"}, Dir: "/home/zeta"},
						"alpha": {SSH: []string{"alpha"}, Dir: "/home/alpha"},
						"beta":  {SSH: []string{"beta"}, Dir: "/home/beta"},
					},
				},
				Project: &Config{},
			},
			preferred: "",
			wantName:  "alpha",
		},
		{
			name: "error when no hosts configured",
			resolved: &ResolvedConfig{
				Global:  &GlobalConfig{Hosts: map[string]Host{}},
				Project: &Config{},
				Source:  ProjectOnly,
			},
			preferred:   "",
			wantErr:     true,
			errContains: "No hosts configured",
		},
		{
			name: "error with contextual message when global only",
			resolved: &ResolvedConfig{
				Global:  &GlobalConfig{Hosts: map[string]Host{}},
				Project: &Config{},
				Source:  GlobalOnly,
			},
			preferred:   "",
			wantErr:     true,
			errContains: "no project config found",
		},
		{
			name: "error when host not found",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Hosts: map[string]Host{
						"dev": {SSH: []string{"dev"}, Dir: "/home/dev"},
					},
				},
				Project: &Config{Host: "nonexistent"},
			},
			preferred:   "",
			wantErr:     true,
			errContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, host, err := ResolveHost(tt.resolved, tt.preferred)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantName, name)
				assert.NotNil(t, host)
			}
		})
	}
}

func TestLoadResolved(t *testing.T) {
	// Save original home and cwd
	originalHome := os.Getenv("HOME")
	originalWd, _ := os.Getwd()
	defer func() {
		os.Setenv("HOME", originalHome)
		os.Chdir(originalWd)
	}()

	t.Run("global only - no project config", func(t *testing.T) {
		tmpHome := t.TempDir()
		tmpProject := t.TempDir()
		os.Setenv("HOME", tmpHome)
		os.Chdir(tmpProject)

		// Create global config
		configDir := filepath.Join(tmpHome, ".rr")
		require.NoError(t, os.MkdirAll(configDir, 0755))
		globalContent := `
version: 1
hosts:
  dev:
    ssh: [dev]
    dir: ~/dev
`
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(globalContent), 0644))

		resolved, err := LoadResolved("")
		require.NoError(t, err)

		assert.Equal(t, GlobalOnly, resolved.Source)
		assert.Len(t, resolved.Global.Hosts, 1)
		assert.NotNil(t, resolved.Project)
	})

	t.Run("both - project and global config", func(t *testing.T) {
		tmpHome := t.TempDir()
		tmpProject := t.TempDir()
		os.Setenv("HOME", tmpHome)
		os.Chdir(tmpProject)

		// Create global config
		configDir := filepath.Join(tmpHome, ".rr")
		require.NoError(t, os.MkdirAll(configDir, 0755))
		globalContent := `
version: 1
hosts:
  dev:
    ssh: [dev]
    dir: ~/dev
`
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(globalContent), 0644))

		// Create project config
		projectContent := `
version: 1
host: dev
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpProject, ".rr.yaml"), []byte(projectContent), 0644))

		resolved, err := LoadResolved("")
		require.NoError(t, err)

		assert.Equal(t, Both, resolved.Source)
		assert.Len(t, resolved.Global.Hosts, 1)
		assert.Equal(t, "dev", resolved.Project.Host)
		// Use EvalSymlinks for comparison to handle /var -> /private/var on macOS
		expectedResolved, _ := filepath.EvalSymlinks(tmpProject)
		actualResolved, _ := filepath.EvalSymlinks(resolved.ProjectRoot)
		assert.Equal(t, expectedResolved, actualResolved)
	})

	t.Run("project root set from subdirectory", func(t *testing.T) {
		tmpHome := t.TempDir()
		tmpProject := t.TempDir()
		os.Setenv("HOME", tmpHome)

		// Create global config
		configDir := filepath.Join(tmpHome, ".rr")
		require.NoError(t, os.MkdirAll(configDir, 0755))
		globalContent := `
version: 1
hosts:
  dev:
    ssh: [dev]
    dir: ~/dev
`
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(globalContent), 0644))

		// Create project config at root
		projectContent := `
version: 1
host: dev
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpProject, ".rr.yaml"), []byte(projectContent), 0644))

		// Create subdirectory and change to it
		subdir := filepath.Join(tmpProject, "src", "components")
		require.NoError(t, os.MkdirAll(subdir, 0755))
		os.Chdir(subdir)

		resolved, err := LoadResolved("")
		require.NoError(t, err)

		assert.Equal(t, Both, resolved.Source)
		// Use EvalSymlinks for comparison to handle /var -> /private/var on macOS
		expectedResolved, _ := filepath.EvalSymlinks(tmpProject)
		actualResolved, _ := filepath.EvalSymlinks(resolved.ProjectRoot)
		assert.Equal(t, expectedResolved, actualResolved)
	})

	t.Run("project root empty when no project config", func(t *testing.T) {
		tmpHome := t.TempDir()
		tmpProject := t.TempDir()
		os.Setenv("HOME", tmpHome)
		os.Chdir(tmpProject)

		// Create global config only
		configDir := filepath.Join(tmpHome, ".rr")
		require.NoError(t, os.MkdirAll(configDir, 0755))
		globalContent := `
version: 1
hosts:
  dev:
    ssh: [dev]
    dir: ~/dev
`
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(globalContent), 0644))

		resolved, err := LoadResolved("")
		require.NoError(t, err)

		assert.Equal(t, GlobalOnly, resolved.Source)
		assert.Empty(t, resolved.ProjectRoot)
	})
}

func TestLoad(t *testing.T) {
	// Create a temp project config file (no hosts - those are in global config now)
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".rr.yaml")

	content := `
version: 1
host: mini
sync:
  exclude:
    - .git/
    - node_modules/
lock:
  enabled: true
  timeout: 3m
  stale: 8m
tasks:
  build:
    run: make build
  test:
    description: Run tests
    run: go test ./...
output:
  color: always
  verbosity: verbose
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, "mini", cfg.Host)
	assert.True(t, cfg.Lock.Enabled)
	assert.Len(t, cfg.Tasks, 2)
	assert.Equal(t, "make build", cfg.Tasks["build"].Run)
	assert.Equal(t, "always", cfg.Output.Color)
}

func TestLoadNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/.rr.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Can't find the config file")
}

func TestFind(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) (string, func())
		explicit string
		wantErr  bool
		wantPath string
	}{
		{
			name: "explicit path exists",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				path := filepath.Join(dir, "custom.yaml")
				err := os.WriteFile(path, []byte("version: 1"), 0644)
				require.NoError(t, err)
				return path, func() {}
			},
			wantErr: false,
		},
		{
			name: "explicit path not found",
			setup: func(t *testing.T) (string, func()) {
				return "/nonexistent/config.yaml", func() {}
			},
			wantErr: true,
		},
		{
			name: "current directory has config",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				path := filepath.Join(dir, ConfigFileName)
				err := os.WriteFile(path, []byte("version: 1"), 0644)
				require.NoError(t, err)

				oldWd, _ := os.Getwd()
				err = os.Chdir(dir)
				require.NoError(t, err)

				return "", func() { os.Chdir(oldWd) }
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explicit, cleanup := tt.setup(t)
			defer cleanup()

			path, err := Find(explicit)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if explicit != "" {
					assert.Equal(t, explicit, path)
				} else {
					assert.NotEmpty(t, path)
				}
			}
		})
	}
}

func TestFind_WalksUpToParent(t *testing.T) {
	// Create project structure:
	// tmpdir/
	//   .rr.yaml
	//   src/
	//     components/
	tmpdir := t.TempDir()
	configPath := filepath.Join(tmpdir, ConfigFileName)
	err := os.WriteFile(configPath, []byte("version: 1"), 0644)
	require.NoError(t, err)

	subdir := filepath.Join(tmpdir, "src", "components")
	err = os.MkdirAll(subdir, 0755)
	require.NoError(t, err)

	// Change to subdirectory (register cleanup before chdir)
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(oldWd)
	err = os.Chdir(subdir)
	require.NoError(t, err)

	// Should find config in parent
	path, err := Find("")
	require.NoError(t, err)
	// Use EvalSymlinks for comparison to handle /var -> /private/var on macOS
	expectedResolved, _ := filepath.EvalSymlinks(configPath)
	actualResolved, _ := filepath.EvalSymlinks(path)
	assert.Equal(t, expectedResolved, actualResolved)
}

func TestFind_FindsConfigAtGitRoot(t *testing.T) {
	// Create project structure:
	// tmpdir/
	//   .git/         <- git root
	//   .rr.yaml      <- config at git root
	//   src/
	//     components/
	tmpdir := t.TempDir()

	// Create .git directory
	gitDir := filepath.Join(tmpdir, ".git")
	err := os.MkdirAll(gitDir, 0755)
	require.NoError(t, err)

	// Create config at git root
	configPath := filepath.Join(tmpdir, ConfigFileName)
	err = os.WriteFile(configPath, []byte("version: 1"), 0644)
	require.NoError(t, err)

	// Create subdirectory
	subdir := filepath.Join(tmpdir, "src", "components")
	err = os.MkdirAll(subdir, 0755)
	require.NoError(t, err)

	// Change to subdirectory (register cleanup before chdir)
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(oldWd)
	err = os.Chdir(subdir)
	require.NoError(t, err)

	// Should find config at git root
	path, err := Find("")
	require.NoError(t, err)
	// Use EvalSymlinks for comparison to handle /var -> /private/var on macOS
	expectedResolved, _ := filepath.EvalSymlinks(configPath)
	actualResolved, _ := filepath.EvalSymlinks(path)
	assert.Equal(t, expectedResolved, actualResolved)
}

func TestFind_StopsAtGitRoot(t *testing.T) {
	// Create project structure:
	// tmpdir/
	//   .rr.yaml      <- config above git root (should NOT be found)
	//   repo/
	//     .git/       <- git root
	//     src/
	tmpdir := t.TempDir()

	// Create config above git root
	err := os.WriteFile(filepath.Join(tmpdir, ConfigFileName), []byte("version: 1"), 0644)
	require.NoError(t, err)

	// Create git repo subdirectory
	repoDir := filepath.Join(tmpdir, "repo")
	gitDir := filepath.Join(repoDir, ".git")
	err = os.MkdirAll(gitDir, 0755)
	require.NoError(t, err)

	// Create subdirectory inside repo
	subdir := filepath.Join(repoDir, "src")
	err = os.MkdirAll(subdir, 0755)
	require.NoError(t, err)

	// Change to subdirectory (register cleanup before chdir)
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer os.Chdir(oldWd)
	err = os.Chdir(subdir)
	require.NoError(t, err)

	// Should NOT find config above git root
	path, err := Find("")
	require.NoError(t, err)
	assert.Empty(t, path)
}

func TestExpand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, result string)
	}{
		{
			name:  "empty string",
			input: "",
			check: func(t *testing.T, result string) {
				assert.Empty(t, result)
			},
		},
		{
			name:  "no variables",
			input: "/home/user/projects/myapp",
			check: func(t *testing.T, result string) {
				assert.Equal(t, "/home/user/projects/myapp", result)
			},
		},
		{
			name:  "USER variable",
			input: "/home/${USER}/projects",
			check: func(t *testing.T, result string) {
				assert.NotContains(t, result, "${USER}")
				assert.Contains(t, result, "/home/")
				assert.Contains(t, result, "/projects")
			},
		},
		{
			name:  "HOME variable",
			input: "${HOME}/projects",
			check: func(t *testing.T, result string) {
				assert.NotContains(t, result, "${HOME}")
				home, _ := os.UserHomeDir()
				if home != "" {
					assert.Contains(t, result, home)
				}
			},
		},
		{
			name:  "PROJECT variable",
			input: "~/dev/${PROJECT}",
			check: func(t *testing.T, result string) {
				assert.NotContains(t, result, "${PROJECT}")
			},
		},
		{
			name:  "multiple variables",
			input: "${HOME}/projects/${PROJECT}",
			check: func(t *testing.T, result string) {
				assert.NotContains(t, result, "${HOME}")
				assert.NotContains(t, result, "${PROJECT}")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Expand(tt.input)
			tt.check(t, result)
		})
	}
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:user/my-repo.git", "my-repo"},
		{"git@github.com:org/project.git", "project"},
		{"https://github.com/user/my-repo.git", "my-repo"},
		{"https://github.com/user/my-repo", "my-repo"},
		{"git@gitlab.com:group/subgroup/repo.git", "repo"}, // Nested paths handled correctly
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractRepoName(tt.url)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		opts    []ValidationOption
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with host reference",
			config: &Config{
				Version: 1,
				Host:    "mini",
			},
			wantErr: false,
		},
		{
			name: "valid config without host reference",
			config: &Config{
				Version: 1,
			},
			wantErr: false,
		},
		{
			name: "version too high",
			config: &Config{
				Version: CurrentConfigVersion + 1,
			},
			wantErr: true,
			errMsg:  "from the future",
		},
		{
			name: "host reference looks like SSH string",
			config: &Config{
				Version: 1,
				Host:    "user@hostname",
			},
			wantErr: true,
			errMsg:  "looks like an SSH string",
		},
		{
			name: "host reference contains path",
			config: &Config{
				Version: 1,
				Host:    "hosts/mini",
			},
			wantErr: true,
			errMsg:  "contains a path separator",
		},
		{
			name: "reserved task name",
			config: &Config{
				Version: 1,
				Tasks: map[string]TaskConfig{
					"init": {Run: "echo hello"},
				},
			},
			wantErr: true,
			errMsg:  "built-in command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.config, tt.opts...)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateGlobal(t *testing.T) {
	tests := []struct {
		name        string
		config      *GlobalConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid global config",
			config: &GlobalConfig{
				Version: 1,
				Hosts: map[string]Host{
					"dev": {SSH: []string{"dev"}, Dir: "/home/user/dev"},
				},
			},
			wantErr: false,
		},
		{
			name:        "nil config",
			config:      nil,
			wantErr:     true,
			errContains: "nil",
		},
		{
			name: "version too high",
			config: &GlobalConfig{
				Version: CurrentGlobalConfigVersion + 1,
				Hosts:   map[string]Host{},
			},
			wantErr:     true,
			errContains: "from the future",
		},
		{
			name: "invalid host config",
			config: &GlobalConfig{
				Version: 1,
				Hosts: map[string]Host{
					"bad": {SSH: []string{}, Dir: "/home"},
				},
			},
			wantErr:     true,
			errContains: "needs at least one SSH",
		},
		{
			name: "empty hosts is allowed for global config",
			config: &GlobalConfig{
				Version: 1,
				Hosts:   map[string]Host{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGlobal(tt.config)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResolved(t *testing.T) {
	tests := []struct {
		name        string
		resolved    *ResolvedConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid resolved config",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Version: 1,
					Hosts: map[string]Host{
						"dev": {SSH: []string{"dev"}, Dir: "/home/dev"},
					},
				},
				Project: &Config{
					Version: 1,
					Host:    "dev",
				},
			},
			wantErr: false,
		},
		{
			name:        "nil resolved config",
			resolved:    nil,
			wantErr:     true,
			errContains: "nil",
		},
		{
			name: "nil global config",
			resolved: &ResolvedConfig{
				Global:  nil,
				Project: &Config{},
			},
			wantErr:     true,
			errContains: "Global config not loaded",
		},
		{
			name: "no hosts configured",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Version: 1,
					Hosts:   map[string]Host{},
				},
				Project: &Config{},
			},
			wantErr:     true,
			errContains: "No hosts configured",
		},
		{
			name: "project references nonexistent host",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Version: 1,
					Hosts: map[string]Host{
						"dev": {SSH: []string{"dev"}, Dir: "/home/dev"},
					},
				},
				Project: &Config{
					Version: 1,
					Host:    "prod",
				},
			},
			wantErr:     true,
			errContains: "doesn't exist in global config",
		},
		{
			name: "project host empty is valid",
			resolved: &ResolvedConfig{
				Global: &GlobalConfig{
					Version: 1,
					Hosts: map[string]Host{
						"dev": {SSH: []string{"dev"}, Dir: "/home/dev"},
					},
				},
				Project: &Config{
					Version: 1,
					Host:    "",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResolved(tt.resolved)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateHost(t *testing.T) {
	tests := []struct {
		name    string
		host    Host
		wantErr bool
	}{
		{
			name:    "valid host with absolute path",
			host:    Host{SSH: []string{"mini-local", "mini"}, Dir: "/home/user/projects/test"},
			wantErr: false,
		},
		{
			name:    "missing ssh",
			host:    Host{Dir: "/home/user/projects/test"},
			wantErr: true,
		},
		{
			name:    "empty ssh entry",
			host:    Host{SSH: []string{"mini", ""}, Dir: "/home/user/projects/test"},
			wantErr: true,
		},
		{
			name:    "missing dir",
			host:    Host{SSH: []string{"mini"}},
			wantErr: true,
		},
		{
			name:    "tilde in remote path (allowed for remote shell expansion)",
			host:    Host{SSH: []string{"mini"}, Dir: "~/projects/test"},
			wantErr: false, // Tilde is allowed - remote shell expands it
		},
		{
			name:    "unexpanded variable in path",
			host:    Host{SSH: []string{"mini"}, Dir: "/home/${USER}/projects"},
			wantErr: true,
		},
		{
			name:    "valid shell format",
			host:    Host{SSH: []string{"mini"}, Dir: "/home/user/project", Shell: "bash -l -c"},
			wantErr: false,
		},
		{
			name:    "invalid shell format without flag",
			host:    Host{SSH: []string{"mini"}, Dir: "/home/user/project", Shell: "bash"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHost("test", tt.host)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTask(t *testing.T) {
	tests := []struct {
		name    string
		task    TaskConfig
		wantErr bool
	}{
		{
			name:    "simple run task",
			task:    TaskConfig{Run: "make build"},
			wantErr: false,
		},
		{
			name: "steps task",
			task: TaskConfig{
				Steps: []TaskStep{
					{Name: "lint", Run: "golangci-lint run"},
					{Name: "test", Run: "go test ./..."},
				},
			},
			wantErr: false,
		},
		{
			name:    "no run or steps",
			task:    TaskConfig{Description: "does nothing"},
			wantErr: true,
		},
		{
			name: "both run and steps",
			task: TaskConfig{
				Run:   "make build",
				Steps: []TaskStep{{Run: "echo hi"}},
			},
			wantErr: true,
		},
		{
			name: "step with invalid on_fail",
			task: TaskConfig{
				Steps: []TaskStep{
					{Name: "test", Run: "go test", OnFail: "ignore"},
				},
			},
			wantErr: true,
		},
		{
			name: "step with valid on_fail continue",
			task: TaskConfig{
				Steps: []TaskStep{
					{Name: "test", Run: "go test", OnFail: "continue"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTask("test", tt.task)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  OutputConfig
		wantErr bool
	}{
		{
			name:    "all defaults",
			output:  OutputConfig{},
			wantErr: false,
		},
		{
			name:    "valid explicit values",
			output:  OutputConfig{Color: "always", Format: "pytest", Verbosity: "verbose"},
			wantErr: false,
		},
		{
			name:    "invalid color",
			output:  OutputConfig{Color: "rainbow"},
			wantErr: true,
		},
		{
			name:    "invalid format",
			output:  OutputConfig{Format: "unknown"},
			wantErr: true,
		},
		{
			name:    "invalid verbosity",
			output:  OutputConfig{Verbosity: "extreme"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOutput(tt.output)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsReservedTaskName(t *testing.T) {
	reserved := []string{"run", "exec", "sync", "init", "setup", "status", "monitor", "doctor", "help", "version", "completion", "update"}
	for _, name := range reserved {
		assert.True(t, IsReservedTaskName(name), "expected %q to be reserved", name)
	}

	notReserved := []string{"build", "test", "deploy", "lint", "my-init", "custom"}
	for _, name := range notReserved {
		assert.False(t, IsReservedTaskName(name), "expected %q to not be reserved", name)
	}
}

func TestValidateLock(t *testing.T) {
	tests := []struct {
		name    string
		lock    LockConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid defaults",
			lock:    LockConfig{Enabled: true, Timeout: 5 * time.Minute, Stale: 10 * time.Minute},
			wantErr: false,
		},
		{
			name:    "disabled lock (no validation needed)",
			lock:    LockConfig{Enabled: false},
			wantErr: false,
		},
		{
			name:    "negative timeout",
			lock:    LockConfig{Enabled: true, Timeout: -1 * time.Second},
			wantErr: true,
			errMsg:  "can't be negative",
		},
		{
			name:    "negative stale",
			lock:    LockConfig{Enabled: true, Stale: -1 * time.Minute},
			wantErr: true,
			errMsg:  "can't be negative",
		},
		{
			name:    "timeout greater than stale",
			lock:    LockConfig{Enabled: true, Timeout: 10 * time.Minute, Stale: 5 * time.Minute},
			wantErr: true,
			errMsg:  "is longer than",
		},
		{
			name:    "zero timeout is allowed",
			lock:    LockConfig{Enabled: true, Timeout: 0, Stale: 5 * time.Minute},
			wantErr: false,
		},
		{
			name:    "zero stale is allowed",
			lock:    LockConfig{Enabled: true, Timeout: 5 * time.Minute, Stale: 0},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLock(tt.lock)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateMonitor(t *testing.T) {
	validHost := map[string]Host{
		"mini": {SSH: []string{"mini"}, Dir: "~/test"},
	}

	tests := []struct {
		name    string
		monitor MonitorConfig
		hosts   map[string]Host
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid defaults",
			monitor: MonitorConfig{Interval: "2s"},
			hosts:   validHost,
			wantErr: false,
		},
		{
			name:    "empty interval (uses default)",
			monitor: MonitorConfig{Interval: ""},
			hosts:   validHost,
			wantErr: false,
		},
		{
			name:    "invalid interval format",
			monitor: MonitorConfig{Interval: "abc"},
			hosts:   validHost,
			wantErr: true,
			errMsg:  "doesn't look like a valid duration",
		},
		{
			name:    "valid interval with minutes",
			monitor: MonitorConfig{Interval: "1m30s"},
			hosts:   validHost,
			wantErr: false,
		},
		{
			name: "exclude with empty entry",
			monitor: MonitorConfig{
				Interval: "2s",
				Exclude:  []string{""},
			},
			hosts:   validHost,
			wantErr: true,
			errMsg:  "empty entry",
		},
		{
			name: "exclude with valid non-existent host (warning only)",
			monitor: MonitorConfig{
				Interval: "2s",
				Exclude:  []string{"nonexistent"},
			},
			hosts:   validHost,
			wantErr: false, // non-existent host is allowed (just a warning)
		},
		{
			name: "invalid cpu threshold warning",
			monitor: MonitorConfig{
				Interval: "2s",
				Thresholds: ThresholdConfig{
					CPU: ThresholdValues{Warning: 150, Critical: 90},
				},
			},
			hosts:   validHost,
			wantErr: true,
			errMsg:  "needs to be 0-100",
		},
		{
			name: "warning greater than critical",
			monitor: MonitorConfig{
				Interval: "2s",
				Thresholds: ThresholdConfig{
					CPU: ThresholdValues{Warning: 95, Critical: 90},
				},
			},
			hosts:   validHost,
			wantErr: true,
			errMsg:  "is higher than critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMonitor(tt.monitor, tt.hosts)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateThresholds(t *testing.T) {
	tests := []struct {
		name    string
		metric  string
		thresh  ThresholdValues
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid thresholds",
			metric:  "cpu",
			thresh:  ThresholdValues{Warning: 70, Critical: 90},
			wantErr: false,
		},
		{
			name:    "zero values (use defaults)",
			metric:  "ram",
			thresh:  ThresholdValues{Warning: 0, Critical: 0},
			wantErr: false,
		},
		{
			name:    "negative warning",
			metric:  "gpu",
			thresh:  ThresholdValues{Warning: -10, Critical: 90},
			wantErr: true,
			errMsg:  "needs to be 0-100",
		},
		{
			name:    "warning over 100",
			metric:  "cpu",
			thresh:  ThresholdValues{Warning: 110, Critical: 120},
			wantErr: true,
			errMsg:  "needs to be 0-100",
		},
		{
			name:    "negative critical",
			metric:  "ram",
			thresh:  ThresholdValues{Warning: 50, Critical: -5},
			wantErr: true,
			errMsg:  "needs to be 0-100",
		},
		{
			name:    "critical over 100",
			metric:  "gpu",
			thresh:  ThresholdValues{Warning: 70, Critical: 150},
			wantErr: true,
			errMsg:  "needs to be 0-100",
		},
		{
			name:    "warning equals critical",
			metric:  "cpu",
			thresh:  ThresholdValues{Warning: 80, Critical: 80},
			wantErr: true,
			errMsg:  "is higher than critical",
		},
		{
			name:    "warning greater than critical",
			metric:  "ram",
			thresh:  ThresholdValues{Warning: 90, Critical: 80},
			wantErr: true,
			errMsg:  "is higher than critical",
		},
		{
			name:    "only warning set (zero critical is allowed)",
			metric:  "gpu",
			thresh:  ThresholdValues{Warning: 70, Critical: 0},
			wantErr: false,
		},
		{
			name:    "only critical set (zero warning is allowed)",
			metric:  "cpu",
			thresh:  ThresholdValues{Warning: 0, Critical: 90},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateThresholds(tt.metric, tt.thresh)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoadOrDefault(t *testing.T) {
	// Change to a directory without config
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	err := os.Chdir(dir)
	require.NoError(t, err)
	defer os.Chdir(oldWd)

	cfg, err := LoadOrDefault()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, CurrentConfigVersion, cfg.Version)
	assert.Empty(t, cfg.Host)
}

func TestDependencyItemUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []DependencyItem
	}{
		{
			name: "simple string dependencies",
			yaml: `
tasks:
  ci:
    depends: [lint, test]
    run: echo done
`,
			expected: []DependencyItem{
				{Task: "lint"},
				{Task: "test"},
			},
		},
		{
			name: "parallel group dependency",
			yaml: `
tasks:
  ci:
    depends:
      - parallel: [lint, typecheck]
    run: echo done
`,
			expected: []DependencyItem{
				{Parallel: []string{"lint", "typecheck"}},
			},
		},
		{
			name: "mixed dependencies",
			yaml: `
tasks:
  ci:
    depends:
      - parallel: [lint, typecheck]
      - test
    run: echo done
`,
			expected: []DependencyItem{
				{Parallel: []string{"lint", "typecheck"}},
				{Task: "test"},
			},
		},
		{
			name: "no dependencies",
			yaml: `
tasks:
  build:
    run: make build
`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, ".rr.yaml")
			content := "version: 1\n" + tt.yaml
			err := os.WriteFile(configPath, []byte(content), 0644)
			require.NoError(t, err)

			cfg, err := Load(configPath)
			require.NoError(t, err)

			// Find the task with depends field
			var task TaskConfig
			for name, t := range cfg.Tasks {
				if name == "ci" || len(t.Depends) > 0 {
					task = t
					break
				}
			}

			if tt.expected == nil {
				assert.Empty(t, task.Depends)
			} else {
				assert.Equal(t, len(tt.expected), len(task.Depends))
				for i, exp := range tt.expected {
					assert.Equal(t, exp.Task, task.Depends[i].Task)
					assert.Equal(t, exp.Parallel, task.Depends[i].Parallel)
				}
			}
		})
	}
}

func TestDependencyItemMethods(t *testing.T) {
	t.Run("IsParallel", func(t *testing.T) {
		simple := DependencyItem{Task: "lint"}
		assert.False(t, simple.IsParallel())

		parallel := DependencyItem{Parallel: []string{"lint", "test"}}
		assert.True(t, parallel.IsParallel())
	})

	t.Run("TaskNames", func(t *testing.T) {
		simple := DependencyItem{Task: "lint"}
		assert.Equal(t, []string{"lint"}, simple.TaskNames())

		parallel := DependencyItem{Parallel: []string{"lint", "test"}}
		assert.Equal(t, []string{"lint", "test"}, parallel.TaskNames())
	})
}

func TestHasDependencies(t *testing.T) {
	t.Run("task with dependencies", func(t *testing.T) {
		task := &TaskConfig{
			Depends: []DependencyItem{{Task: "lint"}},
			Run:     "make test",
		}
		assert.True(t, HasDependencies(task))
	})

	t.Run("task without dependencies", func(t *testing.T) {
		task := &TaskConfig{
			Run: "make test",
		}
		assert.False(t, HasDependencies(task))
	})
}

func TestParallelTaskSetup(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".rr.yaml")

	content := `
version: 1
tasks:
  test-all:
    setup: uv sync --quiet
    parallel:
      - test-1
      - test-2
      - test-3
  test-1:
    run: pytest --test-group 1
  test-2:
    run: pytest --test-group 2
  test-3:
    run: pytest --test-group 3
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	// Check the parallel task has setup
	testAll := cfg.Tasks["test-all"]
	assert.Equal(t, "uv sync --quiet", testAll.Setup)
	assert.True(t, IsParallelTask(&testAll))
	assert.Equal(t, []string{"test-1", "test-2", "test-3"}, testAll.Parallel)

	// Check subtasks don't have setup (it's only on the parent)
	test1 := cfg.Tasks["test-1"]
	assert.Empty(t, test1.Setup)
	assert.Equal(t, "pytest --test-group 1", test1.Run)
}

func TestPullItemUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []PullItem
	}{
		{
			name: "simple string patterns",
			yaml: `
tasks:
  test:
    run: pytest --cov
    pull:
      - coverage.xml
      - htmlcov/
`,
			expected: []PullItem{
				{Src: "coverage.xml"},
				{Src: "htmlcov/"},
			},
		},
		{
			name: "object with src and dest",
			yaml: `
tasks:
  build:
    run: make release
    pull:
      - src: dist/*.whl
        dest: ./artifacts/
      - src: dist/*.tar.gz
        dest: ./artifacts/
`,
			expected: []PullItem{
				{Src: "dist/*.whl", Dest: "./artifacts/"},
				{Src: "dist/*.tar.gz", Dest: "./artifacts/"},
			},
		},
		{
			name: "mixed string and object",
			yaml: `
tasks:
  coverage:
    run: pytest --cov
    pull:
      - coverage.xml
      - src: htmlcov/
        dest: ./reports/coverage/
`,
			expected: []PullItem{
				{Src: "coverage.xml"},
				{Src: "htmlcov/", Dest: "./reports/coverage/"},
			},
		},
		{
			name: "no pull config",
			yaml: `
tasks:
  build:
    run: make build
`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, ".rr.yaml")
			content := "version: 1\n" + tt.yaml
			err := os.WriteFile(configPath, []byte(content), 0644)
			require.NoError(t, err)

			cfg, err := Load(configPath)
			require.NoError(t, err)

			// Find the task with pull field
			var task TaskConfig
			for name, t := range cfg.Tasks {
				if name == "test" || name == "build" || name == "coverage" {
					task = t
					break
				}
			}

			if tt.expected == nil {
				assert.Empty(t, task.Pull)
			} else {
				require.Equal(t, len(tt.expected), len(task.Pull))
				for i, exp := range tt.expected {
					assert.Equal(t, exp.Src, task.Pull[i].Src)
					assert.Equal(t, exp.Dest, task.Pull[i].Dest)
				}
			}
		})
	}
}

func TestPullItemFromInterface(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected PullItem
		wantErr  bool
	}{
		{
			name:     "string input",
			input:    "coverage.xml",
			expected: PullItem{Src: "coverage.xml"},
			wantErr:  false,
		},
		{
			name:     "map with src only",
			input:    map[string]interface{}{"src": "dist/*.whl"},
			expected: PullItem{Src: "dist/*.whl"},
			wantErr:  false,
		},
		{
			name:     "map with src and dest",
			input:    map[string]interface{}{"src": "dist/*.whl", "dest": "./artifacts/"},
			expected: PullItem{Src: "dist/*.whl", Dest: "./artifacts/"},
			wantErr:  false,
		},
		{
			name:    "map missing src",
			input:   map[string]interface{}{"dest": "./artifacts/"},
			wantErr: true,
		},
		{
			name:    "invalid type",
			input:   123,
			wantErr: true,
		},
		{
			name:    "src not a string",
			input:   map[string]interface{}{"src": 123},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := PullItemFromInterface(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.Src, result.Src)
				assert.Equal(t, tt.expected.Dest, result.Dest)
			}
		})
	}
}
