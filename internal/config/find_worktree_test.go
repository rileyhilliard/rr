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
	initRepo(t, repo)
	return repo
}

// initRepo makes dir (created if missing) a git repo with one commit.
// Call it after gitRepo, which sets up the git environment.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	git(t, dir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("hi\n"), 0o644))
	git(t, dir, "add", "README")
	git(t, dir, "commit", "-q", "-m", "init")
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

// A .rr.yaml above a repo with none of its own is only refused when it sits
// in an enclosing checkout. One in a plain directory that holds several
// repos (~/code/.rr.yaml over ~/code/foo) is skipped: the repo is
// not-found, and commands fall back to global hosts as before.
func TestFind_ConfigAboveCheckout(t *testing.T) {
	tests := []struct {
		name string
		// layout builds the tree under root and returns the repo to search
		// from and the .rr.yaml above it.
		layout  func(t *testing.T, root string) (repo, above string)
		refused bool
	}{
		{
			name: "plain parent directory",
			layout: func(t *testing.T, root string) (string, string) {
				repo := filepath.Join(root, "foo")
				initRepo(t, repo)
				return repo, writeRRYAML(t, root)
			},
		},
		{
			name: "enclosing repo's top level",
			layout: func(t *testing.T, root string) (string, string) {
				initRepo(t, root)
				repo := filepath.Join(root, "vendor", "foo")
				initRepo(t, repo)
				return repo, writeRRYAML(t, root)
			},
			refused: true,
		},
		{
			name: "subdirectory of an enclosing repo",
			layout: func(t *testing.T, root string) (string, string) {
				initRepo(t, root)
				sub := filepath.Join(root, "sub")
				repo := filepath.Join(sub, "foo")
				initRepo(t, repo)
				return repo, writeRRYAML(t, sub)
			},
			refused: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRepo(t) // git environment
			root, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			repo, above := tt.layout(t, root)
			t.Chdir(repo)

			path, err := Find("")
			assert.Empty(t, path)
			if !tt.refused {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, errors.IsCode(err, errors.ErrConfigNotFound), "got %v", err)
			assert.Contains(t, err.Error(), above)
		})
	}
}
