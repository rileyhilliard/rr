package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/deps"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Dependency Resolution Integration Tests
// =============================================================================

func TestDependencyResolution_LinearChain(t *testing.T) {
	tasks := map[string]config.TaskConfig{
		"setup": {Run: "echo setup"},
		"lint":  {Run: "echo lint", Depends: []config.DependencyItem{{Task: "setup"}}},
		"test":  {Run: "echo test", Depends: []config.DependencyItem{{Task: "lint"}}},
		"ci": {
			Depends: []config.DependencyItem{{Task: "test"}},
		},
	}

	resolver := deps.NewResolver(tasks)
	plan, err := resolver.Resolve("ci", deps.ResolveOptions{})

	require.NoError(t, err)
	require.NotNil(t, plan)

	// Expect: setup -> lint -> test (ci has no run command)
	assert.Equal(t, 3, plan.TaskCount())
	assert.Equal(t, 3, len(plan.Stages))

	// Verify order
	assert.Equal(t, "setup", plan.Stages[0].Tasks[0])
	assert.Equal(t, "lint", plan.Stages[1].Tasks[0])
	assert.Equal(t, "test", plan.Stages[2].Tasks[0])
}

func TestDependencyResolution_ParallelGroup(t *testing.T) {
	tasks := map[string]config.TaskConfig{
		"lint":      {Run: "echo lint"},
		"typecheck": {Run: "echo typecheck"},
		"test":      {Run: "echo test"},
		"ci": {
			Depends: []config.DependencyItem{
				{Parallel: []string{"lint", "typecheck"}},
				{Task: "test"},
			},
		},
	}

	resolver := deps.NewResolver(tasks)
	plan, err := resolver.Resolve("ci", deps.ResolveOptions{})

	require.NoError(t, err)
	require.NotNil(t, plan)

	// Expect: [lint, typecheck] (parallel) -> test
	assert.Equal(t, 3, plan.TaskCount())
	assert.Equal(t, 2, len(plan.Stages))

	// First stage is parallel
	assert.True(t, plan.Stages[0].Parallel)
	assert.ElementsMatch(t, []string{"lint", "typecheck"}, plan.Stages[0].Tasks)

	// Second stage is sequential
	assert.False(t, plan.Stages[1].Parallel)
	assert.Equal(t, []string{"test"}, plan.Stages[1].Tasks)
}

func TestDependencyResolution_DiamondDependency(t *testing.T) {
	// Diamond pattern: base is depended on by both left and right
	tasks := map[string]config.TaskConfig{
		"base":  {Run: "echo base"},
		"left":  {Run: "echo left", Depends: []config.DependencyItem{{Task: "base"}}},
		"right": {Run: "echo right", Depends: []config.DependencyItem{{Task: "base"}}},
		"top": {
			Depends: []config.DependencyItem{
				{Task: "left"},
				{Task: "right"},
			},
		},
	}

	resolver := deps.NewResolver(tasks)
	plan, err := resolver.Resolve("top", deps.ResolveOptions{})

	require.NoError(t, err)
	require.NotNil(t, plan)

	// Verify base only appears once (deduplication)
	baseCount := 0
	for _, stage := range plan.Stages {
		for _, task := range stage.Tasks {
			if task == "base" {
				baseCount++
			}
		}
	}
	assert.Equal(t, 1, baseCount, "base should appear exactly once")
}

func TestDependencyResolution_SkipDeps(t *testing.T) {
	tasks := map[string]config.TaskConfig{
		"lint": {Run: "echo lint"},
		"test": {Run: "echo test"},
		"deploy": {
			Depends: []config.DependencyItem{
				{Task: "lint"},
				{Task: "test"},
			},
			Run: "echo deploy",
		},
	}

	resolver := deps.NewResolver(tasks)
	plan, err := resolver.Resolve("deploy", deps.ResolveOptions{SkipDeps: true})

	require.NoError(t, err)
	require.NotNil(t, plan)

	// Only deploy should be in the plan
	assert.Equal(t, 1, plan.TaskCount())
	assert.Equal(t, "deploy", plan.Stages[0].Tasks[0])
}

