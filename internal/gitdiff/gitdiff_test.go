package gitdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initGitRepo creates a temp dir with git init, an initial commit, and returns the path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")

	// Create initial commit so HEAD exists
	writeFile(t, dir, "README.md", "# test repo")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "initial commit")

	return dir
}

// commitFile creates or modifies a file and commits it.
func commitFile(t *testing.T, dir, path, content, msg string) {
	t.Helper()
	writeFile(t, dir, path, content)
	run(t, dir, "git", "add", path)
	run(t, dir, "git", "commit", "-m", msg)
}

// writeFile creates a file with the given content, creating parent dirs as needed.
func writeFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0644))
}

// createBranch creates and checks out a new branch.
func createBranch(t *testing.T, dir, name string) {
	t.Helper()
	run(t, dir, "git", "checkout", "-b", name)
}

// run executes a command and requires it to succeed.
func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
}

func TestDetect_ModifiedFiles(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "original", "add file")

	createBranch(t, dir, "feat")
	commitFile(t, dir, "file.txt", "modified", "modify file")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.Equal(t, "feat", result.Branch)
	assert.Contains(t, result.Files, "file.txt")
}

func TestDetect_StagedFiles(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "original", "add file")

	createBranch(t, dir, "feat")

	// Modify and stage but don't commit
	writeFile(t, dir, "file.txt", "staged change")
	run(t, dir, "git", "add", "file.txt")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "file.txt")
}

func TestDetect_UntrackedFiles(t *testing.T) {
	dir := initGitRepo(t)
	createBranch(t, dir, "feat")

	// Create an untracked file
	writeFile(t, dir, "new.txt", "untracked")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "new.txt")
}

func TestDetect_DeletedFiles(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "to be deleted", "add file")

	createBranch(t, dir, "feat")
	require.NoError(t, os.Remove(filepath.Join(dir, "file.txt")))

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	// Deleted file should be in Files for rsync --delete to handle
	assert.Contains(t, result.Files, "file.txt")
}

func TestDetect_CommittedBranchChanges(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.txt", "a", "add a")
	commitFile(t, dir, "b.txt", "b", "add b")
	commitFile(t, dir, "c.txt", "c", "add c")

	createBranch(t, dir, "feat")

	// Modify a, add d, delete c
	commitFile(t, dir, "a.txt", "modified a", "modify a")
	commitFile(t, dir, "d.txt", "new d", "add d")
	require.NoError(t, os.Remove(filepath.Join(dir, "c.txt")))
	run(t, dir, "git", "add", "c.txt")
	run(t, dir, "git", "commit", "-m", "delete c")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "a.txt")
	assert.Contains(t, result.Files, "d.txt")
	assert.Contains(t, result.Files, "c.txt")
	assert.NotContains(t, result.Files, "b.txt")
}

func TestDetect_OnBaseBranch_WorkingTreeOnly(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "original", "add file")

	// Stay on main, make uncommitted changes
	writeFile(t, dir, "file.txt", "modified")
	writeFile(t, dir, "new.txt", "untracked")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "file.txt")
	assert.Contains(t, result.Files, "new.txt")
	assert.Equal(t, "main", result.Branch)
}

func TestDetect_NoChanges(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "content", "add file")

	createBranch(t, dir, "feat")
	// No changes on feat branch

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Files)
}

