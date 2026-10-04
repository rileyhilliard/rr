package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
)

// writeRoutingConfigs writes a global config with an unreachable remote
// "box" whose setup_commands and env must never apply here (its setup
// leaves a marker in the temp HOME), plus a local host "dev" with its own
// setup, env and require when withLocalHost. The project uses
// projectSettings and a temp lock dir, and has a task "show" that writes
// what it sees to show.out. It returns the project dir and the marker path.
func writeRoutingConfigs(t *testing.T, withLocalHost bool, projectSettings string) (projectDir, remoteMarker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	remoteMarker = filepath.Join(home, "remote-setup-ran")
	global := `version: 1
defaults:
  probe_timeout: 1s
hosts:
  box:
    ssh: [nonexistent-host-rr-test.invalid]
    dir: ~/rr
    setup_commands:
      - touch '` + remoteMarker + `'
    env:
      FROM_HOST: box
`
	if withLocalHost {
		global += `  dev:
    local: true
    setup_commands:
      - export FROM_SETUP=yes
    env:
      FROM_HOST: dev
`
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(global), 0o644))

	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	project := `version: 1
` + projectSettings + `
lock:
  timeout: 2s
  dir: ` + filepath.Join(t.TempDir(), "locks") + `
tasks:
  show:
    run: echo "setup=$FROM_SETUP host=$FROM_HOST defaults=$FROM_DEFAULTS" > show.out
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	return projectDir, remoteMarker
}

func readTrimmed(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.TrimSpace(string(data))
}

// --local with a local host runs through it: the host's setup_commands, the
// project's defaults.setup, and its own process group with no terminal.
// Without one it runs bare, in rr's process group, with neither setup.
func TestRun_LocalFlagRoutesThroughLocalHost(t *testing.T) {
	tests := []struct {
		name          string
		withLocalHost bool
		wantOut       string
		wantOwnGroup  bool
	}{
		{name: "local host", withLocalHost: true, wantOut: "setup=yes defaults=1", wantOwnGroup: true},
		{name: "no local host", wantOut: "setup= defaults="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir, _ := writeRoutingConfigs(t, tt.withLocalHost,
				"hosts: [box]\ndefaults:\n  setup:\n    - export FROM_DEFAULTS=1")
			t.Chdir(projectDir)
			out := filepath.Join(projectDir, "run.out")
			pgid := filepath.Join(projectDir, "pgid.out")

			code, events, err := runQuietly(t, RunOptions{
				Command: `echo "setup=$FROM_SETUP defaults=$FROM_DEFAULTS" > '` + out + `'; ps -o pgid= -p $$ > '` + pgid + `'`,
				Local:   true,
			})
			require.NoError(t, err, events)
			require.Equal(t, 0, code, events)
			assert.Equal(t, tt.wantOut, readTrimmed(t, out))

			group, err := strconv.Atoi(readTrimmed(t, pgid))
			require.NoError(t, err)
			assert.Equal(t, tt.wantOwnGroup, group != syscall.Getpgrp(),
				"routed runs get their own session (no TTY); bare runs share rr's")
		})
	}
}

// A task with --local gets the local host's env and setup like a task on
// --host dev, and runs at the project root.
func TestRunTask_LocalFlagRoutesThroughLocalHost(t *testing.T) {
	projectDir, _ := writeRoutingConfigs(t, true, "hosts: [box]")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "sub"), 0o755))
	t.Chdir(filepath.Join(projectDir, "sub"))

	var code int
	var err error
	var events string
	captureStdout(t, func() {
		events = captureStderr(t, func() {
			code, err = RunTask(TaskOptions{TaskName: "show", Local: true})
		})
	})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)
	assert.Equal(t, "setup=yes host=dev defaults=", readTrimmed(t, filepath.Join(projectDir, "show.out")),
		"written at the project root, not in sub/")
}

// A routed --local run checks require: like any run on the host; a bare one
// doesn't.
func TestRun_LocalFlagChecksRequireOnLocalHost(t *testing.T) {
	const require1 = "hosts: [box]\nrequire: [rr-test-missing-tool-xyz]"

	t.Run("local host", func(t *testing.T) {
		projectDir, _ := writeRoutingConfigs(t, true, require1)
		t.Chdir(projectDir)
		marker := filepath.Join(projectDir, "ran")

		_, events, err := runQuietly(t, RunOptions{Command: "touch '" + marker + "'", Local: true})
		require.Error(t, err, events)
		assert.True(t, errors.IsCode(err, errors.ErrDependency), "got %v", err)
		assert.NoFileExists(t, marker)
	})
	t.Run("no local host", func(t *testing.T) {
		projectDir, _ := writeRoutingConfigs(t, false, require1)
		t.Chdir(projectDir)
		marker := filepath.Join(projectDir, "ran")

		code, events, err := runQuietly(t, RunOptions{Command: "touch '" + marker + "'", Local: true})
		require.NoError(t, err, events)
		assert.Equal(t, 0, code)
		assert.FileExists(t, marker)
	})
}

// Falling back from an unreachable remote never applies that remote's
// setup_commands or env here. With a local host the fallback gets the local
// host's instead: setup for both, env for tasks only, as on any host.
func TestFallback_DoesNotApplyUnreachableHostConfig(t *testing.T) {
	tests := []struct {
		name          string
		withLocalHost bool
		wantTask      string
		wantRun       string
	}{
		{name: "local host", withLocalHost: true, wantTask: "setup=yes host=dev defaults=", wantRun: "setup=yes host="},
		{name: "no local host", wantTask: "setup= host= defaults=", wantRun: "setup= host="},
	}
	for _, tt := range tests {
		t.Run("task/"+tt.name, func(t *testing.T) {
			projectDir, remoteMarker := writeRoutingConfigs(t, tt.withLocalHost, "hosts: [box]\nlocal_fallback: always")
			t.Chdir(projectDir)

			var code int
			var err error
			var events string
			captureStdout(t, func() {
				events = captureStderr(t, func() {
					code, err = RunTask(TaskOptions{TaskName: "show"})
				})
			})
			require.NoError(t, err, events)
			require.Equal(t, 0, code, events)
			require.Contains(t, events, `"local_fallback":true`)
			assert.Equal(t, tt.wantTask, readTrimmed(t, filepath.Join(projectDir, "show.out")))
			assert.NoFileExists(t, remoteMarker, "box's setup_commands never ran here")
		})
		t.Run("run/"+tt.name, func(t *testing.T) {
			projectDir, remoteMarker := writeRoutingConfigs(t, tt.withLocalHost, "hosts: [box]\nlocal_fallback: always")
			t.Chdir(projectDir)
			out := filepath.Join(projectDir, "run.out")

			code, events, err := runQuietly(t, RunOptions{Command: `echo "setup=$FROM_SETUP host=$FROM_HOST" > '` + out + `'`})
			require.NoError(t, err, events)
			require.Equal(t, 0, code, events)
			assert.Equal(t, tt.wantRun, readTrimmed(t, out))
			assert.NoFileExists(t, remoteMarker, "box's setup_commands never ran here")
		})
	}
}

// A routed --local run records its job's process group in the local host's
// lock, as a run on --host dev does.
func TestRun_LocalFlagRecordsJobInLock(t *testing.T) {
	projectDir, _ := writeRoutingConfigs(t, true, "hosts: [box]")
	t.Chdir(projectDir)
	lockDir := filepath.Join(lockBaseOf(t, projectDir), "rr.lock")

	code, events, err := runQuietly(t, RunOptions{Command: recordJobProbe(lockDir, projectDir), Local: true})
	require.NoError(t, err, events)
	require.Equal(t, 0, code, events)
	assertJobRecorded(t, projectDir)
}

// A task pinned to other hosts is refused on the host rr was sent to before
// that host's lock is waited on (a busy local host would otherwise hold an
// agent for lock.timeout just to be told no), and the suggestion says what
// to do instead.
func TestRunTask_PinnedTaskRefusedBeforeLock(t *testing.T) {
	projectDir := writePinnedTask(t, true)
	lockCfg := config.LockConfig{Enabled: true, Timeout: 2 * time.Second, Stale: 10 * time.Minute, Dir: lockBaseOf(t, projectDir)}
	holdLocalHostLock(t, lockCfg)

	code, err := runTaskQuietly(t, TaskOptions{TaskName: "pinned", Host: "dev"})
	require.Error(t, err)
	assert.Equal(t, 1, code)
	// ErrLock here would mean rr waited out the held lock first.
	assert.True(t, errors.IsCode(err, errors.ErrConfig), "got %v", err)
	var rrErr *errors.Error
	require.ErrorAs(t, err, &rrErr)
	assert.Contains(t, rrErr.Message, "can't run on host 'dev'")
	assert.Contains(t, rrErr.Suggestion, "--host box")
	assert.Contains(t, rrErr.Suggestion, "--local")
	assert.NoFileExists(t, filepath.Join(projectDir, "ran.out"))
}

// --host outside a task's pin is refused before rr dials it. Dialing first
// would let an unreachable box fall back to dev, which the pin allows, and
// run the task on a host the user didn't ask for.
func TestRunTask_HostFlagOutsidePinRefusedBeforeDial(t *testing.T) {
	projectDir := writeHostConfigs(t, `version: 1
