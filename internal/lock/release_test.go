package lock

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
)

// A run releases its lock early, as soon as the command finishes, and again
// when its workflow closes. If another run takes the lock in between, the
// second release and any late info write must leave that run's lock alone.
func TestRelease_LateCallsDontTouchTheNextHoldersLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: time.Second,
		Stale:   10 * time.Minute,
		Dir:     filepath.Join(t.TempDir(), "locks"),
	}
	conn := host.NewLocalHostConnection("dev", config.Host{Local: true})

	first, err := TryAcquire(conn, cfg, "first run")
	require.NoError(t, err)
	require.NoError(t, first.Release())

	next, err := TryAcquire(conn, cfg, "next run")
	require.NoError(t, err)
	t.Cleanup(func() { _ = next.Release() })

	assert.NoError(t, first.Release(), "a second release is a no-op")
	assert.NoError(t, first.UpdateCommand("late update"))
	assert.NoError(t, first.SetJobPGID(12345))

	require.True(t, IsLocked(conn, cfg), "the next run's lock is still there")
	info := GetLockInfo(conn, cfg)
	require.NotNil(t, info)
	assert.Equal(t, "next run", info.Command)
	assert.Zero(t, info.JobPGID)
}

// A release whose removal fails leaves the lock held by this run, so a
// later release (the workflow's close) retries instead of returning nil and
// leaving the lock for others to wait out until it's stale.
func TestRelease_FailedRemovalIsRetried(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only dir")
	}
	base := filepath.Join(t.TempDir(), "locks")
	cfg := config.LockConfig{Enabled: true, Timeout: time.Second, Stale: 10 * time.Minute, Dir: base}
	conn := host.NewLocalHostConnection("dev", config.Host{Local: true})

	l, err := TryAcquire(conn, cfg, "run")
	require.NoError(t, err)

	// rm -rf can't unlink entries from a read-only parent.
	require.NoError(t, os.Chmod(base, 0o555))
	t.Cleanup(func() { _ = os.Chmod(base, 0o755) })
	require.Error(t, l.Release())
	require.True(t, IsLocked(conn, cfg))

	require.NoError(t, os.Chmod(base, 0o755))
	require.NoError(t, l.Release(), "the retry removes it")
	assert.False(t, IsLocked(conn, cfg))
}
