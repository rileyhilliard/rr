package cli

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/tty"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireConfigErrorWithSuggestion checks err is a structured ErrConfig error
// that tells the user what to do instead.
func requireConfigErrorWithSuggestion(t *testing.T, err error, wantMessage string) {
	t.Helper()
	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrConfig), "want ErrConfig, got: %v", err)
	var rrErr *errors.Error
	require.True(t, stderrors.As(err, &rrErr), "want a structured error, got: %T", err)
	assert.Contains(t, rrErr.Message, wantMessage)
	assert.NotEmpty(t, rrErr.Suggestion)
}

func TestCheckTTYRequest(t *testing.T) {
	ok := ttyRequest{supported: true, terminal: true}
	tests := []struct {
		name        string
		req         ttyRequest
		wantMessage string // empty: allowed
	}{
		{name: "interactive terminal", req: ok},
		{name: "--repeat 1 is a single run", req: ttyRequest{supported: true, terminal: true, repeat: 1}},
		{name: "task with dependencies and --skip-deps", req: ttyRequest{supported: true, terminal: true, hasDeps: true, skipDeps: true}},
		{name: "Windows", req: ttyRequest{supported: false, terminal: true}, wantMessage: "Windows"},
		{name: "not a terminal", req: ttyRequest{supported: true, terminal: false}, wantMessage: "needs a terminal"},
		{name: "--repeat", req: ttyRequest{supported: true, terminal: true, repeat: 3}, wantMessage: "--repeat"},
		{name: "task with dependencies", req: ttyRequest{supported: true, terminal: true, hasDeps: true}, wantMessage: "dependencies"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTTYRequest(tt.req)
			if tt.wantMessage == "" {
				assert.NoError(t, err)
				return
			}
			requireConfigErrorWithSuggestion(t, err, tt.wantMessage)
		})
	}
}

func TestCheckTTYConnection(t *testing.T) {
	tests := []struct {
		name        string
		conn        *host.Connection
		wantMessage string // empty: allowed
	}{
		{name: "remote host", conn: &host.Connection{Name: "mini", Client: sshtesting.NewMockClient("mini")}},
		{
			// --local or a fallback with no global local host: runs in
			// rr's own terminal.
			name: "bare local execution",
			conn: host.LocalRunConnection("", config.Host{}, host.LocalReasonFlag),
		},
		{
			name:        "local: true host",
			conn:        host.NewLocalHostConnection("laptop", config.Host{Local: true}),
			wantMessage: "'laptop', a local: true host",
		},
		{
			// --local or a fallback when the global config has a local host
			// gets that host's LocalClient connection.
			name:        "--local with a global local host",
			conn:        host.LocalRunConnection("laptop", config.Host{Local: true}, host.LocalReasonFlag),
			wantMessage: "local: true host",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTTYConnection(tt.conn)
			if tt.wantMessage == "" {
				assert.NoError(t, err)
				return
			}
			requireConfigErrorWithSuggestion(t, err, tt.wantMessage)
		})
	}
}

// Agents and scripts run rr without a terminal; --tty must refuse there
// before connecting anywhere instead of hanging on a prompt.
func TestRunCommand_TTYWithoutTerminalFailsBeforeConnecting(t *testing.T) {
	skipIfTerminal(t)
	for _, verb := range []string{"run", "exec"} {
		t.Run(verb, func(t *testing.T) {
			// No config or hosts are set up: getting past the check would
			// fail differently.
			err := runCommand([]string{"echo hi"}, runCmdFlags{Verb: verb, TTY: true})
			requireConfigErrorWithSuggestion(t, err, "needs a terminal")
		})
	}
}

