package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/parallel"
)

// A parallel --local run that finds the local host locked says so while it
// waits: one lock/waiting event naming the holder, the same event a single
// run emits.
func TestRunParallelTask_LockWaitReportsHolder(t *testing.T) {
	withStructuredOutput(t)
	projectDir, lockCfg := writeMachineLockConfigs(t, "[box]")
	t.Chdir(projectDir)
	defer lock.SetRetryIntervalForTesting(20 * time.Millisecond)()
	holdMachineLock(t, lockCfg)

	var code int
	var err error
	var events string
	captureStdout(t, func() {
		events = captureStderr(t, func() {
			code, err = RunParallelTask(ParallelTaskOptions{TaskName: "pair", Local: true, NoLogs: true})
		})
	})
	require.NoError(t, err, events)
	assert.Equal(t, 1, code, events)

	waiting := phaseEvents(t, events, "lock", "waiting")
	require.Len(t, waiting, 1, "one wait on one host, however many subtasks: %s", events)
	ev := waiting[0]
	assert.Equal(t, "dev", ev.Host)
	assert.Equal(t, 0.3, ev.Details["wait_timeout_s"])
	assert.Contains(t, ev.Details["message"], "rr test (other run)")
	holders, ok := ev.Details["holders"].([]interface{})
	require.True(t, ok, "holders is a list: %v", ev.Details)
	require.Len(t, holders, 1)
	holder := holders[0].(map[string]interface{})
	assert.Equal(t, "dev", holder["host"])
	assert.Equal(t, "rr test (other run)", holder["command"])
	assert.Equal(t, true, holder["same_machine"])
}

// Ctrl+C while a parallel --local run waits for the local host's lock stops
// the run with INTERRUPTED and exit 130, like a single run, not exit 1 with
// failed subtasks an agent would retry. No subtask runs.
func TestRunParallelTask_InterruptedLockWaitExits130(t *testing.T) {
	projectDir, lockDir := writeLocalHostConfigs(t)
	lockBase := filepath.Dir(lockDir)
	marker := filepath.Join(projectDir, "ran")
	project := `version: 1
hosts: [dev]
lock:
  timeout: ` + queueLockTimeout.String() + `
  dir: ` + lockBase + `
tasks:
  pair:
    parallel: [one, two]
  one:
    run: touch '` + marker + `'
  two:
    run: touch '` + marker + `'
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	lockCfg := config.LockConfig{Enabled: true, Timeout: queueLockTimeout, Stale: 10 * time.Minute, Dir: lockBase}
	holdLocalHostLock(t, lockCfg)

	p := startRR(t, projectDir, "pair", "--local", "--no-logs")
	require.Eventually(t, func() bool {
		return strings.Contains(p.stderr.String(), `"phase":"lock","status":"waiting"`)
	}, 10*time.Second, 20*time.Millisecond, "the run waits for the held lock")
	require.NoError(t, p.cmd.Process.Signal(syscall.SIGINT))

	err := p.wait(t, 10*time.Second)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, p.stderr.String())
	assert.Equal(t, 130, exitErr.ExitCode(), p.stderr.String())

	// The error envelope is the indented JSON after the event lines.
	stderr := p.stderr.String()
	start := strings.LastIndex(stderr, "\n{\n")
	require.GreaterOrEqual(t, start, 0, "an error envelope: %s", stderr)
	var env JSONEnvelope
	require.NoError(t, json.Unmarshal([]byte(stderr[start:]), &env), stderr)
	require.NotNil(t, env.Error, stderr)
	assert.Equal(t, ErrCodeInterrupted, env.Error.Code)
	assert.NotContains(t, stderr, `"type":"result"`, "no result event: no subtask ran")
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "no subtask ran")
}

// A stopped parallel run is INTERRUPTED only when every subtask was still
// waiting for a lock. If any subtask ran or failed another way, the run's
// result is reported as usual, since "nothing ran" would be false.
func TestInterruptedBeforeRunning(t *testing.T) {
	waited := parallel.TaskResult{TaskName: "one", ExitCode: 1, Error: lock.InterruptedError("dev", context.Canceled)}
	ran := parallel.TaskResult{TaskName: "two", ExitCode: 0}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		results []parallel.TaskResult
		want    bool
	}{
		{name: "all waited, stopped", ctx: cancelled, results: []parallel.TaskResult{waited, waited}, want: true},
		{name: "all waited, not stopped", ctx: context.Background(), results: []parallel.TaskResult{waited}},
		{name: "one ran", ctx: cancelled, results: []parallel.TaskResult{waited, ran}},
		{name: "no results", ctx: cancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := interruptedBeforeRunning(tt.ctx, &parallel.Result{TaskResults: tt.results})
			if !tt.want {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, 130, errorExitCode(err))
			assert.Equal(t, ErrCodeInterrupted, ErrorToJSON(err).Code)
		})
	}
}
