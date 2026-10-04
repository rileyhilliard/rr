package cli

import (
	"bufio"
	"context"
	stderrors "errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/lock"
)

// onPhaseEvent routes stderr through a pipe and calls fn when a phase event
// with the given phase and status is written, sending the time it did so on
// the returned channel. It restores stderr on cleanup.
func onPhaseEvent(t *testing.T, phase, status string, fn func()) <-chan time.Time {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stderr
	os.Stderr = w
	fired := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(r)
		once := false
		for scanner.Scan() {
			line := scanner.Text()
			if !once && strings.Contains(line, `"phase":"`+phase+`"`) && strings.Contains(line, `"status":"`+status+`"`) {
				once = true
				fired <- time.Now()
				fn()
			}
		}
	}()
	t.Cleanup(func() {
		os.Stderr = old
		_ = w.Close()
		<-done
		_ = r.Close()
	})
	return fired
}

// Ctrl+C while every host is locked stops the load-balanced wait at once,
// with INTERRUPTED rather than LOCK_HELD after lock.wait_timeout, and leaves
// the other run's lock alone.
func TestFindAvailableHost_CancelStopsAllLockedWait(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	hosts := map[string]config.Host{
		"dev":    {Local: true, Dir: t.TempDir()},
		"remote": {SSH: []string{"nonexistent-host-rr-test.invalid"}, Dir: "~"},
	}
	ctx, lockCfg := localHostWorkflow(t, hosts, []string{"dev", "remote"}, config.LocalFallbackAlways)
	ctx.Resolved.Project.Lock.WaitTimeout = time.Minute
	ctx.ctx, ctx.cancel = context.WithCancel(context.Background())

	holderConn, err := ctx.selector.SelectHost("dev")
	require.NoError(t, err)
	holder, err := lock.TryAcquire(holderConn, lockCfg, "rr test (other run)")
	require.NoError(t, err)
	defer holder.Release()

	cancelled := onPhaseEvent(t, "connect", "waiting", ctx.cancel)

	result, err := findAvailableHost(ctx, WorkflowOptions{Command: "rr test"})
	returned := time.Now()

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.IsCode(err, errors.ErrInterrupted), "got %v", err)
	assert.True(t, stderrors.Is(err, context.Canceled))
	assert.Contains(t, err.Error(), "Stopped waiting for the lock on dev")

	var cancelledAt time.Time
	select {
	case cancelledAt = <-cancelled:
	default:
		t.Fatal("the wait never started")
	}
	assert.Less(t, returned.Sub(cancelledAt), time.Second, "returns promptly, not after the next poll or the wait timeout")

	info := lock.GetLockInfo(holderConn, lockCfg)
	require.NotNil(t, info, "the other run still holds its lock")
	assert.Equal(t, "rr test (other run)", info.Command)
}
