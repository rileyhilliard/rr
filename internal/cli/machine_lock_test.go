package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
)

// writeMachineLockConfigs writes a global config with a local host "dev"
// (tag fast) and an unreachable remote "box" (tag slow), and a project that
// lists hosts, with local_fallback: always and a short lock timeout. It
// returns the project dir and the lock config the project uses.
func writeMachineLockConfigs(t *testing.T, hosts string) (string, config.LockConfig) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	global := `version: 1
defaults:
  probe_timeout: 1s
hosts:
  dev:
    local: true
    tags: [fast]
  box:
    ssh: [nonexistent-host-rr-test.invalid]
    dir: ~/rr
    tags: [slow]
`
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(global), 0o644))

	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	lockBase := filepath.Join(t.TempDir(), "locks")
	project := `version: 1
hosts: ` + hosts + `
local_fallback: always
lock:
  timeout: 300ms
  wait_timeout: 300ms
  dir: ` + lockBase + `
tasks:
  pair:
    parallel: [one, two]
  one:
    run: echo one
  two:
    run: echo two
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	lockCfg := config.LockConfig{Enabled: true, Timeout: 300 * time.Millisecond, Stale: 10 * time.Minute, Dir: lockBase}
	return projectDir, lockCfg
}

// holdMachineLock takes the local host's lock as another run would.
func holdMachineLock(t *testing.T, lockCfg config.LockConfig) {
	t.Helper()
	held, err := lock.TryAcquire(host.NewLocalHostConnection("dev", config.Host{Local: true}), lockCfg, "rr test (other run)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Release() })
}

func runQuietly(t *testing.T, opts RunOptions) (int, string, error) {
	t.Helper()
	var code int
	var err error
	var events string
	captureStdout(t, func() {
		events = captureStderr(t, func() {
			code, err = Run(opts)
		})
	})
	return code, events, err
}

// --tag picks only the remote; it's unreachable, so rr falls back to running
// here. The local host is out of the pool but locked by another run, so the
// fallback must wait for its lock, not run beside that run.
func TestRun_TagExcludesLockedLocalHost_FallbackWaits(t *testing.T) {
	projectDir, lockCfg := writeMachineLockConfigs(t, "[box, dev]")
	t.Chdir(projectDir)
	holdMachineLock(t, lockCfg)

	marker := filepath.Join(projectDir, "ran")
	_, events, err := runQuietly(t, RunOptions{Command: "touch " + marker, Tag: "slow"})
	require.Error(t, err, events)
	assert.True(t, errors.IsCode(err, errors.ErrLock), "got %v", err)
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "the command never ran")
}

// The same when the project's hosts: list leaves the local host out.
func TestRun_HostsListExcludesLockedLocalHost_FallbackWaits(t *testing.T) {
	projectDir, lockCfg := writeMachineLockConfigs(t, "[box]")
	t.Chdir(projectDir)
	holdMachineLock(t, lockCfg)

	marker := filepath.Join(projectDir, "ran")
	_, events, err := runQuietly(t, RunOptions{Command: "touch " + marker})
	require.Error(t, err, events)
	assert.True(t, errors.IsCode(err, errors.ErrLock), "got %v", err)
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "the command never ran")
}

// A fallback with the local host free takes its lock for the run.
func TestRun_FallbackTakesLocalHostLock(t *testing.T) {
	projectDir, lockCfg := writeMachineLockConfigs(t, "[box]")
	t.Chdir(projectDir)

	lockPath := lock.LockDir(lockCfg)
	code, events, err := runQuietly(t, RunOptions{Command: "test -d '" + lockPath + "'", Tag: "slow"})
	require.NoError(t, err, events)
	assert.Equal(t, 0, code, "the lock is held while the command runs")
	assert.Contains(t, events, `"local_fallback":true`)
	_, statErr := os.Stat(lockPath)
	assert.True(t, os.IsNotExist(statErr), "the lock is released after the run")
}

// --local runs here, so it takes the local host's lock like a run on it.
func TestRun_LocalFlagTakesLocalHostLock(t *testing.T) {
	projectDir, lockCfg := writeMachineLockConfigs(t, "[box]")
	t.Chdir(projectDir)

	lockPath := lock.LockDir(lockCfg)
	code, events, err := runQuietly(t, RunOptions{Command: "test -d '" + lockPath + "'", Local: true})
	require.NoError(t, err, events)
	assert.Equal(t, 0, code, "the lock is held while the command runs")

	holdMachineLock(t, lockCfg)
	marker := filepath.Join(projectDir, "ran")
	_, events, err = runQuietly(t, RunOptions{Command: "touch " + marker, Local: true})
	require.Error(t, err, events)
	assert.True(t, errors.IsCode(err, errors.ErrLock), "got %v", err)
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "the command never ran")
}

// A parallel task with --local runs here too, under the same lock.
func TestRunParallelTask_LocalFlagWaitsForLocalHostLock(t *testing.T) {
	projectDir, lockCfg := writeMachineLockConfigs(t, "[box]")
	t.Chdir(projectDir)
	holdMachineLock(t, lockCfg)

	var err error
	captureStdout(t, func() {
		captureStderr(t, func() {
			_, err = RunParallelTask(ParallelTaskOptions{TaskName: "pair", Local: true, NoLogs: true})
		})
	})
	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrLock), "got %v", err)
}

// In the all-locked phase, a fallback is ruled out when this machine's
// local host is locked, whether or not it's in the pool that was tried.
func TestMachineBusy_LocalHostOutsidePool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	lockCfg := config.LockConfig{Enabled: true, Timeout: time.Second, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	ctx := &WorkflowContext{Resolved: &config.ResolvedConfig{Global: &config.GlobalConfig{Hosts: map[string]config.Host{
		"dev": {Local: true},
		"box": {SSH: []string{"box"}},
	}}}}
	lockedRemote := []hostAttempt{{hostName: "box", conn: &host.Connection{Name: "box", Host: config.Host{SSH: []string{"box"}}}}}

	assert.False(t, machineBusy(ctx, lockedRemote, lockCfg))
	holdMachineLock(t, lockCfg)
	assert.True(t, machineBusy(ctx, lockedRemote, lockCfg))

	ctx.Resolved.Global.Hosts = map[string]config.Host{"box": {SSH: []string{"box"}}}
	assert.False(t, machineBusy(ctx, lockedRemote, lockCfg), "no local host, nothing to protect")
}

// A lock on the local host left by a dead rr process on this machine doesn't
// make the machine busy: the next Acquire steals it at once.
func TestMachineBusy_DeadLocalHolderIsFree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dead-pid detection is Unix-only")
	}
	lockCfg := config.LockConfig{Enabled: true, Timeout: time.Second, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}
	ctx := &WorkflowContext{Resolved: &config.ResolvedConfig{Global: &config.GlobalConfig{Hosts: map[string]config.Host{
		"dev": {Local: true},
		"box": {SSH: []string{"box"}},
	}}}}
	lockedRemote := []hostAttempt{{hostName: "box", conn: &host.Connection{Name: "box", Host: config.Host{SSH: []string{"box"}}}}}

	gone := exec.Command("true")
	require.NoError(t, gone.Run())
	info, err := lock.NewLockInfo("rr test (killed)")
	require.NoError(t, err)
	info.PID = gone.Process.Pid
	data, err := info.Marshal()
	require.NoError(t, err)
	lockDir := filepath.Join(lockCfg.Dir, "rr.lock")
	require.NoError(t, os.MkdirAll(lockDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(lockDir, "info.json"), data, 0o644))

	assert.False(t, machineBusy(ctx, lockedRemote, lockCfg))
	_, statErr := os.Stat(lockDir)
	assert.NoError(t, statErr, "the check only reads; the lock is left for Acquire to steal")
}
