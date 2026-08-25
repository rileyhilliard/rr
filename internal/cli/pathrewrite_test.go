package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteLocalPaths(t *testing.T) {
	const root = "/Users/r/app"
	const remote = "~/rr/app"

	tests := []struct {
		name      string
		cmd       string
		want      string
		wantCount int
	}{
		{
			name:      "subpath rewritten",
			cmd:       "pytest /Users/r/app/tests/foo.py -v",
			want:      "pytest $HOME/rr/app/tests/foo.py -v",
			wantCount: 1,
		},
		{
			name:      "exact root at end of command",
			cmd:       "cd /Users/r/app",
			want:      "cd $HOME/rr/app",
			wantCount: 1,
		},
		{
			name:      "sibling directory not rewritten",
			cmd:       "ls /Users/r/app2/file",
			want:      "ls /Users/r/app2/file",
			wantCount: 0,
		},
		{
			name:      "mid-path occurrence not rewritten",
			cmd:       "ls /mnt/Users/r/app/file",
			want:      "ls /mnt/Users/r/app/file",
			wantCount: 0,
		},
		{
			// $HOME can't expand inside single quotes, so the match is
			// left alone rather than rewritten into a broken literal.
			name:      "inside single quotes skipped for tilde remote",
			cmd:       "cd '/Users/r/app/sub dir'",
			want:      "cd '/Users/r/app/sub dir'",
			wantCount: 0,
		},
		{
			name:      "inside double quotes rewritten",
			cmd:       `cd "/Users/r/app/sub dir"`,
			want:      `cd "$HOME/rr/app/sub dir"`,
			wantCount: 1,
		},
		{
			name:      "after equals sign rewritten",
			cmd:       "pytest --junitxml=/Users/r/app/out.xml",
			want:      "pytest --junitxml=$HOME/rr/app/out.xml",
			wantCount: 1,
		},
		{
			name:      "multiple occurrences",
			cmd:       "cp /Users/r/app/a.txt /Users/r/app/b.txt",
			want:      "cp $HOME/rr/app/a.txt $HOME/rr/app/b.txt",
			wantCount: 2,
		},
		{
			name:      "mixed quoting rewrites only expandable match",
			cmd:       "cp '/Users/r/app/a.txt' /Users/r/app/b.txt",
			want:      "cp '/Users/r/app/a.txt' $HOME/rr/app/b.txt",
			wantCount: 1,
		},
		{
			name:      "colon suffix is a boundary",
			cmd:       "vim /Users/r/app:12",
			want:      "vim $HOME/rr/app:12",
			wantCount: 1,
		},
		{
			name:      "pytest node id keeps selector",
			cmd:       "pytest /Users/r/app/tests/foo.py::test_x",
			want:      "pytest $HOME/rr/app/tests/foo.py::test_x",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n := RewriteLocalPaths(tt.cmd, root, remote)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCount, n)
		})
	}
}

func TestRewriteLocalPaths_EmptyInputs(t *testing.T) {
	got, n := RewriteLocalPaths("ls /Users/r/app", "", "~/rr/app")
	assert.Equal(t, "ls /Users/r/app", got)
	assert.Zero(t, n)

	got, n = RewriteLocalPaths("ls /Users/r/app", "/Users/r/app", "")
	assert.Equal(t, "ls /Users/r/app", got)
	assert.Zero(t, n)
}

func TestRewriteLocalPaths_SymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	link := filepath.Join(base, "link")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	require.NoError(t, os.Symlink(realDir, link))

	// The command uses the resolved form while rr knows the symlinked root.
	resolved, err := filepath.EvalSymlinks(link)
	require.NoError(t, err)

	got, n := RewriteLocalPaths("cat "+resolved+"/f.txt", link, "~/rr/app")
	assert.Equal(t, "cat $HOME/rr/app/f.txt", got)
	assert.Equal(t, 1, n)
}

func TestRewriteLocalPaths_AbsoluteRemote(t *testing.T) {
	// Absolute remote dirs need no shell expansion, so single-quoted
	// matches are rewritten too.
	got, n := RewriteLocalPaths("cd '/Users/r/app/sub dir'", "/Users/r/app", "/home/rr/app")
	assert.Equal(t, "cd '/home/rr/app/sub dir'", got)
	assert.Equal(t, 1, n)
}

