//go:build !windows

package exec

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startCancellable runs script under ExecuteLocalContext in the background
// once it has written its pid to dir/pid, and returns the cancel func, the
// command's pid, and a channel that delivers the result.
func startCancellable(t *testing.T, script string) (context.CancelFunc, int, <-chan [2]interface{}) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan [2]interface{}, 1)
	go func() {
		code, err := ExecuteLocalContext(ctx, "echo $$ > '"+pidFile+"'; "+script, "", io.Discard, io.Discard)
		done <- [2]interface{}{code, err}
	}()

	var pid int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil || !strings.HasSuffix(string(data), "\n") {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the command started")
	return cancel, pid, done
}

// processGone reports whether pid no longer exists. ExecuteLocalContext
// still reaps a killed command in the background, so this polls past the
// brief zombie.
func processGone(pid int) func() bool {
	return func() bool { return syscall.Kill(pid, 0) != nil }
}

// Cancelling a bare local command interrupts it: a command that handles
// SIGINT gets to clean up and exit on its own terms.
func TestExecuteLocalContext_CancelInterrupts(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleaned-up")
	cancel, _, done := startCancellable(t,
		"trap 'touch \""+marker+"\"; exit 7' INT; while :; do sleep 0.05; done")

	cancel()
	select {
	case res := <-done:
		assert.Equal(t, 7, res[0], "the command's own exit code")
		assert.ErrorIs(t, res[1].(error), context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("the command didn't stop after the cancel")
	}
	assert.FileExists(t, marker, "the SIGINT handler ran")
}

// A command that ignores SIGINT is killed once the grace runs out.
func TestExecuteLocalContext_CancelKillsAfterGrace(t *testing.T) {
	defer SetLocalInterruptGrace(200 * time.Millisecond)()
	cancel, pid, done := startCancellable(t, "trap '' INT; while :; do sleep 0.05; done")

	start := time.Now()
	cancel()
	select {
	case res := <-done:
		assert.Equal(t, 130, res[0])
		assert.ErrorIs(t, res[1].(error), context.Canceled)
		assert.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "it got the grace first")
	case <-time.After(5 * time.Second):
		t.Fatal("the command wasn't killed after the grace")
	}
	assert.Eventually(t, processGone(pid), 5*time.Second, 10*time.Millisecond, "the shell is gone")
}

// Without a cancel, the context changes nothing: the command runs to its
// end and its exit code is returned with no error.
func TestExecuteLocalContext_NoCancel(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	code, err := ExecuteLocalContext(context.Background(), "exit 3", "", io.Discard, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 3, code)
}
