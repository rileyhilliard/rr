package parallel

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
)

// A host with local: true gets a worker like any other host: subtasks run
// in its dir (the project dir) with its env and setup_commands, under its
// lock, and nothing is synced.
func TestOrchestrator_LocalHostRunsSubtasksInPlaceUnderLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	projectDir := t.TempDir()
	lockBase := filepath.Join(t.TempDir(), "locks")
	lockDir := filepath.Join(lockBase, "rr.lock")

	hosts := map[string]config.Host{
		"dev": {
			Local:         true,
			Dir:           projectDir, // config.ResolveHosts sets this to the project root
			Env:           map[string]string{"FROM_HOST": "env-ok"},
			SetupCommands: []string{"export FROM_SETUP=setup-ok"},
		},
	}
	// Each subtask proves it ran in the project dir, saw the host env and
	// setup, and held the host lock while running.
	cmd := func(name string) string {
		return `test -d '` + lockDir + `' && echo "$FROM_HOST $FROM_SETUP" > ` + name + `.out`
	}
	tasks := []TaskInfo{
		{Name: "a", Index: 0, Command: cmd("a")},
		{Name: "b", Index: 1, Command: cmd("b")},
		{Name: "c", Index: 2, Command: cmd("c")},
	}
	resolved := &config.ResolvedConfig{
		Project: &config.Config{Lock: config.LockConfig{
			Enabled: true, Timeout: 5 * time.Second, Stale: time.Minute, Dir: lockBase,
		}},
	}
	orch := NewOrchestrator(tasks, hosts, []string{"dev"}, resolved, Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := orch.Run(ctx)
	require.NoError(t, err)
	require.Len(t, result.TaskResults, 3)

	for _, tr := range result.TaskResults {
		assert.True(t, tr.Success(), "%s failed: exit %d err %v output %q", tr.TaskName, tr.ExitCode, tr.Error, tr.Output)
		assert.Equal(t, "dev", tr.Host)
		assert.Equal(t, "local", tr.Alias)

		data, readErr := os.ReadFile(filepath.Join(projectDir, tr.TaskName+".out"))
		require.NoError(t, readErr, "subtask %s runs in the project dir", tr.TaskName)
		assert.Equal(t, "env-ok setup-ok", strings.TrimSpace(string(data)))
	}
	assert.Equal(t, []string{"dev"}, result.HostsUsed)

	_, statErr := os.Stat(lockDir)
	assert.True(t, os.IsNotExist(statErr), "the lock is released when the run ends")
}

// The local host shares the work queue with remote hosts: when a remote
// can't be reached, its subtasks move to the local host.
func TestOrchestrator_LocalHostTakesWorkFromUnreachableRemote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	hosts := map[string]config.Host{
		"remote": {SSH: []string{"nonexistent-host-a-xxxx"}, Dir: "~"},
		"dev":    {Local: true, Dir: t.TempDir()},
	}
	tasks := []TaskInfo{
		{Name: "a", Index: 0, Command: "true"},
		{Name: "b", Index: 1, Command: "true"},
	}
	resolved := &config.ResolvedConfig{
		Project: &config.Config{Lock: config.LockConfig{
			Enabled: true, Timeout: 5 * time.Second, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks"),
		}},
	}
	orch := NewOrchestrator(tasks, hosts, []string{"remote", "dev"}, resolved, Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := orch.Run(ctx)
	require.NoError(t, err)
	require.Len(t, result.TaskResults, 2)
	for _, tr := range result.TaskResults {
		assert.True(t, tr.Success(), "%s: exit %d err %v", tr.TaskName, tr.ExitCode, tr.Error)
		assert.Equal(t, "dev", tr.Host)
	}
}
