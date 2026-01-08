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
	assert.NotNil(t, cfg.Hosts)
	assert.Empty(t, cfg.Hosts)
	assert.False(t, cfg.LocalFallback)
	assert.Equal(t, 2*time.Second, cfg.ProbeTimeout)
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

func TestLoad(t *testing.T) {
	// Create a temp config file
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".rr.yaml")

	content := `
version: 1
hosts:
  mini:
    ssh:
      - mini-local
      - mini
    dir: ~/projects/test
    tags: [macos, arm64]
default: mini
probe_timeout: 5s
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
	assert.Len(t, cfg.Hosts, 1)
	assert.Contains(t, cfg.Hosts, "mini")
	assert.Equal(t, []string{"mini-local", "mini"}, cfg.Hosts["mini"].SSH)
	assert.Equal(t, "mini", cfg.Default)
	assert.Equal(t, 5*time.Second, cfg.ProbeTimeout)
	assert.True(t, cfg.Lock.Enabled)
	assert.Len(t, cfg.Tasks, 2)
	assert.Equal(t, "make build", cfg.Tasks["build"].Run)
	assert.Equal(t, "always", cfg.Output.Color)
}

func TestLoadNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/.rr.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Config file not found")
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
			name: "valid config",
			config: &Config{
				Version: 1,
				Hosts: map[string]Host{
					"mini": {SSH: []string{"mini"}, Dir: "~/projects/test"},
				},
				Default: "mini",
			},
			wantErr: false,
		},
		{
			name: "no hosts without option",
			config: &Config{
				Version: 1,
				Hosts:   map[string]Host{},
			},
			wantErr: true,
			errMsg:  "No hosts defined",
		},
		{
			name: "no hosts with AllowNoHosts option",
			config: &Config{
				Version: 1,
				Hosts:   map[string]Host{},
			},
			opts:    []ValidationOption{AllowNoHosts()},
			wantErr: false,
		},
		{
			name: "version too high",
			config: &Config{
				Version: CurrentConfigVersion + 1,
				Hosts: map[string]Host{
					"mini": {SSH: []string{"mini"}, Dir: "~/test"},
				},
			},
			wantErr: true,
			errMsg:  "newer than supported",
		},
		{
			name: "default host not found",
			config: &Config{
				Version: 1,
				Hosts: map[string]Host{
					"mini": {SSH: []string{"mini"}, Dir: "~/test"},
				},
				Default: "nonexistent",
			},
			wantErr: true,
			errMsg:  "not found",
		},
		{
			name: "reserved task name",
			config: &Config{
				Version: 1,
				Hosts: map[string]Host{
					"mini": {SSH: []string{"mini"}, Dir: "~/test"},
				},
				Tasks: map[string]TaskConfig{
					"init": {Run: "echo hello"},
				},
			},
			wantErr: true,
			errMsg:  "reserved command name",
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

func TestValidateHost(t *testing.T) {
	tests := []struct {
		name    string
		host    Host
		wantErr bool
	}{
		{
			name:    "valid host",
			host:    Host{SSH: []string{"mini-local", "mini"}, Dir: "~/projects/test"},
			wantErr: false,
		},
		{
			name:    "missing ssh",
			host:    Host{Dir: "~/projects/test"},
			wantErr: true,
		},
		{
			name:    "empty ssh entry",
			host:    Host{SSH: []string{"mini", ""}, Dir: "~/projects/test"},
			wantErr: true,
		},
		{
			name:    "missing dir",
			host:    Host{SSH: []string{"mini"}},
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
	assert.Empty(t, cfg.Hosts)
}
