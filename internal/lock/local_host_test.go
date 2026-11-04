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

// Some mkdir implementations (uutils coreutils, Ubuntu 26's default) can
// report success to both of two racing calls. The run that loses that race
// must still see the host as locked, and must leave the winner's lock alone.
// The fake mkdir here succeeds every time, as the loser's mkdir does.
func TestLockHeldWhenMkdirReportsAFalseSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	tests := []struct {
		name    string
		acquire func(conn *host.Connection, cfg config.LockConfig) (*Lock, error)
	}{
		{"TryAcquire", func(conn *host.Connection, cfg config.LockConfig) (*Lock, error) {
			return TryAcquire(conn, cfg, "make lint")
		}},
		{"Acquire", func(conn *host.Connection, cfg config.LockConfig) (*Lock, error) {
			return Acquire(conn, cfg, "make lint")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer SetRetryIntervalForTesting(20 * time.Millisecond)()
			cfg := config.LockConfig{
				Enabled: true,
				Timeout: 200 * time.Millisecond,
				Stale:   time.Minute,
				Dir:     filepath.Join(t.TempDir(), "locks"),
			}
			holderConn := host.NewLocalHostConnection("dev", config.Host{Local: true})
			holder, err := TryAcquire(holderConn, cfg, "go test ./...")
			require.NoError(t, err)

			fakeBin := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "mkdir"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
			t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

			other := host.NewLocalHostConnection("dev", config.Host{Local: true})
			got, err := tt.acquire(other, cfg)
			require.Error(t, err, "the second run must not get the lock")
			assert.Nil(t, got)

			data, err := os.ReadFile(filepath.Join(cfg.Dir, "rr.lock", "info.json"))
			require.NoError(t, err, "the holder's lock is still there")
			info, err := ParseLockInfo(data)
			require.NoError(t, err)
			assert.Equal(t, "go test ./...", info.Command, "the holder's info is unchanged")

			require.NoError(t, holder.Release())
		})
	}
}
