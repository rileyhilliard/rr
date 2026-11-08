package sshutil

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"golang.org/x/crypto/ssh"
)

// DefaultTerm is the terminal type requested when TTYOptions.Term is empty.
const DefaultTerm = "xterm-256color"

// ttyCloseWait bounds the wait for a cancelled TTY session to wind down
// after it's closed.
const ttyCloseWait = time.Second

// WindowSize is a terminal size in character cells.
type WindowSize struct {
	Width  int
	Height int
}

// TTYOptions configures ExecTTYContext.
type TTYOptions struct {
	Term   string            // $TERM for the remote PTY; DefaultTerm when empty
	Width  int               // columns; 80 when not positive
	Height int               // rows; 24 when not positive
	Stdin  io.Reader         // what the user types; nil sends nothing
	Output io.Writer         // the PTY merges stdout and stderr into this
	Resize <-chan WindowSize // nil: no resize forwarding
}

// TTYExecer runs a command on a remote PTY. It's separate from SSHClient so
// mocks and other clients don't need it; callers type-assert and fail with a
// clear error when a client doesn't have it.
type TTYExecer interface {
	ExecTTYContext(ctx context.Context, cmd string, opts TTYOptions) (int, error)
}

var _ TTYExecer = (*Client)(nil)

// ExecTTYContext runs cmd on a remote PTY, the way `ssh -t` does, so a
// command that needs a terminal (a sudo password prompt, an apt
// confirmation) can talk to the user. Output arrives as the PTY produces it,
// prompts without a newline included. Resize events are forwarded as
// window-change requests.
//
// When ctx is done the session is closed: the remote PTY gets a hangup and
// its process group goes with it. The result is then 130 with ctx.Err().
// Otherwise exit codes and errors are as for ExecStreamContext.
func (c *Client) ExecTTYContext(ctx context.Context, cmd string, opts TTYOptions) (int, error) {
	session, err := c.newSSHSession()
	if err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't create an SSH session",
			"The connection might have dropped. Try reconnecting.")
	}
	defer session.Close()

	term := opts.Term
	if term == "" {
		term = DefaultTerm
	}
	width, height := opts.Width, opts.Height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	// ECHO on: the remote PTY echoes what the user types, as with ssh -t.
	// The local terminal is in raw mode and echoes nothing itself.
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty(term, height, width, modes); err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't allocate a PTY on the remote",
			"The SSH server may not allow PTYs (PermitTTY no). Run without --tty.")
	}

	session.Stdin = opts.Stdin
	session.Stdout = opts.Output
	session.Stderr = opts.Output

	if err := session.Start(cmd); err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrExec,
			fmt.Sprintf("Couldn't start: %s", cmd),
			"Make sure the command exists on the remote.")
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	stopResize := make(chan struct{})
	defer close(stopResize)
	if opts.Resize != nil {
		go forwardResizes(session, opts.Resize, stopResize)
	}

	select {
	case <-ctx.Done():
		session.Close()
		select {
		case <-done:
		case <-time.After(ttyCloseWait):
		}
		return 130, ctx.Err()
	case runErr := <-done:
		return waitResult(runErr)
	}
}

// forwardResizes sends each size from resize to the remote PTY until stop
// closes or resize does. A failed request means the session is ending, and
// the next size will fail the same way, so errors are dropped.
func forwardResizes(session *ssh.Session, resize <-chan WindowSize, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case size, ok := <-resize:
			if !ok {
				return
			}
			_ = session.WindowChange(size.Height, size.Width)
		}
	}
}