func TestRewriteArgsToRelative(t *testing.T) {
	const root = "/Users/r/app"

	args, n := RewriteArgsToRelative([]string{"/Users/r/app/tests/foo.py", "-k", "bond"}, root)
	assert.Equal(t, []string{"./tests/foo.py", "-k", "bond"}, args)
	assert.Equal(t, 1, n)

	args, n = RewriteArgsToRelative([]string{"/Users/r/app"}, root)
	assert.Equal(t, []string{"."}, args)
	assert.Equal(t, 1, n)

	args, n = RewriteArgsToRelative([]string{"-v", "tests/"}, root)
	assert.Equal(t, []string{"-v", "tests/"}, args)
	assert.Zero(t, n)

	args, n = RewriteArgsToRelative(nil, root)
	assert.Nil(t, args)
	assert.Zero(t, n)
}

func TestFindForeignAbsPaths(t *testing.T) {
	const root = "/Users/r/app"
	const remote = "/home/rr/app"

	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{
			name: "foreign home path found",
			cmd:  "pytest /Users/other/proj/tests",
			want: []string{"/Users/other/proj/tests"},
		},
		{
			name: "path under local root excluded",
			cmd:  "pytest /Users/r/app/tests",
			want: nil,
		},
		{
			name: "path under remote dir excluded",
			cmd:  "ls /home/rr/app/sub",
			want: nil,
		},
		{
			name: "remote sibling dir is foreign",
			cmd:  "ls /home/rr/app2",
			want: []string{"/home/rr/app2"},
		},
		{
			name: "duplicates reported once",
			cmd:  "cp /home/other/a /home/other/a",
			want: []string{"/home/other/a"},
		},
		{
			name: "no absolute paths",
			cmd:  "make test",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FindForeignAbsPaths(tt.cmd, root, remote))
		})
	}
}

func TestLeadingCdTarget(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{"cd with chained command", "cd /Users/x && make", "/Users/x"},
		{"leading whitespace", "  cd /Users/x; ls", "/Users/x"},
		{"double-quoted target", `cd "/Users/x" && ls`, "/Users/x"},
		{"no cd", "make test", ""},
		{"cd substring of another word", "cdx /y", ""},
		{"bare cd", "cd", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, leadingCdTarget(tt.cmd))
		})
	}
}

func TestBuildFailureHint(t *testing.T) {
	const root = "/Users/r/app"
	const remote = "~/rr/app"
	const host = "m4-mini"

	t.Run("missing local path", func(t *testing.T) {
		stderr := "python: can't open file '/Users/r/app/tests/foo.py': [Errno 2] No such file or directory"
		hint := buildFailureHint("pytest /Users/r/app/tests/foo.py", stderr, root, remote, host)
		assert.Contains(t, hint, host)
		assert.Contains(t, hint, root)
		assert.Contains(t, hint, remote)
	})

	t.Run("missing foreign path in stderr only", func(t *testing.T) {
		stderr := "sh: /Users/other/tool.sh: No such file or directory"
		hint := buildFailureHint("./run.sh", stderr, root, remote, host)
		assert.NotEmpty(t, hint)
	})

	t.Run("not a git repository", func(t *testing.T) {
		stderr := "fatal: not a git repository (or any of the parent directories): .git"
		hint := buildFailureHint("git status", stderr, root, remote, host)
		assert.Contains(t, hint, "synced snapshot")
	})

	t.Run("missing relative path gives no hint", func(t *testing.T) {
		stderr := "cat: ./missing.txt: No such file or directory"
		assert.Empty(t, buildFailureHint("cat ./missing.txt", stderr, root, remote, host))
	})

	t.Run("unrelated failure gives no hint", func(t *testing.T) {
		assert.Empty(t, buildFailureHint("make test", "assertion failed", root, remote, host))
	})
}

// relPathHintFixture builds a project root containing sub/tests/foo.py, so
// "tests/foo.py" resolves from sub/ but not from the root.
func relPathHintFixture(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub", "tests"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "tests", "foo.py"), []byte("x"), 0o644))
	return root
}

