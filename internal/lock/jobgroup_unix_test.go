//go:build !windows

package lock

import (
	"context"
	"io"
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

// holderLockDirEnv tells the test binary, re-run as a child, to act as an rr
// run on a local host: take the lock in that dir, record the job it starts
// the way the workflow does, and run a long job until it's killed.
const holderLockDirEnv = "RR_TEST_LOCK_HOLDER_DIR"

func TestMain(m *testing.M) {
	if dir := os.Getenv(holderLockDirEnv); dir != "" {
		os.Exit(runLockHolder(dir))
	}
	os.Exit(m.Run())
}

func runLockHolder(dir string) int {
	conn := host.NewLocalHostConnection("dev", config.Host{Local: true})
	cfg := config.LockConfig{Enabled: true, Timeout: 10 * time.Second, Stale: 10 * time.Minute, Dir: dir}
	lck, err := Acquire(conn, cfg, "go test ./... (killed rr)")
	if err != nil {
		return 2
	}
	client := conn.Client.(*host.LocalClient)
	client.SetOnStart(func(pgid int) { _ = lck.SetJobPGID(pgid) })
	_, _ = client.ExecStreamContext(context.Background(), "sleep 300", io.Discard, io.Discard)
	return 0
}

// An rr killed with SIGKILL leaves its local job running in its own session.
// The lock it held names the job's process group, so the next run waits for
// the job instead of taking the lock from the dead rr; once the job is gone,
// the lock is taken at once.
func TestAcquire_KilledHolderWithLiveJobKeepsLock(t *testing.T) {
	cfg := config.LockConfig{Enabled: true, Timeout: 300 * time.Millisecond, Stale: 10 * time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	defer SetRetryIntervalForTesting(20 * time.Millisecond)()
	infoFile := filepath.Join(cfg.Dir, "rr.lock", "info.json")

	holder := exec.Command(os.Args[0], "-test.run=^$")
	holder.Env = append(os.Environ(), holderLockDirEnv+"="+cfg.Dir)
	require.NoError(t, holder.Start())
	holderDone := false
	t.Cleanup(func() {
		if !holderDone {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	})

	var info *LockInfo
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(infoFile)
		if err != nil {
			return false
		}
		info, err = ParseLockInfo(data)
		return err == nil && info.JobPGID > 0
	}, 10*time.Second, 20*time.Millisecond, "the holder records its job's process group")
	pgid := info.JobPGID
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	assert.Equal(t, holder.Process.Pid, info.PID)
	assert.NotEqual(t, info.PID, pgid, "the job runs in its own group")

	require.NoError(t, holder.Process.Signal(syscall.SIGKILL))
	_ = holder.Wait()
	holderDone = true
	require.False(t, processAlive(info.PID), "rr is dead")
	require.True(t, processGroupAlive(pgid), "its job isn't")

	conn := host.NewLocalHostConnection("dev", config.Host{Local: true})
	assert.True(t, IsHeld(conn, cfg), "a live job keeps the machine busy")
	_, err := Acquire(conn, cfg, "next run")
	require.Error(t, err)
	assert.True(t, rrerrors.IsCode(err, rrerrors.ErrLock), "waits out the timeout instead of stealing: %v", err)

	require.NoError(t, syscall.Kill(-pgid, syscall.SIGKILL))
	require.Eventually(t, func() bool { return !processGroupAlive(pgid) }, 10*time.Second, 20*time.Millisecond,
		"the job's group goes away once killed")

	assert.False(t, IsHeld(conn, cfg), "with the job gone, the lock is free to take")
	var warnings []string
	lck, err := Acquire(conn, cfg, "next run", WithWarnFunc(func(msg string) { warnings = append(warnings, msg) }))
	require.NoError(t, err)
	require.NoError(t, lck.Release())
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "dead local process")
}

// A lock written before job_pgid existed, or by a remote run, has no job to
// check: a dead holder pid alone frees it, as before.
func TestIsDeadLocalHolder_JobPGID(t *testing.T) {
	live := exec.Command("sleep", "300")
	live.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	require.NoError(t, live.Start())
	t.Cleanup(func() {
		_ = syscall.Kill(-live.Process.Pid, syscall.SIGKILL)
		_ = live.Wait()
	})

	tests := []struct {
		name    string
		jobPGID int
		want    bool
	}{
		{name: "no job recorded", jobPGID: 0, want: true},
		{name: "job group still running", jobPGID: live.Process.Pid, want: false},
		{name: "job group gone", jobPGID: deadPid(t), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewLockInfo("rr test")
			require.NoError(t, err)
			info.PID = deadPid(t)
			info.JobPGID = tt.jobPGID
			assert.Equal(t, tt.want, info.IsDeadLocalHolder())
		})
	}
}
