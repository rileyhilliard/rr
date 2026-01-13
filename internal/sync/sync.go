package sync

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/util"
)

// controlSocketDir is the directory for SSH ControlMaster sockets.
// Using /tmp for cross-platform compatibility.
var controlSocketDir = filepath.Join(os.TempDir(), "rr-ssh")

// SSHConfigFile can be set to use a custom SSH config file for rsync.
// If empty, uses the default SSH config. Useful for testing with custom
// host configurations.
var SSHConfigFile string

// Sync transfers files from localDir to the remote host using rsync.
// Progress output is streamed to the progress writer if provided.
//
// If conn.IsLocal is true, sync is skipped entirely since we're already local.
//
// The rsync command follows the pattern from proof-of-concept.sh:
// - Base flags: -az --delete --force
// - Preserve patterns prevent deletion of specified paths on remote
// - Exclude patterns prevent files from being synced
// - Custom flags from config are appended
func Sync(conn *host.Connection, localDir string, cfg config.SyncConfig, progress io.Writer) error {
	// Skip sync for local connections - we're already working with local files
	if conn != nil && conn.IsLocal {
		return nil
	}

	rsyncPath, err := FindRsync()
	if err != nil {
		return err
	}

	// Ensure the SSH control socket directory exists for ControlMaster
	// Non-fatal if it fails: rsync will still work, just without connection reuse
	_ = os.MkdirAll(controlSocketDir, 0700)

	// Ensure remote directory exists before rsync (rsync won't create parent dirs)
	if err := ensureRemoteDir(conn); err != nil {
		return err
	}

	args, err := BuildArgs(conn, localDir, cfg)
	if err != nil {
		return err
	}

	cmd := exec.Command(rsyncPath, args...)

	// Set up progress output if provided
	if progress != nil {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrSync,
				"Couldn't capture rsync output",
				"Try running rsync manually to see what's happening.")
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrSync,
				"Couldn't capture rsync stderr",
				"Try running rsync manually to see what's happening.")
		}

		if err := cmd.Start(); err != nil {
			return errors.WrapWithCode(err, errors.ErrSync,
				"Couldn't start rsync",
				"Make sure rsync is installed and the paths are valid.")
		}

		// Capture stderr for error analysis while also streaming to progress
		var stderrBuf bytes.Buffer
		stderrWriter := io.MultiWriter(&stderrBuf, progress)

		// Stream stdout (progress info)
		go streamOutput(stdout, progress)
		// Stream stderr (errors/warnings) to both buffer and progress
		go streamOutput(stderr, stderrWriter)

		if err := cmd.Wait(); err != nil {
			return handleRsyncError(err, conn.Name, stderrBuf.String())
		}
	} else {
		// No progress output, just run and wait
		output, err := cmd.CombinedOutput()
		if err != nil {
			return handleRsyncError(err, conn.Name, string(output))
		}
	}

	return nil
}

// BuildArgs constructs the rsync command arguments.
// Exported for testing command construction without running rsync.
func BuildArgs(conn *host.Connection, localDir string, cfg config.SyncConfig) ([]string, error) {
	if conn == nil {
		return nil, errors.New(errors.ErrSync,
			"No connection provided",
			"Connect to the remote host first.")
	}

	// Ensure localDir ends with / so rsync syncs contents, not directory itself
	localDir = filepath.Clean(localDir)
	if !strings.HasSuffix(localDir, "/") {
		localDir += "/"
	}

	// Build remote destination: ssh-alias:remote-dir
	remoteDir := config.ExpandRemote(conn.Host.Dir)
	if !strings.HasSuffix(remoteDir, "/") {
		remoteDir += "/"
	}
	remoteDest := fmt.Sprintf("%s:%s", conn.Alias, remoteDir)

	// Base flags following proof-of-concept.sh pattern
	args := []string{
		"-az",      // archive mode, compress
		"--delete", // delete files on remote not in source
		"--force",  // force deletion of non-empty dirs
	}

	// Use SSH ControlMaster to reuse existing connections.
	// This avoids a second SSH handshake when we already have an active connection.
	// ControlPath uses a hash of the host to create unique socket files.
	// ControlMaster=auto: reuse existing socket or create new one if needed.
	// ControlPersist=60: keep the socket alive for 60s after last use.
	// BatchMode=yes: prevent SSH from prompting for input (passwords, host keys, etc.)
	//   which would cause rsync to hang since there's no terminal attached.
	sshCmd := fmt.Sprintf("ssh -o ControlMaster=auto -o ControlPath=%s/%%h-%%p -o ControlPersist=60 -o BatchMode=yes",
		controlSocketDir)
	// Support custom SSH config file (useful for testing)
	if SSHConfigFile != "" {
		sshCmd = fmt.Sprintf("%s -F %q", sshCmd, SSHConfigFile)
	}
	args = append(args, "-e", sshCmd)

	// Add progress info flag for parsing
	args = append(args, "--info=progress2")

	// Add preserve patterns as filters (P = protect from deletion)
	// These go BEFORE excludes so they protect paths that might otherwise be deleted
	for _, pattern := range cfg.Preserve {
		// Handle both simple patterns and patterns with subdirs
		args = append(args, fmt.Sprintf("--filter=P %s", pattern))
		// Also protect the pattern in any subdirectory
		if !strings.HasPrefix(pattern, "**/") {
			args = append(args, fmt.Sprintf("--filter=P **/%s", pattern))
		}
	}

	// Add exclude patterns
	for _, pattern := range cfg.Exclude {
		args = append(args, fmt.Sprintf("--exclude=%s", pattern))
	}

	// Add custom flags from config
	args = append(args, cfg.Flags...)

	// Source and destination last
	args = append(args, localDir, remoteDest)

	return args, nil
}

