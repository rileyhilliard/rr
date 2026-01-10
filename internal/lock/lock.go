package lock

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/logger"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// AcquireOption configures lock acquisition behavior.
type AcquireOption func(*acquireOptions)

type acquireOptions struct {
	logger logger.Logger
}

// WithLogger sets the logger for lock operations.
func WithLogger(l logger.Logger) AcquireOption {
	return func(o *acquireOptions) {
		o.logger = l
	}
}

// defaultLogger returns a logger for lock operations.
// Uses the environment-based logger with [lock] prefix.
var defaultLogger = logger.NewEnvLogger("[lock]")

// debugf logs debug output using the default logger.
// Used by helper functions that don't have access to the per-call logger.
func debugf(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

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
//
// Options can be passed to configure behavior:
//   - WithLogger(l): Use a custom logger instead of the default
//
// Security considerations for the lock directory path:
//
// The lock directory name is deterministic: rr-<projectHash>.lock
// This is intentional - all processes syncing the same project must agree on the
// lock path to achieve mutual exclusion. Adding randomness would break lock detection.
//
// The project hash is derived from the absolute path of the project directory,
// making it unique per-project but consistent across processes. The /tmp base
// directory has standard permissions (typically 1777 with sticky bit), preventing
// users from removing each other's lock directories.
//
// Potential concerns and mitigations:
//   - Predictable paths: An attacker knowing the project path could pre-create the
//     lock directory. Mitigated by: the stale lock detection removes old locks,
//     and legitimate users would investigate stuck locks anyway.
//   - Symlink attacks: mkdir on a symlink would follow it. Mitigated by: we use
//     the directory itself as the lock (not a file inside), and /tmp sticky bit
//     prevents other users from manipulating our directories.
//   - Info file tampering: Another user could write misleading info.json content.
//     Mitigated by: info is purely informational for debugging, not used for
//     security decisions.
func Acquire(conn *host.Connection, cfg config.LockConfig, projectHash string, opts ...AcquireOption) (*Lock, error) {
	// Apply options
	options := &acquireOptions{
		logger: defaultLogger,
	}
	for _, opt := range opts {
		opt(options)
	}
	log := options.logger

	if err := host.ValidateConnectionForLock(conn); err != nil {
		return nil, err
	}

	// Build lock directory path: <dir>/rr-<projectHash>.lock/
	baseDir := cfg.Dir
	if baseDir == "" {
		baseDir = "/tmp"
	}
	lockDir := filepath.Join(baseDir, fmt.Sprintf("rr-%s.lock", projectHash))
	infoFile := filepath.Join(lockDir, "info.json")

	log.Debug("attempting to acquire lock: dir=%s, timeout=%s, stale=%s", lockDir, cfg.Timeout, cfg.Stale)
	log.Debug("using connection: host=%s, address=%s", conn.Client.GetHost(), conn.Client.GetAddress())

	// Create our lock info
	info, err := NewLockInfo()
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrLock,
			"Couldn't create lock info",
			"Check your hostname and user environment variables.")
	}

	// Ensure the parent directory exists (e.g., /tmp/rr-locks)
	// This is done once before the retry loop since parent creation is idempotent
	if baseDir != "/tmp" {
		mkdirParentCmd := fmt.Sprintf("mkdir -p %q", baseDir)
		log.Debug("ensuring parent directory exists: %s", mkdirParentCmd)
		_, stderr, exitCode, err := conn.Client.Exec(mkdirParentCmd)
		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrLock,
				"Couldn't create the lock directory",
				"Check your SSH connection.")
		}
		if exitCode != 0 {
			return nil, errors.New(errors.ErrLock,
				fmt.Sprintf("Couldn't create lock directory at %s", baseDir),
				fmt.Sprintf("Remote error: %s", strings.TrimSpace(string(stderr))))
		}
	}

	startTime := time.Now()
	iteration := 0

	for {
		iteration++
		// Check if we've exceeded the timeout
		elapsed := time.Since(startTime)
		if elapsed > cfg.Timeout {
			// Try to read who holds the lock for a better error message
			holder := readLockHolder(conn.Client, infoFile)
			log.Debug("timeout after %d iterations, elapsed=%s, holder=%s", iteration, elapsed, holder)
			return nil, errors.New(errors.ErrLock,
				fmt.Sprintf("Lock timeout after %s - someone else is using this remote", cfg.Timeout),
				fmt.Sprintf("Held by: %s. Wait for them to finish or use --force-unlock if it's stale.", holder))
		}

		// Check for stale lock
		if isLockStale(conn.Client, infoFile, cfg.Stale) {
			log.Debug("detected stale lock, attempting removal")
			// Remove stale lock
			if err := forceRemove(conn.Client, lockDir); err == nil {
				log.Debug("stale lock removed successfully")
				// Stale lock removed, try again immediately
				continue
			}
			log.Debug("failed to remove stale lock: %v", err)
		}

		// Try to acquire lock using mkdir (atomic operation)
		// mkdir will fail if the directory already exists
		mkdirCmd := fmt.Sprintf("mkdir %q", lockDir)
		log.Debug("iteration %d: executing mkdir command: %s", iteration, mkdirCmd)
		stdout, stderr, exitCode, err := conn.Client.Exec(mkdirCmd)
		log.Debug("mkdir result: exitCode=%d, stdout=%q, stderr=%q, err=%v", exitCode, string(stdout), string(stderr), err)

		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrLock,
				"Lock command failed",
				"Check your SSH connection.")
		}

		if exitCode == 0 {
			log.Debug("lock directory created successfully, writing info file")
			// Lock acquired, write our info
			infoJSON, err := info.Marshal()
			if err != nil {
				// Clean up the lock dir if we can't write info
				forceRemove(conn.Client, lockDir)
				return nil, errors.WrapWithCode(err, errors.ErrLock,
					"Couldn't serialize lock info",
					"This is unexpected - please report this bug!")
			}

			// Write the info file
			writeCmd := fmt.Sprintf("cat > %q << 'LOCKINFO'\n%s\nLOCKINFO", infoFile, string(infoJSON))
			_, _, exitCode, err = conn.Client.Exec(writeCmd)
			if err != nil || exitCode != 0 {
				// Clean up and report error
				forceRemove(conn.Client, lockDir)
				return nil, errors.New(errors.ErrLock,
					"Couldn't write the lock info file",
					"Check disk space and permissions on the remote.")
			}

			log.Debug("lock acquired successfully: %s", lockDir)
			return &Lock{
				Dir:  lockDir,
				Info: info,
				conn: conn,
			}, nil
		}

		// Lock is held by someone else, wait before retrying
		log.Debug("mkdir failed (exitCode=%d), lock may be held by another process, waiting 2s before retry", exitCode)
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
	if err := host.ValidateConnectionForLock(conn); err != nil {
		return err
	}

	return forceRemove(conn.Client, lockDir)
}

