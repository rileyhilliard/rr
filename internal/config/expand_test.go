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

func TestExpandArgs(t *testing.T) {
	tests := []struct {
		name      string
		run       string
		args      []string
		expected  string
		hasHolder bool
	}{
		{
			name:      "no placeholder no args",
			run:       "pytest tests/",
			args:      nil,
			expected:  "pytest tests/",
			hasHolder: false,
		},
		{
			name:      "no placeholder with args returns unchanged",
			run:       "pytest tests/",
			args:      []string{"-k", "foo"},
			expected:  "pytest tests/",
			hasHolder: false,
		},
		{
			name:      "placeholder mid pipeline",
			run:       "pytest {args} -n 4 | grep -v PASS",
			args:      []string{"tests/foo.py"},
			expected:  "pytest 'tests/foo.py' -n 4 | grep -v PASS",
			hasHolder: true,
		},
		{
			name:      "bare placeholder empty args",
			run:       "pytest {args} -n 4",
			args:      nil,
			expected:  "pytest  -n 4",
			hasHolder: true,
		},
		{
			name:      "default used when no args",
			run:       "pytest {args:-.} -n 4",
			args:      nil,
			expected:  "pytest . -n 4",
			hasHolder: true,
		},
		{
			name:      "default overridden by args",
			run:       "pytest {args:-.} -n 4",
			args:      []string{"tests/a.py", "tests/b.py"},
			expected:  "pytest 'tests/a.py' 'tests/b.py' -n 4",
			hasHolder: true,
		},
		{
			name:      "args are shell quoted",
			run:       "pytest {args}",
			args:      []string{"-k", "a b", "it's"},
			expected:  `pytest '-k' 'a b' 'it'\''s'`,
			hasHolder: true,
		},
		{
			name:      "multiple placeholders",
			run:       "echo {args} && pytest {args}",
			args:      []string{"x"},
			expected:  "echo 'x' && pytest 'x'",
			hasHolder: true,
		},
		{
			name:      "escaped placeholder stays literal",
			run:       "jq '{{args}}' file.json",
			args:      []string{"ignored"},
			expected:  "jq '{args}' file.json",
			hasHolder: false,
		},
		{
			name:      "empty default",
			run:       "pytest {args:-}",
			args:      nil,
			expected:  "pytest ",
			hasHolder: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := ExpandArgs(tt.run, tt.args)
			assert.Equal(t, tt.expected, got)
			assert.Equal(t, tt.hasHolder, found)
		})
	}
}
