package integration

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Load Balancing Config Tests
// =============================================================================

func TestLoadBalancing_ConfigWaitTimeout(t *testing.T) {
	t.Run("default wait_timeout is 1 minute", func(t *testing.T) {
		cfg := config.DefaultConfig()
		assert.Equal(t, 1*time.Minute, cfg.Lock.WaitTimeout)
	})

	t.Run("wait_timeout can be customized", func(t *testing.T) {
		dir := t.TempDir()
		configContent := `
version: 1
hosts:
  test-host:
    ssh:
      - localhost
    dir: /tmp/test
lock:
  enabled: true
  wait_timeout: 2m30s
`
		configPath := dir + "/.rr.yaml"
		err := os.WriteFile(configPath, []byte(configContent), 0644)
		require.NoError(t, err)

		cfg, err := config.Load(configPath)
		require.NoError(t, err)

		assert.Equal(t, 2*time.Minute+30*time.Second, cfg.Lock.WaitTimeout)
	})
}

// =============================================================================
// TryAcquire Tests
// =============================================================================

func TestLoadBalancing_TryAcquire_ReturnsImmediately(t *testing.T) {
	conn, mock := newMockConnectionLB("testhost")

	// Pre-create a lock
	mock.GetFS().Mkdir("/tmp/rr-abc123.lock")
	info := &lock.LockInfo{
		User:     "other",
		Hostname: "otherhost",
		Started:  time.Now(),
		PID:      9999,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr-abc123.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Minute, // Long timeout
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// TryAcquire should return immediately, not wait for timeout
	start := time.Now()
	_, err := lock.TryAcquire(conn, cfg, "abc123")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, lock.ErrLocked))
	assert.Less(t, elapsed, 1*time.Second, "TryAcquire should return immediately")
}

func TestLoadBalancing_TryAcquire_SucceedsWhenFree(t *testing.T) {
	conn, mock := newMockConnectionLB("testhost")

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	lck, err := lock.TryAcquire(conn, cfg, "newlock")
	require.NoError(t, err)
	require.NotNil(t, lck)

	// Verify lock was created
	assert.True(t, mock.GetFS().IsDir("/tmp/rr-newlock.lock"))

	// Cleanup
	lck.Release()
}

// =============================================================================
// Multi-Host Selection Tests
// =============================================================================

func TestLoadBalancing_GetHostNames_Alphabetical(t *testing.T) {
	hosts := map[string]config.Host{
		"zebra": {SSH: []string{"localhost"}, Dir: "/tmp"},
		"alpha": {SSH: []string{"localhost"}, Dir: "/tmp"},
		"mango": {SSH: []string{"localhost"}, Dir: "/tmp"},
	}

	selector := host.NewSelector(hosts)
	names := selector.GetHostNames()

	require.Len(t, names, 3)
	assert.Equal(t, []string{"alpha", "mango", "zebra"}, names)
}

func TestLoadBalancing_MultipleHosts_Config(t *testing.T) {
	dir := t.TempDir()
	configContent := `
version: 1
hosts:
  gpu-box:
    ssh:
      - gpu.local
      - gpu.vpn
    dir: /home/user/projects
  cpu-box:
    ssh:
      - cpu.local
    dir: /home/user/projects
default: gpu-box
local_fallback: true
lock:
  enabled: true
  wait_timeout: 30s
`
	configPath := dir + "/.rr.yaml"
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Verify multiple hosts
	assert.Len(t, cfg.Hosts, 2)
	assert.Contains(t, cfg.Hosts, "gpu-box")
	assert.Contains(t, cfg.Hosts, "cpu-box")

	// Verify local_fallback
	assert.True(t, cfg.LocalFallback)

	// Verify wait_timeout
	assert.Equal(t, 30*time.Second, cfg.Lock.WaitTimeout)
}

// =============================================================================
// IsLocked Tests
// =============================================================================

