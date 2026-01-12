package sync

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindRsync(t *testing.T) {
	// rsync should be available on most systems
	path, err := FindRsync()

	// If rsync is installed, verify we got a path
	if err == nil {
		assert.NotEmpty(t, path)
		assert.Contains(t, path, "rsync")
	}
	// If rsync is not installed, the error should be helpful
	if err != nil {
		assert.Contains(t, err.Error(), "rsync not found")
	}
}

func TestVersion(t *testing.T) {
	version, err := Version()

	// If rsync is installed, verify version output
	if err == nil {
		assert.NotEmpty(t, version)
		assert.Contains(t, version, "rsync")
	}
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name      string
		conn      *host.Connection
		localDir  string
		cfg       config.SyncConfig
		wantErr   bool
		checkArgs func(t *testing.T, args []string)
	}{
		{
			name: "basic sync",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			localDir: "/home/user/myapp",
			cfg:      config.SyncConfig{},
			checkArgs: func(t *testing.T, args []string) {
				assert.Contains(t, args, "-az")
				assert.Contains(t, args, "--delete")
				assert.Contains(t, args, "--force")
				assert.Contains(t, args, "--info=progress2")
				// Source should end with /
				assert.Contains(t, args, "/home/user/myapp/")
				// Destination should be alias:path/
				found := false
				for _, arg := range args {
					if arg == "test-alias:~/projects/myapp/" {
						found = true
						break
					}
				}
				assert.True(t, found, "expected destination test-alias:~/projects/myapp/ in args")
			},
		},
		{
			name: "includes SSH ControlMaster options",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			localDir: "/home/user/myapp",
			cfg:      config.SyncConfig{},
			checkArgs: func(t *testing.T, args []string) {
				// Find the -e flag and check its value
				foundE := false
				for i, arg := range args {
					if arg == "-e" && i+1 < len(args) {
						sshCmd := args[i+1]
						foundE = true
						// Should include ControlMaster options for connection reuse
						assert.Contains(t, sshCmd, "ControlMaster=auto", "should use ControlMaster=auto")
						assert.Contains(t, sshCmd, "ControlPath=", "should specify ControlPath")
						assert.Contains(t, sshCmd, "ControlPersist=", "should specify ControlPersist")
						// BatchMode prevents SSH from prompting for input (would hang rsync)
						assert.Contains(t, sshCmd, "BatchMode=yes", "should use BatchMode=yes to prevent prompts")
						break
					}
				}
				assert.True(t, foundE, "expected -e flag with SSH command")
			},
		},
		{
			name: "with exclude patterns",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			localDir: "/home/user/myapp",
			cfg: config.SyncConfig{
				Exclude: []string{".git/", "node_modules/", "*.pyc"},
			},
			checkArgs: func(t *testing.T, args []string) {
				assert.Contains(t, args, "--exclude=.git/")
				assert.Contains(t, args, "--exclude=node_modules/")
				assert.Contains(t, args, "--exclude=*.pyc")
			},
		},
		{
			name: "with preserve patterns",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			localDir: "/home/user/myapp",
			cfg: config.SyncConfig{
				Preserve: []string{".venv/", "data/"},
			},
			checkArgs: func(t *testing.T, args []string) {
				assert.Contains(t, args, "--filter=P .venv/")
				assert.Contains(t, args, "--filter=P **/.venv/")
				assert.Contains(t, args, "--filter=P data/")
				assert.Contains(t, args, "--filter=P **/data/")
			},
		},
		{
			name: "preserve pattern with ** prefix is not duplicated",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			localDir: "/home/user/myapp",
			cfg: config.SyncConfig{
				Preserve: []string{"**/cache/"},
			},
			checkArgs: func(t *testing.T, args []string) {
				assert.Contains(t, args, "--filter=P **/cache/")
				// Count occurrences - should only be one
				count := 0
				for _, arg := range args {
					if arg == "--filter=P **/cache/" {
						count++
					}
				}
				assert.Equal(t, 1, count, "pattern with ** prefix should not be duplicated")
			},
		},
		{
			name: "with custom flags",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			localDir: "/home/user/myapp",
			cfg: config.SyncConfig{
				Flags: []string{"--verbose", "--stats"},
			},
			checkArgs: func(t *testing.T, args []string) {
				assert.Contains(t, args, "--verbose")
				assert.Contains(t, args, "--stats")
			},
		},
		{
			name: "full config",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			localDir: "/home/user/myapp",
			cfg: config.SyncConfig{
				Exclude:  []string{".git/", "__pycache__/"},
				Preserve: []string{".venv/", "node_modules/"},
				Flags:    []string{"--compress-level=9"},
			},
			checkArgs: func(t *testing.T, args []string) {
				// Base flags should be present
				assert.Contains(t, args, "-az")
				assert.Contains(t, args, "--delete")
				assert.Contains(t, args, "--force")

				// Exclude patterns
				assert.Contains(t, args, "--exclude=.git/")
				assert.Contains(t, args, "--exclude=__pycache__/")

				// Preserve patterns
				assert.Contains(t, args, "--filter=P .venv/")
				assert.Contains(t, args, "--filter=P node_modules/")

				// Custom flags
				assert.Contains(t, args, "--compress-level=9")
			},
		},
		{
			name:     "nil connection",
			conn:     nil,
			localDir: "/home/user/myapp",
			cfg:      config.SyncConfig{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := BuildArgs(tt.conn, tt.localDir, tt.cfg)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, tt.checkArgs)
			tt.checkArgs(t, args)
		})
	}
}

