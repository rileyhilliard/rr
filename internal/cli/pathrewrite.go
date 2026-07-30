package cli

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/ui"
)

// foreignPathPrefixes are the locations local user home directories live.
// Absolute paths under these prefixes that aren't inside the sync root (or
// the remote project dir) are almost always copy-paste mistakes from the
// local machine.
var foreignPathPrefixes = []string{"/Users/", "/home/"}

// RewriteLocalPaths replaces absolute references to localRoot in cmd with
// remoteDir so commands authored against the local checkout work on the
// remote mirror. Matching is boundary-aware: a match must sit at a token
// boundary and be followed by '/', whitespace, a quote, a shell separator,
// or the end of the string, so /Users/r/app never matches /Users/r/app2.
// The symlink-resolved form of localRoot is matched too (macOS /tmp and
// /var live behind symlinks). Returns the rewritten command and the number
// of replacements made.
func RewriteLocalPaths(cmd, localRoot, remoteDir string) (string, int) {
	if cmd == "" || localRoot == "" || remoteDir == "" {
		return cmd, 0
	}
	replacement, skipSingleQuoted := remoteReplacement(remoteDir)
	total := 0
	for _, root := range localRootForms(localRoot) {
		var n int
		cmd, n = replacePathPrefix(cmd, root, replacement, skipSingleQuoted)
		total += n
	}
	return cmd, total
}

// remoteReplacement returns the string substituted for localRoot and whether
// matches inside single quotes must be left alone. A remote dir like
// ~/rr/app only tilde-expands as a bare word at the start of a token -
// inside quotes or after '=' (e.g. --junitxml=~/rr/out.xml) the remote
// shell keeps the literal '~' and the command targets a directory literally
// named "~". $HOME expands in every context except single quotes, so
// tilde dirs are rewritten to their $HOME form and single-quoted matches
// are skipped rather than silently broken.
func remoteReplacement(remoteDir string) (string, bool) {
	if remoteDir == "~" || strings.HasPrefix(remoteDir, "~/") {
		return "$HOME" + remoteDir[1:], true
	}
	if strings.HasPrefix(remoteDir, "~") {
		// ~user form: no portable variable equivalent, so keep the tilde
		// but stay out of single quotes where it can't expand.
		return remoteDir, true
	}
	return remoteDir, false
}

// RewriteArgsToRelative converts args that reference absolute paths under
// localRoot into ./-relative form. Task execution cds into the project dir
// on whichever host runs the command, so relative paths resolve correctly
// everywhere - including hosts with different remote dirs. Returns the
// rewritten args and the number of replacements made.
func RewriteArgsToRelative(args []string, localRoot string) ([]string, int) {
	if len(args) == 0 || localRoot == "" {
		return args, 0
	}
	out := make([]string, len(args))
	total := 0
	for i, a := range args {
		r, n := RewriteLocalPaths(a, localRoot, ".")
		out[i] = r
		total += n
	}
	return out, total
}

// localRootForms returns localRoot plus its symlink-resolved form when they
// differ, both cleaned.
func localRootForms(localRoot string) []string {
	roots := []string{filepath.Clean(localRoot)}
	if resolved, err := filepath.EvalSymlinks(localRoot); err == nil {
		resolved = filepath.Clean(resolved)
		if resolved != roots[0] {
			roots = append(roots, resolved)
		}
	}
	return roots
}

// replacePathPrefix replaces boundary-delimited occurrences of prefix in s
// with replacement. When skipSingleQuoted is set, occurrences inside
// single-quoted regions are left untouched (the replacement needs shell
// expansion, which single quotes suppress).
func replacePathPrefix(s, prefix, replacement string, skipSingleQuoted bool) (string, int) {
	var b strings.Builder
	count := 0
	i := 0
	var qs quoteState
	for i < len(s) {
		j := strings.Index(s[i:], prefix)
		if j < 0 {
			break
		}
		j += i
		qs.advance(s[i:j])
		if isPathStart(s, j) && isPathEnd(s, j+len(prefix)) && (!skipSingleQuoted || !qs.inSingle) {
			b.WriteString(s[i:j])
			b.WriteString(replacement)
			count++
			qs.advance(s[j : j+len(prefix)])
			i = j + len(prefix)
		} else {
			b.WriteString(s[i : j+1])
			qs.advance(s[j : j+1])
			i = j + 1
		}
	}
	if count == 0 {
		return s, 0
	}
	b.WriteString(s[i:])
	return b.String(), count
}

// quoteState tracks shell quoting while scanning a command left to right.
type quoteState struct {
	inSingle, inDouble bool
}

// advance updates the state across segment seg.
func (q *quoteState) advance(seg string) {
	for i := 0; i < len(seg); i++ {
		switch {
		case seg[i] == '\\' && !q.inSingle:
			i++ // backslash escapes the next char outside single quotes
		case seg[i] == '\'' && !q.inDouble:
			q.inSingle = !q.inSingle
		case seg[i] == '"' && !q.inSingle:
			q.inDouble = !q.inDouble
		}
	}
}

