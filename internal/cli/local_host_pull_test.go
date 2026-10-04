package cli

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/parallel"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
)

func skipWithoutRsync(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	if _, err := rrsync.FindRsync(); err != nil {
		t.Skip("rsync not installed")
	}
}

// A run on the local host leaves its files in the project dir. Pulling them
// to another dest copies them there, and reports the pull phase.
func TestRun_LocalHostPullCopiesToDest(t *testing.T) {
	skipWithoutRsync(t)
	projectDir, _ := writeLocalHostConfigs(t)
	t.Chdir(projectDir)
	dest := filepath.Join(t.TempDir(), "artifacts")

	code, events, err := runQuietly(t, RunOptions{
		Command:  "mkdir -p out && echo hi > out/report.txt",
		Pull:     []string{"out/report.txt"},
		PullDest: dest,
	})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)

	got, readErr := os.ReadFile(filepath.Join(dest, "report.txt"))
	require.NoError(t, readErr, events)
	assert.Equal(t, "hi\n", string(got))

	complete := eventsWith(parseEvents(t, events), "pull", "complete")
	require.Len(t, complete, 1, events)
	assert.Equal(t, "dev", complete[0].Host)
}

// Pulling into the dir the command ran in copies nothing, and says so.
func TestRun_LocalHostPullIntoProjectDirIsSkipped(t *testing.T) {
	skipWithoutRsync(t)
	projectDir, _ := writeLocalHostConfigs(t)
	t.Chdir(projectDir)

	code, events, err := runQuietly(t, RunOptions{Command: "echo hi > report.txt", Pull: []string{"report.txt"}})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)

	skipped := eventsWith(parseEvents(t, events), "pull", "skipped")
	require.Len(t, skipped, 1, events)
	assert.Equal(t, "same_dir", skipped[0].Details["reason"])
}

// A parallel subtask on the local host gets its own <dest>/<stem>/ copy, as
// a subtask on a remote host does.
func TestPullSubtaskFiles_LocalHostCopiesPerSubtask(t *testing.T) {
	skipWithoutRsync(t)
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "junit.xml"), []byte("<x/>"), 0o644))
	t.Chdir(t.TempDir())

	hosts := map[string]config.Host{"dev": {Local: true, Dir: projectDir}}
	tasks := []parallel.TaskInfo{
		{Name: "unit", Index: 0, Pull: []config.PullItem{{Src: "junit.xml"}}},
		{Name: "lint", Index: 1, Pull: []config.PullItem{{Src: "junit.xml", Dest: "reports"}}},
	}
	result := &parallel.Result{TaskResults: []parallel.TaskResult{
		{TaskName: "unit", TaskIndex: 0, Host: "dev", Alias: host.LocalAlias},
		{TaskName: "lint", TaskIndex: 1, Host: "dev", Alias: host.LocalAlias},
	}}

	called := false
	captureStderr(t, func() {
		pullSubtaskFiles(tasks, result, hosts, projectDir, func(*host.Connection, rrsync.PullOptions, io.Writer) error {
			called = true
			return nil
		})
	})
	assert.False(t, called, "an in-place subtask is copied locally, not pulled over SSH")

	for _, p := range []string{filepath.Join("unit_0", "junit.xml"), filepath.Join("reports", "lint_1", "junit.xml")} {
		_, statErr := os.Stat(p)
		assert.NoError(t, statErr, p)
	}
}

// rr sync and rr pull default to the first remote host. A local host is
// never their default, and naming one is a config error.
func TestSyncAndPull_LocalHost(t *testing.T) {
	projectDir, _ := writeMachineLockConfigs(t, "[dev, box]")
	t.Chdir(projectDir)

	var err error
	captureStdout(t, func() {
		captureStderr(t, func() { err = Sync(SyncOptions{Host: "dev"}) })
	})
	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrConfig), "got %v", err)
	assert.Contains(t, err.Error(), "'dev' is a local host; there's nothing to sync to it")

	captureStdout(t, func() {
		captureStderr(t, func() { err = Sync(SyncOptions{}) })
	})
	require.Error(t, err, "the default is box, which is unreachable, not dev")
	assert.True(t, errors.IsCode(err, errors.ErrSSH), "got %v", err)

	captureStdout(t, func() {
		captureStderr(t, func() { err = Pull(PullOptions{Patterns: []string{"x"}, Host: "dev"}) })
	})
	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrConfig), "got %v", err)
	assert.Contains(t, err.Error(), "'dev' is a local host; there's nothing to pull from it")

	captureStdout(t, func() {
		captureStderr(t, func() { err = Pull(PullOptions{Patterns: []string{"x"}}) })
	})
	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrSSH), "got %v", err)
}

// A run on a local host skips sync because it runs in place; --local and a
// fallback skip it because they run locally. The reasons say which.
func TestSyncPhase_SkipReasons(t *testing.T) {
	withStructuredOutput(t)
	reason := func(conn *host.Connection) interface{} {
		ctx := &WorkflowContext{Conn: conn}
		events := captureStderr(t, func() { require.NoError(t, syncPhase(ctx, WorkflowOptions{})) })
		skipped := eventsWith(parseEvents(t, events), "sync", "skipped")
		require.Len(t, skipped, 1)
		return skipped[0].Details["reason"]
	}

	assert.Equal(t, "in_place", reason(host.NewLocalHostConnection("dev", config.Host{Local: true})))
	assert.Equal(t, "local", reason(localConnection()))
}