// TestBuildArgs_SSHBatchMode verifies that BatchMode=yes is included in the SSH command.
// This is critical to prevent rsync from hanging when SSH would prompt for input
// (e.g., host key verification, password). Without BatchMode, SSH waits for input
// that never comes since there's no terminal attached, causing the sync to hang
// indefinitely on first run.
func TestBuildArgs_SSHBatchMode(t *testing.T) {
	conn := &host.Connection{
		Name:  "test-host",
		Alias: "test-alias",
		Host:  config.Host{Dir: "~/projects/app"},
	}

	args, err := BuildArgs(conn, "/home/user/app", config.SyncConfig{})
	require.NoError(t, err)

	// Find the SSH command passed via -e flag
	var sshCmd string
	for i, arg := range args {
		if arg == "-e" && i+1 < len(args) {
			sshCmd = args[i+1]
			break
		}
	}

	require.NotEmpty(t, sshCmd, "expected -e flag with SSH command")

	// BatchMode=yes is required to prevent SSH from prompting for input.
	// Without this, rsync hangs on first run if SSH needs to verify host keys
	// or prompt for credentials, since there's no terminal for user input.
	assert.Contains(t, sshCmd, "BatchMode=yes",
		"SSH command must include BatchMode=yes to prevent hanging on prompts")

	// Also verify the other expected SSH options are present
	assert.Contains(t, sshCmd, "ControlMaster=auto")
	assert.Contains(t, sshCmd, "ControlPath=")
	assert.Contains(t, sshCmd, "ControlPersist=")
}

func TestParseProgress(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected *Progress
	}{
		{
			name:     "empty line",
			line:     "",
			expected: nil,
		},
		{
			name:     "non-progress line",
			line:     "sending incremental file list",
			expected: nil,
		},
		{
			name: "simple progress line",
			line: "      1,234,567  42%  500.00kB/s    0:01:23",
			expected: &Progress{
				BytesTransferred: 1234567,
				Percentage:       42,
				Speed:            "500.00kB/s",
				TimeRemaining:    "0:01:23",
			},
		},
		{
			name: "progress with file info",
			line: "         32,768 100%    1.23MB/s    0:00:01 (xfr#1, to-chk=99/100)",
			expected: &Progress{
				BytesTransferred: 32768,
				Percentage:       100,
				Speed:            "1.23MB/s",
				TimeRemaining:    "0:00:01",
				FileCount:        1,
				TotalFiles:       100,
			},
		},
		{
			name: "progress with ir-chk format",
			line: "     12,345,678  75%    2.50MB/s    0:00:30 (xfr#25, ir-chk=50/200)",
			expected: &Progress{
				BytesTransferred: 12345678,
				Percentage:       75,
				Speed:            "2.50MB/s",
				TimeRemaining:    "0:00:30",
				FileCount:        25,
				TotalFiles:       200,
			},
		},
		{
			name: "100% complete",
			line: "    987,654,321 100%   10.50MB/s    0:00:00 (xfr#500, to-chk=0/500)",
			expected: &Progress{
				BytesTransferred: 987654321,
				Percentage:       100,
				Speed:            "10.50MB/s",
				TimeRemaining:    "0:00:00",
				FileCount:        500,
				TotalFiles:       500,
			},
		},
		{
			name: "GB speed",
			line: "  1,234,567,890  99%    1.50GB/s    0:00:01",
			expected: &Progress{
				BytesTransferred: 1234567890,
				Percentage:       99,
				Speed:            "1.50GB/s",
				TimeRemaining:    "0:00:01",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseProgress(tt.line)

			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			assert.Equal(t, tt.expected.BytesTransferred, result.BytesTransferred)
			assert.Equal(t, tt.expected.Percentage, result.Percentage)
			assert.Equal(t, tt.expected.Speed, result.Speed)
			assert.Equal(t, tt.expected.TimeRemaining, result.TimeRemaining)
			assert.Equal(t, tt.expected.FileCount, result.FileCount)
			assert.Equal(t, tt.expected.TotalFiles, result.TotalFiles)
		})
	}
}

