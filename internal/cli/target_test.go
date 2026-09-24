package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeGlobalConfig points HOME at a temp dir with the given
// ~/.rr/config.yaml content.
func writeGlobalConfig(t *testing.T, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(content), 0o644))
}

// inProject moves the test into a fresh project dir with the given .rr.yaml.
func inProject(t *testing.T, rrYAML string) string {
	t.Helper()
	dir := inEmptyProjectDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".rr.yaml"), []byte(rrYAML), 0o644))
	return dir
}

// withStructuredOutput forces structured (non-pretty) output for the test.
func withStructuredOutput(t *testing.T) {
	t.Helper()
	prev := prettyMode
	prettyMode = false
	t.Cleanup(func() { prettyMode = prev })
}

// withPrettyOutput forces --pretty output for the test.
func withPrettyOutput(t *testing.T) {
	t.Helper()
	prev := prettyMode
	prettyMode = true
	t.Cleanup(func() { prettyMode = prev })
}

// parseEvents decodes the JSON-line events in captured stderr, failing on any
// line that isn't JSON (structured output must stay machine-readable).
func parseEvents(t *testing.T, stderr string) []PhaseEvent {
	t.Helper()
	var events []PhaseEvent
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if line == "" {
			continue
		}
		var ev PhaseEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev), "non-JSON line on stderr: %q", line)
		events = append(events, ev)
	}
	return events
}

// eventsWith returns the events matching phase and status.
func eventsWith(events []PhaseEvent, phase, status string) []PhaseEvent {
	var out []PhaseEvent
	for _, ev := range events {
		if ev.Phase == phase && ev.Status == status {
			out = append(out, ev)
		}
	}
	return out
}

// resultEvent returns the final result event.
func resultEvent(t *testing.T, events []PhaseEvent) PhaseEvent {
	t.Helper()
	for _, ev := range events {
		if ev.Type == "result" {
			return ev
		}
	}
	require.Fail(t, "no result event")
	return PhaseEvent{}
}

// runCaptured runs fn and returns its stdout and stderr.
func runCaptured(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, fn)
	})
	return stdout, stderr
}

// assertLocalTargetConnect checks the connect phase of a local-target run: a
// normal complete event on host local carrying the reason, the reason again
// as details.local_reason on the result, and no fallback warning or result
// fallback, because nothing went wrong.
func assertLocalTargetConnect(t *testing.T, events []PhaseEvent, wantReason string) {
	t.Helper()
	assertLocalConnectEvents(t, events, wantReason)
	result := resultEvent(t, events)
	assert.Equal(t, wantReason, result.Details["local_reason"])
	assert.NotContains(t, result.Details, "fallback")
}

// assertLocalConnectEvents checks the connect events of a local-target run:
// one complete on host local with the reason, and no fallback warning.
func assertLocalConnectEvents(t *testing.T, events []PhaseEvent, wantReason string) {
	t.Helper()
	assert.Empty(t, eventsWith(events, "connect", "warn"), "a local target is not a fallback")
	complete := eventsWith(events, "connect", "complete")
	require.Len(t, complete, 1)
	assert.Equal(t, "local", complete[0].Host)
	assert.Equal(t, wantReason, complete[0].Details["reason"])
}

