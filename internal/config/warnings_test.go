package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeProjectConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".rr.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func writeGlobalConfig(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, GlobalConfigDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, GlobalConfigFile)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func warningKeys(ws []Warning) []string {
	keys := make([]string, 0, len(ws))
	for _, w := range ws {
		keys = append(keys, w.Key)
	}
	return keys
}

func TestLoad_Warnings(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantKeys []string
		contains string // substring expected in the (single) warning message
	}{
		{
			name:     "valid config has no warnings",
			content:  "version: 1\nhost: dev\ntasks:\n  test:\n    run: go test ./...\n    env:\n      FOO: bar\n",
			wantKeys: []string{},
		},
		{
			name:     "unknown top-level key",
			content:  "version: 1\ntaks:\n  test:\n    run: echo\n",
			wantKeys: []string{"taks"},
			contains: "Unknown config key",
		},
		{
			name:     "unknown nested key",
			content:  "version: 1\nlock:\n  stail: 5m\n",
			wantKeys: []string{"lock.stail"},
		},
		{
			name:     "unknown task key",
			content:  "version: 1\ntasks:\n  test:\n    run: echo\n    descripton: typo\n", //nolint:misspell // intentional typo under test
			wantKeys: []string{"tasks.test.descripton"},
		},
		{
			name:     "unknown step key",
			content:  "version: 1\ntasks:\n  test:\n    steps:\n      - name: a\n        run: echo\n        on_fial: continue\n",
			wantKeys: []string{"tasks.test.steps.0.on_fial"},
		},
		{
			name:     "deprecated output section",
			content:  "version: 1\noutput:\n  color: always\n  format: go\n",
			wantKeys: []string{"output"},
			contains: "no effect",
		},
		{
			name:     "pull on a parallel task",
			content:  "version: 1\ntasks:\n  a:\n    run: echo a\n  all:\n    parallel: [a]\n    pull: [coverage.xml]\n",
			wantKeys: []string{"tasks.all.pull"},
			contains: "parallel",
		},
		{
			name:     "output on a non-parallel task",
			content:  "version: 1\ntasks:\n  a:\n    run: echo a\n    output: quiet\n",
			wantKeys: []string{"tasks.a.output"},
			contains: "parallel",
		},
		{
			name:     "output on a parallel task and pull on a subtask are fine",
			content:  "version: 1\ntasks:\n  a:\n    run: echo a\n    pull: [out.xml]\n  all:\n    parallel: [a]\n    output: quiet\n",
			wantKeys: []string{},
		},
		{
			name:     "depends and pull object forms decode without warnings",
			content:  "version: 1\ntasks:\n  a:\n    run: echo a\n  b:\n    run: echo b\n    depends:\n      - a\n      - parallel: [a]\n    pull:\n      - src: dist/*.whl\n        dest: ./artifacts/\n      - coverage.xml\n",
			wantKeys: []string{},
		},
		{
			name:     "local_fallback boolean and mode decode without warnings",
			content:  "version: 1\nlocal_fallback: true\nrewrite_paths: false\nrequire: [go]\ndefaults:\n  setup: [echo hi]\n  env:\n    A: b\n",
			wantKeys: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeProjectConfig(t, tt.content))
			require.NoError(t, err, "warnings must never fail the load")
			assert.ElementsMatch(t, tt.wantKeys, warningKeys(cfg.Warnings))
			for _, w := range cfg.Warnings {
				assert.NotEmpty(t, w.File)
				assert.NotEmpty(t, w.Message)
				assert.NotEmpty(t, w.Suggestion, "warnings need an actionable suggestion")
			}
			if tt.contains != "" {
				require.Len(t, cfg.Warnings, 1)
				assert.Contains(t, cfg.Warnings[0].Message, tt.contains)
			}
		})
	}
}

func TestLoad_OutputSectionIsNotDecoded(t *testing.T) {
	cfg, err := Load(writeProjectConfig(t, "version: 1\noutput:\n  color: rainbow\n"))
	require.NoError(t, err, "a leftover output: section must still load")
	require.NoError(t, Validate(cfg), "output: values are no longer validated")
	require.Len(t, cfg.Warnings, 1)
	assert.Equal(t, "output", cfg.Warnings[0].Key)
}

func TestLoadGlobal_Warnings(t *testing.T) {
	writeGlobalConfig(t, `version: 1
hosts:
  dev:
    ssh: [dev.local]
    dir: ~/projects/${PROJECT}
    sshh: [typo]
    env:
      PATH_EXTRA: /opt/bin
defaults:
  probe_timeout: 3s
  local_fallback: on-unreachable
logs:
  keep_runs: 5
`)
	cfg, err := LoadGlobal()
	require.NoError(t, err)
	assert.Equal(t, []string{"hosts.dev.sshh"}, warningKeys(cfg.Warnings))
}

func TestLoadGlobal_RemovedDefaultHost(t *testing.T) {
	path := writeGlobalConfig(t, "version: 1\ndefaults:\n  host: dev\nhosts:\n  dev:\n    ssh: [dev]\n    dir: /tmp/x\n")
	cfg, err := LoadGlobal()
	require.NoError(t, err)
	require.Len(t, cfg.Warnings, 1)
	w := cfg.Warnings[0]
	assert.Equal(t, "defaults.host", w.Key)
	assert.Equal(t, path, w.File)
	assert.Contains(t, w.Message, "no longer used")
	assert.NotEmpty(t, w.Suggestion)
}