func TestDependencyResolution_FromTask(t *testing.T) {
	tasks := map[string]config.TaskConfig{
		"lint":  {Run: "echo lint"},
		"test":  {Run: "echo test"},
		"build": {Run: "echo build"},
		"ci": {
			Depends: []config.DependencyItem{
				{Task: "lint"},
				{Task: "test"},
				{Task: "build"},
			},
		},
	}

	resolver := deps.NewResolver(tasks)
	plan, err := resolver.Resolve("ci", deps.ResolveOptions{From: "test"})

	require.NoError(t, err)
	require.NotNil(t, plan)

	// Should start from test, skip lint
	assert.Equal(t, 2, plan.TaskCount())
	assert.Equal(t, "test", plan.Stages[0].Tasks[0])
	assert.Equal(t, "build", plan.Stages[1].Tasks[0])
}

// =============================================================================
// Dependency Executor Integration Tests
// =============================================================================

func TestDependencyExecutor_LocalExecution(t *testing.T) {
	// Create temp dir for test
	dir := t.TempDir()

	// Create a simple config with dependent tasks
	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  local:
    ssh:
      - localhost
    dir: /tmp/rr-test
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	projectContent := `
version: 1
tasks:
  step1:
    run: echo step1
  step2:
    depends:
      - step1
    run: echo step2
  all:
    depends:
      - step2
`
	configPath := filepath.Join(dir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	t.Setenv("HOME", dir)

	// Load config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	globalCfg, err := config.LoadGlobal()
	require.NoError(t, err)

	resolved := &config.ResolvedConfig{
		Project: cfg,
		Global:  globalCfg,
	}

	// Create resolver and get plan
	resolver := deps.NewResolver(cfg.Tasks)
	plan, err := resolver.Resolve("all", deps.ResolveOptions{})
	require.NoError(t, err)

	// Create local connection
	conn := &host.Connection{
		IsLocal: true,
		Name:    "local",
		Alias:   "localhost",
	}

	// Create executor with captured output
	var stdout, stderr bytes.Buffer
	executor := deps.NewExecutor(resolved, conn, deps.ExecutorOptions{
		Stdout:  &stdout,
		Stderr:  &stderr,
		WorkDir: dir,
	})

	// Execute
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.Execute(ctx, plan)
	require.NoError(t, err)

	// Verify success
	assert.True(t, result.Success())
	assert.Equal(t, 0, result.ExitCode())

	// Verify output contains expected content
	output := stdout.String()
	assert.Contains(t, output, "step1")
	assert.Contains(t, output, "step2")
}

func TestDependencyExecutor_FailFast(t *testing.T) {
	// Create temp dir for test
	dir := t.TempDir()

	// Create config with a task that fails
	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  local:
    ssh:
      - localhost
    dir: /tmp/rr-test
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	projectContent := `
version: 1
tasks:
  pass:
    run: echo pass
  fail:
    run: exit 1
  never:
    run: echo never
  all:
    depends:
      - pass
      - fail
      - never
`
	configPath := filepath.Join(dir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	t.Setenv("HOME", dir)

	// Load config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	globalCfg, err := config.LoadGlobal()
	require.NoError(t, err)

	resolved := &config.ResolvedConfig{
		Project: cfg,
		Global:  globalCfg,
	}

	// Create resolver and get plan
	resolver := deps.NewResolver(cfg.Tasks)
	plan, err := resolver.Resolve("all", deps.ResolveOptions{})
	require.NoError(t, err)

	// Create local connection
	conn := &host.Connection{
		IsLocal: true,
		Name:    "local",
		Alias:   "localhost",
	}

	// Create executor with fail-fast enabled
	var stdout, stderr bytes.Buffer
	executor := deps.NewExecutor(resolved, conn, deps.ExecutorOptions{
		FailFast: true,
		Stdout:   &stdout,
		Stderr:   &stderr,
		WorkDir:  dir,
	})

	// Execute
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := executor.Execute(ctx, plan)
	require.NoError(t, err)

	// Should fail
	assert.False(t, result.Success())
	assert.NotEqual(t, 0, result.ExitCode())
	assert.True(t, result.FailFast)

	// Verify pass was executed but never wasn't (fail-fast stops after fail)
	output := stdout.String()
	assert.Contains(t, output, "pass")
	assert.NotContains(t, output, "never")
}

// =============================================================================
// Config Validation Integration Tests
// =============================================================================

func TestConfigValidation_CircularDependency(t *testing.T) {
	dir := t.TempDir()

	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  test-host:
    ssh:
      - localhost
    dir: /tmp/test
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	// Config with circular dependency: a -> b -> c -> a
	projectContent := `
version: 1
host: test-host
tasks:
  a:
    depends:
      - b
    run: echo a
  b:
    depends:
      - c
    run: echo b
  c:
    depends:
      - a
    run: echo c
`
	configPath := filepath.Join(dir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	t.Setenv("HOME", dir)

	// Load config should succeed
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Validation should catch the cycle
	err = config.ValidateDependencyGraph(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestConfigValidation_DependsOnNonexistent(t *testing.T) {
	dir := t.TempDir()

	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  test-host:
    ssh:
      - localhost
    dir: /tmp/test
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	// Config with reference to nonexistent task
	projectContent := `
version: 1
host: test-host
tasks:
  ci:
    depends:
      - missing
`
	configPath := filepath.Join(dir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	t.Setenv("HOME", dir)

	// Load config should succeed
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Validation should catch the missing reference
	err = config.ValidateDependencyGraph(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-existent task")
}

func TestConfigValidation_SelfReference(t *testing.T) {
	dir := t.TempDir()

	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  test-host:
    ssh:
      - localhost
    dir: /tmp/test
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	// Config with self-reference
	projectContent := `
version: 1
host: test-host
tasks:
  recursive:
    depends:
      - recursive
    run: echo recursive
`
	configPath := filepath.Join(dir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	t.Setenv("HOME", dir)

	// Load config should succeed
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Validation should catch the self-reference
	err = config.ValidateDependencyGraph(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can't depend on itself")
}

// =============================================================================
// Config Loading Integration Tests
// =============================================================================

func TestConfigLoad_DependencyItem_StringFormat(t *testing.T) {
	dir := t.TempDir()

	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  test-host:
    ssh:
      - localhost
    dir: /tmp/test
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	// Config with string format dependencies
	projectContent := `
version: 1
host: test-host
tasks:
  lint:
    run: echo lint
  test:
    run: echo test
  ci:
    depends:
      - lint
      - test
`
	configPath := filepath.Join(dir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	t.Setenv("HOME", dir)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	ciTask := cfg.Tasks["ci"]
	assert.Len(t, ciTask.Depends, 2)
	assert.Equal(t, "lint", ciTask.Depends[0].Task)
	assert.Equal(t, "test", ciTask.Depends[1].Task)
}

func TestConfigLoad_DependencyItem_ParallelFormat(t *testing.T) {
	dir := t.TempDir()

	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  test-host:
    ssh:
      - localhost
    dir: /tmp/test
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	// Config with parallel group dependencies
	projectContent := `
version: 1
host: test-host
tasks:
  lint:
    run: echo lint
  typecheck:
    run: echo typecheck
  test:
    run: echo test
  ci:
    depends:
      - parallel: [lint, typecheck]
      - test
`
	configPath := filepath.Join(dir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	t.Setenv("HOME", dir)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	ciTask := cfg.Tasks["ci"]
	assert.Len(t, ciTask.Depends, 2)

	// First dependency is parallel
	assert.True(t, ciTask.Depends[0].IsParallel())
	assert.ElementsMatch(t, []string{"lint", "typecheck"}, ciTask.Depends[0].Parallel)

	// Second is simple task
	assert.False(t, ciTask.Depends[1].IsParallel())
	assert.Equal(t, "test", ciTask.Depends[1].Task)
}

func TestConfigLoad_DependencyItem_MixedFormat(t *testing.T) {
	dir := t.TempDir()

	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  test-host:
    ssh:
      - localhost
    dir: /tmp/test
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)

	// Config with mixed format: strings and parallel groups
	projectContent := `
version: 1
host: test-host
tasks:
  setup:
    run: echo setup
  lint:
    run: echo lint
  typecheck:
    run: echo typecheck
  test:
    run: echo test
  deploy:
    run: echo deploy
  ci:
    depends:
      - setup
      - parallel: [lint, typecheck]
      - test
      - parallel: [deploy]
`
	configPath := filepath.Join(dir, ".rr.yaml")
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	t.Setenv("HOME", dir)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	ciTask := cfg.Tasks["ci"]
	require.Len(t, ciTask.Depends, 4)

	// 1. Simple string
	assert.False(t, ciTask.Depends[0].IsParallel())
	assert.Equal(t, "setup", ciTask.Depends[0].Task)

	// 2. Parallel group
	assert.True(t, ciTask.Depends[1].IsParallel())
	assert.ElementsMatch(t, []string{"lint", "typecheck"}, ciTask.Depends[1].Parallel)

	// 3. Simple string
	assert.False(t, ciTask.Depends[2].IsParallel())
	assert.Equal(t, "test", ciTask.Depends[2].Task)

	// 4. Parallel with single item
	assert.True(t, ciTask.Depends[3].IsParallel())
	assert.Equal(t, []string{"deploy"}, ciTask.Depends[3].Parallel)
}
