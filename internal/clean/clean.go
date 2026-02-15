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

// HostResult contains discovery results for a single host.
type HostResult struct {
	HostName  string
	StaleDirs []StaleDir
	Error     error // Non-nil if discovery failed for this host
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
// Dangerous paths (empty, root, home) are rejected.
// Returns the paths that were successfully removed and any errors.
func Remove(executor RemoteExecutor, dirs []StaleDir) (removed []string, errs []error) {
	for _, dir := range dirs {
		if isDangerousPath(dir.Path) {
			errs = append(errs, fmt.Errorf("refusing to delete dangerous path: %q", dir.Path))
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

// isDangerousPath rejects paths that should never be deleted.
func isDangerousPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "/" || trimmed == "~" || trimmed == "~/" {
		return true
	}
	return false
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
