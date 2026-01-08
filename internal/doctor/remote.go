package doctor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
)

// RemoteDirCheck verifies the working directory exists on a remote host.
type RemoteDirCheck struct {
	HostName string
	Dir      string
	Conn     *host.Connection
}

func (c *RemoteDirCheck) Name() string     { return fmt.Sprintf("remote_dir_%s", c.HostName) }
func (c *RemoteDirCheck) Category() string { return "REMOTE" }

func (c *RemoteDirCheck) Run() CheckResult {
	if c.Conn == nil || c.Conn.Client == nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("Working directory (%s): no connection", c.HostName),
		}
	}

	dir := config.Expand(c.Dir)

	// Check if directory exists
	_, _, exitCode, err := c.Conn.Client.Exec(fmt.Sprintf("test -d %q", dir))
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("Cannot check directory: %v", err),
			Suggestion: "Check SSH connection",
		}
	}

	if exitCode != 0 {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusWarn,
			Message:    fmt.Sprintf("Working directory does not exist: %s", dir),
			Suggestion: "Directory will be created on first sync",
			Fixable:    true,
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: fmt.Sprintf("Working directory exists: %s", dir),
	}
}

func (c *RemoteDirCheck) Fix() error {
	if c.Conn == nil || c.Conn.Client == nil {
		return fmt.Errorf("no connection")
	}

	dir := config.Expand(c.Dir)
	_, _, exitCode, err := c.Conn.Client.Exec(fmt.Sprintf("mkdir -p %q", dir))
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to create directory: %s", dir)
	}

	return nil
}

// RemoteWritePermCheck verifies write permission to the working directory.
type RemoteWritePermCheck struct {
	HostName string
	Dir      string
	Conn     *host.Connection
}

func (c *RemoteWritePermCheck) Name() string     { return fmt.Sprintf("remote_write_%s", c.HostName) }
func (c *RemoteWritePermCheck) Category() string { return "REMOTE" }

func (c *RemoteWritePermCheck) Run() CheckResult {
	if c.Conn == nil || c.Conn.Client == nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("Write permission (%s): no connection", c.HostName),
		}
	}

	dir := config.Expand(c.Dir)

	// First check if dir exists
	_, _, exitCode, _ := c.Conn.Client.Exec(fmt.Sprintf("test -d %q", dir))
	if exitCode != 0 {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass, // Directory doesn't exist yet, will be created
			Message: "Write permission: N/A (directory does not exist)",
		}
	}

	// Try to write a test file
	testFile := filepath.Join(dir, ".rr-write-test")
	_, _, exitCode, err := c.Conn.Client.Exec(fmt.Sprintf("touch %q && rm -f %q", testFile, testFile))
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    "Cannot test write permission",
			Suggestion: "Check SSH connection",
		}
	}

	if exitCode != 0 {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("No write permission to %s", dir),
			Suggestion: "Check directory ownership and permissions on the remote host",
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: "Write permission: OK",
	}
}

func (c *RemoteWritePermCheck) Fix() error {
	return nil // Permission issues require manual intervention
}

// RemoteStaleLockCheck checks for stale lock files.
type RemoteStaleLockCheck struct {
	HostName   string
	Conn       *host.Connection
	LockConfig config.LockConfig
}

func (c *RemoteStaleLockCheck) Name() string     { return fmt.Sprintf("remote_locks_%s", c.HostName) }
func (c *RemoteStaleLockCheck) Category() string { return "REMOTE" }

func (c *RemoteStaleLockCheck) Run() CheckResult {
	if c.Conn == nil || c.Conn.Client == nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass, // Can't check without connection
			Message: "Lock check: no connection",
		}
	}

	if !c.LockConfig.Enabled {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: "Locking disabled",
		}
	}

	lockDir := c.LockConfig.Dir
	if lockDir == "" {
		lockDir = "/tmp/rr-locks"
	}

	// List lock directories
	stdout, _, exitCode, err := c.Conn.Client.Exec(fmt.Sprintf("ls -1 %q 2>/dev/null | grep '\\.lock$' || true", lockDir))
	if err != nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: "Cannot check locks",
		}
	}

	if exitCode != 0 || strings.TrimSpace(string(stdout)) == "" {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: "No stale locks found",
		}
	}

	// Check each lock for staleness
	locks := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	var staleLocks []string

	for _, lockName := range locks {
		if lockName == "" {
			continue
		}

		lockPath := filepath.Join(lockDir, lockName)
		infoPath := filepath.Join(lockPath, "info.json")

		// Read lock info
		infoData, _, exitCode, _ := c.Conn.Client.Exec(fmt.Sprintf("cat %q 2>/dev/null", infoPath))
		if exitCode != 0 {
			continue // Can't read, skip
		}

		info, err := lock.ParseLockInfo(infoData)
		if err != nil {
			continue
		}

		if info.Age() > c.LockConfig.Stale {
			staleLocks = append(staleLocks, fmt.Sprintf("%s (held by %s for %s)",
				lockName, info.User, formatDuration(info.Age())))
		}
	}

	if len(staleLocks) > 0 {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusWarn,
			Message:    fmt.Sprintf("%d stale lock%s found", len(staleLocks), pluralize(len(staleLocks))),
			Suggestion: fmt.Sprintf("Stale locks: %v\nRemove with: rr unlock --force", staleLocks),
			Fixable:    true,
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: "No stale locks found",
	}
}

func (c *RemoteStaleLockCheck) Fix() error {
	// Would need to implement force unlock
	return nil
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// NewRemoteChecks creates all remote environment checks for a host.
func NewRemoteChecks(hostName string, hostCfg config.Host, conn *host.Connection, lockCfg config.LockConfig) []Check {
	return []Check{
		&RemoteDirCheck{
			HostName: hostName,
			Dir:      hostCfg.Dir,
			Conn:     conn,
		},
		&RemoteWritePermCheck{
			HostName: hostName,
			Dir:      hostCfg.Dir,
			Conn:     conn,
		},
		&RemoteStaleLockCheck{
			HostName:   hostName,
			Conn:       conn,
			LockConfig: lockCfg,
		},
	}
}
