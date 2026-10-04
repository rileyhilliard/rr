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

// A recorded job group counts as the job only while its leader is the
// process that started when the job did. Once the job and rr are gone, a
// later process can get the job's pid and lead a group with the same id;
// that group must not keep the lock held forever.
func TestJobGroupLiveness_RecordedStartTime(t *testing.T) {
	pgid := startJobGroup(t)
	started := time.Now()

	// A group whose leader exited while a member runs on, as when a job's
	// shell is gone but something it started in the background isn't.
	orphanDir := t.TempDir()
	orphan := exec.Command("sh", "-c", "sleep 300 & echo $! > '"+filepath.Join(orphanDir, "member")+"'")
	orphan.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	orphanStarted := time.Now()
	require.NoError(t, orphan.Start())
	orphanPGID := orphan.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-orphanPGID, syscall.SIGKILL) })
	require.NoError(t, orphan.Wait(), "the leader exits")
	require.False(t, processAlive(orphanPGID), "the leader is gone")
	require.True(t, processGroupAlive(orphanPGID), "its member isn't")

	tests := []struct {
		name     string
		jobPGID  int
		started  time.Time
		wantLive bool
	}{
		{name: "leader started with the job", jobPGID: pgid, started: started, wantLive: true},
		{name: "leader started an hour after the job: pid reused", jobPGID: pgid, started: started.Add(-time.Hour), wantLive: false},
		{name: "leader started before the job: pid reused", jobPGID: pgid, started: started.Add(time.Hour), wantLive: false},
		{name: "no start time recorded (older lock)", jobPGID: pgid, wantLive: true},
		{name: "leader gone, a member still running", jobPGID: orphanPGID, started: orphanStarted, wantLive: true},
		{name: "group gone", jobPGID: deadPid(t), started: started, wantLive: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewLockInfo("rr test")
			require.NoError(t, err)
			info.PID = deadPid(t)
			info.JobPGID = tt.jobPGID
			info.JobStarted = tt.started
			assert.Equal(t, tt.wantLive, info.HasLiveLocalJob(), "HasLiveLocalJob")
			assert.Equal(t, !tt.wantLive, info.IsDeadLocalHolder(), "IsDeadLocalHolder")
		})
	}
}

func TestParseEtime(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "00:07", want: 7 * time.Second},
		{in: "12:34", want: 12*time.Minute + 34*time.Second},
		{in: "01:02:03", want: time.Hour + 2*time.Minute + 3*time.Second},
		{in: "3-04:05:06", want: 3*24*time.Hour + 4*time.Hour + 5*time.Minute + 6*time.Second},
		{in: "", wantErr: true},
		{in: "7", wantErr: true},
		{in: "3-05:06", wantErr: true},
		{in: "1:2:3:4", wantErr: true},
		{in: "aa:bb", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseEtime(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ps reports a process just started as having run for well under the
// tolerance, on whichever of macOS or Linux the test runs.
func TestProcessElapsed_FreshProcess(t *testing.T) {
	elapsed, err := processElapsed(startJobGroup(t))
	require.NoError(t, err)
	assert.Less(t, elapsed, jobStartTolerance)
}

// A killed rr's lock whose job group id now belongs to an unrelated process
// is taken at once, not held until someone runs rr unlock.
func TestAcquire_ReusedJobPGIDDoesNotHoldLock(t *testing.T) {
	cfg := config.LockConfig{Enabled: true, Timeout: 2 * time.Second, Stale: 10 * time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	defer SetRetryIntervalForTesting(20 * time.Millisecond)()

	holder, err := NewLockInfo("rr test (killed)")
	require.NoError(t, err)
	holder.PID = deadPid(t)
	holder.JobPGID = startJobGroup(t) // the unrelated process now leading that id
	holder.JobStarted = time.Now().Add(-time.Hour)
	lockDir := LockDir(cfg)
	require.NoError(t, os.MkdirAll(lockDir, 0o755))
	data, err := holder.Marshal()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(lockDir, "info.json"), data, 0o644))

	conn := host.NewLocalHostConnection("dev", config.Host{Local: true})
	assert.False(t, IsHeld(conn, cfg))
	lck, err := Acquire(conn, cfg, "next run")
	require.NoError(t, err)
	require.NoError(t, lck.Release())
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
