package host_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
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
	code, err := c.ExecStreamContext(context.Background(), "sleep 3 & echo started", &out, &out)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, out.String(), "started")
	assert.Less(t, time.Since(start), 2500*time.Millisecond)
}
