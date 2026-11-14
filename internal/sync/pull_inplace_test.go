package sync

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	rrerrors "github.com/rileyhilliard/rr/internal/errors"
)

func inPlaceTree(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses rsync")
	}
	if _, err := FindRsync(); err != nil {
		t.Skip("rsync not installed")
	}
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "coverage"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "coverage", "lcov.info"), []byte("lcov"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a.xml"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "b.xml"), []byte("b"), 0o644))
	return src
}

func TestPullInPlace_CopiesToAnotherDest(t *testing.T) {
	src := inPlaceTree(t)
	dest := filepath.Join(t.TempDir(), "artifacts")

	copied, err := PullInPlace(src, PullOptions{
		Patterns: []config.PullItem{{Src: "*.xml", Dest: dest}, {Src: "coverage/", Dest: filepath.Join(dest, "cov")}},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, copied)

	for _, p := range []string{"a.xml", "b.xml", filepath.Join("cov", "lcov.info")} {
		_, statErr := os.Stat(filepath.Join(dest, p))
		assert.NoError(t, statErr, p)
	}
}

// The default dest is the cwd. When that's the dir the command ran in, the
// files are already there and nothing is copied.
func TestPullInPlace_SameDirCopiesNothing(t *testing.T) {
	src := inPlaceTree(t)
	t.Chdir(src)

	copied, err := PullInPlace(src, PullOptions{Patterns: []config.PullItem{{Src: "a.xml"}}}, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, copied)

	copied, err = PullInPlace(src, PullOptions{Patterns: []config.PullItem{{Src: "a.xml", Dest: "./"}}, DefaultDest: "."}, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, copied)
}

func TestPullInPlace_MissingFileFails(t *testing.T) {
	src := inPlaceTree(t)
	_, err := PullInPlace(src, PullOptions{Patterns: []config.PullItem{{Src: "nope-*.xml", Dest: t.TempDir()}}}, nil)
	require.Error(t, err)
	assert.True(t, rrerrors.IsCode(err, rrerrors.ErrSync), "got %v", err)
	assert.Contains(t, err.Error(), "nope-*.xml")
}

// A pattern that matches nothing doesn't stop the others from being copied,
// as on a remote, where rsync copies what it finds and exits 23.
func TestPullInPlace_MissingPatternCopiesTheRest(t *testing.T) {
	src := inPlaceTree(t)
	dest := t.TempDir()

	copied, err := PullInPlace(src, PullOptions{
		Patterns: []config.PullItem{{Src: "a.xml", Dest: dest}, {Src: "nope.xml", Dest: dest}},
	}, nil)
	require.Error(t, err)
	assert.True(t, rrerrors.IsCode(err, rrerrors.ErrSync), "got %v", err)
	assert.Contains(t, err.Error(), "nope.xml")
	assert.NotContains(t, err.Error(), "a.xml")
	assert.Equal(t, 1, copied)

	_, statErr := os.Stat(filepath.Join(dest, "a.xml"))
	assert.NoError(t, statErr, "the matched pattern is still copied")
}

// A dest inside the dir being pulled (a parallel subtask's
// reports/<task>_<n>/ under reports/) isn't copied into itself, so repeated
// runs don't nest it deeper each time.
func TestPullInPlace_DestInsideSourceDoesNotNest(t *testing.T) {
	for _, pattern := range []string{"reports/", "reports"} {
		t.Run(pattern, func(t *testing.T) {
			src := inPlaceTree(t)
			require.NoError(t, os.MkdirAll(filepath.Join(src, "reports"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(src, "reports", "r.xml"), []byte("r"), 0o644))
			dest := filepath.Join(src, "reports", "unit_0")

			for range 3 {
				_, err := PullInPlace(src, PullOptions{Patterns: []config.PullItem{{Src: pattern, Dest: dest}}}, nil)
				require.NoError(t, err)
			}

			copyRoot := dest
			if pattern == "reports" {
				copyRoot = filepath.Join(dest, "reports")
			}
			_, statErr := os.Stat(filepath.Join(copyRoot, "r.xml"))
			assert.NoError(t, statErr, "the source is copied")
			_, statErr = os.Stat(filepath.Join(copyRoot, "unit_0"))
			assert.True(t, os.IsNotExist(statErr), "the dest isn't copied into itself")
		})
	}
}
