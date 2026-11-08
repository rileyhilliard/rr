package exec

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/tty"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// ExecuteTTY runs cmd on a remote PTY with the local terminal lent to it
// (--tty). The terminal is in raw mode only while the command runs; it's
// back to normal when this returns. The PTY merges stdout and stderr, and
// all of it goes to output unbuffered.
func ExecuteTTY(ctx context.Context, client sshutil.SSHClient, cmd string, output io.Writer) (int, error) {
	execer, ok := client.(sshutil.TTYExecer)
	if !ok {
		return -1, errors.New(errors.ErrExec,
			"This connection can't run commands with a terminal",
			"Run without --tty.")
	}

	session, err := tty.Start()
	if err != nil {
		return -1, err
	}

	size := session.Size()
	exitCode, err := execer.ExecTTYContext(ctx, cmd, sshutil.TTYOptions{
		Term:   os.Getenv("TERM"),
		Width:  size.Width,
		Height: size.Height,
		Stdin:  session.Stdin(),
		Output: output,
		Resize: session.Resize(),
	})

	if closeErr := session.Close(); closeErr != nil {
		if err == nil {
			return exitCode, closeErr
		}
		// The command's error is the result. The terminal is still raw,
		// though, and the user needs to know how to fix it.
		fmt.Fprintln(os.Stderr, closeErr)
	}
	return exitCode, err
}
