package lock

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessAlive_OwnPID(t *testing.T) {
	assert.True(t, processAlive(os.Getpid()))
}

func TestProcessAlive_DeadPID(t *testing.T) {
	// Spawn a short-lived child and wait for it; its pid is then dead.
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	require.NoError(t, cmd.Wait())

	assert.False(t, processAlive(pid))
}

func TestSameMachine_TokenMatch(t *testing.T) {
	info, err := NewLockInfo("rr test")
	require.NoError(t, err)

	assert.True(t, info.SameMachine())
}

func TestSameMachine_TokenMismatch(t *testing.T) {
	hostname, _ := os.Hostname()
	// Same hostname and user but a different machine token: another machine
	// with a colliding default hostname. Token comparison must win.
	info := &LockInfo{
		User:         currentUser(),
		Hostname:     hostname,
		Started:      time.Now(),
		PID:          os.Getpid(),
		MachineToken: "00000000000000000000000000000000",
	}
	if machineToken() == "" {
		t.Skip("no machine token available on this system")
	}

	assert.False(t, info.SameMachine())
}

func TestSameMachine_LegacyFallback(t *testing.T) {
	hostname, _ := os.Hostname()

	// Old marker without a token: hostname+user match required
	info := &LockInfo{User: currentUser(), Hostname: hostname, PID: os.Getpid()}
	assert.True(t, info.SameMachine())

	// Different user on same hostname is not treated as same machine
	info = &LockInfo{User: "someone-else", Hostname: hostname, PID: os.Getpid()}
	assert.False(t, info.SameMachine())

	// Different hostname
	info = &LockInfo{User: currentUser(), Hostname: "other-machine", PID: os.Getpid()}
	assert.False(t, info.SameMachine())
}

func TestIsDeadLocalHolder(t *testing.T) {
	// Own (alive) process: not a dead holder
	info, err := NewLockInfo("rr test")
	require.NoError(t, err)
	assert.False(t, info.IsDeadLocalHolder())

	// Dead local process
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	deadPid := cmd.Process.Pid
	require.NoError(t, cmd.Wait())

	dead, err := NewLockInfo("rr test-backend")
	require.NoError(t, err)
	dead.PID = deadPid
	assert.True(t, dead.IsDeadLocalHolder())

	// Same dead pid but from another machine: never steal
	dead.MachineToken = "00000000000000000000000000000000"
	dead.Hostname = "other-machine"
	assert.False(t, dead.IsDeadLocalHolder())

	// Invalid pid: not stealable
	invalid := &LockInfo{PID: 0}
	assert.False(t, invalid.IsDeadLocalHolder())
}

func TestDescribe(t *testing.T) {
	info := &LockInfo{
		User:     "riley",
		Hostname: "other-machine",
		Started:  time.Now().Add(-5 * time.Minute),
		PID:      46822,
		Command:  "rr test-backend",
	}

	desc := info.Describe()
	assert.Contains(t, desc, "'rr test-backend' held by ")
	assert.Contains(t, desc, "riley@other-machine")
	assert.Contains(t, desc, "pid 46822")
	assert.Contains(t, desc, "started 5m0s ago")
	assert.NotContains(t, desc, "this machine")

	// No command, no start time
	bare := &LockInfo{User: "riley", Hostname: "other-machine", PID: 1}
	desc = bare.Describe()
	assert.Equal(t, "riley@other-machine (pid 1)", desc)
}

func TestDescribe_SameMachine(t *testing.T) {
	info, err := NewLockInfo("rr test")
	require.NoError(t, err)

	assert.Contains(t, info.Describe(), "this machine")
}

func TestMachineToken_StableAcrossCalls(t *testing.T) {
	tok := machineToken()
	if tok == "" {
		t.Skip("no machine token available on this system")
	}
	assert.Equal(t, tok, machineToken())
	assert.True(t, isHexToken(tok))
}

func TestIsHexToken(t *testing.T) {
	assert.True(t, isHexToken("0123456789abcdef0123456789abcdef"))
	assert.False(t, isHexToken("short"))
	assert.False(t, isHexToken("0123456789ABCDEF0123456789ABCDEF")) // uppercase rejected
	assert.False(t, isHexToken("0123456789abcdef0123456789abcdeg")) // non-hex char
}
