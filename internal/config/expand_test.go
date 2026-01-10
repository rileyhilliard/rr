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