defaults:
  probe_timeout: 1s
hosts:
  dev:
    local: true
  box:
    ssh: [nonexistent-host-rr-test.invalid]
    dir: ~/rr
`, `version: 1
hosts: [dev, box]
local_fallback: always
tasks:
  pinned:
    hosts: [dev]
    run: touch ran.out
`)
	code, err := runTaskQuietly(t, TaskOptions{TaskName: "pinned", Host: "box"})
	require.Error(t, err)
	assert.Equal(t, 1, code)
	assert.True(t, errors.IsCode(err, errors.ErrConfig), "got %v", err)
	var rrErr *errors.Error
	require.ErrorAs(t, err, &rrErr)
	assert.Equal(t, "Task 'pinned' can't run on host 'box'", rrErr.Message)
	assert.Contains(t, rrErr.Suggestion, "--host dev")
	assert.NoFileExists(t, filepath.Join(projectDir, "ran.out"))

	// A mistyped --host is a host that doesn't exist, not one the pin
	// refuses: agents branch on HOST_NOT_FOUND.
	_, err = runTaskQuietly(t, TaskOptions{TaskName: "pinned", Host: "bxo"})
	assert.True(t, errors.IsCode(err, errors.ErrHostNotFound), "got %v", err)
	assert.NoFileExists(t, filepath.Join(projectDir, "ran.out"))
}

// --local is an explicit "run it here", so it overrides a task's hosts:
// list the way it overrides the project's. A shared .rr.yaml can't name
// each person's local host, so the pin can't be widened to allow it.
func TestRunTask_LocalOverridesTaskHosts(t *testing.T) {
	for _, withLocalHost := range []bool{true, false} {
		t.Run(fmt.Sprintf("local host %v", withLocalHost), func(t *testing.T) {
			projectDir := writePinnedTask(t, withLocalHost)
			code, err := runTaskQuietly(t, TaskOptions{TaskName: "pinned", Local: true})
			require.NoError(t, err)
			assert.Equal(t, 0, code)
			assert.FileExists(t, filepath.Join(projectDir, "ran.out"))
		})
	}
}

// A parallel task with --local runs its pinned subtasks here too.
func TestRunParallelTask_LocalOverridesSubtaskHosts(t *testing.T) {
	for _, withLocalHost := range []bool{true, false} {
		t.Run(fmt.Sprintf("local host %v", withLocalHost), func(t *testing.T) {
			projectDir := writePinnedTask(t, withLocalHost)
			var code int
			var err error
			captureStderr(t, func() {
				captureStdout(t, func() {
					code, err = RunParallelTask(ParallelTaskOptions{TaskName: "both", Local: true})
				})
			})
			require.NoError(t, err)
			assert.Equal(t, 0, code)
			assert.FileExists(t, filepath.Join(projectDir, "ran.out"))
			assert.FileExists(t, filepath.Join(projectDir, "show.out"))
		})
	}
}

// Without --host or --local, a pinned task is placed only on its own hosts:
// the local host comes first in the project's order, but the task allows
// only box, so rr tries box (unreachable here) instead of locking dev and
// then refusing to run there.
func TestRunTask_SelectionHonorsTaskHosts(t *testing.T) {
	projectDir := writePinnedTask(t, true)
	_, err := runTaskQuietly(t, TaskOptions{TaskName: "pinned"})
	require.Error(t, err)
	assert.False(t, errors.IsCode(err, errors.ErrConfig), "picked a host the task doesn't allow: %v", err)
	assert.NotContains(t, err.Error(), "'dev'")
	assert.NoFileExists(t, filepath.Join(projectDir, "ran.out"))
}

// writePinnedTask writes the routing configs with project hosts [dev, box],
// a task "pinned" restricted to box that touches ran.out, and a parallel
// task "both" running pinned and show. It chdirs into the project.
func writePinnedTask(t *testing.T, withLocalHost bool) string {
	t.Helper()
	projectDir, _ := writeRoutingConfigs(t, withLocalHost, "hosts: [dev, box]")
	t.Chdir(projectDir)
	f, err := os.OpenFile(filepath.Join(projectDir, ".rr.yaml"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("  pinned:\n    hosts: [box]\n    run: touch ran.out\n  both:\n    parallel: [pinned, show]\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return projectDir
}

func runTaskQuietly(t *testing.T, opts TaskOptions) (int, error) {
	t.Helper()
	var code int
	var err error
	captureStderr(t, func() {
		captureStdout(t, func() { code, err = RunTask(opts) })
	})
	return code, err
}

// lockBaseOf reads the lock dir the project config in projectDir uses.
func lockBaseOf(t *testing.T, projectDir string) string {
	t.Helper()
	for _, line := range strings.Split(readTrimmed(t, filepath.Join(projectDir, ".rr.yaml")), "\n") {
		if dir, ok := strings.CutPrefix(strings.TrimSpace(line), "dir: "); ok {
			return dir
		}
	}
	t.Fatal("no lock dir in the project config")
	return ""
}

// writeHostConfigs writes global and project configs into a temp HOME and
// project dir, appends a temp lock dir (lock.timeout 2s) to the project,
// and chdirs into it. It returns the project dir.
func writeHostConfigs(t *testing.T, global, project string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(global), 0o644))

	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	project += "lock:\n  timeout: 2s\n  dir: " + filepath.Join(t.TempDir(), "locks") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	t.Chdir(projectDir)
	return projectDir
}

// --tag picks among the hosts a pinned task allows. A tag that only hosts
// outside the pin carry is refused up front with the restriction named,
// not reported as a tag no host has.
func TestRunTask_TagWithTaskHosts(t *testing.T) {
	// gpu is this machine's local host, so a run placed on it succeeds.
	global := `version: 1
