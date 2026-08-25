//go:build !windows

// Dead-pid liveness tests are Unix-only: the Windows processAlive stub
// deliberately reports every pid as alive (mtime staleness still applies
// there), so dead-holder semantics can't be exercised on Windows.

package lock

import (
	"os/exec"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deadPid spawns a short-lived child and reaps it, returning a pid that is
// guaranteed dead.
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	require.NoError(t, cmd.Wait())
	return pid
}

func TestProcessAlive_DeadPID(t *testing.T) {
	// Spawn a short-lived child and wait for it; its pid is then dead.
	pid := deadPid(t)

	assert.False(t, processAlive(pid))
}

func TestIsDeadLocalHolder(t *testing.T) {
	// Own (alive) process: not a dead holder
	info, err := NewLockInfo("rr test")
	require.NoError(t, err)
	assert.False(t, info.IsDeadLocalHolder())

	// Dead local process
	pid := deadPid(t)

	dead, err := NewLockInfo("rr test-backend")
	require.NoError(t, err)
	dead.PID = pid
	assert.True(t, dead.IsDeadLocalHolder())

	// Same dead pid but from another machine: never steal
	dead.MachineToken = "00000000000000000000000000000000"
	dead.Hostname = "other-machine"
	assert.False(t, dead.IsDeadLocalHolder())

	// Invalid pid: not stealable
	invalid := &LockInfo{PID: 0}
	assert.False(t, invalid.IsDeadLocalHolder())
}

func TestAcquire_DeadLocalHolderStolen(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Simulate a lock held by a dead process on THIS machine
	pid := deadPid(t)

	mock.GetFS().Mkdir("/tmp/rr.lock")
	info, err := NewLockInfo("rr test-backend")
	require.NoError(t, err)
	info.PID = pid // recent (not stale), but the process is gone
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 2 * time.Second,
		Stale:   10 * time.Minute, // NOT stale - only the dead-holder path can steal
		Dir:     "/tmp",
	}

	var warnings []string
	l, err := Acquire(conn, cfg, "", WithWarnFunc(func(msg string) {
		warnings = append(warnings, msg)
	}))
	require.NoError(t, err)
	require.NotNil(t, l)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "dead local process")
}

func TestAcquire_DeadRemoteHolderNotStolen(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	pid := deadPid(t)

	mock.GetFS().Mkdir("/tmp/rr.lock")
	// Holder from another machine: same dead pid but never stealable early
	info := &LockInfo{
		User:         "other",
		Hostname:     "otherhost",
		Started:      time.Now(),
		PID:          pid,
		MachineToken: "00000000000000000000000000000000",
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 100 * time.Millisecond,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	_, err := Acquire(conn, cfg, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Lock timeout")
}

func TestTryAcquire_DeadLocalHolderStolen(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	pid := deadPid(t)

	mock.GetFS().Mkdir("/tmp/rr.lock")
	info, err := NewLockInfo("rr test-backend")
	require.NoError(t, err)
	info.PID = pid
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	l, err := TryAcquire(conn, cfg, "")
	require.NoError(t, err)
	require.NotNil(t, l)
}

func TestStealDeadHolderLock_HolderChangedNotStolen(t *testing.T) {
	_, mock := newMockConnection("testhost")

	pid := deadPid(t)

	// Snapshot taken during the liveness check: dead local holder.
	prev, err := NewLockInfo("rr old-run")
	require.NoError(t, err)
	prev.PID = pid

	// By removal time the lock has been released and re-acquired by a
	// live process - the steal must abort.
	current, err := NewLockInfo("rr new-run")
	require.NoError(t, err)
	current.PID = 1
	curJSON, _ := current.Marshal()
	mock.GetFS().Mkdir("/tmp/rr.lock")
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", curJSON)

	assert.False(t, stealDeadHolderLock(mock, "/tmp/rr.lock", "/tmp/rr.lock/info.json", prev))
	assert.True(t, mock.GetFS().Exists("/tmp/rr.lock"))
}

func TestStealDeadHolderLock_SameDeadHolderStolen(t *testing.T) {
	_, mock := newMockConnection("testhost")

	pid := deadPid(t)

	info, err := NewLockInfo("rr old-run")
	require.NoError(t, err)
	info.PID = pid
	infoJSON, _ := info.Marshal()
	mock.GetFS().Mkdir("/tmp/rr.lock")
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	assert.True(t, stealDeadHolderLock(mock, "/tmp/rr.lock", "/tmp/rr.lock/info.json", info))
	assert.False(t, mock.GetFS().Exists("/tmp/rr.lock"))
}
