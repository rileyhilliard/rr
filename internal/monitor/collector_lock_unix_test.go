//go:build !windows

package monitor

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
)

// The monitor shows a lock as held while its job runs on this machine, past
// the stale threshold, the same way lock.Acquire refuses to steal it.
func TestCollector_parseLockSection_LiveLocalJobIsNotStale(t *testing.T) {
	job := exec.Command("sleep", "300")
	job.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	require.NoError(t, job.Start())
	t.Cleanup(func() {
		_ = syscall.Kill(-job.Process.Pid, syscall.SIGKILL)
		_ = job.Wait()
	})

	c := NewCollector(map[string]config.Host{})
	c.SetLockConfig(config.LockConfig{Stale: 5 * time.Minute})

	info, err := lock.NewLockInfo("go test ./...")
	require.NoError(t, err)
	info.Started = time.Now().Add(-time.Hour)
	info.JobPGID = job.Process.Pid
	data, err := info.Marshal()
	require.NoError(t, err)

	got := c.parseLockSection(string(data))
	require.NotNil(t, got, "a running job keeps the lock")
	assert.True(t, got.IsLocked)

	info.JobPGID = 0
	data, err = info.Marshal()
	require.NoError(t, err)
	assert.Nil(t, c.parseLockSection(string(data)), "without a job, age alone makes it stale")
}