// TestExampleConfigs_NoWarnings guards against false positives: every
// shipped example and the repo's own .rr.yaml must load without warnings.
func TestExampleConfigs_NoWarnings(t *testing.T) {
	examples, err := filepath.Glob(filepath.Join("..", "..", "docs", "examples", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, examples)

	for _, path := range append(examples, filepath.Join("..", "..", ".rr.yaml")) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if filepath.Base(path) == "global-config.yaml" {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				writeGlobalConfig(t, string(data))
				cfg, err := LoadGlobal()
				require.NoError(t, err)
				assert.Empty(t, cfg.Warnings)
				return
			}
			cfg, err := Load(path)
			require.NoError(t, err)
			assert.Empty(t, cfg.Warnings)
		})
	}
}

// TestLockDefaults_Parity keeps DefaultConfig() and the viper defaults used
// by Load in agreement for every lock setting.
func TestLockDefaults_Parity(t *testing.T) {
	cfg, err := Load(writeProjectConfig(t, "version: 1\n"))
	require.NoError(t, err)
	assert.Equal(t, DefaultConfig().Lock, cfg.Lock)
}

func TestValidate_TaskOutputMode(t *testing.T) {
	tests := []struct {
		name    string
		tasks   map[string]TaskConfig
		wantErr string
	}{
		{
			name: "valid modes on parallel tasks",
			tasks: map[string]TaskConfig{
				"a":  {Run: "echo a"},
				"p1": {Parallel: []string{"a"}, Output: TaskOutputProgress},
				"p2": {Parallel: []string{"a"}, Output: TaskOutputStream},
				"p3": {Parallel: []string{"a"}, Output: TaskOutputVerbose},
				"p4": {Parallel: []string{"a"}, Output: TaskOutputQuiet},
			},
		},
		{
			name: "invalid mode on parallel task",
			tasks: map[string]TaskConfig{
				"a": {Run: "echo a"},
				"p": {Parallel: []string{"a"}, Output: "loud"},
			},
			wantErr: "output 'loud'",
		},
		{
			name: "invalid mode on regular task",
			tasks: map[string]TaskConfig{
				"a": {Run: "echo a", Output: "streamm"},
			},
			wantErr: "output 'streamm'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&Config{Version: 1, Tasks: tt.tasks})
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidate_ReservedTaskNameNamesTask(t *testing.T) {
	for _, name := range []string{"pull", "logs", "provision"} {
		t.Run(name, func(t *testing.T) {
			err := Validate(&Config{Version: 1, Tasks: map[string]TaskConfig{name: {Run: "echo"}}})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "'"+name+"'")
			assert.Contains(t, err.Error(), "Rename task")
		})
	}
}

func TestValidateResolved_AllowLocalTarget(t *testing.T) {
	noHosts := &GlobalConfig{Version: 1, Hosts: map[string]Host{}}
	oneHost := &GlobalConfig{Version: 1, Hosts: map[string]Host{
		"dev": {SSH: []string{"dev"}, Dir: "/home/dev"},
	}}

	tests := []struct {
		name     string
		resolved *ResolvedConfig
		opts     []ValidationOption
		wantErr  string
	}{
		{
			name:     "zero hosts without option still errors",
			resolved: &ResolvedConfig{Global: noHosts, Project: &Config{Version: 1}},
			wantErr:  "No hosts configured",
		},
		{
			name:     "zero hosts allowed for local target",
			resolved: &ResolvedConfig{Global: noHosts, Project: &Config{Version: 1}},
			opts:     []ValidationOption{AllowLocalTarget()},
		},
		{
			name:     "local target skips host references that are not used",
			resolved: &ResolvedConfig{Global: noHosts, Project: &Config{Version: 1, Host: "dev", Hosts: []string{"gpu"}}},
			opts:     []ValidationOption{AllowLocalTarget()},
		},
		{
			name:     "local target still validates the project",
			resolved: &ResolvedConfig{Global: noHosts, Project: &Config{Version: 99}},
			opts:     []ValidationOption{AllowLocalTarget()},
			wantErr:  "from the future",
		},
		{
			name: "local target still validates global hosts",
			resolved: &ResolvedConfig{Global: &GlobalConfig{Version: 1, Hosts: map[string]Host{
				"bad": {Dir: "/x"},
			}}, Project: &Config{Version: 1}},
			opts:    []ValidationOption{AllowLocalTarget()},
			wantErr: "needs at least one SSH connection",
		},
		{
			name:     "remote target with hosts is unchanged",
			resolved: &ResolvedConfig{Global: oneHost, Project: &Config{Version: 1, Host: "dev"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResolved(tt.resolved, tt.opts...)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestProjectLocalMode(t *testing.T) {
	always := LocalFallbackAlways
	never := LocalFallbackNever
	tests := []struct {
		name    string
		project *Config
		want    bool
	}{
		{"nil project", nil, false},
		{"fallback unset", &Config{}, false},
		{"fallback never", &Config{LocalFallback: &never}, false},
		{"fallback on, no hosts", &Config{LocalFallback: &always}, true},
		{"fallback on, host listed", &Config{LocalFallback: &always, Host: "dev"}, false},
		{"fallback on, hosts listed", &Config{LocalFallback: &always, Hosts: []string{"dev"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ResolvedConfig{Global: DefaultGlobalConfig(), Project: tt.project}
			assert.Equal(t, tt.want, ProjectLocalMode(r))
		})
	}
}
