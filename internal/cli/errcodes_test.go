package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inEmptyProjectDir moves the test into a fresh directory with no .rr.yaml
// anywhere above it (the temp dir gets its own .git so the upward search
// stops there) and clears any --config value.
func inEmptyProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	t.Chdir(dir)
	prevCfg := cfgFile
	cfgFile = ""
	t.Cleanup(func() { cfgFile = prevCfg })
	return dir
}

// TestErrorCodes_CallSites checks the public error code produced by the CLI
// call sites that used to be classified by sniffing message text.
func TestErrorCodes_CallSites(t *testing.T) {
	tests := []struct {
		name     string
		run      func(t *testing.T) error
		wantCode string
	}{
		{
			name: "rr run --host typo",
			run: func(t *testing.T) error {
				withGlobalHosts(t, "dev")
				inEmptyProjectDir(t)
				wf, err := SetupWorkflow(WorkflowOptions{Host: "typo", Quiet: true, SkipSync: true, SkipLock: true})
				if wf != nil {
					wf.Close()
				}
				return err
			},
			wantCode: ErrCodeHostNotFound,
		},
		{
			name: "rr unlock --host typo",
			run: func(t *testing.T) error {
				withGlobalHosts(t, "dev")
				inEmptyProjectDir(t)
				return unlockCommand(UnlockOptions{Host: "typo"})
			},
			wantCode: ErrCodeHostNotFound,
		},
		{
			name: "rr host remove typo",
			run: func(t *testing.T) error {
				withGlobalHosts(t, "dev")
				return hostRemove("typo")
			},
			wantCode: ErrCodeHostNotFound,
		},
		{
			name: "rr provision --host typo",
			run: func(t *testing.T) error {
				_, _, err := getHostsToProvision(&config.ResolvedConfig{
					Global: &config.GlobalConfig{Hosts: map[string]config.Host{"dev": {SSH: []string{"dev"}}}},
				}, "typo")
				return err
			},
			wantCode: ErrCodeHostNotFound,
		},
		{
			name: "rr monitor --hosts typo",
			run: func(t *testing.T) error {
				withGlobalHosts(t, "dev")
				inEmptyProjectDir(t)
				_, err := resolveMonitorScope("typo")
				return err
			},
			wantCode: ErrCodeHostNotFound,
		},
		{
			name: "task lookup with missing .rr.yaml",
			run: func(t *testing.T) error {
				withGlobalHosts(t)
				inEmptyProjectDir(t)
				prevRegistered, prevState := tasksRegistered, discoveryState
				t.Cleanup(func() { tasksRegistered, discoveryState = prevRegistered, prevState })
				tasksRegistered = false
				registerTasksFromConfig("")
				require.NotNil(t, discoveryState)
				return discoveryState.ProjectErr
			},
			wantCode: ErrCodeConfigNotFound,
		},
		{
			name: "--config pointing at a missing file",
			run: func(t *testing.T) error {
				withGlobalHosts(t, "dev")
				inEmptyProjectDir(t)
				cfgFile = filepath.Join(t.TempDir(), "missing.yaml")
				_, err := config.LoadResolved(Config())
				return err
			},
			wantCode: ErrCodeConfigNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			require.Error(t, err)
			jsonErr := ErrorToJSON(err)
			require.NotNil(t, jsonErr)
			assert.Equal(t, tt.wantCode, jsonErr.Code, "error: %v", err)
		})
	}
}

// TestListTasks_ErrorEnvelopes checks that `rr tasks` failures exit non-zero
// with the error envelope on stderr and nothing on stdout.
func TestListTasks_ErrorEnvelopes(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, dir string)
		wantCode string
	}{
		{
			name:     "no .rr.yaml",
			setup:    func(t *testing.T, dir string) {},
			wantCode: ErrCodeConfigNotFound,
		},
		{
			name: "--config pointing at a missing file",
			setup: func(t *testing.T, dir string) {
				cfgFile = filepath.Join(dir, "missing.yaml")
			},
			wantCode: ErrCodeConfigNotFound,
		},
		{
			name: "unparseable .rr.yaml",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".rr.yaml"), []byte("tasks: [\n"), 0o644))
			},
			wantCode: ErrCodeConfigInvalid,
		},
		{
			name: "invalid .rr.yaml (reserved task name)",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".rr.yaml"),
					[]byte("version: 1\ntasks:\n  run:\n    run: echo hi\n"), 0o644))
			},
			wantCode: ErrCodeConfigInvalid,
		},
	}

	for _, tt := range tests {
		for _, jsonFlag := range []bool{false, true} {
			name := tt.name
			if jsonFlag {
				name += " --json"
			}
			t.Run(name, func(t *testing.T) {
				withGlobalHosts(t)
				dir := inEmptyProjectDir(t)
				prevJSON, prevPretty := tasksJSON, prettyMode
				t.Cleanup(func() { tasksJSON, prettyMode = prevJSON, prevPretty })
				tasksJSON, prettyMode = jsonFlag, false
				tt.setup(t, dir)

				var listErr error
				var stderr string
				stdout := captureStdout(t, func() {
					stderr = captureStderr(t, func() { listErr = ListTasks() })
				})

				require.Error(t, listErr)
				assert.Empty(t, stdout, "errors must not go to stdout")
				var env JSONEnvelope
				require.NoError(t, json.Unmarshal([]byte(stderr), &env), "stderr: %s", stderr)
				assert.False(t, env.Success)
				require.NotNil(t, env.Error)
				assert.Equal(t, tt.wantCode, env.Error.Code)
			})
		}
	}
}

// TestListTasks_NoHostsStillLists checks that a fresh clone with tasks but no
// hosts configured still lists its tasks: `rr tasks` validates the project
// config, not the resolved host setup.
func TestListTasks_NoHostsStillLists(t *testing.T) {
	withGlobalHosts(t)
	dir := inEmptyProjectDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".rr.yaml"),
		[]byte("version: 1\ntasks:\n  hi:\n    run: echo hi\n"), 0o644))
	prevJSON, prevPretty := tasksJSON, prettyMode
	t.Cleanup(func() { tasksJSON, prettyMode = prevJSON, prevPretty })
	tasksJSON, prettyMode = false, false

	var listErr error
	stdout := captureStdout(t, func() { listErr = ListTasks() })

	require.NoError(t, listErr)
	var env JSONEnvelope
	require.NoError(t, json.Unmarshal([]byte(stdout), &env))
	assert.True(t, env.Success)
	assert.Contains(t, stdout, `"name": "hi"`)
}

// TestMissingRequirementsError checks the missing-tools error: it maps to
// DEPENDENCY_MISSING and only suggests --skip-requirements where the flag
// exists (rr run / rr exec, not named tasks).
func TestMissingRequirementsError(t *testing.T) {
	tests := []struct {
		name         string
		taskName     string
		wantSkipFlag bool
	}{
		{name: "rr run / rr exec", taskName: "", wantSkipFlag: true},
		{name: "named task", taskName: "test", wantSkipFlag: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := missingRequirementsError("go, node", tt.taskName)
			jsonErr := ErrorToJSON(err)
			require.NotNil(t, jsonErr)
			assert.Equal(t, ErrCodeDependencyMissing, jsonErr.Code)
			assert.Contains(t, jsonErr.Message, "go, node")
			assert.Contains(t, jsonErr.Suggestion, "rr provision")
			if tt.wantSkipFlag {
				assert.Contains(t, jsonErr.Suggestion, "--skip-requirements")
			} else {
				assert.NotContains(t, jsonErr.Suggestion, "--skip-requirements")
			}
		})
	}
}
