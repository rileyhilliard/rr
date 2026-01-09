package lock

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// Lock represents an acquired distributed lock on a remote host.
type Lock struct {
	Dir  string    // The lock directory path on the remote
	Info *LockInfo // Info about the lock holder (us)
	conn *host.Connection
}

// Acquire attempts to acquire a distributed lock on the remote host.
// It uses mkdir as an atomic primitive (mkdir fails if the directory exists).
// If the lock is held, it will wait and retry until timeout.
// Stale locks (older than config.Stale) are automatically removed.
func Acquire(conn *host.Connection, cfg config.LockConfig, projectHash string) (*Lock, error) {
	if conn == nil || conn.Client == nil {
		return nil, errors.New(errors.ErrLock,
			"Cannot acquire lock: no connection",
			"Establish an SSH connection first")
	}

	// Build lock directory path: <dir>/rr-<projectHash>.lock/
	baseDir := cfg.Dir
	if baseDir == "" {
		baseDir = "/tmp"
	}
	lockDir := filepath.Join(baseDir, fmt.Sprintf("rr-%s.lock", projectHash))
	infoFile := filepath.Join(lockDir, "info.json")

	// Create our lock info
	info, err := NewLockInfo()
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrLock,
			"Failed to create lock info",
			"Check hostname and user environment")
	}

	startTime := time.Now()

	for {
		// Check if we've exceeded the timeout
		elapsed := time.Since(startTime)
		if elapsed > cfg.Timeout {
			// Try to read who holds the lock for a better error message
			holder := readLockHolder(conn.Client, infoFile)
			return nil, errors.New(errors.ErrLock,
				fmt.Sprintf("Timed out waiting for lock after %s", cfg.Timeout),
				fmt.Sprintf("Lock held by: %s. Consider using --force-unlock or wait for it to release.", holder))
		}

		// Check for stale lock
		if isLockStale(conn.Client, infoFile, cfg.Stale) {
			// Remove stale lock
			if err := forceRemove(conn.Client, lockDir); err == nil {
				// Stale lock removed, try again immediately
				continue
			}
		}

		// Try to acquire lock using mkdir (atomic operation)
		// mkdir will fail if the directory already exists
		_, _, exitCode, err := conn.Client.Exec(fmt.Sprintf("mkdir %q 2>/dev/null", lockDir))
		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrLock,
				"Failed to execute lock command",
				"Check SSH connection")
		}

		if exitCode == 0 {
			// Lock acquired, write our info
			infoJSON, err := info.Marshal()
			if err != nil {
				// Clean up the lock dir if we can't write info
				forceRemove(conn.Client, lockDir)
				return nil, errors.WrapWithCode(err, errors.ErrLock,
					"Failed to serialize lock info",
					"This shouldn't happen")
			}

			// Write the info file
			writeCmd := fmt.Sprintf("cat > %q << 'LOCKINFO'\n%s\nLOCKINFO", infoFile, string(infoJSON))
			_, _, exitCode, err = conn.Client.Exec(writeCmd)
			if err != nil || exitCode != 0 {
				// Clean up and report error
				forceRemove(conn.Client, lockDir)
				return nil, errors.New(errors.ErrLock,
					"Failed to write lock info file",
					"Check disk space and permissions on remote")
			}

			return &Lock{
				Dir:  lockDir,
				Info: info,
				conn: conn,
			}, nil
		}

		// Lock is held by someone else, wait before retrying
		time.Sleep(2 * time.Second)
	}
}

// Release removes the lock, allowing others to acquire it.
func (l *Lock) Release() error {
	if l == nil || l.conn == nil || l.conn.Client == nil {
		return nil // Nothing to release
	}

	return forceRemove(l.conn.Client, l.Dir)
}

// ForceRelease forcibly removes a lock directory, regardless of who holds it.
// Use with caution - this should only be used for stuck or abandoned locks.
func ForceRelease(conn *host.Connection, lockDir string) error {
	if conn == nil || conn.Client == nil {
		return errors.New(errors.ErrLock,
			"Cannot force release lock: no connection",
			"Establish an SSH connection first")
	}

	return forceRemove(conn.Client, lockDir)
}

// Holder returns information about who holds the lock (if readable).
func Holder(conn *host.Connection, lockDir string) string {
	if conn == nil || conn.Client == nil {
		return "unknown (no connection)"
	}
	infoFile := filepath.Join(lockDir, "info.json")
	return readLockHolder(conn.Client, infoFile)
}

// isLockStale checks if the lock's info file is older than the stale threshold.
func isLockStale(client sshutil.SSHClient, infoFile string, staleThreshold time.Duration) bool {
	if staleThreshold <= 0 {
		return false
	}

	// Read the info file and check the started time
	stdout, _, exitCode, err := client.Exec(fmt.Sprintf("cat %q 2>/dev/null", infoFile))
	if err != nil || exitCode != 0 {
		return false // Can't read, assume not stale
	}

	info, err := ParseLockInfo(stdout)
	if err != nil {
		return false
	}

	return info.Age() > staleThreshold
}

// readLockHolder reads the lock info file and returns a description of the holder.
func readLockHolder(client sshutil.SSHClient, infoFile string) string {
	stdout, _, exitCode, err := client.Exec(fmt.Sprintf("cat %q 2>/dev/null", infoFile))
	if err != nil || exitCode != 0 {
		return "unknown"
	}

	info, err := ParseLockInfo(stdout)
	if err != nil {
		// Fall back to raw content
		return strings.TrimSpace(string(stdout))
	}

	return info.String()
}

// forceRemove removes a directory and all its contents.
func forceRemove(client sshutil.SSHClient, dir string) error {
	_, stderr, exitCode, err := client.Exec(fmt.Sprintf("rm -rf %q", dir))
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrLock,
			fmt.Sprintf("Failed to remove lock directory: %s", dir),
			"Check SSH connection")
	}
	if exitCode != 0 {
		return errors.New(errors.ErrLock,
			fmt.Sprintf("Failed to remove lock directory: %s", dir),
			fmt.Sprintf("Error: %s", string(stderr)))
	}
	return nil
}
