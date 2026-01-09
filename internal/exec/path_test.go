package exec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComparePATHs(t *testing.T) {
	tests := []struct {
		name      string
		login     []string
		inter     []string
		wantLogin []string // LoginOnly
		wantInter []string // InterOnly
		wantComm  []string // Common
	}{
		{
			name:      "identical paths",
			login:     []string{"/usr/bin", "/usr/local/bin"},
			inter:     []string{"/usr/bin", "/usr/local/bin"},
			wantLogin: nil,
			wantInter: nil,
			wantComm:  []string{"/usr/bin", "/usr/local/bin"},
		},
		{
			name:      "interactive has extra paths",
			login:     []string{"/usr/bin"},
			inter:     []string{"/usr/bin", "/home/user/.local/bin", "/home/user/.cargo/bin"},
			wantLogin: nil,
			wantInter: []string{"/home/user/.local/bin", "/home/user/.cargo/bin"},
			wantComm:  []string{"/usr/bin"},
		},
		{
			name:      "login has extra paths",
			login:     []string{"/usr/bin", "/opt/special"},
			inter:     []string{"/usr/bin"},
			wantLogin: []string{"/opt/special"},
			wantInter: nil,
			wantComm:  []string{"/usr/bin"},
		},
		{
			name:      "completely different paths",
			login:     []string{"/login/only"},
			inter:     []string{"/inter/only"},
			wantLogin: []string{"/login/only"},
			wantInter: []string{"/inter/only"},
			wantComm:  nil,
		},
		{
			name:      "empty paths",
			login:     []string{},
			inter:     []string{},
			wantLogin: nil,
			wantInter: nil,
			wantComm:  nil,
		},
		{
			name:      "handles empty strings in path",
			login:     []string{"/usr/bin", "", "/usr/local/bin"},
			inter:     []string{"/usr/bin", "/usr/local/bin", ""},
			wantLogin: nil,
			wantInter: nil,
			wantComm:  []string{"/usr/bin", "/usr/local/bin"},
		},
		{
			name:      "duplicate paths deduplicated in common",
			login:     []string{"/usr/bin", "/usr/bin"},
			inter:     []string{"/usr/bin", "/usr/bin"},
			wantLogin: nil,
			wantInter: nil,
			wantComm:  []string{"/usr/bin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := ComparePATHs(tt.login, tt.inter)

			assert.Equal(t, tt.wantLogin, diff.LoginOnly, "LoginOnly mismatch")
			assert.Equal(t, tt.wantInter, diff.InterOnly, "InterOnly mismatch")
			assert.Equal(t, tt.wantComm, diff.Common, "Common mismatch")
		})
	}
}

func TestToHomeRelative(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "macOS home path",
			input: "/Users/rileyhilliard/.local/bin",
			want:  "$HOME/.local/bin",
		},
		{
			name:  "Linux home path",
			input: "/home/user/.cargo/bin",
			want:  "$HOME/.cargo/bin",
		},
		{
			name:  "root home path",
			input: "/root/.local/bin",
			want:  "$HOME/.local/bin",
		},
		{
			name:  "system path unchanged",
			input: "/usr/local/bin",
			want:  "/usr/local/bin",
		},
		{
			name:  "opt path unchanged",
			input: "/opt/homebrew/bin",
			want:  "/opt/homebrew/bin",
		},
		{
			name:  "macOS username only",
			input: "/Users/someone",
			want:  "$HOME",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toHomeRelative(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGenerateSetupSuggestion_InteractiveOnly(t *testing.T) {
	result := &PathProbeResult{
		Command:      "uv",
		FoundInLogin: false,
		FoundInInter: true,
		InterPath:    "/Users/rileyhilliard/.local/bin/uv",
	}

	suggestion := GenerateSetupSuggestion(result, "m4")

	assert.Contains(t, suggestion, "available in interactive shells")
	assert.Contains(t, suggestion, "setup_commands")
	assert.Contains(t, suggestion, "$HOME/.local/bin")
	assert.Contains(t, suggestion, "m4")
}

func TestGenerateSetupSuggestion_CommonPath(t *testing.T) {
	result := &PathProbeResult{
		Command:     "go",
		CommonPaths: []string{"/usr/local/go/bin/go"},
	}

	suggestion := GenerateSetupSuggestion(result, "server")

	assert.Contains(t, suggestion, "Found 'go' at /usr/local/go/bin/go")
	assert.Contains(t, suggestion, "setup_commands")
	assert.Contains(t, suggestion, "/usr/local/go/bin")
}

func TestGenerateSetupSuggestion_NotFound(t *testing.T) {
	result := &PathProbeResult{
		Command: "mycommand",
	}

	suggestion := GenerateSetupSuggestion(result, "remote")

	assert.Contains(t, suggestion, "wasn't found on the remote")
	assert.Contains(t, suggestion, "Install 'mycommand'")
	assert.Contains(t, suggestion, "setup_commands")
}

func TestGenerateSetupSuggestion_Nil(t *testing.T) {
	suggestion := GenerateSetupSuggestion(nil, "host")
	assert.Equal(t, "", suggestion)
}

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}

	assert.True(t, contains(slice, "a"))
	assert.True(t, contains(slice, "b"))
	assert.True(t, contains(slice, "c"))
	assert.False(t, contains(slice, "d"))
	assert.False(t, contains(nil, "a"))
	assert.False(t, contains([]string{}, "a"))
}
