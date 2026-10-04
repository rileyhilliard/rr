package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	rrerrors "github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// heldLocalLock takes a local host's lock as another run would and returns
// the lock config and a second connection to wait on it with.
func heldLocalLock(t *testing.T, timeout time.Duration) (config.LockConfig, *host.Connection) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: timeout,
		Stale:   10 * time.Minute,
		Dir:     filepath.Join(t.TempDir(), "locks"),
	}
	held, err := TryAcquire(host.NewLocalHostConnection("dev", config.Host{Local: true}), cfg, "go test ./... (other run)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Release() })
	return cfg, host.NewLocalHostConnection("dev", config.Host{Local: true})
}

// Cancelling the context while Acquire waits on a held lock stops the wait
// at once, well before the next retry, with an error that is a cancellation
// and not a held lock.
func TestAcquire_CancelStopsWait(t *testing.T) {
	cfg, conn := heldLocalLock(t, time.Minute)
	defer SetRetryIntervalForTesting(time.Minute)()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := Acquire(conn, cfg, "make test", WithContext(ctx),
			WithWaitFunc(func(*LockInfo) { close(waiting) }))
		done <- err
	}()

	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("Acquire never started waiting")
	}
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled), "got %v", err)
		assert.True(t, rrerrors.IsCode(err, rrerrors.ErrInterrupted), "got %v", err)
		assert.False(t, rrerrors.IsCode(err, rrerrors.ErrLock), "a cancel isn't a held lock")
		assert.NotContains(t, err.Error(), "context canceled", "says why in words, not Go's")
		assert.Contains(t, err.Error(), "rr was stopped")
	case <-time.After(5 * time.Second):
		t.Fatal("Acquire kept waiting after its context was cancelled")
	}
	assert.True(t, IsHeld(conn, cfg), "the other run still holds the lock")
}

// A context that's already done stops Acquire before it takes a free lock.
func TestAcquire_CancelledBeforeStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	cfg := config.LockConfig{Enabled: true, Timeout: time.Second, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	conn := host.NewLocalHostConnection("dev", config.Host{Local: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Acquire(conn, cfg, "make test", WithContext(ctx))
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "got %v", err)
	assert.False(t, IsLocked(conn, cfg), "the lock wasn't taken")
}

// The wait callback fires once, on the first failed attempt, with the
// holder's info; later retries don't repeat it.
func TestAcquire_WaitFuncReportsHolderOnce(t *testing.T) {
	cfg, conn := heldLocalLock(t, 300*time.Millisecond)
	defer SetRetryIntervalForTesting(20 * time.Millisecond)()

	var holders []*LockInfo
	_, err := Acquire(conn, cfg, "make test", WithWaitFunc(func(holder *LockInfo) {
		holders = append(holders, holder)
	}))
	require.Error(t, err)
	assert.True(t, rrerrors.IsCode(err, rrerrors.ErrLock), "times out on the held lock: %v", err)

	require.Len(t, holders, 1, "called once, not per retry")
	require.NotNil(t, holders[0])
	assert.Equal(t, "go test ./... (other run)", holders[0].Command)
}

// A free lock is taken on the first attempt, with no wait to report.
func TestAcquire_WaitFuncNotCalledWhenFree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	cfg := config.LockConfig{Enabled: true, Timeout: time.Second, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	called := false
	lck, err := Acquire(host.NewLocalHostConnection("dev", config.Host{Local: true}), cfg, "make test",
		WithWaitFunc(func(*LockInfo) { called = true }))
	require.NoError(t, err)
	require.NoError(t, lck.Release())
	assert.False(t, called)
}

// A run that loses the mkdir race sees the lock dir before the holder has
// written its info file. The wait is reported with the holder once the file
// appears, not as an unknown holder.
func TestAcquire_WaitFuncWaitsBrieflyForHolderInfo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	cfg := config.LockConfig{Enabled: true, Timeout: 300 * time.Millisecond, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	defer SetRetryIntervalForTesting(20 * time.Millisecond)()
	lockDir := filepath.Join(cfg.Dir, "rr.lock")
	require.NoError(t, os.MkdirAll(lockDir, 0o755))
	info, err := NewLockInfo("go test ./... (other run)")
	require.NoError(t, err)
	data, err := info.Marshal()
	require.NoError(t, err)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(lockDir, "info.json"), data, 0o644)
	}()

	var holders []*LockInfo
	_, err = Acquire(host.NewLocalHostConnection("dev", config.Host{Local: true}), cfg, "make test",
		WithWaitFunc(func(h *LockInfo) { holders = append(holders, h) }))
	require.Error(t, err, "the other run still holds the lock")
	require.Len(t, holders, 1)
	require.NotNil(t, holders[0], "the holder was identified once its info file appeared")
	assert.Equal(t, "go test ./... (other run)", holders[0].Command)
}
