package sync

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/util"
)

// controlSocketDir is the directory for SSH ControlMaster sockets.
// Uses /tmp with per-user namespacing to keep paths short (macOS has a 104-byte
// Unix socket path limit, and os.TempDir() returns long /var/folders/... paths).
var controlSocketDir = fmt.Sprintf("/tmp/rr-ssh-%d", os.Getuid())

// SSHConfigFile can be set to use a custom SSH config file for rsync.
// If empty, uses the default SSH config. Useful for testing with custom
// host configurations.
var SSHConfigFile string

// buildSSHCmd returns the SSH command string for rsync's -e flag.
// It includes ControlMaster for connection reuse and loads the user's SSH config
// so rsync inherits ProxyCommand, IdentityFile, and other host-specific settings.
func buildSSHCmd() string {
	cmd := fmt.Sprintf("ssh -o ControlMaster=auto -o ControlPath=%s/%%h-%%p -o ControlPersist=60 -o BatchMode=yes",
		controlSocketDir)
	configFile := SSHConfigFile
	if configFile == "" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".ssh", "config")
			if _, err := os.Stat(candidate); err == nil {
				configFile = candidate
			}
		}
	}
	if configFile != "" {
		cmd = fmt.Sprintf("%s -F %q", cmd, configFile)
	}
	return cmd
}

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
	return SyncWithOptions(conn, localDir, cfg, progress, nil)
}

// SyncWithOptions is Sync with optional behavior (e.g. a warning callback
// for provenance mismatches).
func SyncWithOptions(conn *host.Connection, localDir string, cfg config.SyncConfig, progress io.Writer, opts *SyncOptions) error {
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

	// Warn if the remote dir was last synced from a different tree/machine
	checkSourceMarker(conn, localDir, opts)

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

	// Record where this sync came from (best-effort)
	writeSourceMarker(conn, localDir)

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

	// Use SSH with ControlMaster for connection reuse and user's SSH config
	// for ProxyCommand, IdentityFile, and other host-specific settings.
	args = append(args, "-e", buildSSHCmd())

	// Add progress info flag for parsing
	args = append(args, "--info=progress2")

	// Protect the provenance marker rr writes after each sync - it never
	// exists locally, so --delete would remove it without this rule
	args = append(args, fmt.Sprintf("--filter=P /%s", sourceMarkerFile))

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

	// Respect .gitignore patterns as additional excludes. Placed after explicit
	// preserves and excludes so .rr.yaml rules take precedence (rsync is first-match-wins).
	if cfg.RespectGitignore {
		gitignoreArgs, err := gitignoreFilterArgs(localDir)
		if err != nil {
			return nil, err
		}
		args = append(args, gitignoreArgs...)
	}

	// Add custom flags from config
	args = append(args, cfg.Flags...)

	// Source and destination last
	args = append(args, localDir, remoteDest)

	return args, nil
}

