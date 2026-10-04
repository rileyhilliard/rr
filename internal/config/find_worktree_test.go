package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/errors"
)

// gitRepo creates a git repo in a temp dir with one commit (a README) and
// returns its resolved path. HOME points at a temp dir so the developer's
// git config (signing, hooks) stays out of it.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "rr test")
	t.Setenv("GIT_AUTHOR_EMAIL", "rr@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "rr test")
	t.Setenv("GIT_COMMITTER_EMAIL", "rr@example.com")

	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git(t, repo, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README"), []byte("hi\n"), 0o644))
	git(t, repo, "add", "README")
	git(t, repo, "commit", "-q", "-m", "init")
	return repo
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func writeRRYAML(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, ConfigFileName)
	require.NoError(t, os.WriteFile(path, []byte("version: 1\n"), 0o644))
	return path
}

// A worktree created inside the main checkout (as .claude/worktrees/x is)
// whose branch has no .rr.yaml must not pick up the main checkout's: its
// project root would be the main checkout, so a local host would run the
// main checkout's code and a remote host would sync it. Find refuses, and
// the error names the config it skipped and why.
func TestFind_NestedWorktreeIgnoresMainCheckoutConfig(t *testing.T) {
	tests := []struct {
		name      string
		committed bool // main's .rr.yaml is committed after the worktree's base
	}{
		{name: "committed on main only", committed: true},
		{name: "untracked in main checkout", committed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := gitRepo(t)
			mainConfig := writeRRYAML(t, repo)
			if tt.committed {
				git(t, repo, "add", ConfigFileName)
				git(t, repo, "commit", "-q", "-m", "add rr config")
			}
			worktree := filepath.Join(repo, ".claude", "worktrees", "x")
			git(t, repo, "worktree", "add", "-q", "-b", "feature", worktree, "HEAD~0")
			if tt.committed {
				// The branch starts before the config commit.
				git(t, worktree, "reset", "-q", "--hard", "HEAD~1")
			}
			_, statErr := os.Stat(filepath.Join(worktree, ConfigFileName))
			require.True(t, os.IsNotExist(statErr), "the worktree has no .rr.yaml of its own")
			sub := filepath.Join(worktree, "src")
			require.NoError(t, os.MkdirAll(sub, 0o755))

			for _, cwd := range []string{worktree, sub} {
				t.Chdir(cwd)
				path, err := Find("")
				require.Error(t, err, "from %s", cwd)
				assert.Empty(t, path)
				assert.True(t, errors.IsCode(err, errors.ErrConfigNotFound), "got %v", err)
				assert.Contains(t, err.Error(), mainConfig)
				assert.Contains(t, err.Error(), worktree)
				var rrErr *errors.Error
				require.ErrorAs(t, err, &rrErr)
				assert.Contains(t, rrErr.Suggestion, "Commit .rr.yaml")
			}
		})
	}
}

// A nested worktree whose branch has the config uses its own.
func TestFind_NestedWorktreeUsesItsOwnConfig(t *testing.T) {
	repo := gitRepo(t)
	writeRRYAML(t, repo)
	git(t, repo, "add", ConfigFileName)
	git(t, repo, "commit", "-q", "-m", "add rr config")
	worktree := filepath.Join(repo, ".claude", "worktrees", "x")
	git(t, repo, "worktree", "add", "-q", "-b", "feature", worktree)
	sub := filepath.Join(worktree, "src")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	for _, cwd := range []string{worktree, sub} {
		t.Chdir(cwd)
		path, err := Find("")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(worktree, ConfigFileName), path, "from %s", cwd)
	}
}

// From a subdirectory of a real repo, the walk reaches .rr.yaml at the
// repo root.
func TestFind_RepoSubdirFindsRootConfig(t *testing.T) {
	repo := gitRepo(t)
	want := writeRRYAML(t, repo)
	sub := filepath.Join(repo, "a", "b")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	path, err := Find("")
	require.NoError(t, err)
	assert.Equal(t, want, path)
}

// Outside git, the walk goes up until it finds .rr.yaml.
func TestFind_NonGitProjectSubdirFindsRootConfig(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	want := writeRRYAML(t, root)
	sub := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	path, err := Find("")
	require.NoError(t, err)
	assert.Equal(t, want, path)
}

// A repo with no .rr.yaml anywhere above it is plain not-found, not an
// error.
func TestFind_RepoWithoutConfigIsNotFound(t *testing.T) {
	repo := gitRepo(t)
	t.Chdir(repo)

	path, err := Find("")
	require.NoError(t, err)
	assert.Empty(t, path)
}