func TestResolveExecTarget(t *testing.T) {
	always := config.LocalFallbackAlways
	oneHost := map[string]config.Host{"dev": {SSH: []string{"dev"}}}

	tests := []struct {
		name     string
		resolved *config.ResolvedConfig
		local    bool
		hostFlag string
		tagFlag  string
		want     execTarget
	}{
		{
			name:     "--local",
			resolved: &config.ResolvedConfig{Global: &config.GlobalConfig{Hosts: oneHost}},
			local:    true,
			want:     execTarget{local: true, reason: host.LocalReasonFlag},
		},
		{
			name: "project local_fallback without hosts",
			resolved: &config.ResolvedConfig{
				Global:  &config.GlobalConfig{Hosts: oneHost},
				Project: &config.Config{LocalFallback: &always},
			},
			want: execTarget{local: true, reason: host.LocalReasonMode},
		},
		{
			name: "global local_fallback with zero hosts",
			resolved: &config.ResolvedConfig{Global: &config.GlobalConfig{
				Hosts:    map[string]config.Host{},
				Defaults: config.GlobalDefaults{LocalFallback: always},
			}},
			want: execTarget{local: true, reason: host.LocalReasonMode},
		},
		{
			name: "--host overrides local mode",
			resolved: &config.ResolvedConfig{
				Global:  &config.GlobalConfig{Hosts: oneHost},
				Project: &config.Config{LocalFallback: &always},
			},
			hostFlag: "dev",
			want:     execTarget{},
		},
		{
			name: "--tag overrides local mode",
			resolved: &config.ResolvedConfig{
				Global:  &config.GlobalConfig{Hosts: oneHost},
				Project: &config.Config{LocalFallback: &always},
			},
			tagFlag: "gpu",
			want:    execTarget{},
		},
		{
			name: "global local_fallback with hosts stays remote",
			resolved: &config.ResolvedConfig{Global: &config.GlobalConfig{
				Hosts:    oneHost,
				Defaults: config.GlobalDefaults{LocalFallback: always},
			}},
			want: execTarget{},
		},
		{
			name: "project lists hosts stays remote",
			resolved: &config.ResolvedConfig{
				Global:  &config.GlobalConfig{Hosts: oneHost},
				Project: &config.Config{LocalFallback: &always, Hosts: []string{"dev"}},
			},
			want: execTarget{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveExecTarget(tt.resolved, tt.local, tt.hostFlag, tt.tagFlag)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRun_LocalTargetWithoutRemoteHosts covers the D-2 reproductions for
// rr run/exec: a local target needs no configured hosts, never dials, and
// reports a normal connect completion with the reason.
func TestRun_LocalTargetWithoutRemoteHosts(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T)
		local      bool
		wantReason string
	}{
		{
			name: "--local with zero global hosts",
			setup: func(t *testing.T) {
				withGlobalHosts(t)
				inProject(t, "version: 1\n")
			},
			local:      true,
			wantReason: host.LocalReasonFlag,
		},
		{
			name: "--local with a global host",
			setup: func(t *testing.T) {
				withGlobalHosts(t, "dev")
				inProject(t, "version: 1\n")
			},
			local:      true,
			wantReason: host.LocalReasonFlag,
		},
		{
			name: "project local_fallback, no hosts listed, zero global hosts",
			setup: func(t *testing.T) {
				withGlobalHosts(t)
				inProject(t, "version: 1\nlocal_fallback: on-unreachable\n")
			},
			wantReason: host.LocalReasonMode,
		},
		{
			name: "project local_fallback, no hosts listed, a global host",
			setup: func(t *testing.T) {
				withGlobalHosts(t, "dev")
				inProject(t, "version: 1\nlocal_fallback: always\n")
			},
			wantReason: host.LocalReasonMode,
		},
		{
			name: "global local_fallback, zero global hosts",
			setup: func(t *testing.T) {
				writeGlobalConfig(t, "version: 1\nhosts: {}\ndefaults:\n  local_fallback: on-unreachable\n")
				inProject(t, "version: 1\n")
			},
			wantReason: host.LocalReasonMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withStructuredOutput(t)
			tt.setup(t)

			var exitCode int
			var runErr error
			stdout, stderr := runCaptured(t, func() {
				exitCode, runErr = Run(RunOptions{Command: "echo ok", Local: tt.local, Quiet: true})
			})

			require.NoError(t, runErr)
			assert.Equal(t, 0, exitCode)
			assert.Contains(t, stdout, "ok")
			assertLocalTargetConnect(t, parseEvents(t, stderr), tt.wantReason)
		})
	}
}

// TestRunTask_LocalFlag covers the task repros: --local with a global host
// is a local_flag target, not a hosts_unreachable fallback, and the exec
// event shows the command with args applied, not the {args} template.
func TestRunTask_LocalFlag(t *testing.T) {
	withStructuredOutput(t)
	withGlobalHosts(t, "dev")
	inProject(t, "version: 1\ntasks:\n  hi:\n    run: echo hi {args}\n")

	var exitCode int
	var runErr error
	stdout, stderr := runCaptured(t, func() {
		exitCode, runErr = RunTask(TaskOptions{TaskName: "hi", Args: []string{"there"}, Local: true, Quiet: true})
	})

	require.NoError(t, runErr)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hi there")

	events := parseEvents(t, stderr)
	assertLocalTargetConnect(t, events, host.LocalReasonFlag)

	want, err := exec.ApplyTaskArgs("echo hi {args}", []string{"there"})
	require.NoError(t, err)
	execStarted := eventsWith(events, "exec", "started")
	require.Len(t, execStarted, 1)
	assert.Equal(t, want, execStarted[0].Details["command"])
	assert.NotContains(t, execStarted[0].Details["command"], "{args}")
}

// TestRunTask_PrettyFailureBlock checks --pretty task runs render the parsed
// test failure block (previously only rr run had it, and it never fired),
// and that the local line names the real reason.
func TestRunTask_PrettyFailureBlock(t *testing.T) {
	withPrettyOutput(t)
	withGlobalHosts(t, "dev")
	dir := inProject(t, "version: 1\ntasks:\n  t:\n    run: cat pytest.log; exit 1\n")
	log := `============================= test session starts ==============================
collected 2 items

tests/test_math.py::test_add PASSED [50%]
tests/test_math.py::test_div FAILED [100%]

=================================== FAILURES ===================================
_________________________________ test_div _________________________________

    def test_div():
>       assert divide(1, 0) == 0
E       ZeroDivisionError: division by zero

tests/test_math.py:12: ZeroDivisionError
========================= 1 failed, 1 passed in 0.03s ==========================
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pytest.log"), []byte(log), 0o644))

	var exitCode int
	stdout, _ := runCaptured(t, func() {
		exitCode, _ = RunTask(TaskOptions{TaskName: "t", Local: true, Quiet: true})
	})

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, stdout, "Running locally (--local)")
	assert.NotContains(t, stdout, "unreachable")
	assert.Contains(t, stdout, "tests/test_math.py:12")
	assert.Contains(t, stdout, "division by zero")
}

// TestRunRepeatedAndParallel_LocalWithoutHosts checks --repeat and parallel
// tasks honor --local before resolving hosts, so zero hosts is fine, and
// report the local target the same way single runs do.
func TestRunRepeatedAndParallel_LocalWithoutHosts(t *testing.T) {
	t.Run("rr run --repeat --local", func(t *testing.T) {
		withStructuredOutput(t)
		withGlobalHosts(t)
		inProject(t, "version: 1\n")

		var exitCode int
		var runErr error
		stdout, stderr := runCaptured(t, func() {
			exitCode, runErr = runRepeated("true", 2, "", "", true)
		})
		require.NoError(t, runErr)
		assert.Equal(t, 0, exitCode)
		assert.Empty(t, stdout, "structured mode prints no summary")
		events := parseEvents(t, stderr)
		assertLocalTargetConnect(t, events, host.LocalReasonFlag)
		result := resultEvent(t, events)
		assert.Equal(t, "success", result.Status)
		assert.EqualValues(t, 2, result.Details["passed"])
	})

	t.Run("rr <task> --repeat --local", func(t *testing.T) {
		withStructuredOutput(t)
		withGlobalHosts(t)
		inProject(t, "version: 1\ntasks:\n  t:\n    run: \"false\"\n")

		var exitCode int
		var runErr error
		stdout, stderr := runCaptured(t, func() {
			exitCode, runErr = runTaskRepeated("t", 2, "", "", true)
		})
		require.NoError(t, runErr)
		assert.Equal(t, 1, exitCode)
		assert.Empty(t, stdout, "structured mode prints no summary")
		events := parseEvents(t, stderr)
		assertLocalTargetConnect(t, events, host.LocalReasonFlag)
		result := resultEvent(t, events)
		assert.Equal(t, "failed", result.Status)
		assert.EqualValues(t, 2, result.Details["failed"])
	})

	t.Run("rr run --repeat --pretty prints the summary", func(t *testing.T) {
		withPrettyOutput(t)
		withGlobalHosts(t)
		inProject(t, "version: 1\n")

		var runErr error
		stdout, _ := runCaptured(t, func() {
			_, runErr = runRepeated("true", 2, "", "", true)
		})
		require.NoError(t, runErr)
		assert.Contains(t, stdout, "Parallel Execution Summary")
		assert.Contains(t, stdout, "2 passed")
	})

	t.Run("parallel task --local", func(t *testing.T) {
		withStructuredOutput(t)
		withGlobalHosts(t)
		inProject(t, "version: 1\ntasks:\n  a:\n    run: \"true\"\n  b:\n    run: \"true\"\n  both:\n    parallel: [a, b]\n")

		var exitCode int
		var runErr error
		_, stderr := runCaptured(t, func() {
			exitCode, runErr = RunParallelTask(ParallelTaskOptions{TaskName: "both", Local: true, NoLogs: true})
		})
		require.NoError(t, runErr)
		assert.Equal(t, 0, exitCode)
		assertLocalTargetConnect(t, parseEvents(t, stderr), host.LocalReasonFlag)
	})

	t.Run("parallel task in local mode", func(t *testing.T) {
		withStructuredOutput(t)
		withGlobalHosts(t)
		inProject(t, "version: 1\nlocal_fallback: on-unreachable\ntasks:\n  a:\n    run: \"true\"\n  both:\n    parallel: [a]\n")

		var runErr error
		_, stderr := runCaptured(t, func() {
			_, runErr = RunParallelTask(ParallelTaskOptions{TaskName: "both", NoLogs: true})
		})
		require.NoError(t, runErr)
		assertLocalTargetConnect(t, parseEvents(t, stderr), host.LocalReasonMode)
	})
}

// TestRun_LoadBalancedUnreachableFallsBack checks the multi-host path falls
// back locally when no host can be reached and local_fallback allows it,
// same as the single-host path, with reason hosts_unreachable.
func TestRun_LoadBalancedUnreachableFallsBack(t *testing.T) {
	withStructuredOutput(t)
	writeGlobalConfig(t, `version: 1
defaults:
  probe_timeout: 1s
hosts:
  a:
    ssh: [rr-test-unreachable-a.invalid]
    dir: ~/rr/proj
  b:
    ssh: [rr-test-unreachable-b.invalid]
    dir: ~/rr/proj
`)
	inProject(t, "version: 1\nhosts: [a, b]\nlocal_fallback: on-unreachable\n")

	var exitCode int
	var runErr error
	_, stderr := runCaptured(t, func() {
		exitCode, runErr = Run(RunOptions{Command: "true", Quiet: true})
	})

	require.NoError(t, runErr)
	assert.Equal(t, 0, exitCode)
	events := parseEvents(t, stderr)
	warns := eventsWith(events, "connect", "warn")
	require.Len(t, warns, 1)
	assert.Equal(t, host.LocalReasonHostsUnreachable, warns[0].Details["reason"])
	fb, ok := resultEvent(t, events).Details["fallback"].(map[string]interface{})
	require.True(t, ok, "result must carry details.fallback")
	assert.Equal(t, host.LocalReasonHostsUnreachable, fb["reason"])
}

func TestSyncOptions_Invalidated(t *testing.T) {
	t.Run("structured emits an invalidated event", func(t *testing.T) {
		withStructuredOutput(t)
		opts := syncOptions()
		require.NotNil(t, opts.Invalidated, "nil falls back to a plain Printf on stdout")

		stdout, stderr := runCaptured(t, func() { opts.Invalidated("node_modules", "package-lock.json") })
		assert.Empty(t, stdout)
		events := parseEvents(t, stderr)
		require.Len(t, events, 1)
		assert.Equal(t, "sync", events[0].Phase)
		assert.Equal(t, "invalidated", events[0].Status)
		assert.Equal(t, "node_modules", events[0].Details["dir"])
		assert.Equal(t, "package-lock.json", events[0].Details["lockfile"])
	})

	t.Run("pretty prints a line", func(t *testing.T) {
		withPrettyOutput(t)
		opts := syncOptions()
		require.NotNil(t, opts.Invalidated)

		stdout, stderr := runCaptured(t, func() { opts.Invalidated("node_modules", "package-lock.json") })
		assert.Contains(t, stdout, "Invalidating stale node_modules (package-lock.json changed)")
		assert.Empty(t, stderr)
	})
}

// A structured parallel run reports a host it couldn't reach as a connect
// warn event and leaves stdout to the subtasks' own output.
func TestRunParallelTask_StructuredRequeueIsAnEvent(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		t.Run(fmt.Sprintf("quiet=%v", quiet), func(t *testing.T) {
			withStructuredOutput(t)
			writeGlobalConfig(t, `version: 1
defaults:
  probe_timeout: 1s
hosts:
  a:
    ssh: [rr-test-unreachable-a.invalid]
    dir: ~/rr/proj
`)
			inProject(t, "version: 1\nhosts: [a]\ntasks:\n  x:\n    run: \"true\"\n  both:\n    parallel: [x]\n")

			stdout, stderr := runCaptured(t, func() {
				_, _ = RunParallelTask(ParallelTaskOptions{TaskName: "both", NoLogs: true, Quiet: quiet})
			})

			assert.Empty(t, stdout)
			warns := eventsWith(parseEvents(t, stderr), "connect", "warn")
			require.Len(t, warns, 1)
			assert.Equal(t, "a", warns[0].Host)
			assert.Equal(t, "connect_failed", warns[0].Details["reason"])
			assert.Equal(t, "x", warns[0].Details["task"])
			errDetail, ok := warns[0].Details["error"].(map[string]interface{})
			require.True(t, ok, "the warn event carries the cause")
			assert.Equal(t, "SSH_CONNECTION_FAILED", errDetail["code"])
		})
	}
}

// A structured run of a task with depends leaves stdout to the tasks' own
// output; the stage display is for --pretty.
func TestRunTask_DependsStructuredStdout(t *testing.T) {
	withStructuredOutput(t)
	inProject(t, "version: 1\ntasks:\n  a:\n    run: echo a\n  b:\n    run: echo b\n    depends: [a]\n")

	var exitCode int
	var runErr error
	stdout, stderr := runCaptured(t, func() {
		exitCode, runErr = RunTask(TaskOptions{TaskName: "b", Local: true})
	})

	require.NoError(t, runErr)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "a\nb\n", stdout)
	resultEvent(t, parseEvents(t, stderr))
}
