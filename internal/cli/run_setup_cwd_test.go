package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// defaults.setup runs at the project root, as the config docs say, even when
// rr run is called from a subdirectory: a setup command like
// `. ./scripts/env.sh` names a path relative to the root. Only the command
// itself runs in the caller's subdirectory.
func TestRun_DefaultsSetupRunsAtProjectRootFromSubdir(t *testing.T) {
	projectDir, _ := writeLocalHostConfigs(t)
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "env.sh"), []byte("export FROM_DEFAULTS=defaults-ok\n"), 0o644))
	cfgPath := filepath.Join(projectDir, ".rr.yaml")
	cfg, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	cfg = append(cfg, []byte("defaults:\n  setup:\n    - . ./env.sh\n")...)
	require.NoError(t, os.WriteFile(cfgPath, cfg, 0o644))
	t.Chdir(filepath.Join(projectDir, "sub"))

	var exitCode int
	var events string
	captureStdout(t, func() {
		events = captureStderr(t, func() {
			exitCode, err = Run(RunOptions{
				Command: `echo "$FROM_DEFAULTS" > run.out && pwd -P > pwd.out`,
			})
		})
	})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, events)

	got, err := os.ReadFile(filepath.Join(projectDir, "sub", "run.out"))
	require.NoError(t, err)
	assert.Equal(t, "defaults-ok\n", string(got), "setup ran, and its env reached the command")
	pwd, err := os.ReadFile(filepath.Join(projectDir, "sub", "pwd.out"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(projectDir, "sub")+"\n", string(pwd), "the command still runs in the caller's subdirectory")
}

// A setup command that sources a missing file fails the run with exit 127.
// The result says which file and that setup named it, rather than nothing
// (structured) or a wrong "tool not found" story (pretty).
func TestRun_MissingSetupFileGetsAHint(t *testing.T) {
	projectDir, _ := writeLocalHostConfigs(t)
	cfgPath := filepath.Join(projectDir, ".rr.yaml")
	cfg, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	cfg = append(cfg, []byte("defaults:\n  setup:\n    - . ./scripts/missing-env.sh\n")...)
	require.NoError(t, os.WriteFile(cfgPath, cfg, 0o644))
	t.Chdir(projectDir)

	var exitCode int
	var events string
	captureStdout(t, func() {
		events = captureStderr(t, func() {
			exitCode, err = Run(RunOptions{Command: "true"})
		})
	})
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, events, `"hint":"`)
	assert.Contains(t, events, "./scripts/missing-env.sh")
	assert.Contains(t, events, "setup command")
}
