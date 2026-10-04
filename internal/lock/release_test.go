package lock

import (
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
