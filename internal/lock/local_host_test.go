package lock

import (
	"errors"
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

// A host with local: true takes the same lock as a remote host, on this
// machine, so two rr runs can't both use it.
func TestLocalHostTakesTheLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: time.Second,
		Stale:   time.Minute,
		Dir:     filepath.Join(t.TempDir(), "locks"),
	}
	conn, err := host.NewSelector(map[string]config.Host{"dev": {Local: true}}).SelectHost("dev")
	require.NoError(t, err)

	lck, err := TryAcquire(conn, cfg, "go test ./...")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(cfg.Dir, "rr.lock", "info.json"))
	require.NoError(t, err, "the lock lives on this machine")
	info, err := ParseLockInfo(data)
	require.NoError(t, err)
	assert.Equal(t, "go test ./...", info.Command)

	// A second run sees the host as locked, and GetLockInfo reports the holder.
	other, err := host.NewSelector(map[string]config.Host{"dev": {Local: true}}).SelectHost("dev")
	require.NoError(t, err)
	_, err = TryAcquire(other, cfg, "make lint")
	assert.True(t, errors.Is(err, ErrLocked), "got %v", err)
	assert.True(t, IsLocked(other, cfg))
	require.NotNil(t, GetLockInfo(other, cfg))

	require.NoError(t, lck.Release())
	_, statErr := os.Stat(filepath.Join(cfg.Dir, "rr.lock"))
	assert.True(t, os.IsNotExist(statErr), "release removes the lock dir")

	again, err := TryAcquire(other, cfg, "make lint")
	require.NoError(t, err)
	require.NoError(t, again.Release())
}