func TestLoadBalancing_IsLocked(t *testing.T) {
	t.Run("returns true when lock exists", func(t *testing.T) {
		conn, mock := newMockConnectionLB("testhost")

		// Create a lock
		mock.GetFS().Mkdir("/tmp/rr-test.lock")
		info := &lock.LockInfo{
			User:     "holder",
			Hostname: "holderhost",
			Started:  time.Now(),
			PID:      1234,
		}
		infoJSON, _ := info.Marshal()
		mock.GetFS().WriteFile("/tmp/rr-test.lock/info.json", infoJSON)

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute,
			Dir:     "/tmp",
		}

		assert.True(t, lock.IsLocked(conn, cfg, "test"))
	})

	t.Run("returns false when no lock", func(t *testing.T) {
		conn, _ := newMockConnectionLB("testhost")

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute,
			Dir:     "/tmp",
		}

		assert.False(t, lock.IsLocked(conn, cfg, "nonexistent"))
	})

	t.Run("returns false for stale lock", func(t *testing.T) {
		conn, mock := newMockConnectionLB("testhost")

		// Create an old lock
		mock.GetFS().Mkdir("/tmp/rr-stale.lock")
		info := &lock.LockInfo{
			User:     "old",
			Hostname: "oldhost",
			Started:  time.Now().Add(-1 * time.Hour),
			PID:      1234,
		}
		infoJSON, _ := info.Marshal()
		mock.GetFS().WriteFile("/tmp/rr-stale.lock/info.json", infoJSON)

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute, // Lock is older than 10 min
			Dir:     "/tmp",
		}

		assert.False(t, lock.IsLocked(conn, cfg, "stale"))
	})
}

// =============================================================================
// GetLockHolder Tests
// =============================================================================

func TestLoadBalancing_GetLockHolder(t *testing.T) {
	t.Run("returns holder info when locked", func(t *testing.T) {
		conn, mock := newMockConnectionLB("testhost")

		mock.GetFS().Mkdir("/tmp/rr-held.lock")
		info := &lock.LockInfo{
			User:     "alice",
			Hostname: "workstation",
			Started:  time.Now(),
			PID:      12345,
		}
		infoJSON, _ := info.Marshal()
		mock.GetFS().WriteFile("/tmp/rr-held.lock/info.json", infoJSON)

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute,
			Dir:     "/tmp",
		}

		holder := lock.GetLockHolder(conn, cfg, "held")
		assert.Contains(t, holder, "alice")
		assert.Contains(t, holder, "workstation")
	})

	t.Run("returns empty when not locked", func(t *testing.T) {
		conn, _ := newMockConnectionLB("testhost")

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute,
			Dir:     "/tmp",
		}

		holder := lock.GetLockHolder(conn, cfg, "notlocked")
		assert.Empty(t, holder)
	})
}

// =============================================================================
// Sequential Lock Acquisition Simulation
// =============================================================================

func TestLoadBalancing_SequentialLockAttempts(t *testing.T) {
	// Simulate the scenario: task1 holds lock on host1, task2 should try host2

	// Host 1: has a lock
	conn1, mock1 := newMockConnectionLB("host1")
	mock1.GetFS().Mkdir("/tmp/rr-project.lock")
	info1 := &lock.LockInfo{
		User:     "task1",
		Hostname: "host1",
		Started:  time.Now(),
		PID:      1111,
	}
	infoJSON1, _ := info1.Marshal()
	mock1.GetFS().WriteFile("/tmp/rr-project.lock/info.json", infoJSON1)

	// Host 2: no lock
	conn2, _ := newMockConnectionLB("host2")

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// Try host1 - should return ErrLocked immediately
	_, err := lock.TryAcquire(conn1, cfg, "project")
	require.Error(t, err)
	assert.True(t, errors.Is(err, lock.ErrLocked))

	// Try host2 - should succeed
	lck2, err := lock.TryAcquire(conn2, cfg, "project")
	require.NoError(t, err)
	require.NotNil(t, lck2)

	// Cleanup
	lck2.Release()
}

// =============================================================================
// Helpers
// =============================================================================

// newMockConnectionLB creates a mock connection for load balancing tests.
// Named differently to avoid conflict with other test files.
func newMockConnectionLB(hostName string) (*host.Connection, *sshtesting.MockClient) {
	mock := sshtesting.NewMockClient(hostName)
	conn := &host.Connection{
		Name:   hostName,
		Alias:  hostName,
		Client: mock,
	}
	return conn, mock
}