func TestBuildRelativePathHint(t *testing.T) {
	root := relPathHintFixture(t)

	t.Run("pytest form names both dirs and the fix", func(t *testing.T) {
		stderr := "ERROR: file or directory not found: tests/foo.py"
		hint := buildRelativePathHint(stderr, root, "sub", "")
		require.NotEmpty(t, hint)
		assert.Contains(t, hint, "tests/foo.py")
		assert.Contains(t, hint, "sub")
		assert.Contains(t, hint, "the project root")
		assert.Contains(t, hint, "'--cwd sub'")
		assert.Contains(t, hint, "'sub/tests/foo.py'")
	})

	// The case auto-cwd made common and the first implementation missed
	// entirely: the offset resolved, so rr ran in sub/, but the user typed a
	// path relative to the project root. invocationDir == runDir here, which
	// the original invocationDir != runDir gate rejected outright.
	t.Run("auto-cwd active and path is root-relative", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "toplevel"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "toplevel", "f.txt"), []byte("x"), 0o644))

		hint := buildRelativePathHint("cat: toplevel/f.txt: No such file or directory", root, "sub", "sub")
		require.NotEmpty(t, hint, "must fire when rr ran in the caller's subdir")
		assert.Contains(t, hint, "the project root")
		assert.Contains(t, hint, "'--cwd .'")
		assert.Contains(t, hint, "../toplevel/f.txt")
	})

	t.Run("generic shell form", func(t *testing.T) {
		stderr := "cat: tests/foo.py: No such file or directory"
		assert.NotEmpty(t, buildRelativePathHint(stderr, root, "sub", ""))
	})

	t.Run("bare form", func(t *testing.T) {
		stderr := "python: No such file or directory: tests/foo.py"
		assert.NotEmpty(t, buildRelativePathHint(stderr, root, "sub", ""))
	})

	t.Run("pytest node id selector stripped", func(t *testing.T) {
		stderr := "ERROR: file or directory not found: tests/foo.py::test_bar"
		hint := buildRelativePathHint(stderr, root, "sub", "")
		require.NotEmpty(t, hint)
		assert.Contains(t, hint, "'sub/tests/foo.py'")
	})

	t.Run("nested offset", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b", "tests"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "a", "b", "tests", "deep.py"), []byte("x"), 0o644))

		hint := buildRelativePathHint("ERROR: file or directory not found: tests/deep.py", root, "a/b", "")
		require.NotEmpty(t, hint)
		assert.Contains(t, hint, "'a/b/tests/deep.py'")
	})

	// The case that motivated the fix's final shape: an explicit --cwd that
	// points somewhere the relative path doesn't resolve. Here the caller ran
	// from sub/ but --cwd sent the command to the project root.
	t.Run("explicit cwd differing from invocation dir", func(t *testing.T) {
		hint := buildRelativePathHint("cat: tests/foo.py: No such file or directory", root, "sub", ".")
		require.NotEmpty(t, hint)
		assert.Contains(t, hint, "'--cwd sub'")
	})

	t.Run("explicit cwd from project root suggests dropping it", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "top"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "top", "f.txt"), []byte("x"), 0o644))

		hint := buildRelativePathHint("cat: top/f.txt: No such file or directory", root, "", "sub")
		require.NotEmpty(t, hint)
		assert.Contains(t, hint, "'--cwd .'", "suggest returning to the project root")
	})

	t.Run("truly missing path gives no hint", func(t *testing.T) {
		stderr := "ERROR: file or directory not found: tests/typo.py"
		assert.Empty(t, buildRelativePathHint(stderr, root, "sub", ""))
	})

	t.Run("path valid in both dirs gives no hint", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "shared"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "shared", "ok.py"), []byte("x"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "sub", "shared"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "shared", "ok.py"), []byte("x"), 0o644))

		stderr := "ERROR: file or directory not found: shared/ok.py"
		assert.Empty(t, buildRelativePathHint(stderr, root, "sub", ""))
	})

	t.Run("absolute path left to buildFailureHint", func(t *testing.T) {
		stderr := "cat: /Users/r/app/tests/foo.py: No such file or directory"
		assert.Empty(t, buildRelativePathHint(stderr, root, "sub", ""))
	})

	t.Run("nothing resolves anywhere gives no hint", func(t *testing.T) {
		stderr := "ERROR: file or directory not found: tests/foo.py"
		// At the root with no offset, tests/foo.py exists nowhere reachable.
		assert.Empty(t, buildRelativePathHint(stderr, root, "", ""))
	})

	t.Run("unrelated stderr gives no hint", func(t *testing.T) {
		assert.Empty(t, buildRelativePathHint("assertion failed", root, "sub", ""))
	})

	t.Run("only first candidate considered", func(t *testing.T) {
		// A traceback mentioning several paths must not be mined for a match
		// further down; the first "not found" line is the failing one.
		stderr := "ERROR: file or directory not found: tests/typo.py\nERROR: file or directory not found: tests/foo.py"
		assert.Empty(t, buildRelativePathHint(stderr, root, "sub", ""))
	})
}
