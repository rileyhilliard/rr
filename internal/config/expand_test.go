package config

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandRemote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "HOME expands to tilde",
			input:    "${HOME}/rr/project",
			expected: "~/rr/project",
		},
		{
			name:     "PROJECT expands",
			input:    "~/rr/${PROJECT}",
			expected: "~/rr/" + getProject(), // Uses current project
		},
		{
			name:     "USER expands",
			input:    "/home/${USER}/rr",
			expected: "/home/" + os.Getenv("USER") + "/rr",
		},
		{
			name:     "tilde unchanged",
			input:    "~/projects/app",
			expected: "~/projects/app",
		},
		{
			name:     "absolute path unchanged",
			input:    "/opt/app/data",
			expected: "/opt/app/data",
		},
		{
			name:     "multiple variables",
			input:    "${HOME}/rr/${PROJECT}",
			expected: "~/rr/" + getProject(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandRemote(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpand_vs_ExpandRemote(t *testing.T) {
	// Expand should use local HOME
	localHome, _ := os.UserHomeDir()
	expandResult := Expand("${HOME}/test")
	assert.Equal(t, localHome+"/test", expandResult)

	// ExpandRemote should use ~ for remote shell expansion
	expandRemoteResult := ExpandRemote("${HOME}/test")
	assert.Equal(t, "~/test", expandRemoteResult)
}

func TestSanitizeBranch(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "simple branch", input: "main", expected: "main"},
		{name: "slash separated", input: "feature/webhooks", expected: "feature-webhooks"},
		{name: "nested slashes", input: "feat/sub/branch", expected: "feat-sub-branch"},
		{name: "backslash", input: "branch\\name", expected: "branch-name"},
		{name: "colon", input: "fix:issue", expected: "fix-issue"},
		{name: "asterisk", input: "feat*wild", expected: "feat-wild"},
		{name: "question mark", input: "feat?maybe", expected: "feat-maybe"},
		{name: "angle brackets", input: "feat<v1>done", expected: "feat-v1-done"},
		{name: "pipe", input: "feat|alt", expected: "feat-alt"},
		{name: "double quotes", input: "feat\"quoted\"", expected: "feat-quoted-"},
		{name: "empty string", input: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeBranch(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpandRemoteGlob(t *testing.T) {
	project := getProject()

	tests := []struct {
		name       string
		input      string
		wantGlob   string
		wantBranch bool
	}{
		{
			name:       "empty string",
			input:      "",
			wantGlob:   "",
			wantBranch: false,
		},
		{
			name:       "no BRANCH variable",
			input:      "~/rr/${PROJECT}",
			wantGlob:   "~/rr/" + project,
			wantBranch: false,
		},
		{
			name:       "BRANCH becomes wildcard",
			input:      "~/rr/${PROJECT}-${BRANCH}",
			wantGlob:   "~/rr/" + project + "-*",
			wantBranch: true,
		},
		{
			name:       "HOME expands to tilde",
			input:      "${HOME}/rr/${PROJECT}-${BRANCH}",
			wantGlob:   "~/rr/" + project + "-*",
			wantBranch: true,
		},
		{
			name:       "BRANCH only",
			input:      "~/rr/${BRANCH}",
			wantGlob:   "~/rr/*",
			wantBranch: true,
		},
		{
			name:       "USER expands normally",
			input:      "/home/${USER}/rr/${BRANCH}",
			wantGlob:   "/home/" + os.Getenv("USER") + "/rr/*",
			wantBranch: true,
		},
		{
			name:       "no variables at all",
			input:      "~/rr/static-dir",
			wantGlob:   "~/rr/static-dir",
			wantBranch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			glob, hasBranch := ExpandRemoteGlob(tt.input)
			assert.Equal(t, tt.wantGlob, glob)
			assert.Equal(t, tt.wantBranch, hasBranch)
		})
	}
}

func TestExtractBranchFromPath(t *testing.T) {
	project := getProject()

	tests := []struct {
		name     string
		template string
		path     string
		expected string
	}{
		{
			name:     "simple branch suffix",
			template: "~/rr/${PROJECT}-${BRANCH}",
			path:     "~/rr/" + project + "-feat-auth",
			expected: "feat-auth",
		},
		{
			name:     "branch only in path",
			template: "~/rr/${BRANCH}",
			path:     "~/rr/main",
			expected: "main",
		},
		{
			name:     "branch with project prefix",
			template: "~/projects/${PROJECT}/${BRANCH}",
			path:     "~/projects/" + project + "/feature-webhooks",
			expected: "feature-webhooks",
		},
		{
			name:     "no branch in template",
			template: "~/rr/${PROJECT}",
			path:     "~/rr/" + project,
			expected: "",
		},
		{
			name:     "empty path",
			template: "~/rr/${PROJECT}-${BRANCH}",
			path:     "",
			expected: "",
		},
		{
			name:     "path does not match template prefix",
			template: "~/rr/${PROJECT}-${BRANCH}",
			path:     "~/other/" + project + "-main",
			expected: "",
		},
		{
			name:     "multi BRANCH returns empty",
			template: "~/rr/${BRANCH}/${BRANCH}",
			path:     "~/rr/main/main",
			expected: "",
		},
		{
			name:     "absolute path from ls -d with tilde template",
			template: "~/rr/${PROJECT}-${BRANCH}",
			path:     "/Users/someone/rr/" + project + "-feat-auth",
			expected: "feat-auth",
		},
		{
			name:     "absolute path with different home dir",
			template: "~/rr/${PROJECT}-${BRANCH}",
			path:     "/home/deploy/rr/" + project + "-main",
			expected: "main",
		},
		{
			name:     "absolute path that does not match relative portion",
			template: "~/rr/${PROJECT}-${BRANCH}",
			path:     "/Users/someone/other/" + project + "-main",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractBranchFromPath(tt.template, tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestListLocalBranches(t *testing.T) {
	// Skip if not in a git repo (e.g., tarball-based CI builds)
	if err := exec.Command("git", "rev-parse", "--git-dir").Run(); err != nil {
		t.Skip("not inside a git repository")
	}

	// This test runs in a real git repo, so it should find at least one branch.
	// The results should be sanitized (no slashes).
	branches, err := ListLocalBranches()
	require.NoError(t, err)
	assert.NotEmpty(t, branches, "should find at least one local branch")

	// All branches should be sanitized (no filesystem-unsafe chars)
	for _, b := range branches {
		assert.NotContains(t, b, "/", "branch names should be sanitized")
		assert.NotContains(t, b, "\\", "branch names should be sanitized")
		assert.NotContains(t, b, ":", "branch names should be sanitized")
	}
}

func TestSanitizeBranch_CollisionDetection(t *testing.T) {
	// Verify that branches differing only by sanitized characters produce
	// identical sanitized names (the known collision documented on branchSanitizer).
	assert.Equal(t, sanitizeBranch("feature/login"), sanitizeBranch("feature-login"),
		"slash and hyphen should produce the same sanitized output")
	assert.Equal(t, sanitizeBranch("fix\\bug"), sanitizeBranch("fix-bug"),
		"backslash and hyphen should produce the same sanitized output")

	// Verify branches with different meaningful content don't collide
	assert.NotEqual(t, sanitizeBranch("feature/login"), sanitizeBranch("feature/signup"))
}

func TestExpandRemote_Branch(t *testing.T) {
	branch := getBranch()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "BRANCH expands",
			input:    "~/rr/${BRANCH}",
			expected: "~/rr/" + branch,
		},
		{
			name:     "PROJECT and BRANCH combined",
			input:    "~/rr/${PROJECT}-${BRANCH}",
			expected: "~/rr/" + getProject() + "-" + branch,
		},
		{
			name:     "no BRANCH variable unchanged",
			input:    "~/rr/static-dir",
			expected: "~/rr/static-dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandRemote(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
