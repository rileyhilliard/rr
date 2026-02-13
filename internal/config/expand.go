package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExpandTilde replaces ~ or ~/path with the user's home directory.
// Does not support ~username syntax - just ~ for the current user.
// Use this for LOCAL paths only. Remote paths should keep ~ for the remote shell.
func ExpandTilde(path string) string {
	if path == "" {
		return path
	}

	// Handle ~/path
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path // Return unchanged if we can't get home
		}
		return filepath.Join(home, path[2:])
	}

	// Handle standalone ~
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}

	return path
}

// Expand replaces variables in a string with their values.
// Supported variables:
//   - ${PROJECT} - git repo name or directory name
//   - ${USER}    - current username
//   - ${HOME}    - user's home directory (LOCAL - use ExpandRemote for remote paths)
//   - ${BRANCH}  - current git branch (sanitized for filesystem safety)
//
// Note: Does NOT expand ~ - use ExpandTilde for local paths if needed.
// For remote paths (like host.Dir), use ExpandRemote instead.
func Expand(s string) string {
	if s == "" {
		return s
	}

	// Get values lazily to avoid unnecessary work
	result := s

	if strings.Contains(result, "${PROJECT}") {
		result = strings.ReplaceAll(result, "${PROJECT}", getProject())
	}

	if strings.Contains(result, "${USER}") {
		result = strings.ReplaceAll(result, "${USER}", getUser())
	}

	if strings.Contains(result, "${HOME}") {
		result = strings.ReplaceAll(result, "${HOME}", getHome())
	}

	if strings.Contains(result, "${BRANCH}") {
		result = strings.ReplaceAll(result, "${BRANCH}", getBranch())
	}

	return result
}

// ExpandRemote replaces variables in a string intended for a remote host.
// Unlike Expand, this keeps ${HOME} and ~ as ~ so the remote shell expands them.
// Supported variables:
//   - ${PROJECT} - git repo name or directory name (from local context)
//   - ${USER}    - current username (from local context)
//   - ${HOME}    - expands to ~ (for remote shell to expand)
//   - ${BRANCH}  - current git branch, sanitized for filesystem safety (from local context)
//   - ~          - kept as ~ (for remote shell to expand)
func ExpandRemote(s string) string {
	if s == "" {
		return s
	}

	result := s

	if strings.Contains(result, "${PROJECT}") {
		result = strings.ReplaceAll(result, "${PROJECT}", getProject())
	}

	if strings.Contains(result, "${USER}") {
		result = strings.ReplaceAll(result, "${USER}", getUser())
	}

	// For remote paths, ${HOME} becomes ~ so the remote shell expands it
	if strings.Contains(result, "${HOME}") {
		result = strings.ReplaceAll(result, "${HOME}", "~")
	}

	if strings.Contains(result, "${BRANCH}") {
		result = strings.ReplaceAll(result, "${BRANCH}", getBranch())
	}

	return result
}

// ExpandHost expands variables in a Host configuration.
// Uses ExpandRemote for Dir since it's a remote path.
func ExpandHost(h Host) Host {
	h.Dir = ExpandRemote(h.Dir)
	return h
}

// getProject returns the project name for ${PROJECT} expansion.
// Priority: git repo name > directory name.
func getProject() string {
	// Try git repo name first
	if name := getGitRepoName(); name != "" {
		return name
	}

	// Fallback to current directory name
	cwd, err := os.Getwd()
	if err != nil {
		return "project"
	}
	return filepath.Base(cwd)
}

// getGitRepoName extracts the repository name from git remote origin.
func getGitRepoName() string {
	// Try to get remote URL
	cmd := exec.Command("git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		// No git remote, try to get repo root directory name
		cmd = exec.Command("git", "rev-parse", "--show-toplevel")
		out, err = cmd.Output()
		if err != nil {
			return ""
		}
		return filepath.Base(strings.TrimSpace(string(out)))
	}

	url := strings.TrimSpace(string(out))
	return extractRepoName(url)
}

