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

	return result
}

// ExpandRemote replaces variables in a string intended for a remote host.
// Unlike Expand, this keeps ${HOME} and ~ as ~ so the remote shell expands them.
// Supported variables:
//   - ${PROJECT} - git repo name or directory name (from local context)
//   - ${USER}    - current username (from local context)
//   - ${HOME}    - expands to ~ (for remote shell to expand)
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
