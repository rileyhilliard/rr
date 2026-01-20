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
		// Project config (hosts are now in global config)
		configContent := `
version: 1
hosts:
  - test-host
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

	// Pre-create a lock (now per-host, not per-project)
	mock.GetFS().Mkdir("/tmp/rr.lock")
	info := &lock.LockInfo{
		User:     "other",
		Hostname: "otherhost",
		Started:  time.Now(),
		PID:      9999,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Minute, // Long timeout
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// TryAcquire should return immediately, not wait for timeout
	start := time.Now()
	_, err := lock.TryAcquire(conn, cfg, "")
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

	lck, err := lock.TryAcquire(conn, cfg, "")
	require.NoError(t, err)
	require.NotNil(t, lck)

	// Verify lock was created (per-host lock)
	assert.True(t, mock.GetFS().IsDir("/tmp/rr.lock"))

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

	// Set up global config with hosts
	globalDir := dir + "/.rr"
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
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
defaults:
  local_fallback: true
`
	err := os.WriteFile(globalDir+"/config.yaml", []byte(globalContent), 0644)
	require.NoError(t, err)
	t.Setenv("HOME", dir)

	globalCfg, err := config.LoadGlobal()
	require.NoError(t, err)

	// Verify multiple hosts
	assert.Len(t, globalCfg.Hosts, 2)
	assert.Contains(t, globalCfg.Hosts, "gpu-box")
	assert.Contains(t, globalCfg.Hosts, "cpu-box")

	// Verify local_fallback
	assert.True(t, globalCfg.Defaults.LocalFallback)

	// Write project config with lock settings
	projectContent := `
version: 1
host: gpu-box
lock:
  enabled: true
  wait_timeout: 30s
`
	err = os.WriteFile(dir+"/.rr.yaml", []byte(projectContent), 0644)
	require.NoError(t, err)

	cfg, err := config.Load(dir + "/.rr.yaml")
	require.NoError(t, err)

	// Verify wait_timeout
	assert.Equal(t, 30*time.Second, cfg.Lock.WaitTimeout)
}

// =============================================================================
// IsLocked Tests
// =============================================================================

func TestLoadBalancing_IsLocked(t *testing.T) {
	t.Run("returns true when lock exists", func(t *testing.T) {
		conn, mock := newMockConnectionLB("testhost")

		// Create a lock (per-host lock)
		mock.GetFS().Mkdir("/tmp/rr.lock")
		info := &lock.LockInfo{
			User:     "holder",
			Hostname: "holderhost",
			Started:  time.Now(),
			PID:      1234,
		}
		infoJSON, _ := info.Marshal()
		mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute,
			Dir:     "/tmp",
		}

		assert.True(t, lock.IsLocked(conn, cfg))
	})

	t.Run("returns false when no lock", func(t *testing.T) {
		conn, _ := newMockConnectionLB("testhost")

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute,
			Dir:     "/tmp",
		}

		assert.False(t, lock.IsLocked(conn, cfg))
	})

	t.Run("returns false for stale lock", func(t *testing.T) {
		conn, mock := newMockConnectionLB("testhost")

		// Create an old lock (per-host lock)
		mock.GetFS().Mkdir("/tmp/rr.lock")
		info := &lock.LockInfo{
			User:     "old",
			Hostname: "oldhost",
			Started:  time.Now().Add(-1 * time.Hour),
			PID:      1234,
		}
		infoJSON, _ := info.Marshal()
		mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute, // Lock is older than 10 min
			Dir:     "/tmp",
		}

		assert.False(t, lock.IsLocked(conn, cfg))
	})
}

// =============================================================================
// GetLockHolder Tests
// =============================================================================

func TestLoadBalancing_GetLockHolder(t *testing.T) {
	t.Run("returns holder info when locked", func(t *testing.T) {
		conn, mock := newMockConnectionLB("testhost")

		// Per-host lock
		mock.GetFS().Mkdir("/tmp/rr.lock")
		info := &lock.LockInfo{
			User:     "alice",
			Hostname: "workstation",
			Started:  time.Now(),
			PID:      12345,
		}
		infoJSON, _ := info.Marshal()
		mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

		cfg := config.LockConfig{
			Enabled: true,
			Stale:   10 * time.Minute,
			Dir:     "/tmp",
		}

		holder := lock.GetLockHolder(conn, cfg)
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

		holder := lock.GetLockHolder(conn, cfg)
		assert.Empty(t, holder)
	})
}

// =============================================================================
// Sequential Lock Acquisition Simulation
// =============================================================================

func TestLoadBalancing_SequentialLockAttempts(t *testing.T) {
	// Simulate the scenario: task1 holds lock on host1, task2 should try host2
	// With per-host locking, each host has one lock that blocks all tasks

	// Host 1: has a lock (per-host, not per-project)
	conn1, mock1 := newMockConnectionLB("host1")
	mock1.GetFS().Mkdir("/tmp/rr.lock")
	info1 := &lock.LockInfo{
		User:     "task1",
		Hostname: "host1",
		Started:  time.Now(),
		PID:      1111,
	}
	infoJSON1, _ := info1.Marshal()
	mock1.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON1)

	// Host 2: no lock
	conn2, _ := newMockConnectionLB("host2")

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// Try host1 - should return ErrLocked immediately
	_, err := lock.TryAcquire(conn1, cfg, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, lock.ErrLocked))

	// Try host2 - should succeed (each host has independent lock)
	lck2, err := lock.TryAcquire(conn2, cfg, "")
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
