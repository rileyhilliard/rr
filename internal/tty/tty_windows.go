//go:build windows

package tty

import (
	"io"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// Supported reports whether --tty works on this platform. Windows has no
// SIGWINCH, and the stoppable stdin reader relies on poll.
const Supported = false

// Session is unavailable on Windows; Start always fails.
type Session struct{}

// Start fails on Windows. The CLI rejects --tty there before it gets this far.
func Start() (*Session, error) {
	return nil, errors.New(errors.ErrConfig,
		"--tty isn't supported on Windows",
		"Run rr from WSL to use --tty, or run without it.")
}

// Stdin is never reached on Windows.
func (s *Session) Stdin() io.Reader { return nil }

// Size is never reached on Windows.
func (s *Session) Size() sshutil.WindowSize { return sshutil.WindowSize{} }

// Resize is never reached on Windows.
func (s *Session) Resize() <-chan sshutil.WindowSize { return nil }

// Close is never reached on Windows.
func (s *Session) Close() error { return nil }

// RestoreAll does nothing on Windows: no Session ever changes the terminal.
func RestoreAll() {}
