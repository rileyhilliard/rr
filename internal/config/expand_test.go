package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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