// Holder returns information about who holds the lock (if readable).
func Holder(conn *host.Connection, lockDir string) string {
	if !host.HasClient(conn) {
		return "unknown (no connection)"
	}
	infoFile := filepath.Join(lockDir, "info.json")
	return readLockHolder(conn.Client, infoFile)
}

// isLockStale checks if the lock's info file is older than the stale threshold.
//
// Stale detection prevents orphaned locks from permanently blocking the remote.
// If a process crashes or loses network before releasing, the lock would persist
// forever without this mechanism. We use the timestamp in info.json rather than
// file mtime because mtime can be affected by file copies and NFS quirks.
//
// We err on the side of "not stale" when we can't read the file - better to
// wait for a lock that might be legitimate than to break into an active one.
func isLockStale(client sshutil.SSHClient, infoFile string, staleThreshold time.Duration) bool {
	if staleThreshold <= 0 {
		return false
	}

	// Read the info file and check the started time
	catCmd := fmt.Sprintf("cat %q", infoFile)
	stdout, _, exitCode, err := client.Exec(catCmd)
	debugf("isLockStale: cmd=%q, exitCode=%d, err=%v", catCmd, exitCode, err)
	if err != nil || exitCode != 0 {
		debugf("isLockStale: cannot read info file, assuming not stale")
		return false // Can't read, assume not stale
	}

	info, err := ParseLockInfo(stdout)
	if err != nil {
		debugf("isLockStale: failed to parse lock info: %v", err)
		return false
	}

	isStale := info.Age() > staleThreshold
	debugf("isLockStale: age=%s, threshold=%s, isStale=%v", info.Age(), staleThreshold, isStale)
	return isStale
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
	rmCmd := fmt.Sprintf("rm -rf %q", dir)
	debugf("forceRemove: executing %s", rmCmd)
	_, stderr, exitCode, err := client.Exec(rmCmd)
	debugf("forceRemove: exitCode=%d, stderr=%q, err=%v", exitCode, string(stderr), err)
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrLock,
			fmt.Sprintf("Couldn't remove lock directory at %s", dir),
			"Check your SSH connection.")
	}
	if exitCode != 0 {
		return errors.New(errors.ErrLock,
			fmt.Sprintf("Couldn't remove lock directory at %s", dir),
			fmt.Sprintf("Remote error: %s", string(stderr)))
	}
	return nil
}
