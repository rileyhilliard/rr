package cli

import (
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
	tests := []struct {
		name          string
		withLocalHost bool
		opts          TaskOptions
		wantSuggests  []string
		notSuggests   []string
	}{
		{
			name:          "--local with a local host",
			withLocalHost: true,
			opts:          TaskOptions{TaskName: "pinned", Local: true},
			wantSuggests:  []string{"box", "Drop --local", "add 'dev' to the task's hosts"},
		},
		{
			name:         "--local without a local host",
			opts:         TaskOptions{TaskName: "pinned", Local: true},
			wantSuggests: []string{"box", "Drop --local"},
			notSuggests:  []string{"add 'local'"},
		},
		{
			name:          "--host naming the local host",
			withLocalHost: true,
			opts:          TaskOptions{TaskName: "pinned", Host: "dev"},
			wantSuggests:  []string{"--host box", "add 'dev' to the task's hosts"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir, _ := writeRoutingConfigs(t, tt.withLocalHost, "hosts: [box, dev]")
			t.Chdir(projectDir)
			f, err := os.OpenFile(filepath.Join(projectDir, ".rr.yaml"), os.O_APPEND|os.O_WRONLY, 0)
			require.NoError(t, err)
			_, err = f.WriteString("  pinned:\n    hosts: [box]\n    run: touch ran.out\n")
			require.NoError(t, err)
			require.NoError(t, f.Close())
			if tt.withLocalHost {
				lockCfg := config.LockConfig{Enabled: true, Timeout: 2 * time.Second, Stale: 10 * time.Minute, Dir: lockBaseOf(t, projectDir)}
				holdLocalHostLock(t, lockCfg)
			}

			var code int
			captureStderr(t, func() {
				captureStdout(t, func() { code, err = RunTask(tt.opts) })
			})
			require.Error(t, err)
			assert.Equal(t, 1, code)
			// ErrLock here would mean rr waited out the held lock first.
			assert.True(t, errors.IsCode(err, errors.ErrConfig), "got %v", err)
			var rrErr *errors.Error
			require.ErrorAs(t, err, &rrErr)
			assert.Contains(t, rrErr.Message, "can't run on")
			for _, s := range tt.wantSuggests {
				assert.Contains(t, rrErr.Suggestion, s)
			}
			for _, s := range tt.notSuggests {
				assert.NotContains(t, rrErr.Suggestion, s)
			}
			assert.NoFileExists(t, filepath.Join(projectDir, "ran.out"))
		})
	}
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
