//go:build windows

package lock

// processAlive conservatively reports true on Windows: rr never fast-steals
// locks there, leaving stale-lock detection to the mtime heartbeat check.
func processAlive(pid int) bool {
	return true
}
