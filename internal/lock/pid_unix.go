//go:build !windows

package lock

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the given pid exists on this
// machine. Signal 0 performs the existence check without sending anything;
// EPERM means the process exists but belongs to another user.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
