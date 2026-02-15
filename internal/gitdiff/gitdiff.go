package gitdiff

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DetectOptions configures how git change detection works.
type DetectOptions struct {
	// WorkDir is the directory to run git commands in.
	WorkDir string

	// BaseBranch is an explicit base branch override. If empty, auto-detected.
	BaseBranch string

	// ProjectDir is the project root, which may differ from the git root
	// in monorepo setups. Git paths are filtered and made relative to this.
	ProjectDir string
}

// ChangedFiles contains the result of git change detection.
type ChangedFiles struct {
	// Files contains all changed/added/deleted paths, relative to ProjectDir.
	Files []string

	// Branch is the current branch name (for sync state tracking).
	Branch string
}

// Detect runs git commands to find all files that differ between the current
// state and the base branch. Returns an error on any git failure, which should
// trigger a full sync fallback.
func Detect(opts DetectOptions) (*ChangedFiles, error) {
	// Verify git is installed
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git not found: %w", err)
	}

	// Verify we're in a git repo and get the root
	gitRoot, err := gitOutput(opts.WorkDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}

	// Verify there's at least one commit
	if _, err := gitOutput(opts.WorkDir, "rev-parse", "HEAD"); err != nil {
		return nil, fmt.Errorf("no commits in repository: %w", err)
	}

	// Get current branch
	branch, err := gitOutput(opts.WorkDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("getting current branch: %w", err)
	}

	// Check for submodules under ProjectDir
	if err := checkSubmodules(opts.WorkDir, gitRoot, opts.ProjectDir); err != nil {
		return nil, err
	}

	// Resolve the base branch
	baseBranch := opts.BaseBranch
	if baseBranch == "" {
		baseBranch = detectBaseBranch(opts.WorkDir)
	}

	// Resolve the base branch ref to make sure it exists
	baseRef, err := resolveRef(opts.WorkDir, baseBranch)
	if err != nil {
		return nil, fmt.Errorf("base branch %q not found: %w", baseBranch, err)
	}

	fileSet := make(map[string]struct{})
	deletedSet := make(map[string]struct{})

	// Only diff against base branch if we're NOT on the base branch
	onBaseBranch := branch == baseBranch || branch == stripOriginPrefix(baseBranch)
	if !onBaseBranch {
		// Committed changes on this branch (three-dot = from merge base).
		// --no-renames reports both old and new paths for renamed files,
		// so rsync can delete the old path and create the new one.
		branchFiles, err := gitLines(opts.WorkDir, "diff", "--name-only", "--no-renames", baseRef+"...HEAD")
		if err != nil {
			return nil, fmt.Errorf("git diff against base branch: %w", err)
		}
		for _, f := range branchFiles {
			fileSet[f] = struct{}{}
		}
	}

	// Working tree changes (staged + unstaged combined)
	workingFiles, err := gitLines(opts.WorkDir, "diff", "--name-only", "--no-renames", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git diff working tree: %w", err)
	}
	for _, f := range workingFiles {
		fileSet[f] = struct{}{}
	}

	// Untracked files
	untrackedFiles, err := gitLines(opts.WorkDir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files untracked: %w", err)
	}
	for _, f := range untrackedFiles {
		fileSet[f] = struct{}{}
	}

	// Deleted files
	deletedFiles, err := gitLines(opts.WorkDir, "ls-files", "--deleted")
	if err != nil {
		return nil, fmt.Errorf("git ls-files deleted: %w", err)
	}
	for _, f := range deletedFiles {
		deletedSet[f] = struct{}{}
		// Also add to fileSet so rsync knows about them (--delete removes on remote)
		fileSet[f] = struct{}{}
	}

	// Compute relative path prefix for monorepo filtering
	projectDir := opts.ProjectDir
	if projectDir == "" {
		projectDir = opts.WorkDir
	}
	prefix, err := computePrefix(gitRoot, projectDir)
	if err != nil {
		return nil, fmt.Errorf("computing project prefix: %w", err)
	}

	// Filter and relativize paths
	var files []string
	for f := range fileSet {
		rel, ok := filterPath(f, prefix)
		if ok {
			files = append(files, rel)
		}
	}

	// Sort for deterministic output
	sort.Strings(files)

	return &ChangedFiles{
		Files:  files,
		Branch: branch,
	}, nil
}

