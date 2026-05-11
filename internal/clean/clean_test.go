package clean

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Standard template used across tests. Uses a literal project name
// (not ${PROJECT}) so tests don't depend on the test runner's git context.
const testTemplate = "~/rr/myproject-${BRANCH}"

// mockExecutor is a test double for RemoteExecutor.
type mockExecutor struct {
	// responses maps command prefixes to (stdout, stderr, exitCode, err).
	responses map[string]execResponse
}

type execResponse struct {
	stdout   []byte
	stderr   []byte
	exitCode int
	err      error
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{responses: make(map[string]execResponse)}
}

func (m *mockExecutor) onCommand(prefix string, stdout string, exitCode int) {
	m.responses[prefix] = execResponse{stdout: []byte(stdout), exitCode: exitCode}
}

func (m *mockExecutor) onCommandErr(prefix string, err error) {
	m.responses[prefix] = execResponse{err: err}
}

func (m *mockExecutor) Exec(cmd string) (stdout, stderr []byte, exitCode int, err error) {
	// Match longest prefix first to avoid non-deterministic behavior
	// when prefixes overlap (e.g., "rm" vs "rm -rf").
	bestPrefix := ""
	var bestResp execResponse
	for prefix, resp := range m.responses {
		if strings.HasPrefix(cmd, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			bestResp = resp
		}
	}
	if bestPrefix != "" {
		return bestResp.stdout, bestResp.stderr, bestResp.exitCode, bestResp.err
	}
	return nil, []byte("command not found"), 127, nil
}

func TestDiscover_NoBranchInTemplate(t *testing.T) {
	executor := newMockExecutor()
	result, err := Discover(executor, "~/rr/myproject", []string{"main"})
	assert.NoError(t, err)
	assert.Nil(t, result, "should return nil when template has no ${BRANCH}")
}

