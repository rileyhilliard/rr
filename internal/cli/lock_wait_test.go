package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/lock"
)

// phaseEvents parses the JSON lines in events and returns those matching
// phase and status.
func phaseEvents(t *testing.T, events, phase, status string) []PhaseEvent {
	t.Helper()
	var found []PhaseEvent
	for _, line := range strings.Split(events, "\n") {
		var ev PhaseEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "phase" && ev.Phase == phase && ev.Status == status {
			found = append(found, ev)
		}
	}
	return found
}

// A run that finds its single host locked says so while it waits: one
// lock/waiting event naming the holder, in the same details shape the
// load-balanced connect/waiting event uses.
func TestRun_LockWaitReportsHolder(t *testing.T) {
	projectDir, lockCfg := writeMachineLockConfigs(t, "[dev]")
	t.Chdir(projectDir)
	defer lock.SetRetryIntervalForTesting(20 * time.Millisecond)()
	holdMachineLock(t, lockCfg)

	_, events, err := runQuietly(t, RunOptions{Command: "true"})
	require.Error(t, err, events)
	assert.True(t, errors.IsCode(err, errors.ErrLock), "got %v", err)

	waiting := phaseEvents(t, events, "lock", "waiting")
	require.Len(t, waiting, 1, events)
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
	assert.Equal(t, float64(os.Getpid()), holder["pid"])
}

// recordJobProbe returns a command that waits for job_pgid to appear in the
// lock's info file, then copies the file and its own process group into dir.
func recordJobProbe(lockDir, dir string) string {
	info := filepath.Join(lockDir, "info.json")
	return `for i in $(seq 50); do grep -q job_pgid '` + info + `' && break; sleep 0.1; done; ` +
		`cp '` + info + `' '` + filepath.Join(dir, "info.out") + `'; ` +
		`ps -o pgid= -p $$ > '` + filepath.Join(dir, "pgid.out") + `'`
}

// assertJobRecorded checks that the lock info the probe saw names the
// probe's own process group.
func assertJobRecorded(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "info.out"))
	require.NoError(t, err)
	info, err := lock.ParseLockInfo(data)
	require.NoError(t, err)
	pgidOut, err := os.ReadFile(filepath.Join(dir, "pgid.out"))
	require.NoError(t, err)
	pgid, err := strconv.Atoi(strings.TrimSpace(string(pgidOut)))
	require.NoError(t, err)
	assert.Equal(t, pgid, info.JobPGID, "the lock names the running job's process group")
	assert.Equal(t, os.Getpid(), info.PID, "and still names rr as the holder")
}

// A run on a local host records its job's process group in the lock, so a
// SIGKILLed rr doesn't free the lock while the job runs on.
func TestRun_LocalHostRecordsJobInLock(t *testing.T) {
	projectDir, lockDir := writeLocalHostConfigs(t)
	t.Chdir(projectDir)

	code, events, err := runQuietly(t, RunOptions{Command: recordJobProbe(lockDir, projectDir)})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)
	assertJobRecorded(t, projectDir)
}

// A parallel task on a local host records each subtask's process group too.
func TestRunParallelTask_LocalHostRecordsJobInLock(t *testing.T) {
	projectDir, lockDir := writeLocalHostConfigs(t)
	t.Chdir(projectDir)
	project := `version: 1
hosts: [dev]
lock:
  dir: ` + filepath.Dir(lockDir) + `
tasks:
  pair:
    parallel: [probe]
  probe:
    run: ` + strconv.Quote(recordJobProbe(lockDir, projectDir)) + `
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))

	var code int
	var err error
	var events string
	captureStdout(t, func() {
		events = captureStderr(t, func() {
			code, err = RunParallelTask(ParallelTaskOptions{TaskName: "pair", NoLogs: true})
		})
	})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)
	assertJobRecorded(t, projectDir)
}