// isPathStart reports whether position idx begins a fresh path token: the
// preceding character (if any) must not itself be part of a path.
func isPathStart(s string, idx int) bool {
	if idx == 0 {
		return true
	}
	c := s[idx-1]
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c == '.', c == '_', c == '-', c == '/', c == '~':
		return false
	}
	return true
}

// isPathEnd reports whether the character at idx (the byte after a matched
// prefix) terminates the path cleanly: a subpath, whitespace, quote, shell
// separator, or end of string.
func isPathEnd(s string, idx int) bool {
	if idx >= len(s) {
		return true
	}
	switch s[idx] {
	case '/', ' ', '\t', '\n', '\'', '"', '`', ';', ':', ',', ')', '|', '&', '<', '>':
		return true
	}
	return false
}

// FindForeignAbsPaths returns absolute paths in cmd that look like they
// belong to a local machine (under /Users/ or /home/) but sit neither under
// localRoot nor under the remote project dir. Paths under remoteDir are
// legitimate remote-targeting references, not mistakes - Linux remotes keep
// projects under /home/<user>/..., Mac remotes under /Users/....
func FindForeignAbsPaths(cmd, localRoot, remoteDir string) []string {
	var found []string
	seen := make(map[string]bool)

	exclusions := localRootForms(localRoot)
	if remoteDir != "" {
		exclusions = append(exclusions, filepath.Clean(remoteDir))
	}

	for i := 0; i < len(cmd); i++ {
		var plen int
		for _, p := range foreignPathPrefixes {
			if strings.HasPrefix(cmd[i:], p) {
				plen = len(p)
				break
			}
		}
		if plen == 0 || !isPathStart(cmd, i) {
			continue
		}

		end := i + plen
		for end < len(cmd) && !isTokenDelimiter(cmd[end]) {
			end++
		}
		token := cmd[i:end]
		i = end - 1

		if isUnderAny(token, exclusions) || seen[token] {
			continue
		}
		seen[token] = true
		found = append(found, token)
	}
	return found
}

// isTokenDelimiter reports whether c ends a path token during extraction.
func isTokenDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\'', '"', '`', ';', '|', '&', '<', '>', '(', ')':
		return true
	}
	return false
}

// isUnderAny reports whether path equals or sits under any of the roots.
func isUnderAny(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// leadingCdTarget returns the target of a leading "cd <path>" in cmd, with
// surrounding quotes stripped, or "" when the command doesn't start with cd.
func leadingCdTarget(cmd string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(cmd), "cd ")
	if !ok {
		return ""
	}
	rest = strings.TrimSpace(rest)
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case ' ', '\t', ';', '&', '|':
			rest = rest[:i]
			i = len(rest)
		}
	}
	return strings.Trim(rest, `'"`)
}

// checkForeignPaths warns about absolute paths that belong to neither the
// sync root nor the remote project dir. A command that starts by cd'ing
// into such a path is rejected outright when the directory exists on the
// local filesystem - proof the command was authored against this machine.
func checkForeignPaths(wf *WorkflowContext, cmd, remoteDir string) error {
	foreign := FindForeignAbsPaths(cmd, wf.WorkDir, remoteDir)
	if len(foreign) == 0 {
		return nil
	}

	if target := leadingCdTarget(cmd); target != "" && isUnderAny(target, foreign) {
		if st, err := os.Stat(target); err == nil && st.IsDir() {
			return errors.New(errors.ErrExec,
				fmt.Sprintf("Command cd's into '%s', which is a directory on this machine, not on %s", target, wf.Conn.Name),
				fmt.Sprintf("rr syncs %s to %s on the remote. Use a path under the project root, or pass --local to run here.", wf.WorkDir, remoteDir))
		}
	}

	msg := fmt.Sprintf("Command references path(s) outside the synced project: %s - these likely don't exist on %s",
		strings.Join(foreign, ", "), wf.Conn.Name)
	if PrettyMode() {
		ui.PrintWarning(msg)
	} else {
		WritePhaseEvent(PhaseEvent{
			Type:   "phase",
			Phase:  "exec",
			Status: "warn",
			Details: map[string]interface{}{
				"foreign_paths": foreign,
				"message":       msg,
			},
		})
	}
	return nil
}

// announcePathRewrites surfaces path rewrites in the active output mode.
func announcePathRewrites(n int, from, to string) {
	if PrettyMode() {
		muted := lipgloss.NewStyle().Foreground(ui.ColorMuted)
		fmt.Println(muted.Render(fmt.Sprintf("Rewrote %d local path reference(s): %s -> %s", n, from, to)))
		return
	}
	WritePhaseEvent(PhaseEvent{
		Type:   "phase",
		Phase:  "exec",
		Status: "info",
		Details: map[string]interface{}{
			"path_rewrites": n,
			"from":          from,
			"to":            to,
		},
	})
}

