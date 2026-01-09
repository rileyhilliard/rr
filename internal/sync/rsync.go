package sync

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// FindRsync locates the rsync binary on the local system.
// Returns the full path to rsync or an error if not found.
func FindRsync() (string, error) {
	path, err := exec.LookPath("rsync")
	if err != nil {
		return "", errors.New(errors.ErrSync,
			"rsync isn't installed locally",
			"Grab it with: brew install rsync (macOS) or apt install rsync (Linux)")
	}
	return path, nil
}

// Version returns the rsync version string from the local installation.
func Version() (string, error) {
	rsyncPath, err := FindRsync()
	if err != nil {
		return "", err
	}

	cmd := exec.Command(rsyncPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", errors.WrapWithCode(err, errors.ErrSync,
			"Couldn't get rsync version",
			"Make sure rsync is installed correctly.")
	}

	// First line typically contains version info like:
	// "rsync  version 3.2.7  protocol version 31"
	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0]), nil
	}

	return "", errors.New(errors.ErrSync,
		"Couldn't parse the rsync version output",
		"Try running 'rsync --version' to check your installation.")
}

// CheckRemote verifies that rsync is available on the remote host.
func CheckRemote(conn *host.Connection) error {
	if conn == nil || conn.Client == nil {
		return errors.New(errors.ErrSync,
			"No active SSH connection",
			"Connect to the remote host first.")
	}

	// Check if rsync exists on remote using Exec
	_, _, exitCode, err := conn.Client.Exec("which rsync")
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrSSH,
			"Failed to check for rsync on remote",
			"Check your SSH connection")
	}
	if exitCode != 0 {
		return errors.New(errors.ErrSync,
			fmt.Sprintf("rsync isn't installed on %s", conn.Name),
			"Install it on the remote: apt install rsync (Debian/Ubuntu) or yum install rsync (RHEL)")
	}

	return nil
}