func TestProgressIsComplete(t *testing.T) {
	tests := []struct {
		name     string
		progress *Progress
		expected bool
	}{
		{
			name:     "nil progress",
			progress: nil,
			expected: false,
		},
		{
			name:     "0 percent",
			progress: &Progress{Percentage: 0},
			expected: false,
		},
		{
			name:     "50 percent",
			progress: &Progress{Percentage: 50},
			expected: false,
		},
		{
			name:     "99 percent",
			progress: &Progress{Percentage: 99},
			expected: false,
		},
		{
			name:     "100 percent",
			progress: &Progress{Percentage: 100},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.progress.IsComplete()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSync_SkipsForLocalConnection(t *testing.T) {
	// Create a local connection
	localConn := &host.Connection{
		Name:    "local",
		Alias:   "local",
		IsLocal: true,
		Client:  nil,
		Host:    config.Host{Dir: "/tmp/test"},
	}

	// Sync should return immediately without error for local connections
	err := Sync(localConn, "/some/path", config.SyncConfig{}, nil)
	assert.NoError(t, err)
}

func TestSync_NilConnection(t *testing.T) {
	// Sync with nil connection should fail (but not panic)
	err := Sync(nil, "/some/path", config.SyncConfig{}, nil)
	assert.Error(t, err)
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1572864, "1.50 MB"},
		{1073741824, "1.00 GB"},
		{1610612736, "1.50 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleRsyncError(t *testing.T) {
	tests := []struct {
		name         string
		exitCode     int
		hostName     string
		wantContains string
	}{
		{
			name:         "exit code 1 - syntax error",
			exitCode:     1,
			hostName:     "testhost",
			wantContains: "syntax or usage error",
		},
		{
			name:         "exit code 2 - protocol incompatibility",
			exitCode:     2,
			hostName:     "testhost",
			wantContains: "protocol incompatibility",
		},
		{
			name:         "exit code 3 - file selection error",
			exitCode:     3,
			hostName:     "testhost",
			wantContains: "File selection error",
		},
		{
			name:         "exit code 5 - client-server protocol",
			exitCode:     5,
			hostName:     "testhost",
			wantContains: "client-server protocol",
		},
		{
			name:         "exit code 10 - socket I/O",
			exitCode:     10,
			hostName:     "testhost",
			wantContains: "socket I/O",
		},
		{
			name:         "exit code 11 - file I/O",
			exitCode:     11,
			hostName:     "testhost",
			wantContains: "file I/O",
		},
		{
			name:         "exit code 12 - protocol data stream",
			exitCode:     12,
			hostName:     "testhost",
			wantContains: "protocol data stream",
		},
		{
			name:         "exit code 23 - partial transfer",
			exitCode:     23,
			hostName:     "testhost",
			wantContains: "Partial transfer due to error",
		},
		{
			name:         "exit code 24 - vanished source",
			exitCode:     24,
			hostName:     "testhost",
			wantContains: "vanished source files",
		},
		{
			name:         "exit code 255 - SSH failure",
			exitCode:     255,
			hostName:     "myserver",
			wantContains: "SSH connection to 'myserver' failed",
		},
		{
			name:         "unknown exit code",
			exitCode:     99,
			hostName:     "testhost",
			wantContains: "exited with code 99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a real exec.ExitError by running a command that exits with the code
			cmd := exec.Command("sh", "-c", "exit "+strconv.Itoa(tt.exitCode))
			err := cmd.Run()
			require.Error(t, err, "command should fail with exit code %d", tt.exitCode)

			result := handleRsyncError(err, tt.hostName, "")
			assert.Error(t, result)
			assert.Contains(t, result.Error(), tt.wantContains)
		})
	}
}

func TestHandleRsyncError_NonExitError(t *testing.T) {
	// Test with a non-ExitError
	regularErr := assert.AnError
	result := handleRsyncError(regularErr, "testhost", "")

	assert.Error(t, result)
	assert.Contains(t, result.Error(), "rsync failed")
}

func TestHandleRsyncError_VersionTooOld(t *testing.T) {
	// Simulate rsync exiting with code 1 and stderr containing the version error
	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	require.Error(t, err)

	stderrOutput := `rsync: unrecognized option '--info=progress2'
usage: rsync [-v] [-q] [more options]`

	result := handleRsyncError(err, "testhost", stderrOutput)

	assert.Error(t, result)
	assert.Contains(t, result.Error(), "rsync version too old")
	assert.Contains(t, result.Error(), "rsync 3.1.0+")
	assert.Contains(t, result.Error(), "brew install rsync")
}

func TestIsRsyncVersionError(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "version error detected",
			output:   "rsync: unrecognized option '--info=progress2'\nusage: rsync [options]",
			expected: true,
		},
		{
			name:     "different unrecognized option",
			output:   "rsync: unrecognized option '--foo'\nusage: rsync [options]",
			expected: false,
		},
		{
			name:     "empty output",
			output:   "",
			expected: false,
		},
		{
			name:     "normal rsync error",
			output:   "rsync: connection unexpectedly closed",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRsyncVersionError(tt.output)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStreamOutput(t *testing.T) {
	input := "line1\nline2\nline3\n"
	reader := strings.NewReader(input)
	var output strings.Builder

	streamOutput(reader, &output)

	// Each line should be written with a newline
	assert.Contains(t, output.String(), "line1")
	assert.Contains(t, output.String(), "line2")
	assert.Contains(t, output.String(), "line3")
}

func TestStreamOutput_EmptyInput(t *testing.T) {
	reader := strings.NewReader("")
	var output strings.Builder

	streamOutput(reader, &output)

	assert.Empty(t, output.String())
}

func TestStreamOutput_CarriageReturns(t *testing.T) {
	// rsync uses \r to update progress in-place
	input := "progress 10%\rprogress 50%\rprogress 100%\n"
	reader := strings.NewReader(input)
	var output strings.Builder

	streamOutput(reader, &output)

	// Each \r-delimited segment should be written as a separate line
	assert.Contains(t, output.String(), "progress 10%")
	assert.Contains(t, output.String(), "progress 50%")
	assert.Contains(t, output.String(), "progress 100%")
}

func TestStreamOutput_MixedDelimiters(t *testing.T) {
	// Test with mixed \r and \n
	input := "line1\rline2\nline3\r\nline4\n"
	reader := strings.NewReader(input)
	var output strings.Builder

	streamOutput(reader, &output)

	assert.Contains(t, output.String(), "line1")
	assert.Contains(t, output.String(), "line2")
	assert.Contains(t, output.String(), "line3")
	assert.Contains(t, output.String(), "line4")
}

func TestScanLinesWithCR(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		atEOF    bool
		wantAdv  int
		wantTok  string
		wantMore bool // true if we expect 0, nil, nil (need more data)
	}{
		{
			name:     "empty at EOF",
			data:     []byte{},
			atEOF:    true,
			wantAdv:  0,
			wantTok:  "",
			wantMore: false,
		},
		{
			name:     "line with newline",
			data:     []byte("hello\nworld"),
			atEOF:    false,
			wantAdv:  6,
			wantTok:  "hello",
			wantMore: false,
		},
		{
			name:     "line with carriage return",
			data:     []byte("hello\rworld"),
			atEOF:    false,
			wantAdv:  6,
			wantTok:  "hello",
			wantMore: false,
		},
		{
			name:     "no delimiter, not EOF",
			data:     []byte("hello"),
			atEOF:    false,
			wantAdv:  0,
			wantTok:  "",
			wantMore: true,
		},
		{
			name:     "no delimiter, at EOF",
			data:     []byte("hello"),
			atEOF:    true,
			wantAdv:  5,
			wantTok:  "hello",
			wantMore: false,
		},
		{
			name:     "rsync progress format",
			data:     []byte("      1,234,567  42%  500.00kB/s    0:01:23\r"),
			atEOF:    false,
			wantAdv:  44,
			wantTok:  "      1,234,567  42%  500.00kB/s    0:01:23",
			wantMore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adv, tok, err := scanLinesWithCR(tt.data, tt.atEOF)
			assert.NoError(t, err)

			if tt.wantMore {
				assert.Equal(t, 0, adv)
				assert.Nil(t, tok)
			} else {
				assert.Equal(t, tt.wantAdv, adv)
				assert.Equal(t, tt.wantTok, string(tok))
			}
		})
	}
}