func TestDiscover_FindsStaleDirs(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("ls -d", "~/rr/myproject-main\n~/rr/myproject-feat-auth\n~/rr/myproject-old-experiment\n", 0)
	executor.onCommand("du -sh", "142M\t~/rr/myproject-old-experiment\n", 0)

	result, err := Discover(executor, testTemplate, []string{"main", "feat-auth"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "~/rr/myproject-old-experiment", result[0].Path)
	assert.Equal(t, "old-experiment", result[0].BranchName)
}

func TestDiscover_AllBranchesActive(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("ls -d", "~/rr/myproject-main\n~/rr/myproject-dev\n", 0)

	result, err := Discover(executor, testTemplate, []string{"main", "dev"})
	assert.NoError(t, err)
	assert.Empty(t, result, "should return empty when all branches are active")
}

func TestDiscover_NoDirsOnRemote(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("ls -d", "", 0)

	result, err := Discover(executor, testTemplate, []string{"main"})
	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestDiscover_RemoteCommandFails(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommandErr("ls -d", fmt.Errorf("connection lost"))

	_, err := Discover(executor, testTemplate, []string{"main"})
	assert.Error(t, err)
}

func TestDiscover_ProtectsCurrentBranch(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("ls -d", "~/rr/myproject-main\n~/rr/myproject-stale-one\n", 0)
	executor.onCommand("du -sh", "50M\t~/rr/myproject-stale-one\n", 0)

	result, err := Discover(executor, testTemplate, []string{"main"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "stale-one", result[0].BranchName, "main should not appear as stale")
}

func TestRemove_Success(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("rm -rf", "", 0)

	dirs := []StaleDir{
		{Path: "~/rr/myproject-old", BranchName: "old"},
		{Path: "~/rr/myproject-stale", BranchName: "stale"},
	}
	removed, errs := Remove(executor, testTemplate, dirs)
	assert.Len(t, removed, 2)
	assert.Empty(t, errs)
}

func TestRemove_PartialFailure(t *testing.T) {
	callCount := 0
	executor := &countingExecutor{
		execFunc: func(cmd string) ([]byte, []byte, int, error) {
			callCount++
			if callCount == 1 {
				return nil, nil, 0, nil // first succeeds
			}
			return nil, []byte("permission denied"), 1, nil // second fails
		},
	}

	dirs := []StaleDir{
		{Path: "~/rr/myproject-old", BranchName: "old"},
		{Path: "~/rr/myproject-stale", BranchName: "stale"},
	}
	removed, errs := Remove(executor, testTemplate, dirs)
	assert.Len(t, removed, 1)
	assert.Len(t, errs, 1)
}

func TestDiscover_RemoteLsNonZeroExit(t *testing.T) {
	executor := newMockExecutor()
	executor.responses["ls -d"] = execResponse{stderr: []byte("permission denied"), exitCode: 2}

	_, err := Discover(executor, testTemplate, []string{"main"})
	assert.Error(t, err, "non-zero exit from ls should be an error")
}

func TestDiscover_RemoteLsNoMatches(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("ls -d", "", 0)

	result, err := Discover(executor, testTemplate, []string{"main"})
	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestShellQuoteGlob(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tilde and glob preserved",
			input:    "~/rr/myproject-*",
			expected: "~/'rr/myproject-'*",
		},
		{
			name:     "no glob or tilde",
			input:    "/opt/projects/myapp",
			expected: "'/opt/projects/myapp'",
		},
		{
			name:     "tilde only",
			input:    "~/rr/static",
			expected: "~/'rr/static'",
		},
		{
			name:     "glob at end",
			input:    "/data/rr-*",
			expected: "'/data/rr-'*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shellQuoteGlob(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestValidateRemovalTarget exercises the allowlist safety checks.
// Rather than maintaining a denylist of dangerous paths, validation ensures
// every path provably matches the DirTemplate pattern before deletion.
func TestValidateRemovalTarget(t *testing.T) {
	template := "~/rr/myproject-${BRANCH}"

	tests := []struct {
		name    string
		path    string
		wantErr string // empty = valid
	}{
		// Valid paths that match the template (tilde prefix)
		{name: "valid branch dir", path: "~/rr/myproject-feat-auth"},
		{name: "valid simple branch", path: "~/rr/myproject-main"},
		{name: "valid long branch", path: "~/rr/myproject-fix-login-page-v2"},

		// Valid absolute paths (as returned by ls -d on remote)
		{name: "absolute path linux", path: "/home/deploy/rr/myproject-feat-auth"},
		{name: "absolute path macos", path: "/Users/someone/rr/myproject-main"},

		// Dangerous system paths - all rejected because they don't match the template
		{name: "empty path", path: "", wantErr: "empty path"},
		{name: "root", path: "/", wantErr: "does not match template"},
		{name: "home tilde", path: "~", wantErr: "does not match template"},
		{name: "home slash", path: "~/", wantErr: "does not match template"},
		{name: "/home", path: "/home", wantErr: "does not match template"},
		{name: "/tmp", path: "/tmp", wantErr: "does not match template"},
		{name: "/var", path: "/var", wantErr: "does not match template"},
		{name: "/etc", path: "/etc", wantErr: "does not match template"},
		{name: "/usr", path: "/usr", wantErr: "does not match template"},

		// Paths that look similar but don't match the template
		{name: "wrong prefix", path: "~/other/myproject-main", wantErr: "does not match template"},
		{name: "completely different", path: "/opt/important/data", wantErr: "does not match template"},

		// Shallow paths are caught by template mismatch before depth check
		{name: "shallow path", path: "~/x", wantErr: "does not match template"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRemovalTarget(tt.path, template)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateRemovalTarget_NoTemplate verifies that removal is blocked
// when the template doesn't contain ${BRANCH}.
func TestValidateRemovalTarget_NoTemplate(t *testing.T) {
	err := validateRemovalTarget("~/rr/myproject", "~/rr/myproject")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no ${BRANCH}")
}

// TestRemove_RejectsPathsNotMatchingTemplate verifies Remove refuses to delete
// paths that don't match the DirTemplate pattern, even if they appear in the
// StaleDir list (defense against programming errors in the caller).
func TestRemove_RejectsPathsNotMatchingTemplate(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("rm -rf", "", 0)

	dirs := []StaleDir{
		{Path: "", BranchName: "empty"},
		{Path: "/", BranchName: "root"},
		{Path: "~", BranchName: "home"},
		{Path: "~/", BranchName: "home-slash"},
		{Path: "/home", BranchName: "home-dir"},
		{Path: "/tmp", BranchName: "tmp-dir"},
		{Path: "/var/", BranchName: "var-dir"},
		{Path: "/etc", BranchName: "etc-dir"},
		{Path: "~/other/myproject-main", BranchName: "wrong-prefix"},
		{Path: "~/rr/myproject-valid", BranchName: "valid"},
	}
	removed, errs := Remove(executor, testTemplate, dirs)
	assert.Len(t, removed, 1, "only the path matching the template should be removed")
	assert.Equal(t, "~/rr/myproject-valid", removed[0])
	assert.Len(t, errs, 9, "all non-matching paths should produce errors")
}

// TestDiscover_ExpandedDirReturnsNil verifies that passing an already-expanded dir
// (without ${BRANCH}) to Discover correctly returns nil, while the raw template works.
func TestDiscover_ExpandedDirReturnsNil(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("ls -d", "~/rr/myproject-main\n~/rr/myproject-stale-one\n", 0)
	executor.onCommand("du -sh", "50M\t~/rr/myproject-stale-one\n", 0)

	expandedDir := "~/rr/myproject-main"
	result, err := Discover(executor, expandedDir, []string{"main"})
	assert.NoError(t, err)
	assert.Nil(t, result, "expanded dir (no ${BRANCH}) should return nil")

	rawTemplate := "~/rr/myproject-${BRANCH}"
	result, err = Discover(executor, rawTemplate, []string{"main"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "stale-one", result[0].BranchName)
}

// TestDiscover_LsNoMatchesExitCode1 verifies that ls returning exit code 1
// with empty output is treated as "no matches found" rather than an error.
func TestDiscover_LsNoMatchesExitCode1(t *testing.T) {
	executor := newMockExecutor()
	executor.responses["ls -d"] = execResponse{exitCode: 1}

	result, err := Discover(executor, testTemplate, []string{"main"})
	assert.NoError(t, err, "exit code 1 with empty output should not be an error")
	assert.Empty(t, result)
}

// TestDiscover_ZshNoMatchesFound verifies that zsh's "no matches found" error
// is treated as "no directories" rather than a hard error.
func TestDiscover_ZshNoMatchesFound(t *testing.T) {
	executor := newMockExecutor()
	executor.responses["ls -d"] = execResponse{
		stderr:   []byte("zsh:1: no matches found: ~/rr/myproject-*"),
		exitCode: 1,
	}

	result, err := Discover(executor, testTemplate, []string{"main"})
	assert.NoError(t, err, "zsh 'no matches found' should not be an error")
	assert.Empty(t, result)
}

func TestGetDiskUsage_EmptyOutput(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("du -sh", "", 0)

	result := getDiskUsage(executor, "~/rr/some-dir")
	assert.Equal(t, "?", result, "empty du output should return ?")
}

func TestGetDiskUsage_ValidOutput(t *testing.T) {
	executor := newMockExecutor()
	executor.onCommand("du -sh", "142M\t~/rr/some-dir\n", 0)

	result := getDiskUsage(executor, "~/rr/some-dir")
	assert.Equal(t, "142M", result)
}

func TestRemove_CombinesErrAndStderr(t *testing.T) {
	executor := &countingExecutor{
		execFunc: func(cmd string) ([]byte, []byte, int, error) {
			return nil, []byte("disk full"), 1, fmt.Errorf("ssh timeout")
		},
	}
	dirs := []StaleDir{{Path: "~/rr/myproject-old", BranchName: "old"}}
	_, errs := Remove(executor, testTemplate, dirs)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "ssh timeout")
	assert.Contains(t, errs[0].Error(), "disk full")
}

// countingExecutor lets tests control per-call behavior.
type countingExecutor struct {
	execFunc func(cmd string) ([]byte, []byte, int, error)
}

func (c *countingExecutor) Exec(cmd string) (stdout, stderr []byte, exitCode int, err error) {
	return c.execFunc(cmd)
}
