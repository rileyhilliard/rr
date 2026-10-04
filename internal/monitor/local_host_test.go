package monitor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local metrics run under sh")
	}
}

// A local command that outlives its deadline is stopped with everything it
// started, and the error is the deadline, not "signal: killed".
func TestRunLocal_TimeoutStopsTheCommandTree(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	c := NewCollector(map[string]config.Host{"dev": {Local: true}})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := c.runLocal(ctx, func(Platform, string) string { return "sleep 3; echo late" })
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 1500*time.Millisecond, "doesn't wait for the grandchild")
}

// writeLockInfo writes a held lock's info.json under lockBase.
func writeLockInfo(t *testing.T, lockBase, command string) {
	t.Helper()
	lockDir := filepath.Join(lockBase, "rr.lock")
	require.NoError(t, os.MkdirAll(lockDir, 0o755))
	info, err := lock.NewLockInfo(command)
	require.NoError(t, err)
	data, err := info.Marshal()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(lockDir, "info.json"), data, 0o644))
}

// cannedCommand writes payload to a file and returns a builder whose command
// prints it, then the real lock section for the lock dir it's given. It
// records the platform and lock dir it was called with.
func cannedCommand(t *testing.T, payload string, gotPlatform *Platform, gotLockDir *string) func(Platform, string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "payload")
	require.NoError(t, os.WriteFile(file, []byte(payload), 0o644))
	return func(p Platform, lockDir string) string {
		*gotPlatform, *gotLockDir = p, lockDir
		return "cat '" + file + "'; " + buildLockSection(lockDir)
	}
}

// A streaming round on a local host runs this machine's metrics command
// locally, through the collector's lock dir, and reports it as local.
func TestCollector_LocalHost(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	lockBase := t.TempDir()
	writeLockInfo(t, lockBase, "rr test")

	c := NewCollector(map[string]config.Host{"dev": {Local: true}})
	c.SetLockConfig(config.LockConfig{Dir: lockBase, Stale: time.Minute})
	defer c.Close()

	// The regular Linux sections of the snapshot fixture, with the lock
	// section left to the real command.
	sep := "\n" + OutputSeparator + "\n"
	sections := strings.Split(buildLinuxSnapshotFixture(), sep)
	regular := sections[SnapshotPrimeSections(PlatformLinux) : len(sections)-1]
	var platform Platform
	var lockDir string
	c.localPlatform = PlatformLinux
	c.buildMetrics = cannedCommand(t, strings.Join(regular, sep)+sep, &platform, &lockDir)

	var results []HostResult
	for r := range c.CollectStreaming(context.Background()) {
		results = append(results, r)
	}

	require.Len(t, results, 1)
	r := results[0]
	require.NoError(t, r.Error)
	assert.Equal(t, PlatformLinux, platform)
	assert.Equal(t, filepath.Join(lockBase, "rr.lock"), lockDir)
	require.NotNil(t, r.Metrics)
	assert.Equal(t, int64(16000000*1024), r.Metrics.RAM.TotalBytes)
	assert.Equal(t, "local", r.ConnectedVia)
	require.NotNil(t, r.LockInfo)
	assert.True(t, r.LockInfo.IsLocked)
	assert.Equal(t, "rr test", r.LockInfo.Command)
}

func TestCollector_SnapshotLocalHost(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	c := NewCollector(map[string]config.Host{"dev": {Local: true}})
	c.SetLockConfig(config.LockConfig{Dir: t.TempDir(), Stale: time.Minute})
	defer c.Close()

	sep := "\n" + OutputSeparator + "\n"
	sections := strings.Split(buildLinuxSnapshotFixture(), sep)
	var platform Platform
	var lockDir string
	c.localPlatform = PlatformLinux
	c.buildSnapshot = cannedCommand(t, strings.Join(sections[:len(sections)-1], sep)+sep, &platform, &lockDir)

	results := c.CollectSnapshot(context.Background())

	require.Len(t, results, 1)
	require.NoError(t, results[0].Error)
	assert.Equal(t, PlatformLinux, results[0].Platform)
	require.NotNil(t, results[0].Metrics)
	assert.InDelta(t, 50.0, results[0].Metrics.CPU.Percent, 0.01, "both samples parsed")
	assert.Equal(t, "local", results[0].ConnectedVia)
	assert.Nil(t, results[0].LockInfo, "no lock held")
}

// The real metrics commands on this machine. They take seconds, so this
// only runs with RR_TEST_REAL_METRICS=1.
func TestCollector_SnapshotLocalHostRealCommands(t *testing.T) {
	skipOnWindows(t)
	if os.Getenv("RR_TEST_REAL_METRICS") == "" {
		t.Skip("set RR_TEST_REAL_METRICS=1 to run the real metrics commands")
	}
	c := NewCollector(map[string]config.Host{"dev": {Local: true}})
	c.SetLockConfig(config.LockConfig{Dir: t.TempDir(), Stale: time.Minute})
	c.SetTimeout(10 * time.Second)
	defer c.Close()

	results := c.CollectSnapshot(context.Background())
	require.Len(t, results, 1)
	require.NoError(t, results[0].Error)
	require.NotNil(t, results[0].Metrics)
	assert.Positive(t, results[0].Metrics.RAM.TotalBytes)
}
