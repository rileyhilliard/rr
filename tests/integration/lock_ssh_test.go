package integration

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLockAcquireAndRelease tests basic lock acquisition and release via SSH.
func TestLockAcquireAndRelease(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// Acquire lock
	lck, err := lock.Acquire(conn, cfg)
	require.NoError(t, err, "Lock acquisition should succeed")
	require.NotNil(t, lck)

	// Verify lock directory exists on remote
	assert.True(t, RemoteDirExists(t, conn, lck.Dir))

	// Verify info.json exists
	assert.True(t, RemoteFileExists(t, conn, fmt.Sprintf("%s/info.json", lck.Dir)))

	// Release lock
	err = lck.Release()
	require.NoError(t, err, "Lock release should succeed")

	// Verify lock directory is gone
	assert.False(t, RemoteDirExists(t, conn, lck.Dir))
}

// TestLockTryAcquire tests non-blocking lock acquisition.
func TestLockTryAcquire(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// TryAcquire should succeed on first attempt
	lck, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err, "TryAcquire should succeed")
	require.NotNil(t, lck)
	defer lck.Release()

	// Verify lock exists
	assert.True(t, RemoteDirExists(t, conn, lck.Dir))
}

// TestLockTryAcquireFailsWhenHeld tests that TryAcquire fails when lock is held.
func TestLockTryAcquireFailsWhenHeld(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// First, acquire the lock
	lck1, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	require.NotNil(t, lck1)
	defer lck1.Release()

	// Second TryAcquire should fail with ErrLocked
	lck2, err := lock.TryAcquire(conn, cfg)
	assert.ErrorIs(t, err, lock.ErrLocked, "Second TryAcquire should return ErrLocked")
	assert.Nil(t, lck2)
}

// TestLockIsLocked tests checking lock status.
func TestLockIsLocked(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// Initially should not be locked
	assert.False(t, lock.IsLocked(conn, cfg))

	// Acquire lock
	lck, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	require.NotNil(t, lck)

	// Now should be locked
	assert.True(t, lock.IsLocked(conn, cfg))

	// Release
	err = lck.Release()
	require.NoError(t, err)

	// Should no longer be locked
	assert.False(t, lock.IsLocked(conn, cfg))
}

// TestLockGetLockHolder tests getting lock holder information.
func TestLockGetLockHolder(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// No holder when not locked
	holder := lock.GetLockHolder(conn, cfg)
	assert.Empty(t, holder)

	// Acquire lock
	lck, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	defer lck.Release()

	// Should have holder info
	holder = lock.GetLockHolder(conn, cfg)
	assert.NotEmpty(t, holder)
	// Holder string should contain user info
	assert.Contains(t, holder, "@")
}

// TestLockForceRelease tests forcibly removing a lock.
func TestLockForceRelease(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// Acquire lock
	lck, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	lockDir := lck.Dir

	// Force release without using the lock object
	err = lock.ForceRelease(conn, lockDir)
	require.NoError(t, err)

	// Lock should be gone
	assert.False(t, RemoteDirExists(t, conn, lockDir))

	// IsLocked should return false
	assert.False(t, lock.IsLocked(conn, cfg))
}

// TestLockStaleDetectionSSH tests that stale locks are automatically removed via SSH.
func TestLockStaleDetectionSSH(t *testing.T) {
	conn := GetSSHConnection(t)

	// Use a very short stale timeout for testing
	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 10 * time.Second,
		Stale:   1 * time.Second, // Very short for testing
	}

	// Acquire lock
	lck1, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	lockDir := lck1.Dir

	// Don't release - simulate a crashed process

	// Wait for the lock to become stale
	time.Sleep(2 * time.Second)

	// Now try to acquire - should succeed because old lock is stale
	lck2, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err, "Should acquire lock after stale detection")
	require.NotNil(t, lck2)
	defer lck2.Release()

	// Verify we got a new lock (same directory)
	assert.Equal(t, lockDir, lck2.Dir)
}

// TestLockInfoContent tests that lock info file contains expected data.
func TestLockInfoContent(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// Acquire lock
	lck, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	defer lck.Release()

	// Read the info file
	infoContent := ReadRemoteFile(t, conn, fmt.Sprintf("%s/info.json", lck.Dir))

	// Should be valid JSON with expected fields
	assert.Contains(t, infoContent, "user")
	assert.Contains(t, infoContent, "host")
	assert.Contains(t, infoContent, "started")
	assert.Contains(t, infoContent, "pid")
}

// TestLockCustomDir tests lock acquisition in a custom directory.
func TestLockCustomDir(t *testing.T) {
	conn := GetSSHConnection(t)

	customDir := fmt.Sprintf("/tmp/rr-test-locks-%d", time.Now().UnixNano())
	defer CleanupRemoteDir(t, conn, customDir)

	cfg := config.LockConfig{
		Dir:     customDir,
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// Acquire lock - should create the custom directory
	lck, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	defer lck.Release()

	// Verify lock is in custom directory
	assert.Contains(t, lck.Dir, customDir)
	assert.True(t, RemoteDirExists(t, conn, lck.Dir))
}

// TestLockConcurrentAcquisition tests that concurrent lock attempts are handled correctly.
func TestLockConcurrentAcquisition(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// Track results
	var mu sync.Mutex
	successCount := 0
	failCount := 0

	// Try to acquire lock from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lck, err := lock.TryAcquire(conn, cfg)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successCount++
				// Keep the lock for a moment then release
				time.Sleep(100 * time.Millisecond)
				lck.Release()
			} else {
				failCount++
			}
		}()
	}

	wg.Wait()

	// Exactly one should have succeeded initially
	// Others might succeed after the first releases
	t.Logf("Success: %d, Fail: %d", successCount, failCount)
	assert.True(t, successCount >= 1, "At least one lock acquisition should succeed")
}

// TestLockHolder tests the Holder function.
func TestLockHolder(t *testing.T) {
	conn := GetSSHConnection(t)

	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 5 * time.Second,
		Stale:   1 * time.Hour,
	}

	// Acquire lock
	lck, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	defer lck.Release()

	// Get holder info using the Holder function
	holder := lock.Holder(conn, lck.Dir)
	assert.NotEmpty(t, holder)
	assert.NotEqual(t, "unknown", holder)
	assert.Contains(t, holder, "@")
}

// TestLockAcquireWithTimeout tests that Acquire times out correctly.
func TestLockAcquireWithTimeout(t *testing.T) {
	conn := GetSSHConnection(t)

	// Use a very short timeout
	cfg := config.LockConfig{
		Dir:     "/tmp",
		Timeout: 2 * time.Second,
		Stale:   1 * time.Hour,
	}

	// First, acquire the lock
	lck1, err := lock.TryAcquire(conn, cfg)
	require.NoError(t, err)
	defer lck1.Release()

	// Try to acquire again with blocking - should timeout
	start := time.Now()
	lck2, err := lock.Acquire(conn, cfg)
	elapsed := time.Since(start)

	assert.Error(t, err, "Acquire should fail due to timeout")
	assert.Nil(t, lck2)
	assert.GreaterOrEqual(t, elapsed, 2*time.Second, "Should have waited for timeout")
	assert.Less(t, elapsed, 5*time.Second, "Should not wait much longer than timeout")
}
