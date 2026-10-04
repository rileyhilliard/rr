package monitor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
)

// A host with local: true is monitored by running the metrics commands on
// this machine, lock status included.
func TestCollector_LocalHost(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("metrics commands support linux and darwin")
	}
	if testing.Short() {
		t.Skip("runs the real metrics commands (several seconds on macOS)")
	}
	lockBase := t.TempDir()
	lockDir := filepath.Join(lockBase, "rr.lock")
	require.NoError(t, os.MkdirAll(lockDir, 0o755))
	info, err := lock.NewLockInfo("rr test")
	require.NoError(t, err)
	data, err := info.Marshal()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(lockDir, "info.json"), data, 0o644))

	c := NewCollector(map[string]config.Host{"dev": {Local: true}})
	c.SetLockConfig(config.LockConfig{Dir: lockBase, Stale: time.Minute})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var results []HostResult
	for r := range c.CollectStreaming(ctx) {
		results = append(results, r)
	}

	require.Len(t, results, 1)
	r := results[0]
	require.NoError(t, r.Error)
	require.NotNil(t, r.Metrics)
	assert.Positive(t, r.Metrics.RAM.TotalBytes)
	assert.Equal(t, "local", r.ConnectedVia)
	require.NotNil(t, r.LockInfo)
	assert.True(t, r.LockInfo.IsLocked)
	assert.Equal(t, "rr test", r.LockInfo.Command)
}

func TestCollector_SnapshotLocalHost(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("metrics commands support linux and darwin")
	}
	if testing.Short() {
		t.Skip("runs the real metrics commands (several seconds on macOS)")
	}
	c := NewCollector(map[string]config.Host{"dev": {Local: true}})
	c.SetLockConfig(config.LockConfig{Dir: t.TempDir(), Stale: time.Minute})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results := c.CollectSnapshot(ctx)

	require.Len(t, results, 1)
	require.NoError(t, results[0].Error)
	require.NotNil(t, results[0].Metrics)
	assert.Positive(t, results[0].Metrics.RAM.TotalBytes)
	assert.Equal(t, "local", results[0].ConnectedVia)
	assert.Nil(t, results[0].LockInfo, "no lock held")
}
