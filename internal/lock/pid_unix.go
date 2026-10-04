//go:build !windows

package lock

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// processGroupAlive reports whether any process in the process group pgid
// still exists. As with processAlive, EPERM means it exists.
func processGroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

// jobStartTolerance is how much later than the recorded JobStarted the start
// time ps gives a job group's leader may be and still be the same process. ps
// reports elapsed time in whole seconds, and JobStarted is taken just after
// the job starts.
const jobStartTolerance = 2 * time.Second

// jobGroupAlive reports whether the job recorded as process group pgid,
// started at started, is still running.
//
// A group's id is its leader's pid, and the kernel doesn't reuse a pid while
// a group with that id exists. So a running leader that started later than
// the job got the pid after the job and rr were both gone.
//
// If the leader is gone, the group is alive while any member runs (a job's
// shell can exit before what it started in the background). That's taken
// as the job: for it to be a reused id instead, a new process would have to
// get the pid, lead a group and exit, leaving members behind.
//
// With no started (a lock written before JobStarted existed), or when ps
// can't say when the leader started, any group with that id counts as the
// job, as before: keeping the lock is the safe side, and rr unlock clears it.
func jobGroupAlive(pgid int, started time.Time) bool {
	if started.IsZero() || !processAlive(pgid) {
		return processGroupAlive(pgid)
	}
	elapsed, err := processElapsed(pgid)
	if err != nil {
		if !processAlive(pgid) {
			// The leader exited between the checks.
			return processGroupAlive(pgid)
		}
		debugf("jobGroupAlive: couldn't get the start time of pid %d (%v); counting group %d as the job", pgid, err, pgid)
		return true
	}
	// A reused pid's process started after the job was gone, so only a
	// leader that started later than the job can be one. One that looks
	// older is the job with the clock moved since; it keeps the lock.
	if time.Since(started)-elapsed > jobStartTolerance {
		debugf("jobGroupAlive: pid %d started %s ago, the job %s ago; the pid was reused", pgid, elapsed, time.Since(started).Truncate(time.Second))
		return false
	}
	return true
}

// processElapsed returns how long ago pid started, from ps's etime, which
// macOS and Linux both print as [[dd-]hh:]mm:ss. Elapsed time rather than
// lstart's date avoids parsing a local time, which is ambiguous in the hour
// a DST change repeats.
func processElapsed(pid int) (time.Duration, error) {
	cmd := exec.Command("ps", "-o", "etime=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ps -o etime= -p %d: %w", pid, err)
	}
	return parseEtime(strings.TrimSpace(string(out)))
}

// parseEtime parses ps's etime format, [[dd-]hh:]mm:ss.
func parseEtime(s string) (time.Duration, error) {
	var days int
	rest := s
	if d, r, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, fmt.Errorf("etime %q: bad days", s)
		}
		days, rest = n, r
	}
	parts := strings.Split(rest, ":")
	if len(parts) < 2 || len(parts) > 3 || (days > 0 && len(parts) != 3) {
		return 0, fmt.Errorf("etime %q: want [[dd-]hh:]mm:ss", s)
	}
	total := time.Duration(days) * 24 * time.Hour
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("etime %q: bad field %q", s, p)
		}
		unit := []time.Duration{time.Hour, time.Minute, time.Second}[3-len(parts)+i]
		total += time.Duration(n) * unit
	}
	return total, nil
}
