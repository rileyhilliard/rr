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
