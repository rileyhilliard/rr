package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
)

// queueLockTimeout is how long a queued run waits for the local host. It's
// far above the holds in these tests, so a waiter that stops polling fails
// on a timeout instead of passing by luck.
const queueLockTimeout = 10 * time.Second

// writeQueueProject replaces the project config from writeLocalHostConfigs
// with one whose lock waits queueLockTimeout, and returns the lock config a
// competing run would use.
func writeQueueProject(t *testing.T, projectDir, lockDir string) config.LockConfig {
	t.Helper()
	lockBase := filepath.Dir(lockDir)
	project := `version: 1
hosts: [dev]
lock:
  timeout: ` + queueLockTimeout.String() + `
  dir: ` + lockBase + `
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	return config.LockConfig{Enabled: true, Timeout: queueLockTimeout, Stale: 10 * time.Minute, Dir: lockBase}
}

// holdLocalHostLock takes the local host's lock as another run would and
// returns it; the caller releases it. Cleanup releases it too, in case the
// test fails first.
func holdLocalHostLock(t *testing.T, lockCfg config.LockConfig) *lock.Lock {
	t.Helper()
	held, err := lock.TryAcquire(host.NewLocalHostConnection("dev", config.Host{Local: true}), lockCfg, "rr test (other run)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Release() })
	return held
}

// rrProcess is the test binary re-run as rr (see runCLIEnv).
type rrProcess struct {
	cmd    *exec.Cmd
	stderr *lockedBuffer
	done   chan error
}

// startRR runs rr with args in dir as a separate process, with this test's
// HOME. Its structured events go to stderr, which the test can read while
// it runs.
func startRR(t *testing.T, dir string, args ...string) *rrProcess {
	t.Helper()
	p := &rrProcess{cmd: exec.Command(os.Args[0], args...), stderr: &lockedBuffer{}, done: make(chan error, 1)}
	p.cmd.Dir = dir
	p.cmd.Env = append(os.Environ(), runCLIEnv+"=1")
	p.cmd.Stdout = &lockedBuffer{}
	p.cmd.Stderr = p.stderr
	require.NoError(t, p.cmd.Start())
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

// wait returns the process's exit error, failing the test if it doesn't
// exit within timeout.
func (p *rrProcess) wait(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-p.done:
		p.done <- err // later waits and Cleanup see it too
		return err
	case <-time.After(timeout):
		t.Fatalf("rr %v didn't exit within %s; stderr:\n%s", p.cmd.Args[1:], timeout, p.stderr.String())
		return nil
	}
}

// A run that finds the local host locked waits in the queue, then runs as
// soon as the holder releases: exit 0, the command ran, the wait was
// reported, and the lock phase took at least as long as the hold.
func TestRun_LocalHostQueueProceedsAfterRelease(t *testing.T) {
	projectDir, lockDir := writeLocalHostConfigs(t)
	lockCfg := writeQueueProject(t, projectDir, lockDir)
	t.Chdir(projectDir)
	defer lock.SetRetryIntervalForTesting(50 * time.Millisecond)()

	const hold = 300 * time.Millisecond
	held := holdLocalHostLock(t, lockCfg)
	released := make(chan struct{})
	timer := time.AfterFunc(hold, func() {
		_ = held.Release()
		close(released)
	})
	defer timer.Stop()

	marker := filepath.Join(projectDir, "ran")
	code, events, err := runQuietly(t, RunOptions{Command: "touch '" + marker + "'"})
	require.NoError(t, err, events)
	assert.Equal(t, 0, code, events)

	select {
	case <-released:
	default:
		t.Fatalf("the run finished while the lock was still held:\n%s", events)
	}
	_, statErr := os.Stat(marker)
	assert.NoError(t, statErr, "the command ran after the lock was released")

	require.Len(t, phaseEvents(t, events, "lock", "waiting"), 1, events)
	complete := phaseEvents(t, events, "lock", "complete")
	require.Len(t, complete, 1, events)
	assert.Equal(t, "dev", complete[0].Host)
	// The hold timer starts just before Run, and Run reaches the lock phase
	// within milliseconds (nothing is dialed or synced for a local host), so
	// nearly all of the hold is spent waiting in the lock phase.
	assert.GreaterOrEqual(t, complete[0].Duration, (hold - 50*time.Millisecond).Seconds(), events)
}

// Two runs queued behind the same holder both run once it releases, one
// after the other: their commands never overlap.
//
// The waiters are separate rr processes (the test binary re-run as rr):
// two Run calls can't share this process, since flags, output mode and the
// stdout/stderr capture are process-global, and two real processes is what
// two agents on one machine are anyway.
func TestRun_LocalHostQueueSerializesWaiters(t *testing.T) {
	projectDir, lockDir := writeLocalHostConfigs(t)
	lockCfg := writeQueueProject(t, projectDir, lockDir)
	held := holdLocalHostLock(t, lockCfg)

	logFile := filepath.Join(projectDir, "order.log")
	waiter := func(id string) []string {
		// The sleep keeps each command running long enough that an
		// overlapping second run would write its start inside the first's
		// start/end pair.
		cmd := "echo start-" + id + " >> '" + logFile + "'; sleep 0.3; echo end-" + id + " >> '" + logFile + "'"
		return []string{"run", cmd}
	}
	a := startRR(t, projectDir, waiter("a")...)
	b := startRR(t, projectDir, waiter("b")...)

	// Release only once both are queued, so neither can slip in first.
	for _, p := range []*rrProcess{a, b} {
		require.Eventually(t, func() bool {
			return strings.Contains(p.stderr.String(), `"phase":"lock","status":"waiting"`)
		}, 10*time.Second, 20*time.Millisecond, "both runs wait for the held lock")
	}
	_, statErr := os.Stat(logFile)
	require.True(t, os.IsNotExist(statErr), "nothing ran while the lock was held")
	require.NoError(t, held.Release())

	for _, p := range []*rrProcess{a, b} {
		require.NoError(t, p.wait(t, 15*time.Second), p.stderr.String())
	}

	data, err := os.ReadFile(logFile)
	require.NoError(t, err)
	lines := strings.Fields(string(data))
	require.Len(t, lines, 4, "both commands ran once: %q", lines)
	first := strings.TrimPrefix(lines[0], "start-")
	second := map[string]string{"a": "b", "b": "a"}[first]
	require.NotEmpty(t, second, "the log starts with a start marker: %q", lines)
	assert.Equal(t, []string{"start-" + first, "end-" + first, "start-" + second, "end-" + second}, lines,
		"each run finishes before the next starts")

	_, statErr = os.Stat(lockDir)
	assert.True(t, os.IsNotExist(statErr), "the last run releases the lock")
}
