package testing

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeLockManager_Acquire_Success(t *testing.T) {
	mgr := NewFakeLockManager()

	conn := &host.Connection{Name: "test-host"}
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
	}

	lock, err := mgr.Acquire(conn, cfg, "")

	require.NoError(t, err)
	require.NotNil(t, lock)
	assert.Equal(t, "test-host", lock.Holder)
	assert.False(t, lock.Released)
	assert.True(t, mgr.AssertAcquireCalled())
}

func TestFakeLockManager_Acquire_Failure(t *testing.T) {
	mgr := NewFakeLockManager().SetFail(nil)

	conn := &host.Connection{Name: "test-host"}
	cfg := config.LockConfig{Enabled: true}

	lock, err := mgr.Acquire(conn, cfg, "")

	assert.Error(t, err)
	assert.Nil(t, lock)
	assert.Equal(t, 1, len(mgr.AcquireCalls))
	assert.False(t, mgr.AcquireCalls[0].Success)
}

func TestFakeLockManager_Contention(t *testing.T) {
	mgr := NewFakeLockManager().SetContention("other-user", 100*time.Millisecond)

	conn := &host.Connection{Name: "test-host"}
	cfg := config.LockConfig{Enabled: true}

	// Should fail while lock is held
	_, err := mgr.Acquire(conn, cfg, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "other-user")

	// Wait for contention to clear
	time.Sleep(150 * time.Millisecond)

	// Should succeed now
	lock, err := mgr.Acquire(conn, cfg, "")
	require.NoError(t, err)
	assert.NotNil(t, lock)
}

func TestFakeLockManager_Release(t *testing.T) {
	mgr := NewFakeLockManager()

	conn := &host.Connection{Name: "test-host"}
	lock, err := mgr.Acquire(conn, config.LockConfig{}, "")
	require.NoError(t, err)

	assert.False(t, lock.Released)

	err = lock.Release()
	require.NoError(t, err)
	assert.True(t, lock.Released)
}

func TestFakeLockManager_Delay(t *testing.T) {
	mgr := NewFakeLockManager().SetDelay(50 * time.Millisecond)

	conn := &host.Connection{Name: "test-host"}

	start := time.Now()
	_, err := mgr.Acquire(conn, config.LockConfig{}, "")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
}

func TestFakeLockManager_SuccessfulAcquires(t *testing.T) {
	mgr := NewFakeLockManager()
	conn := &host.Connection{Name: "test-host"}

	// Two successful acquires
	mgr.Acquire(conn, config.LockConfig{}, "")
	mgr.Acquire(conn, config.LockConfig{}, "")

	// One failed acquire
	mgr.SetFail(nil)
	mgr.Acquire(conn, config.LockConfig{}, "")

	assert.Equal(t, 2, mgr.SuccessfulAcquires())
	assert.True(t, mgr.AssertAcquireCount(3))
}

func TestFakeLockManager_Reset(t *testing.T) {
	mgr := NewFakeLockManager()
	conn := &host.Connection{Name: "test-host"}

	mgr.Acquire(conn, config.LockConfig{}, "")
	assert.True(t, mgr.AssertAcquireCalled())

	mgr.Reset()
	assert.False(t, mgr.AssertAcquireCalled())
}
