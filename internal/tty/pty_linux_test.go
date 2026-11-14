//go:build linux

package tty

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY opens a PTY pair, the stand-in for the user's terminal: the
// returned slave is what rr would see as stdin and stdout. It skips the test
// when no PTY can be opened (a container without /dev/ptmx).
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("can't open a PTY: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	fd := int(master.Fd())

	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("can't unlock the PTY: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Skipf("can't get the PTY's number: %v", err)
	}

	name := fmt.Sprintf("/dev/pts/%d", n)
	slave, err = os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("can't open the PTY's slave %s: %v", name, err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}
