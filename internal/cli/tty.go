package cli

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/tty"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/spf13/pflag"
)

// ttyFlagUsage is the --tty help text on run, exec and task commands.
const ttyFlagUsage = "give the command a terminal, for prompts like sudo's (interactive use only)"

// ttyRequest is what decides whether --tty can work, known before rr
// connects anywhere.
type ttyRequest struct {
	supported bool // tty.Supported: false on Windows
	terminal  bool // stdin and stdout are both terminals
	repeat    int  // --repeat
	hasDeps   bool // the task has dependencies
	skipDeps  bool // --skip-deps
}

// currentTTYRequest fills in the platform and terminal facts.
func currentTTYRequest() ttyRequest {
	return ttyRequest{supported: tty.Supported, terminal: tty.IsTerminal()}
}

// checkTTYRequest rejects --tty where it can't work, before connecting. A
// silent fallback to running without a terminal would bring back the hang
// on a prompt that --tty is there to avoid.
func checkTTYRequest(r ttyRequest) error {
	if !r.supported {
		return errors.New(errors.ErrConfig,
			"--tty isn't supported on Windows",
			"Run rr from WSL to use --tty, or run without it.")
	}
	if !r.terminal {
		return errors.New(errors.ErrConfig,
			"--tty needs a terminal, but stdin or stdout isn't one",
			"Run it from an interactive shell, without redirecting input or output. Scripts and agents should run without --tty.")
	}
	if r.repeat > 1 {
		return errors.New(errors.ErrConfig,
			"--tty doesn't work with --repeat",
			"Repeated runs go in parallel and can't share one terminal. Drop --repeat, or drop --tty.")
	}
	if r.hasDeps && !r.skipDeps {
		return errors.New(errors.ErrConfig,
			"--tty doesn't work with task dependencies",
			"Add --skip-deps to run just this task with a terminal, or run the dependencies first without --tty.")
	}
	return nil
}

// checkTTYConnection rejects --tty on a local: true host, which runs
// commands through host.LocalClient in their own session with no
// controlling terminal, on purpose. Bare local execution (no client) runs
// in rr's terminal and is fine.
func checkTTYConnection(conn *host.Connection) error {
	if _, ok := conn.Client.(*host.LocalClient); ok {
		return ttyLocalHostError(conn.Name)
	}
	return nil
}

// ttyLocalHostError explains why --tty can't run on the named local: true
// hosts.
func ttyLocalHostError(names ...string) error {
	what := fmt.Sprintf("'%s', a local: true host", names[0])
	if len(names) > 1 {
		what = fmt.Sprintf("'%s', local: true hosts", strings.Join(names, "', '"))
	}
	return errors.New(errors.ErrConfig,
		"--tty doesn't work on "+what,
		"A local host runs commands detached from the terminal. Use a remote host with --host, or run without --tty.")
}

// parallelTaskTTYError explains --tty on a parallel task, which doesn't
// register the flag: the generic unknown-flag hint about '--' would send
// the user the wrong way. Returns nil for any other flag error, --ttyx or
// --tty-foo included.
func parallelTaskTTYError(name string, err error) error {
	var notExist *pflag.NotExistError
	if !stderrors.As(err, &notExist) || notExist.GetSpecifiedName() != "tty" {
		return nil
	}
	return errors.New(errors.ErrConfig,
		fmt.Sprintf("--tty doesn't work with parallel task '%s'", name),
		"Its subtasks run at the same time and can't share one terminal. Run a single subtask with --tty: rr <subtask> --tty")
}

// warnTerminalRestore tells the user how to fix a terminal a --tty command
// left in raw mode, when the restore error won't reach them otherwise: a
// cancelled run returns 130 and drops err, and a failed run reports only
// its first structured error (ErrorToJSON), which can be the command's.
// When the restore error is the one reported, it already carries the
// advice, so a warning would say it twice.
func warnTerminalRestore(err error, cancelled bool) {
	if !stderrors.Is(err, tty.ErrRestore) {
		return
	}
	var reported *errors.Error
	if !cancelled && stderrors.As(err, &reported) && stderrors.Is(reported, tty.ErrRestore) {
		return
	}
	const msg = "Couldn't restore the terminal after the command"
	const suggestion = "Run 'stty sane' (or 'reset') to fix the terminal."
	emitWarning(PhaseEvent{
		Type:   "phase",
		Phase:  "exec",
		Status: "warn",
		Details: map[string]interface{}{
			"message":    msg,
			"suggestion": suggestion,
		},
	}, msg+". "+suggestion)
}

// runOutput sets up where a run's output goes and opens its run log. Without
// --tty the StreamHandler tees into the log. A remote --tty command writes
// its raw PTY output to the returned writer: the terminal, with a copy in
// the log, so rr logs, --tail and the test outcome still work. A bare local
// --tty command writes to the terminal directly, so it has no log, and
// --tail (tail > 0) gets a warning that it has nothing to print.
func runOutput(wf *WorkflowContext, useTTY bool, name string, tail int, sh *output.StreamHandler) (logPath string, ttyOutput io.Writer, closeLog func()) {
	if !useTTY {
		logPath, closeLog = setupRunLog(wf, name, sh)
		return logPath, nil, closeLog
	}
	if wf.Conn.IsLocal {
		if tail > 0 {
			warnTailWithoutRunLog()
		}
		return "", nil, func() {}
	}
	logPath, logFile, closeLog := openRunLog(wf, name)
	return logPath, rawTee{out: os.Stdout, log: logFile}, closeLog
}

// warnTailWithoutRunLog says --tail does nothing on a bare local --tty run.
// It's a warning, not an error: whether a run is bare local (local mode,
// --local, a fallback when the hosts are busy) is only known after
// connecting, and failing a fallback run over --tail would be worse than
// skipping the tail.
func warnTailWithoutRunLog() {
	const msg = "--tail has no effect here: a local --tty command writes straight to the terminal, with no run log"
	const suggestion = "Drop --tail; the output is already on screen."
	emitWarning(PhaseEvent{
		Type:   "phase",
		Phase:  "exec",
		Status: "warn",
		Details: map[string]interface{}{
			"flag":       "--tail",
			"message":    msg,
			"suggestion": suggestion,
		},
	}, msg+". "+suggestion)
}

// runLocal runs a bare local rr run/exec command: with --tty in rr's own
// terminal, otherwise through the stream handler.
func runLocal(ctx context.Context, useTTY bool, cmd, dir string, sh *output.StreamHandler) (int, error) {
	if useTTY {
		return exec.ExecuteLocalTTYContext(ctx, cmd, dir)
	}
	return exec.ExecuteLocalContext(ctx, cmd, dir, sh.Stdout(), sh.Stderr())
}

// runRemote runs an rr run/exec command on the host: with --tty on a PTY
// whose output goes to ttyOutput, otherwise through the stream handler.
func runRemote(ctx context.Context, client sshutil.SSHClient, useTTY bool, cmd string, ttyOutput io.Writer, sh *output.StreamHandler) (int, error) {
	if useTTY {
		return exec.ExecuteTTY(ctx, client, cmd, ttyOutput)
	}
	return client.ExecStreamContext(ctx, cmd, sh.Stdout(), sh.Stderr())
}

// rawTee writes a TTY command's output to the terminal as it arrives, and a
// copy to the run log. The log is best-effort, as with StreamHandler's tee:
// a failed log write must not fail the command.
type rawTee struct {
	out io.Writer
	log io.Writer
}

func (t rawTee) Write(p []byte) (int, error) {
	if t.log != nil {
		_, _ = t.log.Write(p)
	}
	return t.out.Write(p)
}
