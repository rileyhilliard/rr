package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/lock"
)

// gitIn runs git in dir and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// localHostRepo sets up a temp HOME whose global config has a local host
// "dev", and a git repo whose first commit has a README and whose second
// commits a .rr.yaml using dev, with locks under a temp dir. It returns the
// repo (the main checkout) and the lock dir.
func localHostRepo(t *testing.T) (repo, lockDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	_, lockDir = writeLocalHostConfigs(t) // HOME and the global config
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "rr test")
	t.Setenv("GIT_AUTHOR_EMAIL", "rr@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "rr test")
	t.Setenv("GIT_COMMITTER_EMAIL", "rr@example.com")

	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	gitIn(t, repo, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README"), []byte("hi\n"), 0o644))
	gitIn(t, repo, "add", "README")
	gitIn(t, repo, "commit", "-q", "-m", "init")

	project := `version: 1
hosts: [dev]
lock:
  timeout: ` + queueLockTimeout.String() + `
  dir: ` + filepath.Dir(lockDir) + `
tasks:
  where:
    run: pwd -P > where-task.out
`
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".rr.yaml"), []byte(project), 0o644))
	gitIn(t, repo, "add", ".rr.yaml")
	gitIn(t, repo, "commit", "-q", "-m", "add rr config")
	return repo, lockDir
}

// addWorktree adds a linked worktree on a new branch at path, starting
// from base.
func addWorktree(t *testing.T, repo, path, branch, base string) string {
	t.Helper()
	gitIn(t, repo, "worktree", "add", "-q", "-b", branch, path, base)
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return resolved
}

// A run or task from a linked worktree runs in that worktree, not the main
// checkout: rr run in the caller's dir, a task at the worktree root.
func TestRun_LocalHostLinkedWorktreeRunsInWorktree(t *testing.T) {
	repo, _ := localHostRepo(t)
	worktree := addWorktree(t, repo, filepath.Join(t.TempDir(), "wt"), "feature", "HEAD")
	sub := filepath.Join(worktree, "src")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	code, events, err := runQuietly(t, RunOptions{Command: "pwd -P > where.out"})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)
	got, err := os.ReadFile(filepath.Join(sub, "where.out"))
	require.NoError(t, err)
	assert.Equal(t, sub+"\n", string(got))

	var taskEvents string
	captureStdout(t, func() {
		taskEvents = captureStderr(t, func() {
			code, err = RunTask(TaskOptions{TaskName: "where"})
		})
	})
	require.NoError(t, err, taskEvents)
	require.Equal(t, 0, code, taskEvents)
	got, err = os.ReadFile(filepath.Join(worktree, "where-task.out"))
	require.NoError(t, err)
	assert.Equal(t, worktree+"\n", string(got))

	for _, f := range []string{"where.out", "where-task.out", filepath.Join("src", "where.out")} {
		_, statErr := os.Stat(filepath.Join(repo, f))
		assert.True(t, os.IsNotExist(statErr), "nothing ran in the main checkout: %s", f)
	}
}

// Two worktrees of one repo share the local host's lock: while a run from
// one holds it, a run from the other waits, then runs when it's released.
func TestRun_LocalHostWorktreesShareLock(t *testing.T) {
	repo, lockDir := localHostRepo(t)
	defer lock.SetRetryIntervalForTesting(50 * time.Millisecond)()
	base := t.TempDir()
	wtA := addWorktree(t, repo, filepath.Join(base, "a"), "branch-a", "HEAD")
	wtB := addWorktree(t, repo, filepath.Join(base, "b"), "branch-b", "HEAD")

	// The run from worktree A holds the lock until the test creates "go".
	goFile := filepath.Join(base, "go")
	holderCmd := "touch started; while [ ! -f '" + goFile + "' ]; do sleep 0.05; done"
	holder := startRR(t, wtA, "run", holderCmd)
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(wtA, "started"))
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the run from worktree A starts: %s", holder.stderr.String())

	const hold = 300 * time.Millisecond
	timer := time.AfterFunc(hold, func() { _ = os.WriteFile(goFile, nil, 0o644) })
	defer timer.Stop()

	t.Chdir(wtB)
	code, events, err := runQuietly(t, RunOptions{Command: "touch ran"})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)
	_, statErr := os.Stat(filepath.Join(wtB, "ran"))
	assert.NoError(t, statErr, "the run from worktree B ran, in worktree B")

	waiting := phaseEvents(t, events, "lock", "waiting")
	require.Len(t, waiting, 1, events)
	holders, ok := waiting[0].Details["holders"].([]interface{})
	require.True(t, ok, "holders is a list: %v", waiting[0].Details)
	require.Len(t, holders, 1)
	assert.Equal(t, holderCmd, holders[0].(map[string]interface{})["command"], "B waits on A's run")
	complete := phaseEvents(t, events, "lock", "complete")
	require.Len(t, complete, 1, events)
	assert.GreaterOrEqual(t, complete[0].Duration, (hold - 50*time.Millisecond).Seconds(), events)

	require.NoError(t, holder.wait(t, 10*time.Second), holder.stderr.String())
	_, statErr = os.Stat(lockDir)
	assert.True(t, os.IsNotExist(statErr), "both runs released the lock")
}

