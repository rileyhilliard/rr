package sync

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// PullOptions configures a pull operation.
type PullOptions struct {
	// Patterns are the remote paths or glob patterns to pull.
	Patterns []config.PullItem

	// DefaultDest is the default destination directory for patterns without a dest.
	// If empty, current directory is used.
	DefaultDest string

	// Flags are extra rsync flags to pass.
	Flags []string
}

// Pull downloads files from the remote host to the local machine using rsync.
// Progress output is streamed to the progress writer if provided.
//
// If conn.IsLocal is true, pull is skipped since we're already local.
//
// The rsync command follows this pattern:
// - Base flags: -az (archive mode, compress)
// - No --delete (don't remove local files)
// - Glob patterns are expanded on the remote side
func Pull(conn *host.Connection, opts PullOptions, progress io.Writer) error {
	// Validate connection
	if conn == nil {
		return errors.New(errors.ErrSync,
			"No connection provided",
			"Connect to the remote host first.")
	}

	// Skip pull for local connections - we're already working with local files
	if conn.IsLocal {
		return nil
	}

	if len(opts.Patterns) == 0 {
		return nil // Nothing to pull
	}

	rsyncPath, err := FindRsync()
	if err != nil {
		return err
	}

	// Ensure the SSH control socket directory exists for ControlMaster
	if err := os.MkdirAll(controlSocketDir, 0700); err != nil {
		return errors.WrapWithCode(err, errors.ErrSync,
			fmt.Sprintf("creating SSH control socket dir %s", controlSocketDir),
			"Check directory permissions and disk space")
	}

	// Group patterns by destination
	groups := groupByDest(opts.Patterns, opts.DefaultDest)

	// Sort destinations for deterministic execution order
	dests := make([]string, 0, len(groups))
	for dest := range groups {
		dests = append(dests, dest)
	}
	sort.Strings(dests)

	// Execute one rsync per destination group
	for _, dest := range dests {
		patterns := groups[dest]
		args, err := BuildPullArgs(conn, patterns, dest, opts.Flags)
		if err != nil {
			return err
		}

		if err := runRsyncPull(rsyncPath, args, conn.Name, progress); err != nil {
			return err
		}
	}

	return nil
}

// groupByDest groups pull items by their destination directory.
func groupByDest(items []config.PullItem, defaultDest string) map[string][]string {
	groups := make(map[string][]string)

	for _, item := range items {
		dest := item.Dest
		if dest == "" {
			dest = defaultDest
		}
		if dest == "" {
			dest = "."
		}
		groups[dest] = append(groups[dest], item.Src)
	}

	return groups
}

// BuildPullArgs constructs the rsync command arguments for pulling files.
// Exported for testing command construction without running rsync.
func BuildPullArgs(conn *host.Connection, patterns []string, localDest string, extraFlags []string) ([]string, error) {
	if conn == nil {
		return nil, errors.New(errors.ErrSync,
			"No connection provided",
			"Connect to the remote host first.")
	}

	if len(patterns) == 0 {
		return nil, errors.New(errors.ErrSync,
			"No patterns provided",
			"Specify at least one file or pattern to pull.")
	}

	// Ensure localDest ends with / for directory behavior
	localDest = filepath.Clean(localDest)
	if localDest != "." && !strings.HasSuffix(localDest, "/") {
		localDest += "/"
	}

	// Ensure the local destination directory exists
	if localDest != "." {
		if err := os.MkdirAll(strings.TrimSuffix(localDest, "/"), 0755); err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrSync,
				fmt.Sprintf("Couldn't create destination directory %s", localDest),
				"Check file permissions.")
		}
	}

	// Build remote base path: ssh-alias:remote-dir/
	remoteDir := config.ExpandRemote(conn.Host.Dir)
	if !strings.HasSuffix(remoteDir, "/") {
		remoteDir += "/"
	}

	// Base flags for pull (archive mode, compress, but no --delete)
	args := []string{
		"-az", // archive mode, compress
	}

	// Use SSH ControlMaster to reuse existing connections
	sshCmd := fmt.Sprintf("ssh -o ControlMaster=auto -o ControlPath=%s/%%h-%%p -o ControlPersist=60 -o BatchMode=yes",
		controlSocketDir)
	if SSHConfigFile != "" {
		sshCmd = fmt.Sprintf("%s -F %q", sshCmd, SSHConfigFile)
	}
	args = append(args, "-e", sshCmd)

	// Add progress info flag for parsing
	args = append(args, "--info=progress2")

	// Add extra flags
	args = append(args, extraFlags...)

	// Build remote sources: ssh-alias:remote-dir/pattern for each pattern
	for _, pattern := range patterns {
		remoteSrc := fmt.Sprintf("%s:%s%s", conn.Alias, remoteDir, pattern)
		args = append(args, remoteSrc)
	}

	// Destination last
	args = append(args, localDest)

	return args, nil
}

// runRsyncPull executes rsync with the given arguments and handles output.
func runRsyncPull(rsyncPath string, args []string, hostName string, progress io.Writer) error {
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
			return handlePullError(err, hostName, stderrBuf.String())
		}
	} else {
		// No progress output - stream stdout to discard, capture only stderr
		// This avoids OOM with large rsync progress output
		var stderrBuf bytes.Buffer
		cmd.Stdout = io.Discard
		cmd.Stderr = &stderrBuf
		if err := cmd.Run(); err != nil {
			return handlePullError(err, hostName, stderrBuf.String())
		}
	}

	return nil
}

// handlePullError wraps rsync exit errors with helpful messages for pull operations.
func handlePullError(err error, hostName string, stderrOutput string) error {
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return errors.WrapWithCode(err, errors.ErrSync,
			"rsync pull failed",
			"Try running rsync manually to diagnose")
	}

	exitCode := exitErr.ExitCode()
	var msg, suggestion string

	// Check for rsync version incompatibility
	if isRsyncVersionError(stderrOutput) {
		return errors.New(errors.ErrSync,
			"rsync version too old",
			rsyncVersionSuggestion())
	}

	// Check for common pull-specific errors
	if strings.Contains(stderrOutput, "No such file or directory") {
		return errors.New(errors.ErrSync,
			"Remote file or pattern not found",
			"Check that the file exists on the remote host.")
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
		suggestion = "Check that the remote paths exist and are readable"
	case 5:
		msg = "Error starting client-server protocol"
		suggestion = "Check SSH connection and remote rsync installation"
	case 10:
		msg = "Error in socket I/O"
		suggestion = "Check network connectivity to the remote host"
	case 11:
		msg = "Error in file I/O"
		suggestion = "Check disk space and file permissions locally"
	case 12:
		msg = "Error in rsync protocol data stream"
		suggestion = "This may indicate a corrupted transfer, try again"
	case 23:
		msg = "Partial transfer due to error"
		suggestion = "Some files may not exist or have permission issues"
	case 255:
		msg = fmt.Sprintf("SSH connection to '%s' failed", hostName)
		suggestion = "Check that the host is reachable: ssh " + hostName
	default:
		msg = fmt.Sprintf("rsync exited with code %d", exitCode)
		suggestion = "Check the output above for specific error details"
	}

	return errors.WrapWithCode(err, errors.ErrSync, msg, suggestion)
}