// detectBaseBranch auto-detects the default branch.
// Tries: git symbolic-ref refs/remotes/origin/HEAD -> origin/main -> main
func detectBaseBranch(workDir string) string {
	// Try symbolic-ref first (most reliable)
	ref, err := gitOutput(workDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		// ref looks like "refs/remotes/origin/main" -> extract "main"
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}

	// Fallback: try origin/main, then main
	if _, err := gitOutput(workDir, "rev-parse", "--verify", "origin/main"); err == nil {
		return "main"
	}

	// Last resort
	return "main"
}

// resolveRef verifies a ref exists in the repo. Tries the bare name first,
// then origin/<name> if bare doesn't exist.
func resolveRef(workDir, ref string) (string, error) {
	// Try the ref as-is
	if _, err := gitOutput(workDir, "rev-parse", "--verify", ref); err == nil {
		return ref, nil
	}

	// Try with origin/ prefix
	originRef := "origin/" + ref
	if _, err := gitOutput(workDir, "rev-parse", "--verify", originRef); err == nil {
		return originRef, nil
	}

	return "", fmt.Errorf("ref %q does not exist (tried %q and %q)", ref, ref, originRef)
}

// checkSubmodules returns an error if any submodule is under ProjectDir.
func checkSubmodules(workDir, gitRoot, projectDir string) error {
	// Check if .gitmodules exists
	modulesPath := filepath.Join(gitRoot, ".gitmodules")
	lines, err := gitLines(workDir, "config", "--file", modulesPath, "--get-regexp", "submodule\\..*\\.path")
	if err != nil {
		// No .gitmodules or no submodules - that's fine
		return nil
	}

	prefix, err := computePrefix(gitRoot, projectDir)
	if err != nil {
		return nil // Can't compute prefix, skip check
	}

	for _, line := range lines {
		// Lines look like: submodule.foo.path some/path
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		subPath := parts[1]

		// Check if submodule is under our project dir
		if prefix == "" {
			// ProjectDir == git root, any submodule is "under" it
			return fmt.Errorf("submodule at %q is under project directory; git-aware sync can't track submodule contents", subPath)
		}
		if strings.HasPrefix(subPath, prefix) || strings.HasPrefix(prefix, subPath+"/") {
			return fmt.Errorf("submodule at %q overlaps with project directory; git-aware sync can't track submodule contents", subPath)
		}
	}

	return nil
}

// computePrefix returns the relative path from gitRoot to projectDir.
// Returns "" if they're the same directory.
// Uses EvalSymlinks to resolve symlinks (e.g., macOS /tmp -> /private/tmp).
func computePrefix(gitRoot, projectDir string) (string, error) {
	absGitRoot, err := filepath.EvalSymlinks(gitRoot)
	if err != nil {
		return "", err
	}
	absProjectDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		return "", err
	}

	if absGitRoot == absProjectDir {
		return "", nil
	}

	rel, err := filepath.Rel(absGitRoot, absProjectDir)
	if err != nil {
		return "", err
	}

	// Ensure trailing slash for prefix matching
	if !strings.HasSuffix(rel, "/") {
		rel += "/"
	}

	return rel, nil
}

// filterPath checks if a git-relative path is under the project prefix,
// and returns the path relative to the project dir.
func filterPath(gitPath, prefix string) (string, bool) {
	if prefix == "" {
		return gitPath, true
	}
	if strings.HasPrefix(gitPath, prefix) {
		return strings.TrimPrefix(gitPath, prefix), true
	}
	return "", false
}

// stripOriginPrefix removes "origin/" from the front of a ref string.
func stripOriginPrefix(ref string) string {
	return strings.TrimPrefix(ref, "origin/")
}

// gitOutput runs a git command and returns trimmed stdout.
func gitOutput(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitLines runs a git command and returns non-empty lines from stdout.
func gitLines(workDir string, args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