func TestTaskCommand_TTY(t *testing.T) {
	t.Run("single task without a terminal", func(t *testing.T) {
		skipIfTerminal(t)
		cmd := createTaskCommand("deploy", config.TaskConfig{Run: "sudo systemctl restart app"})
		quietCommand(cmd)
		cmd.SetArgs([]string{"--tty"})
		err := cmd.Execute()
		requireConfigErrorWithSuggestion(t, err, "needs a terminal")
	})

	t.Run("no -t shorthand, which would take jest's -t", func(t *testing.T) {
		cmd := createTaskCommand("test", config.TaskConfig{Run: "bunx jest"})
		f := cmd.Flags().Lookup("tty")
		require.NotNil(t, f)
		assert.Empty(t, f.Shorthand)
	})

	for _, forwardArgs := range []bool{false, true} {
		for _, tc := range []struct {
			flag      string
			ttyReason bool // the parallel-task --tty explanation, not the generic unknown-flag error
		}{
			{flag: "--tty", ttyReason: true},
			{flag: "--ttyx"},
			{flag: "--tty-foo"},
		} {
			name := "parallel task " + tc.flag
			if forwardArgs {
				name += " with forward_args"
			}
			t.Run(name, func(t *testing.T) {
				cmd := createParallelTaskCommand("checks", config.TaskConfig{Parallel: []string{"lint", "test"}, ForwardArgs: forwardArgs})
				quietCommand(cmd)
				cmd.SetArgs([]string{tc.flag})
				err := cmd.Execute()
				if tc.ttyReason {
					requireConfigErrorWithSuggestion(t, err, "parallel task 'checks'")
					return
				}
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "--tty doesn't work with parallel task")
			})
		}
	}
}

// A local: true host is rejected once it's picked, before the command runs.
func TestTTY_LocalHostRejectedBeforeRunning(t *testing.T) {
	projectDir, lockDir := writeLocalHostConfigs(t)
	t.Chdir(projectDir)
	marker := filepath.Join(projectDir, "ran")

	var err error
	captureStdout(t, func() {
		captureStderr(t, func() {
			_, err = Run(RunOptions{Command: "touch " + marker, TTY: true})
		})
	})
	requireConfigErrorWithSuggestion(t, err, "'dev', a local: true host")
	assert.NoFileExists(t, marker)
	assert.NoDirExists(t, lockDir, "the lock taken while connecting is released")

	captureStdout(t, func() {
		captureStderr(t, func() {
			_, err = RunTask(TaskOptions{TaskName: "check", TTY: true})
		})
	})
	requireConfigErrorWithSuggestion(t, err, "'dev', a local: true host")
	assert.NoFileExists(t, filepath.Join(projectDir, "task.out"))
}

// ttyLockWaitBound is how long a --tty run on a busy local host may take to
// be refused. The tests set lock.timeout (or lock.wait_timeout) to
// ttyLockTimeout, well above it, so a run that waited for the lock would
// fail the bound.
const (
	ttyLockTimeout   = 10 * time.Second
	ttyLockWaitBound = 3 * time.Second
)

// ttyHeldLock is the lock config another run holds "dev" under, with the
// lock dir lockDir.
func ttyHeldLock(lockDir string) config.LockConfig {
	return config.LockConfig{Enabled: true, Timeout: ttyLockTimeout, Stale: time.Minute, Dir: filepath.Dir(lockDir)}
}

// setProjectLockTimeout sets lock.timeout in projectDir's .rr.yaml.
func setProjectLockTimeout(t *testing.T, projectDir string, timeout time.Duration) {
	t.Helper()
	path := filepath.Join(projectDir, ".rr.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := strings.Replace(string(data), "lock:\n", "lock:\n  timeout: "+timeout.String()+"\n", 1)
	require.NotEqual(t, string(data), updated, "the project config has a lock: section")
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))
}

// --tty on a local: true host is refused before its lock: a busy host would
// otherwise hold the run for lock.timeout only to refuse it.
func TestTTY_LocalTargetRejectedBeforeLockWait(t *testing.T) {
	tests := []struct {
		name string
		opts RunOptions
	}{
		{name: "--local", opts: RunOptions{Local: true}},
		{name: "--host", opts: RunOptions{Host: "dev"}},
		{name: "single-host project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir, lockDir := writeLocalHostConfigs(t)
			setProjectLockTimeout(t, projectDir, ttyLockTimeout)
			t.Chdir(projectDir)
			holdLocalHostLock(t, ttyHeldLock(lockDir))

			opts := tt.opts
			opts.Command = "true"
			opts.TTY = true
			start := time.Now()
			var err error
			captureStdout(t, func() {
				captureStderr(t, func() {
					_, err = Run(opts)
				})
			})
			requireConfigErrorWithSuggestion(t, err, "'dev', a local: true host")
			assert.Less(t, time.Since(start), ttyLockWaitBound, "refused without waiting for the held lock")
		})
	}
}

