//go:build !windows && !darwin && !linux

package tty

import (
	"os"
	"testing"
)

// openPTY skips: the tests only know how to open a PTY on darwin and linux.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	t.Skip("no PTY helper for this platform")
	return nil, nil
}
