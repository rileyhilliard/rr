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
	cancel, pid, done := startCancellableCause(t, script)
	return func() { cancel(nil) }, pid, done
}

// startCancellableCause is startCancellable whose cancel takes a cause, as
// rr's signal handler cancels with.
func startCancellableCause(t *testing.T, script string) (context.CancelCauseFunc, int, <-chan [2]interface{}) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")

	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })
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

// When rr is cancelled for a SIGINT, the command already got that Ctrl+C
// from the terminal, since it shares rr's process group. rr sends it
// nothing more and doesn't kill it after the grace: it waits for the
// command to finish on its own.
func TestExecuteLocalContext_SIGINTCauseSendsNothing(t *testing.T) {
	defer SetLocalInterruptGrace(100 * time.Millisecond)()
	ints := filepath.Join(t.TempDir(), "ints")
	cancel, _, done := startCancellableCause(t,
		"trap 'echo int >> \""+ints+"\"' INT; i=0; while [ $i -lt 10 ]; do sleep 0.05; i=$((i+1)); done; exit 4")

	cancel(SignalCause{Signal: os.Interrupt})
	select {
	case res := <-done:
		assert.Equal(t, 4, res[0], "the command ran to its own end, past the grace")
		assert.ErrorIs(t, res[1].(error), context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("the command didn't finish")
	}
	assert.NoFileExists(t, ints, "rr sent no SIGINT")
}

// A signal the terminal didn't send the command, such as SIGTERM or SIGHUP
// to rr alone, still stops it: SIGINT, then a kill once the grace is up.
func TestExecuteLocalContext_OtherSignalCauseStops(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			defer SetLocalInterruptGrace(200 * time.Millisecond)()
			ints := filepath.Join(t.TempDir(), "ints")
			// Records the SIGINT, then keeps running until it's killed.
			cancel, pid, done := startCancellableCause(t,
				"trap 'echo int >> \""+ints+"\"' INT; while :; do sleep 0.05; done")

			cancel(SignalCause{Signal: sig})
			select {
			case res := <-done:
				assert.Equal(t, 130, res[0], "killed after the grace")
				assert.ErrorIs(t, res[1].(error), context.Canceled)
			case <-time.After(5 * time.Second):
				t.Fatal("the command wasn't stopped")
			}
			assert.Eventually(t, processGone(pid), 5*time.Second, 10*time.Millisecond, "the shell is gone")
			data, err := os.ReadFile(ints)
			require.NoError(t, err, "the command got SIGINT first")
			assert.Equal(t, "int\n", string(data))
		})
	}
}

// A Ctrl+C that lands before the command starts (during a host probe, say)
// never reached it from the terminal, so the command isn't started at all.
func TestExecuteLocalContext_CancelledBeforeStartDoesNotRun(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	marker := filepath.Join(t.TempDir(), "ran")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(SignalCause{Signal: os.Interrupt})

	code, err := ExecuteLocalContext(ctx, "touch "+marker, "", io.Discard, io.Discard)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 130, code)
	assert.NoFileExists(t, marker)
}

// Without a cancel, the context changes nothing: the command runs to its
// end and its exit code is returned with no error.
func TestExecuteLocalContext_NoCancel(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	code, err := ExecuteLocalContext(context.Background(), "exit 3", "", io.Discard, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, 3, code)
}
