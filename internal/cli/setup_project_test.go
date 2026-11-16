package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupProjectEnv makes a temp home with the given global config (none when
// empty) and a git checkout inside it holding files, and chdirs into it.
func setupProjectEnv(t *testing.T, globalConfig string, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if globalConfig != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(globalConfig), 0o644))
	}
	project := filepath.Join(home, "proj")
	require.NoError(t, os.MkdirAll(filepath.Join(project, ".git"), 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(project, name), []byte(content), 0o644))
	}
	t.Chdir(project)
	return project
}

func TestProjectSetup_NoHosts_WritesConfigFromStack(t *testing.T) {
	withStructuredOutput(t)
	project := setupProjectEnv(t, "", map[string]string{"go.mod": "module x\n"})

	stdout := captureStdout(t, func() { require.NoError(t, ProjectSetup()) })

	var env struct {
		Success bool               `json:"success"`
		Data    ProjectSetupOutput `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &env), stdout)
	require.True(t, env.Success)
	out := env.Data

	configPath := filepath.Join(project, config.ConfigFileName)
	assert.Equal(t, []string{configPath}, out.Created)
	assert.Empty(t, out.Hosts)
	assert.Contains(t, out.NextSteps[0], "Ask the user")
	assert.Contains(t, out.Guide, "rr host add --local")

	// The generated file is a valid config carrying the detected stack.
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	require.NoError(t, config.Validate(cfg))
	assert.Equal(t, []string{"go"}, cfg.Require)
	assert.Equal(t, "go test ./...", cfg.Tasks["test"].Run)
	assert.Equal(t, "go build ./...", cfg.Tasks["build"].Run)
}

func TestProjectSetup_ExistingConfigLeftAlone(t *testing.T) {
	const existing = "version: 1\n"
	project := setupProjectEnv(t,
		"version: 1\nhosts:\n  box:\n    ssh: [box.invalid]\n    dir: ~/rr/${PROJECT}\n",
		map[string]string{config.ConfigFileName: existing, "Makefile": "test:\n\tgo test ./...\n"})

	out, err := projectSetup()
	require.NoError(t, err)

	assert.Empty(t, out.Created)
	assert.Equal(t, filepath.Join(project, config.ConfigFileName), out.ProjectConfig)
	assert.Equal(t, []ProjectSetupHost{{Name: "box"}}, out.Hosts)
	assert.Contains(t, out.NextSteps[0], "Compare")
	assert.Contains(t, out.NextSteps, "rr doctor")
	assert.Equal(t, "rr test", out.NextSteps[len(out.NextSteps)-1])

	data, err := os.ReadFile(filepath.Join(project, config.ConfigFileName))
	require.NoError(t, err)
	assert.Equal(t, existing, string(data))
}

// In a worktree nested inside the main checkout, the main checkout's
// .rr.yaml is reported, not shadowed by a freshly written one.
func TestProjectSetup_NestedWorktreeReportsParentConfig(t *testing.T) {
	main := setupProjectEnv(t, "", map[string]string{config.ConfigFileName: "version: 1\n"})
	worktree := filepath.Join(main, "wt")
	require.NoError(t, os.MkdirAll(worktree, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: x\n"), 0o644))
	t.Chdir(worktree)

	_, err := projectSetup()
	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrConfigNotFound), err)
	_, statErr := os.Stat(filepath.Join(worktree, config.ConfigFileName))
	assert.True(t, os.IsNotExist(statErr))
}

// An explicit --config that doesn't exist is an error, not a reason to write
// a fresh .rr.yaml in the current directory.
func TestProjectSetup_MissingExplicitConfig(t *testing.T) {
	project := setupProjectEnv(t, "", map[string]string{"go.mod": "module x\n"})
	prev := cfgFile
	cfgFile = filepath.Join(project, "nope.yaml")
	t.Cleanup(func() { cfgFile = prev })

	_, err := projectSetup()
	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrConfigNotFound), err)
	_, statErr := os.Stat(filepath.Join(project, config.ConfigFileName))
	assert.True(t, os.IsNotExist(statErr))
}