// A worktree created inside the main checkout (as .claude/worktrees/x is),
// on a branch without .rr.yaml, doesn't pick up the main checkout's config:
// its project root would be the main checkout, and a local host would run
// the main checkout's code. The run stops with CONFIG_NOT_FOUND naming the
// config it skipped.
func TestRun_NestedWorktreeWithoutConfigDoesNotRunMainCheckout(t *testing.T) {
	repo, _ := localHostRepo(t)
	nested := addWorktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "x"), "old", "HEAD~1")
	_, statErr := os.Stat(filepath.Join(nested, ".rr.yaml"))
	require.True(t, os.IsNotExist(statErr), "the branch predates .rr.yaml")
	t.Chdir(nested)

	code, events, err := runQuietly(t, RunOptions{Command: "pwd -P > where.out"})
	require.Error(t, err, events)
	assert.NotEqual(t, 0, code)
	assert.True(t, errors.IsCode(err, errors.ErrConfigNotFound), "got %v", err)
	assert.Contains(t, err.Error(), filepath.Join(repo, ".rr.yaml"))
	assert.Contains(t, err.Error(), "belongs to another checkout")

	// A task is where it would hurt most: tasks run at the project root,
	// which would be the main checkout.
	var taskEvents string
	captureStdout(t, func() {
		taskEvents = captureStderr(t, func() {
			_, err = RunTask(TaskOptions{TaskName: "where"})
		})
	})
	require.Error(t, err, taskEvents)
	assert.True(t, errors.IsCode(err, errors.ErrConfigNotFound), "got %v", err)

	for _, dir := range []string{repo, nested} {
		for _, f := range []string{"where.out", "where-task.out"} {
			_, statErr := os.Stat(filepath.Join(dir, f))
			assert.True(t, os.IsNotExist(statErr), "nothing ran: %s", filepath.Join(dir, f))
		}
	}
}

// Monitor only needs the global hosts, so the skipped main-checkout config
// that stops a run doesn't stop it: it shows the global hosts, as it does
// anywhere without a project config.
func TestResolveMonitorScope_NestedWorktreeWithoutConfigUsesGlobalHosts(t *testing.T) {
	repo, _ := localHostRepo(t)
	nested := addWorktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "x"), "old", "HEAD~1")
	t.Chdir(nested)

	scope, err := resolveMonitorScope("")
	require.NoError(t, err)
	assert.Equal(t, []string{"dev"}, scope.order)
	assert.Contains(t, scope.hosts, "dev")
}

// Status, like monitor, runs no code: in the same nested worktree it shows the
// global hosts instead of failing with CONFIG_NOT_FOUND.
func TestStatus_NestedWorktreeWithoutConfigUsesGlobalHosts(t *testing.T) {
	repo, _ := localHostRepo(t)
	nested := addWorktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "x"), "old", "HEAD~1")
	t.Chdir(nested)

	got := runStatusJSON(t)
	assert.Equal(t, []string{"dev"}, statusHostNames(got))
	assert.Equal(t, statusScopeGlobal, got.Scope)
}

// With the config committed on its branch, the same nested worktree uses its
// own config and runs in place.
func TestRun_NestedWorktreeWithConfigRunsInWorktree(t *testing.T) {
	repo, _ := localHostRepo(t)
	nested := addWorktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "x"), "new", "HEAD")
	t.Chdir(nested)

	code, events, err := runQuietly(t, RunOptions{Command: "pwd -P > where.out"})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)
	got, err := os.ReadFile(filepath.Join(nested, "where.out"))
	require.NoError(t, err)
	assert.Equal(t, nested, strings.TrimSpace(string(got)))
	_, statErr := os.Stat(filepath.Join(repo, "where.out"))
	assert.True(t, os.IsNotExist(statErr), "nothing ran in the main checkout")
}
