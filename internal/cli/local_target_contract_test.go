package cli

// Characterization tests of the pre-routing behavior: what --local, local
// mode (local_fallback with no hosts list) and a local_fallback run report
// in structured output today, and where their command runs, both without a
// local host in the global config and with one.
//
// These pin current values on purpose, including ones planned to change
// (with a local host, the next change routes these runs through it, so the
// host becomes "dev" and the sync skip reason "in_place"). That change
// should show up here as explicit diffs to the expected values, not as a
// silent shift in the contract agents parse.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// localTargetObserved is what a run reports about where it ran.
type localTargetObserved struct {
	ConnectHost    string // host on the connect complete event
	ConnectReason  string // details.reason on the connect complete event
	WarnHost       string // host on the connect warn (fallback) event
	WarnReason     string // details.reason on the connect warn event
	LockHost       string // host on the lock complete event; "" when no lock phase ran
	SyncSkipReason string // details.reason on the sync skipped event
	ResultHost     string // host on the result event
	LocalReason    string // result details.local_reason
	FallbackReason string // result details.fallback.reason
	Cwd            string // where the command ran, relative to the project root
}

func observeLocalTarget(t *testing.T, events, projectDir, outFile string) localTargetObserved {
	t.Helper()
	var obs localTargetObserved
	for _, line := range strings.Split(events, "\n") {
		var ev PhaseEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		reason, _ := ev.Details["reason"].(string)
		switch {
		case ev.Type == "phase" && ev.Phase == "connect" && ev.Status == "complete":
			obs.ConnectHost, obs.ConnectReason = ev.Host, reason
		case ev.Type == "phase" && ev.Phase == "connect" && ev.Status == "warn":
			obs.WarnHost, obs.WarnReason = ev.Host, reason
		case ev.Type == "phase" && ev.Phase == "lock" && ev.Status == "complete":
			obs.LockHost = ev.Host
		case ev.Type == "phase" && ev.Phase == "sync" && ev.Status == "skipped":
			obs.SyncSkipReason = reason
		case ev.Type == "result":
			obs.ResultHost = ev.Host
			obs.LocalReason, _ = ev.Details["local_reason"].(string)
			if fb, ok := ev.Details["fallback"].(map[string]interface{}); ok {
				obs.FallbackReason, _ = fb["reason"].(string)
			}
		}
	}
	data, err := os.ReadFile(outFile)
	require.NoError(t, err, "the command ran: %s", events)
	rel, err := filepath.Rel(projectDir, strings.TrimSpace(string(data)))
	require.NoError(t, err)
	obs.Cwd = filepath.ToSlash(rel)
	return obs
}

// writeLocalTargetConfigs writes a global config with an unreachable remote
// "box" (and a local host "dev" when withLocalHost), and a project whose
// host settings are projectHosts, with a task that records its cwd. It
// returns the project dir; the caller runs from its "sub" dir.
func writeLocalTargetConfigs(t *testing.T, withLocalHost bool, projectHosts string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	global := `version: 1
defaults:
  probe_timeout: 1s
hosts:
  box:
    ssh: [nonexistent-host-rr-test.invalid]
    dir: ~/rr
`
	if withLocalHost {
		global += `  dev:
    local: true
`
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(global), 0o644))

	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	project := `version: 1
` + projectHosts + `
lock:
  timeout: 2s
  dir: ` + filepath.Join(t.TempDir(), "locks") + `
tasks:
  where:
    run: pwd -P > '` + filepath.Join(projectDir, "task.out") + `'
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "sub"), 0o755))
	return projectDir
}

const (
	hostsBox         = "hosts: [box]"
	hostsBoxFallback = "hosts: [box]\nlocal_fallback: always"
	localModeProject = "local_fallback: always" // no hosts list: local mode
)

// Today a local target reports host "local" and skips sync as "local"
// whether or not a local host exists; a local host only adds a lock phase
// on it. rr run and a task report the same, and both run in the caller's
// directory (a task on a local *host* runs at the project root instead).
func TestLocalTargetContract(t *testing.T) {
	flag := localTargetObserved{
		ConnectHost: "local", ConnectReason: "local_flag",
		SyncSkipReason: "local", ResultHost: "local", LocalReason: "local_flag", Cwd: "sub",
	}
	mode := localTargetObserved{
		ConnectHost: "local", ConnectReason: "local_mode",
		SyncSkipReason: "local", ResultHost: "local", LocalReason: "local_mode", Cwd: "sub",
	}
	fallback := localTargetObserved{
		ConnectHost: "local", WarnHost: "local", WarnReason: "hosts_unreachable",
		SyncSkipReason: "local", ResultHost: "local", FallbackReason: "hosts_unreachable", Cwd: "sub",
	}
	withLock := func(o localTargetObserved) localTargetObserved {
		o.LockHost = "dev"
		return o
	}

	tests := []struct {
		name          string
		withLocalHost bool
		projectHosts  string
		localFlag     bool
		want          localTargetObserved
	}{
		{name: "--local, no local host", projectHosts: hostsBox, localFlag: true, want: flag},
		{name: "local mode, no local host", projectHosts: localModeProject, want: mode},
		{name: "local_fallback, no local host", projectHosts: hostsBoxFallback, want: fallback},
		{name: "--local, local host configured", withLocalHost: true, projectHosts: hostsBox, localFlag: true, want: withLock(flag)},
		{name: "local mode, local host configured", withLocalHost: true, projectHosts: localModeProject, want: withLock(mode)},
		{name: "local_fallback, local host configured", withLocalHost: true, projectHosts: hostsBoxFallback, want: withLock(fallback)},
	}
	for _, tt := range tests {
		t.Run("run/"+tt.name, func(t *testing.T) {
			projectDir := writeLocalTargetConfigs(t, tt.withLocalHost, tt.projectHosts)
			t.Chdir(filepath.Join(projectDir, "sub"))
			outFile := filepath.Join(projectDir, "run.out")

			code, events, err := runQuietly(t, RunOptions{Command: "pwd -P > '" + outFile + "'", Local: tt.localFlag})
			require.NoError(t, err, events)
			require.Equal(t, 0, code, events)
			assert.Equal(t, tt.want, observeLocalTarget(t, events, projectDir, outFile), events)
		})
		t.Run("task/"+tt.name, func(t *testing.T) {
			projectDir := writeLocalTargetConfigs(t, tt.withLocalHost, tt.projectHosts)
			t.Chdir(filepath.Join(projectDir, "sub"))

			var code int
			var err error
			var events string
			captureStdout(t, func() {
				events = captureStderr(t, func() {
					code, err = RunTask(TaskOptions{TaskName: "where", Local: tt.localFlag})
				})
			})
			require.NoError(t, err, events)
			require.Equal(t, 0, code, events)
			assert.Equal(t, tt.want, observeLocalTarget(t, events, projectDir, filepath.Join(projectDir, "task.out")), events)
		})
	}
}
