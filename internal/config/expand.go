package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rileyhilliard/rr/internal/util"
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

// argsPlaceholderRe matches {{args}} (escaped literal), {args}, and
// {args:-default}. Defaults cannot contain '}' - the match stops at the
// first closing brace.
var argsPlaceholderRe = regexp.MustCompile(`\{\{args\}\}|\{args(:-([^}]*))?\}`)

// ExpandArgs substitutes {args} / {args:-default} placeholders in a task run
// string. Extra args are shell-quoted before substitution. With no args,
// {args} becomes empty and {args:-default} inserts the default verbatim
// (author-controlled, not quoted). {{args}} escapes to the literal {args}.
// The second return value reports whether any (unescaped) placeholder was
// found, so callers can decide between substitution and legacy append.
func ExpandArgs(run string, args []string) (string, bool) {
	if !strings.Contains(run, "{args") && !strings.Contains(run, "{{args}}") {
		return run, false
	}

	found := false
	quoted := util.ShellQuoteJoin(args)
	result := argsPlaceholderRe.ReplaceAllStringFunc(run, func(m string) string {
		if m == "{{args}}" {
			return "{args}"
		}
		found = true
		if len(args) > 0 {
			return quoted
		}
		sub := argsPlaceholderRe.FindStringSubmatch(m)
		if len(sub) > 2 {
			return sub[2] // default value (empty for bare {args})
		}
		return ""
	})
	return result, found
}

// ExpandHost expands variables in a Host configuration.
// Uses ExpandRemote for Dir since it's a remote path.
func ExpandHost(h Host) Host {
	h.Dir = ExpandRemote(h.Dir)
	return h
}

// WorktreeInfo describes the git worktree containing the current directory.
type WorktreeInfo struct {
	IsLinked bool   // true when this is a linked worktree, not the main checkout
	Name     string // sanitized worktree directory basename (linked only)
	TopLevel string // absolute path of the worktree root
}

// projectFacts caches the git-derived facts behind ${PROJECT}. Only the
// facts are cached, never the final name: the worktree-isolation flag is
// read per call because global config loads (and could expand strings)
// before the project config that carries sync.worktree_isolation is
// available.
type projectFacts struct {
	baseName string
	worktree WorktreeInfo
}

var (
	projectFactsOnce  sync.Once
	projectFactsValue projectFacts

	// worktreeIsolationDisabled: zero value (false) means isolation is ON,
	// so the default applies before any config is loaded.
	worktreeIsolationDisabled atomic.Bool
)

// SetWorktreeIsolation enables or disables per-worktree remote directories.
// Call after loading the project config; safe to call at any time because
// the final project name is computed per expansion.
func SetWorktreeIsolation(enabled bool) {
	worktreeIsolationDisabled.Store(!enabled)
}

// resetProjectCache clears cached git facts. Test hook only.
func resetProjectCache() {
	projectFactsOnce = sync.Once{}
	projectFactsValue = projectFacts{}
	worktreeIsolationDisabled.Store(false)
}

// DetectWorktree reports whether the current directory is inside a linked
// git worktree. Returns the zero value outside a git repository.
func DetectWorktree() WorktreeInfo {
	cmd := exec.Command("git", "rev-parse", "--git-dir", "--git-common-dir", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return WorktreeInfo{}
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		return WorktreeInfo{}
	}

	gitDir, err := filepath.Abs(strings.TrimSpace(lines[0]))
	if err != nil {
		return WorktreeInfo{}
	}
	commonDir, err := filepath.Abs(strings.TrimSpace(lines[1]))
	if err != nil {
		return WorktreeInfo{}
	}
	topLevel := strings.TrimSpace(lines[2])

	if gitDir == commonDir {
		// Main checkout: --git-dir and --git-common-dir are the same .git
		return WorktreeInfo{TopLevel: topLevel}
	}

	return WorktreeInfo{
		IsLinked: true,
		Name:     sanitizeWorktreeName(filepath.Base(topLevel)),
		TopLevel: topLevel,
	}
}

// sanitizeWorktreeName restricts a worktree basename to filesystem- and
// shell-safe characters for use in remote directory names.
func sanitizeWorktreeName(name string) string {
	var b strings.Builder
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
			b.WriteRune(c)
		default:
			b.WriteRune('-')
		}
	}
	const maxLen = 40
	s := b.String()
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// loadProjectFacts computes and caches the repo name and worktree info.
func loadProjectFacts() projectFacts {
	projectFactsOnce.Do(func() {
		facts := projectFacts{worktree: DetectWorktree()}

		// Try git repo name first
		if name := getGitRepoName(); name != "" {
			facts.baseName = name
		} else if cwd, err := os.Getwd(); err == nil {
			facts.baseName = filepath.Base(cwd)
		} else {
			facts.baseName = "project"
		}

		projectFactsValue = facts
	})
	return projectFactsValue
}

// getProject returns the project name for ${PROJECT} expansion.
// Priority: git repo name > directory name. In a linked git worktree with
// worktree isolation enabled (the default), the name is suffixed with the
// worktree basename ("repo@branch-dir") so each worktree syncs to its own
// remote directory instead of clobbering the main checkout's.
func getProject() string {
	facts := loadProjectFacts()
	if facts.worktree.IsLinked && !worktreeIsolationDisabled.Load() && facts.worktree.Name != "" {
		return facts.baseName + "@" + facts.worktree.Name
	}
	return facts.baseName
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
