package cli

import (
	"bytes"
	stderrors "errors"
	"io"
	"os"
	"path/filepath"
	"testing"

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
		name := "parallel task"
		if forwardArgs {
			name += " with forward_args"
		}
		t.Run(name, func(t *testing.T) {
			cmd := createParallelTaskCommand("checks", config.TaskConfig{Parallel: []string{"lint", "test"}, ForwardArgs: forwardArgs})
			quietCommand(cmd)
			cmd.SetArgs([]string{"--tty"})
			err := cmd.Execute()
			requireConfigErrorWithSuggestion(t, err, "parallel task 'checks'")
		})
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
