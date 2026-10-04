//go:build windows

package lock

// processAlive conservatively reports true on Windows: rr never fast-steals
// locks there, leaving stale-lock detection to the mtime heartbeat check.
func processAlive(pid int) bool {
	return true
}

// processGroupAlive reports false on Windows, where a local job isn't
// recorded by process group; processAlive already keeps the holder alive.
func processGroupAlive(pgid int) bool {
	return false
}