// A load-balanced run whose hosts are unreachable falls back to the global
// local host, and --tty is refused there before its lock too.
func TestTTY_FallbackToLocalHostRejectedBeforeLockWait(t *testing.T) {
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
  box1:
    ssh: [nonexistent-host-rr-test.invalid]
    dir: ~/rr
  box2:
    ssh: [nonexistent-host-rr-test.invalid]
    dir: ~/rr
  dev:
    local: true
`
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(global), 0o644))
	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	lockBase := filepath.Join(t.TempDir(), "locks")
	project := `version: 1
hosts: [box1, box2]
local_fallback: true
lock:
  timeout: ` + ttyLockTimeout.String() + `
  dir: ` + lockBase + `
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	t.Chdir(projectDir)
	holdLocalHostLock(t, ttyHeldLock(filepath.Join(lockBase, "rr.lock")))

	start := time.Now()
	captureStdout(t, func() {
		captureStderr(t, func() {
			_, err = Run(RunOptions{Command: "true", TTY: true})
		})
	})
	requireConfigErrorWithSuggestion(t, err, "'dev', a local: true host")
	assert.Less(t, time.Since(start), ttyLockWaitBound, "refused without waiting for the held lock")
}

// Under --tty the load balancer passes over a local: true host while a
// remote is left to try, and refuses the run up front when every candidate
// is a local host.
func TestFindAvailableHost_TTYSkipsLocalHost(t *testing.T) {
	hosts := map[string]config.Host{
		"dev":    {Local: true, Dir: t.TempDir()},
		"remote": {SSH: []string{"nonexistent-host-a-xxxx"}, Dir: "~"},
	}

	t.Run("local host first, remote after", func(t *testing.T) {
		ctx, lockCfg := localHostWorkflow(t, hosts, []string{"dev", "remote"}, config.LocalFallbackNever)

		result, err := findAvailableHost(ctx, WorkflowOptions{Command: "rr test", TTY: true})
		require.Error(t, err, "the unreachable remote is the only candidate")
		assert.Nil(t, result)
		assert.True(t, errors.IsCode(err, errors.ErrSSH), "the remote's dial error, got %v", err)
		assert.NoDirExists(t, filepath.Join(lockCfg.Dir, "rr.lock"), "the local host is never tried")
	})

	t.Run("only local hosts, one busy", func(t *testing.T) {
		localHosts := map[string]config.Host{
			"dev":    hosts["dev"],
			"laptop": {Local: true, Dir: t.TempDir()},
		}
		ctx, lockCfg := localHostWorkflow(t, localHosts, []string{"dev", "laptop"}, config.LocalFallbackNever)
		// A run that waited on the busy host would take this long.
		ctx.Resolved.Project.Lock.WaitTimeout = ttyLockTimeout
		ctx.Resolved.Project.Lock.Timeout = ttyLockTimeout
		holdLocalHostLock(t, lockCfg)

		start := time.Now()
		result, err := findAvailableHost(ctx, WorkflowOptions{Command: "rr test", TTY: true})
		assert.Nil(t, result)
		requireConfigErrorWithSuggestion(t, err, "'dev', 'laptop', local: true hosts")
		assert.Less(t, time.Since(start), ttyLockWaitBound, "refused without waiting for the held lock")
	})
}

// --local with no local host configured runs in rr's own terminal: the
// command writes straight to stdout, and nothing is logged.
func TestTTY_BareLocalRunWritesToTerminalWithoutRunLog(t *testing.T) {
	projectDir, _ := writeRoutingConfigs(t, false, "hosts: [box]")
	t.Chdir(projectDir)

	var exitCode int
	var err error
	out := captureStdout(t, func() {
		captureStderr(t, func() {
			exitCode, err = Run(RunOptions{Command: "echo from-the-command; exit 3", Local: true, TTY: true})
		})
	})
	require.NoError(t, err)
	assert.Equal(t, 3, exitCode)
	assert.Contains(t, out, "from-the-command")
	assert.NoDirExists(t, filepath.Join(os.Getenv("HOME"), ".rr", "logs"), "a bare local --tty run has no run log")
}