defaults:
  probe_timeout: 1s
hosts:
  gpu:
    local: true
    tags: [big]
  cpu:
    ssh: [nonexistent-host-rr-test.invalid]
    dir: ~/rr
    tags: [fast]
`
	project := `version: 1
hosts: [gpu, cpu]
tasks:
  train:
    hosts: [gpu]
    run: touch ran.out
`
	t.Run("tag only outside the pin", func(t *testing.T) {
		projectDir := writeHostConfigs(t, global, project)
		code, err := runTaskQuietly(t, TaskOptions{TaskName: "train", Tag: "fast"})
		require.Error(t, err)
		assert.Equal(t, 1, code)
		assert.True(t, errors.IsCode(err, errors.ErrConfig), "got %v", err)
		var rrErr *errors.Error
		require.ErrorAs(t, err, &rrErr)
		assert.Equal(t, "Task 'train' can't run on any host tagged 'fast'", rrErr.Message)
		assert.Contains(t, rrErr.Suggestion, "restricted to: gpu")
		assert.NoFileExists(t, filepath.Join(projectDir, "ran.out"))
	})
	t.Run("tag on a pinned host", func(t *testing.T) {
		projectDir := writeHostConfigs(t, global, project)
		code, err := runTaskQuietly(t, TaskOptions{TaskName: "train", Tag: "big"})
		require.NoError(t, err)
		assert.Equal(t, 0, code)
		assert.FileExists(t, filepath.Join(projectDir, "ran.out"))
	})
	t.Run("tag no host has", func(t *testing.T) {
		writeHostConfigs(t, global, project)
		_, err := runTaskQuietly(t, TaskOptions{TaskName: "train", Tag: "nope"})
		require.Error(t, err)
		var rrErr *errors.Error
		require.ErrorAs(t, err, &rrErr)
		assert.Equal(t, "No hosts have the 'nope' tag", rrErr.Message)
		assert.Contains(t, rrErr.Suggestion, "big")
		assert.Contains(t, rrErr.Suggestion, "fast", "lists the tags of every host, not just the pinned ones")
	})
}

// A task pinned to hosts that leave out this machine never falls back to
// it: the fallback would only be refused, after waiting on the local host's
// lock when it's busy. rr reports the pinned hosts it couldn't reach instead.
func TestRunTask_PinnedTaskDoesNotFallBack(t *testing.T) {
	global := `version: 1
