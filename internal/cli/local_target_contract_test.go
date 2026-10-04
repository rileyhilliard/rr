package cli

// Contract tests of the routed behavior: what --local, local mode
// (local_fallback with no hosts list) and a local_fallback run report in
// structured output, and where their command runs, both without a local host
// in the global config and with one.
//
// With a local host these runs are routed through it: the host is reported
// as "dev", it's locked, sync is skipped as "in_place", and a task runs at
// the project root, while details.reason, local_reason and fallback still
// say why the run is here. Without one they run bare, exactly as before
// routing. Agents parse these values, so a change to them should show up
// here as an explicit diff, not as a silent shift.

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

// Without a local host, a local target reports host "local", skips sync as
// "local", takes no lock, and rr run and a task both run in the caller's
// directory. With one, it runs on that host as --host dev would: host "dev"
// everywhere, its lock, sync skipped as "in_place", rr run in the caller's
// directory and a task at the project root.
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
	// Routed through the local host "dev": its name wherever a host is
	// reported, its lock, and sync skipped as in_place. The reasons stay.
	routed := func(o localTargetObserved) localTargetObserved {
		o.ConnectHost, o.LockHost, o.ResultHost = "dev", "dev", "dev"
		if o.WarnHost != "" {
			o.WarnHost = "dev"
		}
		o.SyncSkipReason = "in_place"
		return o
	}

	tests := []struct {
		name          string
		withLocalHost bool
		projectHosts  string
		localFlag     bool
		want          localTargetObserved
		taskCwd       string // where a task runs, when not want.Cwd
	}{
		{name: "--local, no local host", projectHosts: hostsBox, localFlag: true, want: flag},
		{name: "local mode, no local host", projectHosts: localModeProject, want: mode},
		{name: "local_fallback, no local host", projectHosts: hostsBoxFallback, want: fallback},
		{name: "--local, local host configured", withLocalHost: true, projectHosts: hostsBox, localFlag: true, want: routed(flag), taskCwd: "."},
		{name: "local mode, local host configured", withLocalHost: true, projectHosts: localModeProject, want: routed(mode), taskCwd: "."},
		{name: "local_fallback, local host configured", withLocalHost: true, projectHosts: hostsBoxFallback, want: routed(fallback), taskCwd: "."},
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
			want := tt.want
			if tt.taskCwd != "" {
				want.Cwd = tt.taskCwd
			}
			assert.Equal(t, want, observeLocalTarget(t, events, projectDir, filepath.Join(projectDir, "task.out")), events)
		})
	}
}
