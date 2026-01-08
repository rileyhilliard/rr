package cli

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestVersionCmd creates a standalone version command for testing
func createTestVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			if short {
				cmd.Println(version)
				return
			}

			cmd.Printf("rr %s\n", formatVersion(version))
			cmd.Printf("commit: %s\n", commit)
			cmd.Printf("built: %s\n", date)
			cmd.Printf("go: %s\n", runtime.Version())
			cmd.Printf("os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "Print only the version number")
	return cmd
}

func TestVersionOutput(t *testing.T) {
	// Save original values
	originalVersion := version
	originalCommit := commit
	originalDate := date

	// Restore after test
	defer func() {
		version = originalVersion
		commit = originalCommit
		date = originalDate
	}()

	// Set test values
	version = "1.2.3"
	commit = "abc1234"
	date = "2025-01-08T12:00:00Z"

	// Create isolated test command
	cmd := createTestVersionCmd()

	// Capture output
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()

	// Verify all expected fields are present
	assert.Contains(t, output, "rr v1.2.3", "should show version with v prefix")
	assert.Contains(t, output, "commit: abc1234", "should show commit")
	assert.Contains(t, output, "built: 2025-01-08T12:00:00Z", "should show build date")
	assert.Contains(t, output, "go: "+runtime.Version(), "should show Go version")
	assert.Contains(t, output, "os/arch: "+runtime.GOOS+"/"+runtime.GOARCH, "should show os/arch")
}

func TestVersionOutputShort(t *testing.T) {
	// Save original values
	originalVersion := version

	// Restore after test
	defer func() {
		version = originalVersion
	}()

	version = "1.2.3"

	cmd := createTestVersionCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--short"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := strings.TrimSpace(buf.String())
	assert.Equal(t, "1.2.3", output, "short output should only show version")
}

func TestVersionOutputDev(t *testing.T) {
	// Save original values
	originalVersion := version
	originalCommit := commit
	originalDate := date

	// Restore after test
	defer func() {
		version = originalVersion
		commit = originalCommit
		date = originalDate
	}()

	version = "dev"
	commit = "none"
	date = "unknown"

	cmd := createTestVersionCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "rr dev", "dev version should not have v prefix")
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "dev version",
			input: "dev",
			want:  "dev",
		},
		{
			name:  "version without prefix",
			input: "1.2.3",
			want:  "v1.2.3",
		},
		{
			name:  "version with prefix",
			input: "v1.2.3",
			want:  "v1.2.3",
		},
		{
			name:  "version with prerelease",
			input: "1.2.3-beta.1",
			want:  "v1.2.3-beta.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersion(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVersionCommandHasShortFlag(t *testing.T) {
	flag := versionCmd.Flags().Lookup("short")
	require.NotNil(t, flag, "version command should have --short flag")
	assert.Equal(t, "bool", flag.Value.Type())
	assert.Equal(t, "false", flag.DefValue)
}

func TestSetVersionInfo(t *testing.T) {
	// Save original values
	originalVersion := version
	originalCommit := commit
	originalDate := date

	// Restore after test
	defer func() {
		version = originalVersion
		commit = originalCommit
		date = originalDate
	}()

	SetVersionInfo("2.0.0", "def5678", "2025-06-15T10:00:00Z")

	assert.Equal(t, "2.0.0", version)
	assert.Equal(t, "def5678", commit)
	assert.Equal(t, "2025-06-15T10:00:00Z", date)
}

func TestGetVersion(t *testing.T) {
	// Save original value
	originalVersion := version
	defer func() { version = originalVersion }()

	version = "3.0.0"
	assert.Equal(t, "3.0.0", GetVersion())
}
