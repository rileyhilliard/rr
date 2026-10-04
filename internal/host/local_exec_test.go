package host_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	rrexec "github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client tests use a POSIX shell")
	}
}

// A cancelled command built the way `rr run` builds one is a nested shell
// running a compound list. Cancel has to stop the whole tree, not only the
// outer shell, and return without waiting on the grandchild's pipes.
func TestLocalClient_CancelStopsNestedCommand(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	c := host.NewLocalClient()
	marker := filepath.Join(t.TempDir(), "ran")
	cmd := rrexec.BuildRemoteCommand("sleep 1; touch "+marker, &config.Host{Shell: "sh"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out bytes.Buffer
	_, err := c.ExecStreamContext(ctx, cmd, &out, &out)
	elapsed := time.Since(start)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 800*time.Millisecond, "returns on SIGINT, not when the grandchild exits")

	time.Sleep(1500 * time.Millisecond)
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "the rest of the compound list never ran")
}

// A command that ignores SIGINT is killed, with its whole tree, after the
// grace period.
func TestLocalClient_CancelKillsTreeIgnoringInterrupt(t *testing.T) {
	skipOnWindows(t)
	t.Cleanup(host.SetLocalInterruptGrace(200 * time.Millisecond))

	c := host.NewLocalClient()
	marker := filepath.Join(t.TempDir(), "ran")
	cmd := rrexec.BuildRemoteCommand("trap '' INT; sleep 1; touch "+marker, &config.Host{Shell: "sh"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out bytes.Buffer
	code, err := c.ExecStreamContext(ctx, cmd, &out, &out)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 130, code)
	assert.Less(t, time.Since(start), 800*time.Millisecond, "killed after the grace period")

	time.Sleep(1500 * time.Millisecond)
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "the rest of the compound list never ran")
}

// A command killed by a signal reports 128+signal, as a shell would.
func TestLocalClient_SignalExitCode(t *testing.T) {
	skipOnWindows(t)
	c := host.NewLocalClient()

	_, _, code, err := c.Exec("kill -TERM $$")
	require.NoError(t, err)
	assert.Equal(t, 143, code)

	var out bytes.Buffer
	code, err = c.ExecStreamContext(context.Background(), "kill -TERM $$", &out, &out)
	require.NoError(t, err)
	assert.Equal(t, 143, code)
}

// sshd starts a command in the user's home directory, so the local client
// does too: setup commands that run before the cd, and a relative lock dir,
// don't depend on where rr was invoked.
func TestLocalClient_RunsInHomeDir(t *testing.T) {
	skipOnWindows(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	want, err := filepath.EvalSymlinks(home)
	require.NoError(t, err)

	c := host.NewLocalClient()
	stdout, _, code, err := c.Exec("pwd -P")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, want+"\n", string(stdout))

	var out bytes.Buffer
	_, err = c.ExecStreamContext(context.Background(), "pwd -P", &out, &out)
	require.NoError(t, err)
	assert.Equal(t, want+"\n", out.String())
}

// A command that exits 0 but leaves a background process holding its output
// open still returns, with exit code 0.
func TestLocalClient_BackgroundChildHoldingOutput(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	c := host.NewLocalClient()

	start := time.Now()
	var out bytes.Buffer
	code, err := c.ExecStreamContext(context.Background(), "sleep 3 & echo started $!", &out, &out)
	require.NoError(t, err)
	var pid int
	_, scanErr := fmt.Sscanf(out.String(), "started %d", &pid)
	require.NoError(t, scanErr, out.String())
	t.Cleanup(func() { killPid(pid) })
	assert.Equal(t, 0, code)
	assert.Less(t, time.Since(start), 2500*time.Millisecond)
}

// startBackgroundSleep is a command whose background sleep ignores SIGINT,
// as an async job in a non-interactive shell does, and outlives the shell
// unless something kills it. It writes the sleep's pid to the returned file.
func startBackgroundSleep(t *testing.T) (cmd, pidFile string) {
	t.Helper()
	pidFile = filepath.Join(t.TempDir(), "pid")
	return "sleep 30 & echo $! > " + pidFile + "; wait", pidFile
}

// readPid waits for pidFile and returns the pid in it, killing that process
// when the test ends in case the test failed before it did.
func readPid(t *testing.T, pidFile string) int {
	t.Helper()
	var pid int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		_, err = fmt.Sscanf(string(data), "%d", &pid)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
	t.Cleanup(func() { killPid(pid) })
	return pid
}

func killPid(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

func assertGone(t *testing.T, pid int) {
	t.Helper()
	assert.Eventually(t, func() bool {
		p, err := os.FindProcess(pid)
		return err != nil || p.Signal(syscall.Signal(0)) != nil
	},
		2*time.Second, 20*time.Millisecond, "pid %d is still running", pid)
}

// A cancel stops background jobs too. SIGINT ends the shell, but its
// background sleep ignores SIGINT; it must not outlive the cancel.
func TestLocalClient_CancelKillsBackgroundJob(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	c := host.NewLocalClient()
	cmd, pidFile := startBackgroundSleep(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	_, err := c.ExecStreamContext(ctx, cmd, &out, &out)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	assertGone(t, readPid(t, pidFile))
}

// A force-quit kills every local command still running, background jobs
// included, before rr exits. Not parallel: KillLocalCommands kills every
// command in the process.
func TestKillLocalCommands(t *testing.T) {
	skipOnWindows(t)
	c := host.NewLocalClient()
	cmd, pidFile := startBackgroundSleep(t)

	done := make(chan struct{})
	go func() {
		var out bytes.Buffer
		_, _ = c.ExecStreamContext(context.Background(), cmd, &out, &out)
		close(done)
	}()
	pid := readPid(t, pidFile)

	host.KillLocalCommands()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the command didn't return after KillLocalCommands")
	}
	assertGone(t, pid)
}

// A local command has no controlling terminal, as on a remote host without
// a pty: opening /dev/tty fails at once rather than the job stopping on
// SIGTTIN when it reads. (Without a terminal in the test run, this can't
// tell the difference.)
func TestLocalClient_NoControllingTerminal(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	c := host.NewLocalClient()

	stdout, _, code, err := c.Exec("if (exec </dev/tty) 2>/dev/null; then echo tty; else echo notty; fi")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "notty\n", string(stdout))
}

// The client's own shell is /bin/sh, not $SHELL, which may be a shell (fish)
// that can't parse the command rr builds.
func TestLocalClient_OuterShellIsSh(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	c := host.NewLocalClient()

	stdout, _, code, err := c.Exec("x=1; [ \"$x\" = 1 ] && echo ok")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "ok\n", string(stdout))
}