func TestDetect_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()

	_, err := Detect(DetectOptions{
		WorkDir:    dir,
		ProjectDir: dir,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestDetect_NoCommits(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init")

	_, err := Detect(DetectOptions{
		WorkDir:    dir,
		ProjectDir: dir,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no commits")
}

func TestDetect_MonorepoProjectRoot(t *testing.T) {
	dir := initGitRepo(t)

	// Create files in two services
	commitFile(t, dir, "services/myapp/foo.go", "package main", "add myapp")
	commitFile(t, dir, "other/bar.go", "package other", "add other")

	createBranch(t, dir, "feat")
	commitFile(t, dir, "services/myapp/foo.go", "package main // modified", "modify myapp")
	commitFile(t, dir, "other/bar.go", "package other // modified", "modify other")

	// Detect with ProjectDir scoped to services/myapp
	projectDir := filepath.Join(dir, "services", "myapp")
	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: projectDir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "foo.go")
	assert.NotContains(t, result.Files, "bar.go")
	assert.NotContains(t, result.Files, "other/bar.go")
	assert.NotContains(t, result.Files, "services/myapp/foo.go")
}

func TestDetect_BaseBranchAutoDetect(t *testing.T) {
	// Create a bare repo to act as origin
	bareDir := t.TempDir()
	run(t, bareDir, "git", "init", "--bare", "-b", "main")

	// Clone it to get a working copy with origin
	dir := t.TempDir()
	run(t, dir, "git", "clone", bareDir, "repo")
	repoDir := filepath.Join(dir, "repo")
	run(t, repoDir, "git", "config", "user.email", "test@test.com")
	run(t, repoDir, "git", "config", "user.name", "Test")

	// Create initial commit and push
	writeFile(t, repoDir, "file.txt", "content")
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "initial")
	run(t, repoDir, "git", "push", "-u", "origin", "main")

	// Set the origin HEAD (simulating remote default branch)
	run(t, repoDir, "git", "remote", "set-head", "origin", "main")

	// Create a feature branch with changes
	createBranch(t, repoDir, "feat")
	commitFile(t, repoDir, "new.txt", "new", "add new")

	// Detect without explicit BaseBranch - should auto-detect main
	result, err := Detect(DetectOptions{
		WorkDir:    repoDir,
		ProjectDir: repoDir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "new.txt")
	assert.Equal(t, "feat", result.Branch)
}

func TestDetect_BaseBranchExplicitOverride(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "original", "add file")

	// Create develop branch
	createBranch(t, dir, "develop")
	commitFile(t, dir, "develop-only.txt", "develop", "develop change")

	// Create feat branch off develop
	createBranch(t, dir, "feat")
	commitFile(t, dir, "feat-only.txt", "feat", "feat change")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "develop",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "feat-only.txt")
	// develop-only.txt should NOT appear since we're diffing against develop
	assert.NotContains(t, result.Files, "develop-only.txt")
}

func TestDetect_BaseBranchOriginFallback(t *testing.T) {
	// Create a repo with origin/main but test resolveRef fallback
	dir := initGitRepo(t)

	// resolveRef should try bare name first, then origin/<name>
	// Test with a ref that exists locally
	ref, err := resolveRef(dir, "main")
	require.NoError(t, err)
	assert.Equal(t, "main", ref)

	// Test with a ref that doesn't exist at all
	_, err = resolveRef(dir, "nonexistent")
	assert.Error(t, err)
}

func TestDetect_DeduplicationAcrossSources(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "file.txt", "original", "add file")

	createBranch(t, dir, "feat")
	commitFile(t, dir, "file.txt", "committed change", "modify file")

	// Also modify the working tree (uncommitted)
	writeFile(t, dir, "file.txt", "working tree change")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)

	// file.txt appears in both branch diff AND working tree diff, but should be deduped
	count := 0
	for _, f := range result.Files {
		if f == "file.txt" {
			count++
		}
	}
	assert.Equal(t, 1, count, "file.txt should appear exactly once")
}

func TestDetect_RenamedFiles(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "old.txt", "content", "add old")

	createBranch(t, dir, "feat")
	run(t, dir, "git", "mv", "old.txt", "new.txt")
	run(t, dir, "git", "commit", "-m", "rename old to new")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	// Both old (for deletion on remote) and new (for creation) should appear
	assert.Contains(t, result.Files, "old.txt")
	assert.Contains(t, result.Files, "new.txt")
}

func TestDetect_SubmoduleUnderProjectDir(t *testing.T) {
	dir := initGitRepo(t)

	// Create a separate repo to use as a submodule
	subRepo := t.TempDir()
	run(t, subRepo, "git", "init", "-b", "main")
	run(t, subRepo, "git", "config", "user.email", "test@test.com")
	run(t, subRepo, "git", "config", "user.name", "Test")
	writeFile(t, subRepo, "sub.txt", "submodule content")
	run(t, subRepo, "git", "add", ".")
	run(t, subRepo, "git", "commit", "-m", "sub initial")

	// Add submodule under project dir (allow local file transport)
	run(t, dir, "git", "-c", "protocol.file.allow=always", "submodule", "add", subRepo, "submod")
	run(t, dir, "git", "commit", "-m", "add submodule")

	_, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "submodule")
}

