// Package clean discovers and removes stale per-branch directories on remote hosts.
package clean

import (
	"fmt"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/util"
)

// RemoteExecutor runs commands on a remote host.
// Satisfied by sshutil.Client and test mocks.
type RemoteExecutor interface {
	Exec(cmd string) (stdout, stderr []byte, exitCode int, err error)
}

// StaleDir represents a remote directory for a branch that no longer exists locally.
type StaleDir struct {
	Path       string // Full remote path (e.g., ~/rr/myproject-feature-old)
	BranchName string // Extracted branch segment (e.g., "feature-old")
	DiskUsage  string // Human-readable size (e.g., "142M")
}

// Discover finds stale per-branch directories on a remote host.
// It lists directories matching the glob pattern derived from dirTemplate,
// extracts branch names, and compares against activeBranches.
// Returns nil if the template does not contain ${BRANCH}.
func Discover(executor RemoteExecutor, dirTemplate string, activeBranches []string) ([]StaleDir, error) {
	glob, hasBranch := config.ExpandRemoteGlob(dirTemplate)
	if !hasBranch {
		return nil, nil
	}

	dirs, err := listRemoteDirs(executor, glob)
	if err != nil {
		return nil, err
	}

	// Build active branch set for O(1) lookup
	activeSet := make(map[string]bool, len(activeBranches))
	for _, b := range activeBranches {
		activeSet[b] = true
	}

	var stale []StaleDir
	for _, dir := range dirs {
		branchName := config.ExtractBranchFromPath(dirTemplate, dir.path)
		if branchName == "" || activeSet[branchName] {
			continue
		}
		stale = append(stale, StaleDir{
			Path:       dir.path,
			BranchName: branchName,
			DiskUsage:  dir.diskUsage,
		})
	}
	return stale, nil
}

// Remove deletes the specified directories on the remote host.
// Each path is validated against dirTemplate to ensure it matches the expected
// pattern from ${BRANCH} expansion. This is an allowlist approach: only paths
// that demonstrably came from the template are eligible for deletion.
// Returns the paths that were successfully removed and any errors.
func Remove(executor RemoteExecutor, dirTemplate string, dirs []StaleDir) (removed []string, errs []error) {
	for _, dir := range dirs {
		if err := validateRemovalTarget(dir.Path, dirTemplate); err != nil {
			errs = append(errs, fmt.Errorf("refusing to delete %q: %s", dir.Path, err))
			continue
		}
		cmd := fmt.Sprintf("rm -rf %s", util.ShellQuotePreserveTilde(dir.Path))
		_, stderr, exitCode, err := executor.Exec(cmd)
		if err != nil || exitCode != 0 {
			errMsg := strings.TrimSpace(string(stderr))
			if err != nil {
				if errMsg != "" {
					errMsg = fmt.Sprintf("%v (stderr: %s)", err, errMsg)
				} else {
					errMsg = err.Error()
				}
			} else if errMsg == "" {
				errMsg = fmt.Sprintf("exit code %d", exitCode)
			}
			errs = append(errs, fmt.Errorf("failed to remove %s: %s", dir.Path, errMsg))
			continue
		}
		removed = append(removed, dir.Path)
	}
	return removed, errs
}

// validateRemovalTarget checks that a path is safe to delete by verifying it
// matches the expected pattern from the host's DirTemplate. This is an allowlist:
// instead of trying to enumerate dangerous paths, we only allow paths that
// provably came from the template's ${BRANCH} expansion.
//
// A path is valid for deletion when ALL of:
//  1. The dirTemplate contains ${BRANCH} (otherwise we shouldn't be deleting anything)
//  2. The path matches the template pattern (prefix/suffix from ExpandRemoteGlob)
//  3. The extracted branch segment is non-empty and doesn't contain path separators
//  4. The path has at least 3 components (defense-in-depth minimum depth)
func validateRemovalTarget(path, dirTemplate string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("empty path")
	}

	// The template must contain ${BRANCH} for any removal to make sense
	if !strings.Contains(dirTemplate, "${BRANCH}") {
		return fmt.Errorf("dir template has no ${BRANCH}")
	}

	// Extract the branch segment using the same logic Discover uses.
	// This proves the path matches the template's prefix and suffix.
	branch := config.ExtractBranchFromPath(dirTemplate, trimmed)
	if branch == "" {
		return fmt.Errorf("path does not match template pattern")
	}

	// The branch segment must not contain path separators (prevents traversal)
	if strings.ContainsAny(branch, "/\\") {
		return fmt.Errorf("extracted branch contains path separators")
	}

	// Defense-in-depth: require minimum path depth.
	// Even if everything above passes, short paths like "/" or "~" are never OK.
	// Count meaningful segments (split on / and ignore empty parts from leading /).
	// ~/rr/project-branch -> ["~", "rr", "project-branch"] = 3 segments
	segments := 0
	for _, seg := range strings.Split(trimmed, "/") {
		if seg != "" {
			segments++
		}
	}
	if segments < 3 {
		return fmt.Errorf("path too shallow (need at least 3 components, got %d)", segments)
	}

	return nil
}

type remoteDir struct {
	path      string
	diskUsage string
}

func listRemoteDirs(executor RemoteExecutor, glob string) ([]remoteDir, error) {
	// Use ls -d to list directories matching the glob.
	// The glob must not be fully quoted — the * needs to be interpreted by the shell.
	cmd := fmt.Sprintf("ls -d %s 2>/dev/null", shellQuoteGlob(glob))
	stdout, stderr, exitCode, err := executor.Exec(cmd)
	if err != nil {
		return nil, err
	}
	// ls -d returns exit 1 when glob matches nothing (stderr silenced by 2>/dev/null).
	// Treat non-zero exit with empty stdout+stderr as "no matches found".
	if exitCode != 0 {
		trimmedOut := strings.TrimSpace(string(stdout))
		trimmedErr := strings.TrimSpace(string(stderr))
		if trimmedOut == "" && trimmedErr == "" {
			return nil, nil
		}
		msg := trimmedErr
		if msg == "" {
			msg = fmt.Sprintf("ls exited with code %d", exitCode)
		}
		return nil, fmt.Errorf("remote directory listing failed: %s", msg)
	}

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	var dirs []remoteDir
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		usage := getDiskUsage(executor, line)
		dirs = append(dirs, remoteDir{path: line, diskUsage: usage})
	}
	return dirs, nil
}

func getDiskUsage(executor RemoteExecutor, path string) string {
	cmd := fmt.Sprintf("du -sh %s 2>/dev/null", util.ShellQuotePreserveTilde(path))
	stdout, _, _, err := executor.Exec(cmd)
	if err != nil {
		return "?"
	}
	output := strings.TrimSpace(string(stdout))
	if output == "" {
		return "?"
	}
	// du output format: "142M\t/path/to/dir"
	if parts := strings.SplitN(output, "\t", 2); parts[0] != "" {
		return parts[0]
	}
	return "?"
}

// shellQuoteGlob quotes a path for shell execution, preserving tilde (~) and glob (*).
// Only safe to use with output from config.ExpandRemoteGlob, where * comes exclusively
// from ${BRANCH} substitution. Project names, usernames, etc. cannot contain literal *.
func shellQuoteGlob(path string) string {
	// Strategy: split on * boundaries, quote each non-empty segment preserving tilde,
	// rejoin with * between every segment (including empty ones for leading/trailing *).
	segments := strings.Split(path, "*")
	for i, seg := range segments {
		if seg != "" {
			segments[i] = util.ShellQuotePreserveTilde(seg)
		}
	}
	return strings.Join(segments, "*")
}