// reportPathRewrites announces rewrites and records them in the result
// envelope.
func reportPathRewrites(wf *WorkflowContext, n int, from, to string) {
	wf.AddResultDetail("path_rewrites", n)
	announcePathRewrites(n, from, to)
}

// buildFailureHint inspects a failed remote command's stderr for signs the
// failure came from treating the remote as a copy of the local machine, and
// returns an actionable hint (or "" when nothing matches).
func buildFailureHint(command, stderr, localRoot, remoteDir, host string) string {
	lower := strings.ToLower(stderr)

	if strings.Contains(lower, "not a git repository") {
		return fmt.Sprintf("%s:%s is a synced snapshot of your working tree, not a git clone - git commands won't work there. Run git commands locally instead.", host, remoteDir)
	}

	if strings.Contains(lower, "no such file or directory") &&
		(mentionsLocalPath(command, localRoot, remoteDir) || mentionsLocalPath(stderr, localRoot, remoteDir)) {
		return fmt.Sprintf("A path in this command exists on this machine but not on %s. rr syncs %s to %s:%s - use paths under the project root (or relative paths).", host, localRoot, host, remoteDir)
	}

	return ""
}

// missingPathPatterns extract the offending path from a runner's "not found"
// message. Ordered most specific first.
var missingPathPatterns = []*regexp.Regexp{
	// pytest: "ERROR: file or directory not found: tests/foo.py"
	regexp.MustCompile(`(?i)file or directory not found:\s*(\S+)`),
	// generic shell/tool: "cat: tests/foo.py: No such file or directory"
	regexp.MustCompile(`(?i)^[^:\n]*:\s*(\S+):\s*no such file or directory`),
	// bare: "no such file or directory: tests/foo.py"
	regexp.MustCompile(`(?i)no such file or directory:\s*(\S+)`),
}

// buildRelativePathHint explains a failure caused by a relative path that
// resolves from the directory the user typed the command in, but not from the
// directory rr actually ran it in.
//
// rr runs at the project root (or an explicit --cwd), so a relative path
// written elsewhere means something different. That asymmetry is checkable
// locally for free: both candidate paths are on this machine. Returns "" unless
// the path exists under invocationDir and not under runDir - a path missing
// from both is an ordinary typo, and inventing an offset explanation for it
// would mislead.
//
// invocationDir and runDir are project-root-relative ("" means the root).
func buildRelativePathHint(stderr, projectRoot, invocationDir, runDir string) string {
	invocationDir = normalizeOffset(invocationDir)
	runDir = normalizeOffset(runDir)
	if projectRoot == "" || invocationDir == runDir {
		return ""
	}

	rel := extractMissingRelPath(stderr)
	if rel == "" {
		return ""
	}

	resolve := func(base string) string {
		return filepath.Join(projectRoot, filepath.FromSlash(base), filepath.FromSlash(rel))
	}
	if _, err := os.Stat(resolve(invocationDir)); err != nil {
		return ""
	}
	if _, err := os.Stat(resolve(runDir)); err == nil {
		return ""
	}

	ranIn := "the project root"
	if runDir != "" {
		ranIn = runDir
	}
	from := "the project root"
	fix := fmt.Sprintf("drop --cwd %s", runDir)
	if invocationDir != "" {
		from = invocationDir
		fix = fmt.Sprintf("pass --cwd %s", invocationDir)
	}

	return fmt.Sprintf("'%s' exists relative to %s (where you ran rr) but not relative to %s, where rr executed the command. "+
		"Use '%s', or %s.",
		rel, from, ranIn, path.Join(invocationDir, rel), fix)
}

// normalizeOffset reduces a project-root-relative directory to a canonical
// form so "", ".", and "./sub" compare as expected against "sub".
func normalizeOffset(offset string) string {
	if offset == "" {
		return ""
	}
	cleaned := path.Clean(filepath.ToSlash(offset))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

// extractMissingRelPath pulls a relative path out of a "not found" message,
// or "" when none is found. Absolute paths are left to buildFailureHint, and
// only the first match is considered: stderr often mentions several paths
// (traceback frames, for one) and guessing among them invents failures.
func extractMissingRelPath(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		for _, re := range missingPathPatterns {
			m := re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			candidate := strings.Trim(m[1], `'"`)
			if candidate == "" || strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "~") {
				continue
			}
			// Strip pytest node-id selectors ("tests/foo.py::test_bar").
			if idx := strings.Index(candidate, "::"); idx > 0 {
				candidate = candidate[:idx]
			}
			return candidate
		}
	}
	return ""
}

// mentionsLocalPath reports whether text references the local sync root or
// any local-looking home-directory path that isn't under the remote dir.
func mentionsLocalPath(text, localRoot, remoteDir string) bool {
	for _, root := range localRootForms(localRoot) {
		if strings.Contains(text, root) {
			return true
		}
	}
	return len(FindForeignAbsPaths(text, localRoot, remoteDir)) > 0
}