func TestDetect_SubmoduleOutsideProjectDir(t *testing.T) {
	dir := initGitRepo(t)

	// Create subdirectory structure
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "services", "myapp"), 0755))
	commitFile(t, dir, "services/myapp/main.go", "package main", "add myapp")

	// Create a separate repo to use as a submodule
	subRepo := t.TempDir()
	run(t, subRepo, "git", "init", "-b", "main")
	run(t, subRepo, "git", "config", "user.email", "test@test.com")
	run(t, subRepo, "git", "config", "user.name", "Test")
	writeFile(t, subRepo, "sub.txt", "submodule content")
	run(t, subRepo, "git", "add", ".")
	run(t, subRepo, "git", "commit", "-m", "sub initial")

	// Add submodule OUTSIDE the project dir (allow local file transport)
	run(t, dir, "git", "-c", "protocol.file.allow=always", "submodule", "add", subRepo, "external/submod")
	run(t, dir, "git", "commit", "-m", "add submodule")

	createBranch(t, dir, "feat")
	commitFile(t, dir, "services/myapp/main.go", "package main // modified", "modify")

	// Detect scoped to services/myapp (submodule is in external/)
	projectDir := filepath.Join(dir, "services", "myapp")
	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: projectDir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "main.go")
}

func TestDetect_GitignoredFilesExcluded(t *testing.T) {
	dir := initGitRepo(t)

	// Add .gitignore
	commitFile(t, dir, ".gitignore", "*.log\n", "add gitignore")

	createBranch(t, dir, "feat")

	// Create a gitignored file
	writeFile(t, dir, "debug.log", "log content")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.NotContains(t, result.Files, "debug.log")
}

func TestDetect_NestedDirectoryChanges(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "src/pkg/deep/file.go", "package deep", "add deep file")

	createBranch(t, dir, "feat")
	commitFile(t, dir, "src/pkg/deep/file.go", "package deep // modified", "modify deep file")

	result, err := Detect(DetectOptions{
		WorkDir:    dir,
		BaseBranch: "main",
		ProjectDir: dir,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Files, "src/pkg/deep/file.go")
}

func TestDetectBaseBranch(t *testing.T) {
	dir := initGitRepo(t)
	// Without origin, should fall back to "main"
	branch := detectBaseBranch(dir)
	assert.Equal(t, "main", branch)
}

func TestComputePrefix(t *testing.T) {
	// Use real directories since computePrefix uses EvalSymlinks
	base := t.TempDir()
	sub := filepath.Join(base, "services", "myapp")
	require.NoError(t, os.MkdirAll(sub, 0755))

	t.Run("same directory", func(t *testing.T) {
		result, err := computePrefix(base, base)
		require.NoError(t, err)
		assert.Equal(t, "", result)
	})

	t.Run("subdirectory", func(t *testing.T) {
		result, err := computePrefix(base, sub)
		require.NoError(t, err)
		assert.Equal(t, "services/myapp/", result)
	})
}

func TestFilterPath(t *testing.T) {
	tests := []struct {
		name     string
		gitPath  string
		prefix   string
		wantPath string
		wantOK   bool
	}{
		{
			name:     "no prefix",
			gitPath:  "foo.go",
			prefix:   "",
			wantPath: "foo.go",
			wantOK:   true,
		},
		{
			name:     "matching prefix",
			gitPath:  "services/myapp/foo.go",
			prefix:   "services/myapp/",
			wantPath: "foo.go",
			wantOK:   true,
		},
		{
			name:     "non-matching prefix",
			gitPath:  "other/bar.go",
			prefix:   "services/myapp/",
			wantPath: "",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := filterPath(tt.gitPath, tt.prefix)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}
