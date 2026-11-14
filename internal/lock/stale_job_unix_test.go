//go:build !windows

package lock

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	rrerrors "github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// startJobGroup starts a long sleep in its own session, as a local host runs
// a job, and returns its process group id. The group is killed on cleanup.
func startJobGroup(t *testing.T) int {
	t.Helper()
	job := exec.Command("sleep", "300")
	job.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	require.NoError(t, job.Start())
	pgid := job.Process.Pid
	// Reap the leader in the background so it doesn't linger as a zombie,
	// which would keep the group "alive" after it's killed.
	go func() { _ = job.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	return pgid
}

// writeStaleLock writes a lock whose heartbeat stopped an hour ago, held by
// a dead pid, as a SIGKILLed rr leaves it.
func writeStaleLock(t *testing.T, cfg config.LockConfig, info *LockInfo) {
	t.Helper()
	lockDir := LockDir(cfg)
	require.NoError(t, os.MkdirAll(lockDir, 0o755))
	data, err := info.Marshal()
	require.NoError(t, err)
	infoFile := filepath.Join(lockDir, "info.json")
	require.NoError(t, os.WriteFile(infoFile, data, 0o644))
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(infoFile, old, old))
}

// A killed rr's heartbeat stops, so its lock goes stale on mtime. While the
// job it started still runs on this machine, the lock isn't stale: the next
// run waits instead of starting beside it. Once the job is gone, the lock is
// taken.
func TestAcquire_StaleLockWithLiveLocalJobIsNotStolen(t *testing.T) {
	cfg := config.LockConfig{Enabled: true, Timeout: 300 * time.Millisecond, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	defer SetRetryIntervalForTesting(20 * time.Millisecond)()
	pgid := startJobGroup(t)

	holder, err := NewLockInfo("go test ./... (killed rr)")
	require.NoError(t, err)
	holder.PID = deadPid(t)
	holder.Started = time.Now().Add(-time.Hour)
	holder.JobPGID = pgid
	writeStaleLock(t, cfg, holder)

	conn := host.NewLocalHostConnection("dev", config.Host{Local: true})
	assert.True(t, IsLocked(conn, cfg), "a running job keeps the lock")
	assert.True(t, IsHeld(conn, cfg))

	_, err = TryAcquire(conn, cfg, "next run")
	assert.ErrorIs(t, err, ErrLocked)

	_, err = Acquire(conn, cfg, "next run")
	require.Error(t, err)
	assert.True(t, rrerrors.IsCode(err, rrerrors.ErrLock), "waits out the timeout instead of stealing: %v", err)
	got := GetLockInfo(conn, cfg)
	require.NotNil(t, got, "the lock is still there")
	assert.Equal(t, pgid, got.JobPGID)

	require.NoError(t, syscall.Kill(-pgid, syscall.SIGKILL))
	require.Eventually(t, func() bool { return !processGroupAlive(pgid) }, 10*time.Second, 20*time.Millisecond)

	assert.False(t, IsHeld(conn, cfg), "with the job gone, the lock is free to take")
	lck, err := Acquire(conn, cfg, "next run")
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), lck.Info.PID)
	require.NoError(t, lck.Release())
}

// The job check only applies to a holder on this machine. A stale lock from
// another machine is stolen on mtime as before, even if a process group with
// the recorded id happens to be running here.
func TestAcquire_StaleLockFromOtherMachineIsStolen(t *testing.T) {
	cfg := config.LockConfig{Enabled: true, Timeout: 300 * time.Millisecond, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	defer SetRetryIntervalForTesting(20 * time.Millisecond)()

	holder := &LockInfo{
		User:         "someone",
		Hostname:     "other-box.invalid",
		Started:      time.Now().Add(-time.Hour),
		PID:          4242,
		Command:      "go test ./...",
		MachineToken: "not-this-machine",
		JobPGID:      startJobGroup(t),
	}
	writeStaleLock(t, cfg, holder)

	conn := host.NewLocalHostConnection("dev", config.Host{Local: true})
	assert.False(t, IsLocked(conn, cfg), "stale on mtime")

	var warnings []string
	lck, err := Acquire(conn, cfg, "next run", WithWarnFunc(func(msg string) { warnings = append(warnings, msg) }))
	require.NoError(t, err)
	require.NoError(t, lck.Release())
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "stealing stale lock")
}