// gitignoreFilterArgs reads the top-level .gitignore in localDir and
// translates it into rsync --filter rules.
//
// This exists because rsync's own .gitignore support (--filter=':- .gitignore')
// has no concept of git's "!pattern" negation. Rsync's merge-file format
// only understands a bare "!" as "clear every rule read so far" - it is not
// per-pattern and does not mean "re-include this path". So a .gitignore
// containing an unanchored "data/" exclude followed by
// "!frontend/tests/mocks/data/" to carve out an exception (a completely
// normal, common pattern) would silently drop that path from every sync,
// with git itself reporting the path as NOT ignored the whole time -
// making the discrepancy very hard to spot.
//
// Git resolves a .gitignore with "last matching pattern wins" (top to
// bottom). Rsync's filter list is "first matching rule wins". To reproduce
// git's semantics we emit rules in REVERSED file order, with each
// "!pattern" line becoming an rsync include ("+") and every other line
// becoming an exclude ("-").
//
// There's one more wrinkle, straight from `git help gitignore`: "It is not
// possible to re-include a file if a parent directory of that file is
// excluded." Whether a negation is actually reachable depends on
// resolving EVERY ancestor directory independently, root to leaf, and
// stopping the moment one resolves as excluded - deeper patterns never
// get a say once that happens. Concretely (all verified against real
// git):
//
//   - A bare directory-unit exclude ("build/") always kills a negation of
//     anything underneath, in EITHER file order, unless "build/" is
//     itself separately re-included by its own "!build/" line.
//   - A wildcard exclude of a directory's children ("build/*") does NOT
//     poison the directory itself, so "!build/keep-me/" can still live -
//     provided it comes after "build/*" in the file (ordinary
//     last-match-wins).
//   - This nests: "a/" excluded then "!a/" re-included then "a/b/"
//     excluded then "!a/b/" re-included means a/b/anything is NOT
//     ignored, because each ancestor resolves independently once its own
//     parent is clear.
//
// We approximate full git resolution with a bounded check scoped to what
// this function actually needs (only NEGATED lines can possibly need
// rescuing from an rsync exclude): for a "!pattern" line, walk pattern's
// ancestors root to leaf. An ancestor is "poisoned" if the LAST bare
// directory-unit pattern for that exact ancestor, anywhere earlier in the
// file, is an exclude with no later re-include for that same ancestor.
// The negation is dead the moment any ancestor is poisoned. This covers
// arbitrarily deep nesting without a full gitignore glob matcher, at the
// cost of only comparing bare directory-unit patterns exactly (not
// wildcards) - sufficient because a wildcard exclude can never poison an
// ancestor per the rule above.
//
// Live negations still need their ancestor directories explicitly
// included, or rsync will never descend far enough to see the file - an
// exclude on an ancestor directory prevents rsync from looking inside it
// at all, independent of the poisoned-ancestor check above.
//
// Scope: only the repo-root .gitignore is read, matching what rsync's
// previous ':- .gitignore' invocation covered. Nested per-directory
// .gitignore files are not merged; if that's needed later, extend this to
// walk the tree the way git itself does.
func gitignoreFilterArgs(localDir string) ([]string, error) {
	path := filepath.Join(localDir, ".gitignore")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.WrapWithCode(err, errors.ErrSync,
			"Couldn't read .gitignore",
			"Check the file's permissions or disable respect_gitignore in .rr.yaml.")
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, trimmed)
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrSync,
			"Couldn't read .gitignore",
			"Check the file's permissions or disable respect_gitignore in .rr.yaml.")
	}

	var args []string
	seenIncludes := make(map[string]bool)

	// Reversed file order: git's last-matching-pattern-wins becomes
	// rsync's first-matching-rule-wins.
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]

		negated := strings.HasPrefix(line, "!")
		pattern := line
		if negated {
			pattern = line[1:]
		}
		pattern = unescapeGitignorePattern(pattern)

		if negated {
			if anyAncestorPoisoned(pattern, lines) {
				// Dead negation: an ancestor directory resolves excluded
				// per git's own rules. Git can never resurrect this path
				// either, so emit nothing.
				continue
			}
			for _, parent := range ancestorDirs(pattern) {
				if seenIncludes[parent] {
					continue
				}
				seenIncludes[parent] = true
				args = append(args, fmt.Sprintf("--filter=+ %s", parent))
			}
			if !seenIncludes[pattern] {
				seenIncludes[pattern] = true
				args = append(args, fmt.Sprintf("--filter=+ %s", pattern))
			}
		} else {
			args = append(args, fmt.Sprintf("--filter=- %s", pattern))
		}
	}

	return args, nil
}

// unescapeGitignorePattern strips gitignore's backslash-escape from a
// literal leading "!" or "#" so rsync sees the intended literal character.
func unescapeGitignorePattern(pattern string) string {
	if strings.HasPrefix(pattern, "\\!") || strings.HasPrefix(pattern, "\\#") {
		return pattern[1:]
	}
	return pattern
}

// anyAncestorPoisoned reports whether any ancestor directory of pattern
// resolves to excluded, per git's rule that a directory-unit exclude with
// no later re-include for that exact directory poisons everything
// beneath it regardless of deeper negations. Ancestors are checked root
// to leaf so a poisoned parent short-circuits before a clear grandparent
// could otherwise be mistaken for permission to descend.
func anyAncestorPoisoned(pattern string, lines []string) bool {
	for _, ancestor := range ancestorDirs(pattern) {
		if dirResolvesExcluded(ancestor, lines) {
			return true
		}
	}
	return false
}

// dirResolvesExcluded reports whether dirPattern (a directory-unit
// pattern like "build/" or "frontend/tests/mocks/") is excluded per the
// LAST bare directory-unit rule anywhere in the file that names that
// directory. Only bare directory patterns count, never wildcards, since
// a wildcard exclude of a directory's children (e.g. "build/*") can
// never poison the directory itself.
//
// Per gitignore's own matching rules, a pattern with no slash (besides a
// possible trailing one) is UNANCHORED and matches at any depth by its
// final path segment alone - e.g. "mocks/" matches
// "frontend/tests/mocks/" the same way it matches a top-level "mocks/".
// A pattern containing an interior slash is anchored to that exact path.
// This mirrors real git: verified with git check-ignore that a bare
// "mocks/" rule poisons a "!frontend/tests/mocks/data/" negation even
// though the exclude pattern is never anchored to the full ancestor path.
func dirResolvesExcluded(dirPattern string, lines []string) bool {
	excluded := false
	for _, line := range lines {
		negated := strings.HasPrefix(line, "!")
		candidate := line
		if negated {
			candidate = line[1:]
		}
		candidate = unescapeGitignorePattern(candidate)
		if !dirPatternMatches(candidate, dirPattern) {
			continue
		}
		excluded = !negated
	}
	return excluded
}

// dirPatternMatches reports whether gitignore directory pattern matches
// ancestor (both normalized as "a/b/" style paths). An exact match always
// counts. An unanchored pattern (no interior slash) additionally matches
// by ancestor's final path segment, per gitignore's own rule that such a
// pattern applies at any depth.
func dirPatternMatches(pattern, ancestor string) bool {
	if pattern == ancestor {
		return true
	}
	interior := strings.TrimSuffix(pattern, "/")
	if strings.Contains(interior, "/") {
		return false // anchored to a specific path, already checked above
	}
	trimmedAncestor := strings.TrimSuffix(ancestor, "/")
	segments := strings.Split(trimmedAncestor, "/")
	return segments[len(segments)-1]+"/" == pattern
}