// --tail has nothing to print on a bare local --tty run, and says so
// instead of doing nothing.
func TestTTY_BareLocalRunWarnsAboutTail(t *testing.T) {
	projectDir, _ := writeRoutingConfigs(t, false, "hosts: [box]")
	t.Chdir(projectDir)
	withStructuredOutput(t)

	var err error
	events := captureStderr(t, func() {
		captureStdout(t, func() {
			_, err = Run(RunOptions{Command: "true", Local: true, TTY: true, Tail: 5})
		})
	})
	require.NoError(t, err)
	assert.Contains(t, events, `"flag":"--tail"`)
	assert.Contains(t, events, "no run log")
}

// A failed terminal restore is warned about only when its error won't be
// reported: a cancelled run drops it, and a failed run reports the
// command's structured error ahead of it. A restore error reported as the
// run's error already carries the advice.
func TestWarnTerminalRestore(t *testing.T) {
	withStructuredOutput(t)
	restoreErr := errors.WrapWithCode(fmt.Errorf("%w: input/output error", tty.ErrRestore), errors.ErrExec, "Couldn't restore the terminal after the command", "Run 'stty sane'.")
	commandErr := errors.New(errors.ErrExec, "command failed", "Check the command.")

	tests := []struct {
		name      string
		err       error
		cancelled bool
		want      bool
	}{
		{name: "no error"},
		{name: "a command error", err: commandErr},
		{name: "a cancelled run's command error", err: stderrors.New("context canceled"), cancelled: true},
		{name: "restore failure reported as the run's error", err: restoreErr},
		{name: "restore failure in a cancelled run", err: restoreErr, cancelled: true, want: true},
		{name: "restore failure joined after the command's error", err: stderrors.Join(commandErr, restoreErr), want: true},
		{name: "restore failure joined to a cancelled run's error", err: stderrors.Join(stderrors.New("context canceled"), restoreErr), cancelled: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := captureStderr(t, func() { warnTerminalRestore(tt.err, tt.cancelled) })
			if !tt.want {
				assert.Empty(t, events)
				return
			}
			assert.Contains(t, events, "stty sane")
		})
	}
}

// skipIfTerminal skips a test that needs stdin or stdout not to be a
// terminal, as they aren't under go test unless run unusually.
func skipIfTerminal(t *testing.T) {
	t.Helper()
	if tty.IsTerminal() {
		t.Skip("stdin and stdout are a terminal")
	}
}

// quietCommand keeps cobra's usage and error text out of the test output.
func quietCommand(cmd *cobra.Command) {
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, stderrors.New("disk full") }

func TestRawTee(t *testing.T) {
	t.Run("copies bytes to both, unchanged", func(t *testing.T) {
		var out, log bytes.Buffer
		tee := rawTee{out: &out, log: &log}
		n, err := tee.Write([]byte("[sudo] password for riley: "))
		require.NoError(t, err)
		assert.Equal(t, 27, n)
		assert.Equal(t, "[sudo] password for riley: ", out.String(), "a prompt with no newline isn't held back")
		assert.Equal(t, "[sudo] password for riley: ", log.String())
	})

	t.Run("a failed log write doesn't fail the output", func(t *testing.T) {
		var out bytes.Buffer
		tee := rawTee{out: &out, log: failingWriter{}}
		_, err := tee.Write([]byte("ok\r\n"))
		require.NoError(t, err)
		assert.Equal(t, "ok\r\n", out.String())
	})

	t.Run("no log", func(t *testing.T) {
		var out bytes.Buffer
		_, err := rawTee{out: &out}.Write([]byte("ok"))
		require.NoError(t, err)
		assert.Equal(t, "ok", out.String())
	})
}