// streamOutput reads from r and writes each line to w.
// It handles both \n and \r as line delimiters since rsync uses \r for progress updates.
func streamOutput(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	scanner.Split(scanLinesWithCR)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			fmt.Fprintln(w, line)
		}
	}
}

// scanLinesWithCR is a split function that handles both \n and \r as line delimiters.
// This is necessary because rsync's --info=progress2 uses \r to update progress in-place.
func scanLinesWithCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	// Look for \r or \n
	for i, b := range data {
		if b == '\n' || b == '\r' {
			// Skip empty tokens from consecutive delimiters (e.g., \r\n)
			return i + 1, data[0:i], nil
		}
	}
	// At EOF, return remaining data
	if atEOF {
		return len(data), data, nil
	}
	// Request more data
	return 0, nil, nil
}

// isRsyncVersionError checks if the error output indicates an rsync version incompatibility.
// The --info=progress2 flag requires rsync 3.1.0 or later.
func isRsyncVersionError(output string) bool {
	return strings.Contains(output, "unrecognized option") &&
		strings.Contains(output, "--info=progress2")
}

// rsyncVersionSuggestion returns platform-specific upgrade instructions.
func rsyncVersionSuggestion() string {
	return "The --info=progress2 flag requires rsync 3.1.0+.\n" +
		"  macOS: brew install rsync (then ensure /opt/homebrew/bin is in PATH)\n" +
		"  Linux: apt install rsync or yum install rsync\n" +
		"  Run 'rr doctor' to check your rsync version."
}

// handleRsyncError wraps rsync exit errors with helpful messages.
func handleRsyncError(err error, hostName string, stderrOutput string) error {
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return errors.WrapWithCode(err, errors.ErrSync,
			"rsync failed",
			"Try running rsync manually to diagnose")
	}

	// rsync exit codes have specific meanings
	// See: https://download.samba.org/pub/rsync/rsync.1
	exitCode := exitErr.ExitCode()
	var msg, suggestion string

	// Check for rsync version incompatibility first (before generic exit code handling)
	if isRsyncVersionError(stderrOutput) {
		return errors.New(errors.ErrSync,
			"rsync version too old",
			rsyncVersionSuggestion())
	}

	switch exitCode {
	case 1:
		msg = "rsync syntax or usage error"
		suggestion = "Check your rsync configuration for invalid options"
	case 2:
		msg = "rsync protocol incompatibility"
		suggestion = "Ensure rsync versions are compatible on local and remote"
	case 3:
		msg = "File selection error"
		suggestion = "Check that source paths exist and are readable"
	case 5:
		msg = "Error starting client-server protocol"
		suggestion = "Check SSH connection and remote rsync installation"
	case 10:
		msg = "Error in socket I/O"
		suggestion = "Check network connectivity to the remote host"
	case 11:
		msg = "Error in file I/O"
		suggestion = "Check disk space and file permissions on both local and remote"
	case 12:
		msg = "Error in rsync protocol data stream"
		suggestion = "This may indicate a corrupted transfer, try again"
	case 23:
		msg = "Partial transfer due to error"
		suggestion = "Some files may have permission issues, check the output above"
	case 24:
		msg = "Partial transfer due to vanished source files"
		suggestion = "Files were modified during sync, this is usually harmless"
	case 255:
		msg = fmt.Sprintf("SSH connection to '%s' failed", hostName)
		suggestion = "Check that the host is reachable: ssh " + hostName
	default:
		msg = fmt.Sprintf("rsync exited with code %d", exitCode)
		suggestion = "Check the output above for specific error details"
	}

	return errors.WrapWithCode(err, errors.ErrSync, msg, suggestion)
}

// ensureRemoteDir creates the remote sync directory if it doesn't exist.
// rsync requires the target directory (or at least its parent) to exist.
func ensureRemoteDir(conn *host.Connection) error {
	if conn == nil || conn.Client == nil {
		return errors.New(errors.ErrSync,
			"No SSH connection available",
			"Connect to the remote host first.")
	}

	remoteDir := config.ExpandRemote(conn.Host.Dir)
	// Use single quotes around all but leading ~ so tilde expands but spaces are safe
	// e.g. ~/rr/rr -> mkdir -p ~/'rr/rr'
	mkdirCmd := fmt.Sprintf("mkdir -p %s", util.ShellQuotePreserveTilde(remoteDir))

	_, stderr, exitCode, err := conn.Client.Exec(mkdirCmd)
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrSync,
			"Couldn't create remote directory",
			"Check your SSH connection.")
	}
	if exitCode != 0 {
		return errors.New(errors.ErrSync,
			fmt.Sprintf("Couldn't create remote directory %s", remoteDir),
			fmt.Sprintf("Remote error: %s", strings.TrimSpace(string(stderr))))
	}

	return nil
}
