package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandRemote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "HOME expands to tilde",
			input:    "${HOME}/rr/project",
			expected: "~/rr/project",
		},
		{
			name:     "PROJECT expands",
			input:    "~/rr/${PROJECT}",
			expected: "~/rr/" + getProject(), // Uses current project
		},
		{
			name:     "USER expands",
			input:    "/home/${USER}/rr",
			expected: "/home/" + os.Getenv("USER") + "/rr",
		},
		{
			name:     "tilde unchanged",
			input:    "~/projects/app",
			expected: "~/projects/app",
		},
		{
			name:     "absolute path unchanged",
			input:    "/opt/app/data",
			expected: "/opt/app/data",
		},
		{
			name:     "multiple variables",
			input:    "${HOME}/rr/${PROJECT}",
			expected: "~/rr/" + getProject(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandRemote(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpand_vs_ExpandRemote(t *testing.T) {
	// Expand should use local HOME
	localHome, _ := os.UserHomeDir()
	expandResult := Expand("${HOME}/test")
	assert.Equal(t, localHome+"/test", expandResult)

	// ExpandRemote should use ~ for remote shell expansion
	expandRemoteResult := ExpandRemote("${HOME}/test")
	assert.Equal(t, "~/test", expandRemoteResult)
}

func TestExpandArgs(t *testing.T) {
	tests := []struct {
		name      string
		run       string
		args      []string
		expected  string
		hasHolder bool
	}{
		{
			name:      "no placeholder no args",
			run:       "pytest tests/",
			args:      nil,
			expected:  "pytest tests/",
			hasHolder: false,
		},
		{
			name:      "no placeholder with args returns unchanged",
			run:       "pytest tests/",
			args:      []string{"-k", "foo"},
			expected:  "pytest tests/",
			hasHolder: false,
		},
		{
			name:      "placeholder mid pipeline",
			run:       "pytest {args} -n 4 | grep -v PASS",
			args:      []string{"tests/foo.py"},
			expected:  "pytest 'tests/foo.py' -n 4 | grep -v PASS",
			hasHolder: true,
		},
		{
			name:      "bare placeholder empty args",
			run:       "pytest {args} -n 4",
			args:      nil,
			expected:  "pytest  -n 4",
			hasHolder: true,
		},
		{
			name:      "default used when no args",
			run:       "pytest {args:-.} -n 4",
			args:      nil,
			expected:  "pytest . -n 4",
			hasHolder: true,
		},
		{
			name:      "default overridden by args",
			run:       "pytest {args:-.} -n 4",
			args:      []string{"tests/a.py", "tests/b.py"},
			expected:  "pytest 'tests/a.py' 'tests/b.py' -n 4",
			hasHolder: true,
		},
		{
			name:      "args are shell quoted",
			run:       "pytest {args}",
			args:      []string{"-k", "a b", "it's"},
			expected:  `pytest '-k' 'a b' 'it'\''s'`,
			hasHolder: true,
		},
		{
			name:      "multiple placeholders",
			run:       "echo {args} && pytest {args}",
			args:      []string{"x"},
			expected:  "echo 'x' && pytest 'x'",
			hasHolder: true,
		},
		{
			name:      "escaped placeholder stays literal",
			run:       "jq '{{args}}' file.json",
			args:      []string{"ignored"},
			expected:  "jq '{args}' file.json",
			hasHolder: false,
		},
		{
			name:      "empty default",
			run:       "pytest {args:-}",
			args:      nil,
			expected:  "pytest ",
			hasHolder: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := ExpandArgs(tt.run, tt.args)
			assert.Equal(t, tt.expected, got)
			assert.Equal(t, tt.hasHolder, found)
		})
	}
}

func TestSanitizeWorktreeName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain name unchanged", "feature-x", "feature-x"},
		{"dots and underscores kept", "my.branch_2", "my.branch_2"},
		{"slashes and spaces replaced", "feat/US-123 fix", "feat-US-123-fix"},
		{"non-ascii replaced", "héllo", "h-llo"},
		{"capped at 40 chars", strings.Repeat("a", 50), strings.Repeat("a", 40)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeWorktreeName(tt.input))
		})
	}
}

