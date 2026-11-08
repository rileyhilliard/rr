package cli

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
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

// --local --tty with a local: true host is refused before its lock: a busy
// host would otherwise hold the run for lock.timeout only to refuse it.
func TestTTY_LocalTargetRejectedBeforeLockWait(t *testing.T) {
	projectDir, lockDir := writeLocalHostConfigs(t)
	t.Chdir(projectDir)

	lockCfg := config.LockConfig{Enabled: true, Timeout: time.Second, Stale: time.Minute, Dir: filepath.Dir(lockDir)}
	held, err := lock.TryAcquire(host.NewLocalHostConnection("dev", config.Host{Local: true}), lockCfg, "rr test (other run)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Release() })

	start := time.Now()
	captureStdout(t, func() {
		captureStderr(t, func() {
			_, err = Run(RunOptions{Command: "true", Local: true, TTY: true})
		})
	})
	requireConfigErrorWithSuggestion(t, err, "'dev', a local: true host")
	assert.Less(t, time.Since(start), 5*time.Second, "refused without waiting for the held lock")
}

// Under --tty the load balancer passes over a local: true host while a
// remote is left to try, and keeps it when it's the only kind there is.
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

	t.Run("only local hosts", func(t *testing.T) {
		ctx, _ := localHostWorkflow(t, map[string]config.Host{"dev": hosts["dev"]}, []string{"dev"}, config.LocalFallbackNever)

		result, err := findAvailableHost(ctx, WorkflowOptions{Command: "rr test", TTY: true})
		require.NoError(t, err)
		ctx.Lock = result.lock
		assert.Equal(t, "dev", result.conn.Name, "kept, so the run is refused with the reason")
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

// A cancelled run drops its error, so a failed terminal restore in it is
// reported as a warning; other errors aren't.
func TestWarnTerminalRestore(t *testing.T) {
	withStructuredOutput(t)
	restoreErr := errors.WrapWithCode(fmt.Errorf("%w: input/output error", tty.ErrRestore), errors.ErrExec, "Couldn't restore the terminal after the command", "Run 'stty sane'.")

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "no error"},
		{name: "a command error", err: stderrors.New("context canceled")},
		{name: "restore failure", err: restoreErr, want: true},
		{name: "restore failure joined to the command's error", err: stderrors.Join(stderrors.New("context canceled"), restoreErr), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := captureStderr(t, func() { warnTerminalRestore(tt.err) })
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
