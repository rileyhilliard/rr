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
			want:      "pytest ~/rr/app/tests/foo.py -v",
			wantCount: 1,
		},
		{
			name:      "exact root at end of command",
			cmd:       "cd /Users/r/app",
			want:      "cd ~/rr/app",
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
			name:      "inside single quotes",
			cmd:       "cd '/Users/r/app/sub dir'",
			want:      "cd '~/rr/app/sub dir'",
			wantCount: 1,
		},
		{
			name:      "multiple occurrences",
			cmd:       "cp /Users/r/app/a.txt /Users/r/app/b.txt",
			want:      "cp ~/rr/app/a.txt ~/rr/app/b.txt",
			wantCount: 2,
		},
		{
			name:      "colon suffix is a boundary",
			cmd:       "vim /Users/r/app:12",
			want:      "vim ~/rr/app:12",
			wantCount: 1,
		},
		{
			name:      "pytest node id keeps selector",
			cmd:       "pytest /Users/r/app/tests/foo.py::test_x",
			want:      "pytest ~/rr/app/tests/foo.py::test_x",
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
	assert.Equal(t, "cat ~/rr/app/f.txt", got)
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
