package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddSetupCommand(t *testing.T) {
	tests := []struct {
		name         string
		initialYAML  string
		hostName     string
		setupCommand string
		wantContains []string
		wantErr      bool
	}{
		{
			name: "add to host without setup_commands",
			initialYAML: `version: 1
hosts:
  myhost:
    ssh:
      - myhost.local
    dir: /home/user/project
`,
			hostName:     "myhost",
			setupCommand: "export PATH=$HOME/go/bin:$PATH",
			wantContains: []string{
				"setup_commands:",
				"export PATH=$HOME/go/bin:$PATH",
			},
		},
		{
			name: "add to host with existing setup_commands",
			initialYAML: `version: 1
hosts:
  myhost:
    ssh:
      - myhost.local
    dir: /home/user/project
    setup_commands:
      - export EXISTING=1
`,
			hostName:     "myhost",
			setupCommand: "export PATH=$HOME/go/bin:$PATH",
			wantContains: []string{
				"export EXISTING=1",
				"export PATH=$HOME/go/bin:$PATH",
			},
		},
		{
			name: "skip if command already exists",
			initialYAML: `version: 1
hosts:
  myhost:
    ssh:
      - myhost.local
    setup_commands:
      - export PATH=$HOME/go/bin:$PATH
`,
			hostName:     "myhost",
			setupCommand: "export PATH=$HOME/go/bin:$PATH",
			wantContains: []string{
				"export PATH=$HOME/go/bin:$PATH",
			},
		},
		{
			name: "error if host not found",
			initialYAML: `version: 1
hosts:
  otherhost:
    ssh:
      - other.local
`,
			hostName:     "myhost",
			setupCommand: "export PATH=$HOME/go/bin:$PATH",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, ".rr.yaml")
			err := os.WriteFile(configPath, []byte(tt.initialYAML), 0644)
			require.NoError(t, err)

			// Run the function
			err = AddSetupCommand(configPath, tt.hostName, tt.setupCommand)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Read back the file
			content, err := os.ReadFile(configPath)
			require.NoError(t, err)

			// Check expected content
			for _, want := range tt.wantContains {
				assert.Contains(t, string(content), want)
			}
		})
	}
}

func TestAddSetupCommand_DoesNotDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".rr.yaml")

	initialYAML := `version: 1
hosts:
  myhost:
    ssh:
      - myhost.local
    dir: /home/user/project
`
	err := os.WriteFile(configPath, []byte(initialYAML), 0644)
	require.NoError(t, err)

	// Add command twice
	err = AddSetupCommand(configPath, "myhost", "export PATH=$HOME/go/bin:$PATH")
	require.NoError(t, err)

	err = AddSetupCommand(configPath, "myhost", "export PATH=$HOME/go/bin:$PATH")
	require.NoError(t, err)

	// Read back and verify only one instance
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)

	count := strings.Count(string(content), "export PATH=$HOME/go/bin:$PATH")
	assert.Equal(t, 1, count, "command should appear exactly once")
}

func TestGeneratePATHExportCommand(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{
			name:  "single path",
			paths: []string{"$HOME/go/bin"},
			want:  "export PATH=$HOME/go/bin:$PATH",
		},
		{
			name:  "multiple paths",
			paths: []string{"$HOME/go/bin", "$HOME/.cargo/bin"},
			want:  "export PATH=$HOME/go/bin:$HOME/.cargo/bin:$PATH",
		},
		{
			name:  "empty paths",
			paths: []string{},
			want:  "",
		},
		{
			name:  "absolute path converted to home relative",
			paths: []string{"/home/user/go/bin"},
			want:  "export PATH=$HOME/go/bin:$PATH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GeneratePATHExportCommand(tt.paths)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToHomeRelativePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "macOS home path",
			input: "/Users/john/.local/bin",
			want:  "$HOME/.local/bin",
		},
		{
			name:  "Linux home path",
			input: "/home/john/.cargo/bin",
			want:  "$HOME/.cargo/bin",
		},
		{
			name:  "root home path",
			input: "/root/.local/bin",
			want:  "$HOME/.local/bin",
		},
		{
			name:  "already has $HOME",
			input: "$HOME/go/bin",
			want:  "$HOME/go/bin",
		},
		{
			name:  "non-home path unchanged",
			input: "/usr/local/bin",
			want:  "/usr/local/bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toHomeRelativePath(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