// setupWorktreeRepo builds a real git repo with one linked worktree and
// returns (mainDir, worktreeDir). The origin remote pins the repo name to
// "myrepo" so ${PROJECT} assertions are deterministic.
func setupWorktreeRepo(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	mainDir := filepath.Join(base, "myrepo")
	require.NoError(t, os.MkdirAll(mainDir, 0o755))

	git := func(dir string, args ...string) {
		t.Helper()
		full := append([]string{"-c", "user.name=test", "-c", "user.email=test@test", "-c", "commit.gpgsign=false"}, args...)
		cmd := exec.Command("git", full...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	git(mainDir, "init", "-q")
	git(mainDir, "remote", "add", "origin", "git@example.com:user/myrepo.git")
	require.NoError(t, os.WriteFile(filepath.Join(mainDir, "README"), []byte("x"), 0o644))
	git(mainDir, "add", "README")
	git(mainDir, "commit", "-q", "-m", "init")

	wtDir := filepath.Join(base, "myrepo-feature.x")
	git(mainDir, "worktree", "add", "-q", "-b", "feature-x", wtDir)

	return mainDir, wtDir
}

func TestDetectWorktree(t *testing.T) {
	mainDir, wtDir := setupWorktreeRepo(t)

	t.Run("main checkout is not linked", func(t *testing.T) {
		t.Chdir(mainDir)
		wt := DetectWorktree()
		assert.False(t, wt.IsLinked)
		assert.Empty(t, wt.Name)
		wantTop, err := filepath.EvalSymlinks(mainDir)
		require.NoError(t, err)
		gotTop, err := filepath.EvalSymlinks(wt.TopLevel)
		require.NoError(t, err)
		assert.Equal(t, wantTop, gotTop)
	})

	t.Run("linked worktree detected with sanitized name", func(t *testing.T) {
		t.Chdir(wtDir)
		wt := DetectWorktree()
		assert.True(t, wt.IsLinked)
		assert.Equal(t, "myrepo-feature.x", wt.Name)
	})

	t.Run("outside a repo returns zero value", func(t *testing.T) {
		t.Chdir(t.TempDir())
		assert.Equal(t, WorktreeInfo{}, DetectWorktree())
	})
}

func TestGetProject_WorktreeIsolation(t *testing.T) {
	mainDir, wtDir := setupWorktreeRepo(t)

	t.Run("main checkout name is never suffixed", func(t *testing.T) {
		t.Chdir(mainDir)
		resetProjectCache()
		t.Cleanup(resetProjectCache)
		assert.Equal(t, "myrepo", getProject())
	})

	t.Run("linked worktree gets suffix by default", func(t *testing.T) {
		t.Chdir(wtDir)
		resetProjectCache()
		t.Cleanup(resetProjectCache)
		assert.Equal(t, "myrepo@myrepo-feature.x", getProject())
	})

	t.Run("SetWorktreeIsolation toggles after facts are cached", func(t *testing.T) {
		t.Chdir(wtDir)
		resetProjectCache()
		t.Cleanup(resetProjectCache)

		// Prime the facts cache with isolation on (mirrors LoadGlobal
		// running before the project config is read).
		assert.Equal(t, "myrepo@myrepo-feature.x", getProject())

		SetWorktreeIsolation(false)
		assert.Equal(t, "myrepo", getProject())

		SetWorktreeIsolation(true)
		assert.Equal(t, "myrepo@myrepo-feature.x", getProject())
	})
}

// TestLoad_WorktreeIsolationEscapeHatch is the end-to-end regression test:
// a project config with worktree_isolation: false must yield the unsuffixed
// remote dir even though expansion could have happened earlier with the
// default (on) setting.
func TestLoad_WorktreeIsolationEscapeHatch(t *testing.T) {
	_, wtDir := setupWorktreeRepo(t)
	t.Chdir(wtDir)
	resetProjectCache()
	t.Cleanup(resetProjectCache)

	// Default: isolation on, suffixed name.
	assert.Equal(t, "~/rr/myrepo@myrepo-feature.x", ExpandRemote("~/rr/${PROJECT}"))

	cfgPath := filepath.Join(wtDir, ".rr.yaml")
	content := "version: 1\nhost: mini\nsync:\n  worktree_isolation: false\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0o644))

	cfg, err := Load(cfgPath)
	require.NoError(t, err)
	require.NotNil(t, cfg.Sync.WorktreeIsolation)
	assert.False(t, *cfg.Sync.WorktreeIsolation)

	// The escape hatch must apply to expansions after project load.
	assert.Equal(t, "~/rr/myrepo", ExpandRemote("~/rr/${PROJECT}"))
}
