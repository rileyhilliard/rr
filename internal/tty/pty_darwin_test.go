//go:build darwin

package tty

import (
	"bytes"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY opens a PTY pair, the stand-in for the user's terminal: the
// returned slave is what rr would see as stdin and stdout. It skips the test
// when no PTY can be opened (a sandbox without /dev/ptmx).
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("can't open a PTY: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	fd := int(master.Fd())

	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		t.Skipf("can't grant the PTY: %v", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		t.Skipf("can't unlock the PTY: %v", err)
	}
	// TIOCPTYGNAME fills a 128-byte buffer with the slave's path. x/sys has
	// no libSystem wrapper for an ioctl that writes a buffer, hence the raw
	// syscall.
	name := make([]byte, 128)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, //nolint:staticcheck // see above
		uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		t.Skipf("can't get the PTY's name: %v", errno)
	}
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}

	slave, err = os.OpenFile(string(name), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("can't open the PTY's slave %s: %v", name, err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}