// extractRepoName parses repo name from various git URL formats.
func extractRepoName(url string) string {
	// Handle SSH URLs: git@github.com:user/repo.git
	if strings.Contains(url, ":") && !strings.Contains(url, "://") {
		parts := strings.Split(url, ":")
		if len(parts) == 2 {
			path := parts[1]
			return strings.TrimSuffix(filepath.Base(path), ".git")
		}
	}

	// Handle HTTPS URLs: https://github.com/user/repo.git
	// and other URL formats
	name := filepath.Base(url)
	return strings.TrimSuffix(name, ".git")
}

// getUser returns the current username for ${USER} expansion.
func getUser() string {
	// Try USER env var first (most common)
	if user := os.Getenv("USER"); user != "" {
		return user
	}

	// Try LOGNAME (POSIX standard)
	if user := os.Getenv("LOGNAME"); user != "" {
		return user
	}

	// Try USERNAME (Windows)
	if user := os.Getenv("USERNAME"); user != "" {
		return user
	}

	// Last resort: whoami command
	cmd := exec.Command("whoami")
	out, err := cmd.Output()
	if err != nil {
		return "user"
	}
	return strings.TrimSpace(string(out))
}

// getHome returns the home directory for ${HOME} expansion.
func getHome() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}

	// Fallback to HOME env var
	if home := os.Getenv("HOME"); home != "" {
		return home
	}

	return "~"
}

// getBranch returns the current git branch name, sanitized for filesystem safety.
// Falls back to "HEAD" when in detached HEAD state or outside a git repo.
func getBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "HEAD"
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "HEAD"
	}
	return sanitizeBranch(branch)
}

// ExpandRemoteGlob expands variables in a remote path template, but replaces
// ${BRANCH} with a shell glob wildcard (*) instead of the current branch.
// Returns the glob pattern and whether ${BRANCH} was present in the template.
func ExpandRemoteGlob(s string) (pattern string, hasBranch bool) {
	if s == "" {
		return s, false
	}

	hasBranch = strings.Contains(s, "${BRANCH}")
	result := s

	if strings.Contains(result, "${PROJECT}") {
		result = strings.ReplaceAll(result, "${PROJECT}", getProject())
	}

	if strings.Contains(result, "${USER}") {
		result = strings.ReplaceAll(result, "${USER}", getUser())
	}

	// For remote paths, ${HOME} becomes ~ so the remote shell expands it
	if strings.Contains(result, "${HOME}") {
		result = strings.ReplaceAll(result, "${HOME}", "~")
	}

	if hasBranch {
		result = strings.ReplaceAll(result, "${BRANCH}", "*")
	}

	return result, hasBranch
}

// ExtractBranchFromPath extracts the branch segment from a fully-expanded
// remote directory path, given the original dir template.
// Returns empty string if the template has no ${BRANCH}, has multiple ${BRANCH}
// occurrences, or the path doesn't match.
func ExtractBranchFromPath(template, expandedPath string) string {
	if expandedPath == "" {
		return ""
	}

	glob, hasBranch := ExpandRemoteGlob(template)
	if !hasBranch {
		return ""
	}

	// Only support templates with exactly one ${BRANCH} (one wildcard in glob)
	if strings.Count(glob, "*") != 1 {
		return ""
	}

	// Split on the wildcard to get prefix and suffix
	parts := strings.SplitN(glob, "*", 2)
	if len(parts) != 2 {
		return ""
	}
	prefix := parts[0]
	suffix := parts[1]

	// Verify the path matches the prefix and suffix
	if !strings.HasPrefix(expandedPath, prefix) {
		return ""
	}
	if suffix != "" && !strings.HasSuffix(expandedPath, suffix) {
		return ""
	}

	// Extract the branch segment
	result := expandedPath[len(prefix):]
	if suffix != "" {
		result = result[:len(result)-len(suffix)]
	}
	return result
}

// ListLocalBranches returns the sanitized names of all local git branches.
// Each branch name is passed through sanitizeBranch to match the format
// used in directory names created by ${BRANCH} expansion.
func ListLocalBranches() ([]string, error) {
	cmd := exec.Command("git", "branch", "--format=%(refname:short)")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, sanitizeBranch(line))
		}
	}
	return branches, nil
}

// sanitizeBranch replaces characters unsafe for filesystems with hyphens.
func sanitizeBranch(branch string) string {
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
	)
	return replacer.Replace(branch)
}