defaults:
  probe_timeout: 1s
hosts:
  dev:
    local: true
  box:
    ssh: [nonexistent-host-rr-test.invalid]
    dir: ~/rr
  box2:
    ssh: [nonexistent-host-rr-test-2.invalid]
    dir: ~/rr
`
	tests := []struct {
		name  string
		pin   string
		hosts []string
	}{
		// Two pinned hosts take the load-balanced path, one the single-host path.
		{name: "load balanced", pin: "[box, box2]", hosts: []string{"box", "box2"}},
		{name: "single host", pin: "[box]", hosts: []string{"box"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := writeHostConfigs(t, global, `version: 1
hosts: [dev, box, box2]
local_fallback: always
tasks:
  pinned:
    hosts: `+tt.pin+`
    run: touch ran.out
`)
			lockCfg := config.LockConfig{Enabled: true, Timeout: 2 * time.Second, Stale: 10 * time.Minute, Dir: lockBaseOf(t, projectDir)}
			holdLocalHostLock(t, lockCfg)

			start := time.Now()
			code, err := runTaskQuietly(t, TaskOptions{TaskName: "pinned"})
			require.Error(t, err)
			assert.Equal(t, 1, code)
			// ErrLock would mean rr fell back and waited out dev's lock.
			assert.False(t, errors.IsCode(err, errors.ErrLock), "waited on the local host's lock: %v", err)
			assert.True(t, errors.IsCode(err, errors.ErrSSH), "got %v", err)
			var rrErr *errors.Error
			require.ErrorAs(t, err, &rrErr)
			for _, h := range tt.hosts {
				assert.Contains(t, rrErr.Message, h)
			}
			assert.NotContains(t, rrErr.Suggestion, "set 'local_fallback: true'", "local_fallback is already on")
			assert.Contains(t, rrErr.Suggestion, "--local")
			assert.Less(t, time.Since(start), 2*time.Second, "returned before lock.timeout")
			assert.NoFileExists(t, filepath.Join(projectDir, "ran.out"))
		})
	}
}
