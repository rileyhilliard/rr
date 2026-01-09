package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toHomeRelative(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatPathSuggestion(t *testing.T) {
	interOnly := []string{
		"/Users/test/.local/bin",
		"/Users/test/.cargo/bin",
	}

	suggestion := formatPathSuggestion("myhost", interOnly)

	assert.Contains(t, suggestion, "interactive shell PATH but not login shell")
	assert.Contains(t, suggestion, "/Users/test/.local/bin")
	assert.Contains(t, suggestion, "/Users/test/.cargo/bin")
	assert.Contains(t, suggestion, "setup_commands")
	assert.Contains(t, suggestion, "myhost")
	assert.Contains(t, suggestion, "$HOME/.local/bin:$HOME/.cargo/bin")
}

func TestFormatPathSuggestion_TruncatesLongList(t *testing.T) {
	interOnly := []string{
		"/a", "/b", "/c", "/d", "/e", "/f", "/g",
	}

	suggestion := formatPathSuggestion("host", interOnly)

	// Should show first 5 and indicate more
	assert.Contains(t, suggestion, "/a")
	assert.Contains(t, suggestion, "/e")
	assert.Contains(t, suggestion, "... and 2 more")

	// Export should only have first 3
	assert.Contains(t, suggestion, "export PATH=/a:/b:/c:$PATH")
}

func TestPathCheck_NilClient(t *testing.T) {
	check := &PathCheck{
		HostName: "test",
		Client:   nil,
	}

	result := check.Run()

	assert.Equal(t, StatusFail, result.Status)
	assert.Contains(t, result.Message, "no connection")
}

func TestPathCheck_Name(t *testing.T) {
	check := &PathCheck{HostName: "myserver"}
	assert.Equal(t, "path_myserver", check.Name())
}

func TestPathCheck_Category(t *testing.T) {
	check := &PathCheck{}
	assert.Equal(t, "PATH", check.Category())
}

func TestPathCheck_Fix(t *testing.T) {
	check := &PathCheck{}
	err := check.Fix()
	assert.Nil(t, err)
}
