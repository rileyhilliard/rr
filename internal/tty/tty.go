// Package tty hands the local terminal to one command run with --tty: raw
// mode, the window size and its changes, and a stdin reader that stops when
// the command ends.
//
// A Session covers a single command. rr's own phases (connect, lock wait,
// sync, step headers, the summary) run with the terminal in its normal
// cooked mode, so Ctrl+C works there as usual and rr's output isn't
// mangled; only the command itself sees a raw terminal.
package tty

import (
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether both stdin and stdout are terminals, which
// --tty needs: input comes from the user and output goes back to them.
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
