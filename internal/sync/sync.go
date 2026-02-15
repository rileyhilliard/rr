package sync

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/gitdiff"
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

// maxGitAwareFiles is the threshold above which git-aware sync falls back to
// full rsync. At this scale the include filter processing overhead approaches
// the full stat pass cost.
const maxGitAwareFiles = 500

// Sync transfers files from localDir to the remote host using rsync.
// Progress output is streamed to the progress writer if provided.
//
// If conn.IsLocal is true, sync is skipped entirely since we're already local.
//
// When cfg.GitAware is true, Sync tries a fast path that uses git to detect
// changed files and scopes rsync to only those files. If anything goes wrong,
// it falls back to a full rsync transparently.
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

	// Try git-aware fast path when enabled
	if cfg.GitAware {
		err := gitAwareSync(rsyncPath, conn, localDir, cfg, progress)
		if err == nil {
			// Fast path succeeded, save state and return
			saveSyncStateAfterSuccess(conn, localDir)
			return nil
		}
		log.Printf("git-aware sync fallback: %v", err)
		// Fall through to full sync
	}

	// Full rsync (default path or fallback from git-aware)
	args, err := BuildArgs(conn, localDir, cfg)
	if err != nil {
		return err
	}

	if err := runRsync(rsyncPath, args, conn.Name, progress); err != nil {
		return err
	}

	// Save sync state after any successful sync so git-aware can work next time
	if cfg.GitAware {
		saveSyncStateAfterSuccess(conn, localDir)
	}

	return nil
}

// gitAwareSync uses git to detect changed files and scopes rsync to only
// those files. Returns an error to trigger full sync fallback.
func gitAwareSync(rsyncPath string, conn *host.Connection, localDir string, cfg config.SyncConfig, progress io.Writer) error {
	changes, err := gitdiff.Detect(gitdiff.DetectOptions{
		WorkDir:    localDir,
		BaseBranch: cfg.BaseBranch,
		ProjectDir: localDir,
	})
	if err != nil {
		return err
	}

	// Check sync state - force full sync on branch/host switch
	prevState, _ := LoadSyncState(localDir)
	currentState := &SyncState{Branch: changes.Branch, Host: conn.Name, Alias: conn.Alias}
	if prevState == nil || SyncStateChanged(currentState, prevState) {
		return fmt.Errorf("branch or host changed (prev=%v, curr=%v), full sync required", prevState, currentState)
	}

	if len(changes.Files) == 0 {
		// No changes detected. Remote should already match since we're on the
		// same branch/host and there are no new changes.
		return nil
	}

	if len(changes.Files) > maxGitAwareFiles {
		return fmt.Errorf("too many changed files (%d > %d), full sync required", len(changes.Files), maxGitAwareFiles)
	}

	args, err := BuildGitAwareArgs(conn, localDir, cfg, changes.Files)
	if err != nil {
		return err
	}

	return runRsync(rsyncPath, args, conn.Name, progress)
}

// saveSyncStateAfterSuccess updates the sync state file after a successful sync.
// Errors are logged but don't fail the sync since this is best-effort.
func saveSyncStateAfterSuccess(conn *host.Connection, localDir string) {
	changes, err := gitdiff.Detect(gitdiff.DetectOptions{
		WorkDir:    localDir,
		ProjectDir: localDir,
	})
	if err != nil {
		return
	}
	state := &SyncState{Branch: changes.Branch, Host: conn.Name, Alias: conn.Alias}
	if err := SaveSyncState(localDir, state); err != nil {
		log.Printf("warning: couldn't save sync state: %v", err)
	}
}

// BuildGitAwareArgs constructs rsync arguments scoped to only the changed files.
// Uses include/exclude filters to limit rsync's stat pass to changed files while
// keeping --delete active for proper remote cleanup.
func BuildGitAwareArgs(conn *host.Connection, localDir string, cfg config.SyncConfig, files []string) ([]string, error) {
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

	args := []string{
		"-az",      // archive mode, compress
		"--delete", // delete files on remote not in source
		"--force",  // force deletion of non-empty dirs
	}

	// SSH options (same as BuildArgs)
	sshCmd := fmt.Sprintf("ssh -o ControlMaster=auto -o ControlPath=%s/%%h-%%p -o ControlPersist=60 -o BatchMode=yes",
		controlSocketDir)
	if SSHConfigFile != "" {
		sshCmd = fmt.Sprintf("%s -F %q", sshCmd, SSHConfigFile)
	}
	args = append(args, "-e", sshCmd)

	args = append(args, "--info=progress2")

	// Preserve patterns (same as BuildArgs: --filter=P pattern + --filter=P **/pattern)
	// These go first so they protect paths from deletion
	for _, pattern := range cfg.Preserve {
		args = append(args, fmt.Sprintf("--filter=P %s", pattern))
		if !strings.HasPrefix(pattern, "**/") {
			args = append(args, fmt.Sprintf("--filter=P **/%s", pattern))
		}
	}

	// Scope rsync to only changed files via include/exclude filters.
	// Order matters: rsync uses first-match-wins.
	args = append(args, "--include=*/") // traverse all directories
	for _, f := range files {
		args = append(args, fmt.Sprintf("--include=%s", f))
	}
	args = append(args, "--exclude=*") // catch-all: ignore everything else

	// NOTE: config excludes are intentionally omitted here.
	// The explicit include list + catch-all exclude already restricts rsync
	// to only the files git identified. Config excludes would be dead rules
	// since any non-included file is already caught by --exclude=*.

	// Custom flags from config
	args = append(args, cfg.Flags...)

	// Source and destination last
	args = append(args, localDir, remoteDest)

	return args, nil
}

// runRsync executes rsync with the given arguments, streaming progress if provided.
func runRsync(rsyncPath string, args []string, hostName string, progress io.Writer) error {
	cmd := exec.Command(rsyncPath, args...)

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

		go streamOutput(stdout, progress)
		go streamOutput(stderr, stderrWriter)

		if err := cmd.Wait(); err != nil {
			return handleRsyncError(err, hostName, stderrBuf.String())
		}
	} else {
		output, err := cmd.CombinedOutput()
		if err != nil {
			return handleRsyncError(err, hostName, string(output))
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