// ancestorDirs returns rsync directory-match patterns (each ending in "/")
// for every parent directory of pattern, from shallowest to deepest. E.g.
// "frontend/tests/mocks/data/" yields ["frontend/", "frontend/tests/",
// "frontend/tests/mocks/"].
func ancestorDirs(pattern string) []string {
	// Anchoring ("/" prefix) doesn't change parent segmentation, only where
	// rsync starts matching from - strip it for splitting, the "/" prefix
	// on each emitted parent below re-adds equivalent anchoring behavior
	// via rsync's leading-slash rule.
	anchored := strings.HasPrefix(pattern, "/")
	trimmed := strings.TrimPrefix(pattern, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")

	segments := strings.Split(trimmed, "/")
	if len(segments) <= 1 {
		return nil
	}

	var parents []string
	var prefix string
	for _, seg := range segments[:len(segments)-1] {
		prefix += seg + "/"
		if anchored {
			parents = append(parents, "/"+prefix)
		} else {
			parents = append(parents, prefix)
		}
	}
	return parents
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

// InvalidateStaleDirectories checks each lockfile invalidation entry and deletes
// the corresponding remote directories when the local lockfile is newer than the
// remote directory. This handles the common case where a lockfile (bun.lock,
// package-lock.json, etc.) is updated locally but the remote install directory
// (node_modules/, .venv/, etc.) is stale and won't be re-installed because rsync
// preserves it.
//
// Skip silently if conn is nil, local, or invalidations is empty.
// InvalidationNotifyFunc is called when a stale directory is about to be removed.
// dir is the relative path (e.g. "node_modules/"), lockfile is the triggering lockfile.
// When nil, a plain fmt.Printf line is written to stdout.
type InvalidationNotifyFunc func(dir, lockfile string)

func InvalidateStaleDirectories(conn *host.Connection, localDir string, invalidations []config.LockfileInvalidation, notify InvalidationNotifyFunc) error {
	if conn == nil || conn.IsLocal || conn.Client == nil || len(invalidations) == 0 {
		return nil
	}

	remoteDir := config.ExpandRemote(conn.Host.Dir)

	for _, inv := range invalidations {
		if inv.Lockfile == "" || len(inv.Dirs) == 0 {
			continue
		}

		// Check if lockfile exists locally
		localLockfile := filepath.Join(localDir, inv.Lockfile)
		localInfo, err := os.Stat(localLockfile)
		if err != nil {
			// Lockfile doesn't exist locally - nothing to invalidate
			continue
		}
		localMtime := localInfo.ModTime().Unix()

		// For each directory this lockfile controls, check the remote mtime
		for _, dir := range inv.Dirs {
			remotePath := remoteDir + "/" + strings.TrimSuffix(dir, "/")

			// Get remote directory mtime. We try Linux stat first, then fall back
			// to macOS stat. If the directory doesn't exist, echo 0.
			statCmd := fmt.Sprintf(
				`d=%s; if [ -e "$d" ]; then stat -c "%%Y" "$d" 2>/dev/null || stat -f "%%m" "$d" 2>/dev/null || echo 0; else echo 0; fi`,
				util.ShellQuotePreserveTilde(remotePath),
			)

			stdout, _, _, execErr := conn.Client.Exec(statCmd)
			remoteMtimeStr := strings.TrimSpace(string(stdout))

			var remoteMtime int64
			if execErr != nil || remoteMtimeStr == "" {
				// On any error, assume stale - safer to delete than to leave stale
				remoteMtime = 0
			} else {
				parsed, parseErr := strconv.ParseInt(remoteMtimeStr, 10, 64)
				if parseErr != nil {
					remoteMtime = 0
				} else {
					remoteMtime = parsed
				}
			}

			// If local lockfile is newer than remote dir (or remote dir is missing),
			// delete the remote directory so the package manager reinstalls
			if localMtime > remoteMtime {
				if notify != nil {
					notify(dir, inv.Lockfile)
				} else {
					fmt.Printf("Invalidating stale %s (%s changed)\n", dir, inv.Lockfile)
				}

				rmCmd := fmt.Sprintf("rm -rf %s", util.ShellQuotePreserveTilde(remotePath))
				_, rmStderr, rmExit, rmErr := conn.Client.Exec(rmCmd)
				if rmErr != nil {
					return errors.WrapWithCode(rmErr, errors.ErrSync,
						fmt.Sprintf("Failed to delete stale remote directory %s", dir),
						"Check SSH connection and remote permissions.")
				}
				if rmExit != 0 {
					return errors.New(errors.ErrSync,
						fmt.Sprintf("Failed to delete stale remote directory %s", dir),
						fmt.Sprintf("Remote error: %s", strings.TrimSpace(string(rmStderr))))
				}
			}
		}
	}

	return nil
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
