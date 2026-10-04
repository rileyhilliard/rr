package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/errors"
)

func TestHomebrewPrefix(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"apple silicon cask", "/opt/homebrew/Caskroom/rr/0.29.0/rr", "/opt/homebrew"},
		{"intel cask", "/usr/local/Caskroom/rr/0.28.0/rr", "/usr/local"},
		{"go install", "/Users/me/go/bin/rr", ""},
		{"install script", "/usr/local/bin/rr", ""},
		{"another cask's dir", "/opt/homebrew/Caskroom/rrr/1.0.0/rr", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, homebrewPrefix(tt.path))
		})
	}
}

// fakeBrewPrefix makes a Homebrew prefix whose bin/brew is a script: it
// records its arguments, then runs body. brew is a third-party tool, so
// it's the one thing these tests stand in for.
func fakeBrewPrefix(t *testing.T, body string) (prefix, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew doesn't run on Windows")
	}
	prefix = t.TempDir()
	argsFile = filepath.Join(prefix, "brew-args")
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "bin"), 0o755))
	script := "#!/bin/sh\necho \"$@\" > '" + argsFile + "'\n" + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(prefix, "bin", "brew"), []byte(script), 0o755)) // #nosec G306 -- test script must be executable
	return prefix, argsFile
}

// Regression: rr update on a Homebrew install replaced the binary inside
// Caskroom/rr/<old version>, so brew kept recording the old version. It
// must hand the update to brew.
func TestUpdateViaBrew(t *testing.T) {
	tests := []struct {
		name     string
		force    bool
		body     string
		wantArgs string
		wantErr  string // substring of the error's suggestion or message; "" for success
	}{
		{
			name:     "upgrades the cask",
			body:     `mkdir -p "$(dirname "$0")/../Caskroom/rr/0.29.1"`,
			wantArgs: "upgrade --cask rileyhilliard/tap/rr",
		},
		{
			name:     "force reinstalls",
			force:    true,
			body:     `mkdir -p "$(dirname "$0")/../Caskroom/rr/0.29.1"`,
			wantArgs: "reinstall --cask rileyhilliard/tap/rr",
		},
		{
			name:     "brew fails",
			body:     "exit 1",
			wantArgs: "upgrade --cask rileyhilliard/tap/rr",
			wantErr:  "brew upgrade --cask rileyhilliard/tap/rr",
		},
		{
			name:     "brew succeeds without installing the release",
			body:     "exit 0",
			wantArgs: "upgrade --cask rileyhilliard/tap/rr",
			wantErr:  "brew update",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, argsFile := fakeBrewPrefix(t, tt.body)

			var err error
			out := captureStdout(t, func() {
				err = updateViaBrew(prefix, "v0.29.0", "v0.29.1", "https://example.test/release", tt.force)
			})

			args, readErr := os.ReadFile(argsFile)
			require.NoError(t, readErr, "brew was run")
			assert.Equal(t, tt.wantArgs, strings.TrimSpace(string(args)))

			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Contains(t, out, "Updated to")
				return
			}
			require.Error(t, err)
			assert.True(t, errors.IsCode(err, errors.ErrExec), "got %v", err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.NotContains(t, out, "Updated to", "a failed update never claims success")
		})
	}
}
