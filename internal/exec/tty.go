package exec

import (
	"context"
	stderrors "errors"
	"io"
	"os"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/tty"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// ExecuteTTY runs cmd on a remote PTY with the local terminal lent to it
// (--tty). The terminal is in raw mode only while the command runs; it's
// back to normal when this returns, a panic included. The PTY merges stdout
// and stderr, and all of it goes to output unbuffered. A failed terminal
// restore is in the returned error (tty.ErrRestore), joined to the
// command's own error if it has one.
func ExecuteTTY(ctx context.Context, client sshutil.SSHClient, cmd string, output io.Writer) (exitCode int, err error) {
	execer, ok := client.(sshutil.TTYExecer)
	if !ok {
		return -1, errors.New(errors.ErrExec,
			"This connection can't run commands with a terminal",
			"Run without --tty.")
	}

	// A Ctrl+C between the steps of a steps: task must not start the next
	// one, as RunLocalCommand does for local commands.
	if ctx.Err() != nil {
		return 130, ctx.Err()
	}

	session, err := tty.Start()
	if err != nil {
		return -1, err
	}
	defer func() {
		closeErr := session.Close()
		switch {
		case closeErr == nil:
		case err == nil:
			err = closeErr
		default:
			err = stderrors.Join(err, closeErr)
		}
	}()

	size := session.Size()
	return execer.ExecTTYContext(ctx, cmd, sshutil.TTYOptions{
		Term:   os.Getenv("TERM"),
		Width:  size.Width,
		Height: size.Height,
		Stdin:  session.Stdin(),
		Output: output,
		Resize: session.Resize(),
	})
}
