package sync

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPullArgs(t *testing.T) {
	tests := []struct {
		name       string
		conn       *host.Connection
		patterns   []string
		localDest  string
		extraFlags []string
		wantErr    bool
		checkArgs  func(t *testing.T, args []string)
	}{
		{
			name: "basic pull single file",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			patterns:  []string{"coverage.xml"},
			localDest: ".",
			checkArgs: func(t *testing.T, args []string) {
				assert.Contains(t, args, "-az")
				assert.Contains(t, args, "--info=progress2")
				// Should NOT have --delete for pull
				assert.NotContains(t, args, "--delete")
				assert.NotContains(t, args, "--force")
				// Source should be remote:path/pattern
				found := false
				for _, arg := range args {
					if arg == "test-alias:~/projects/myapp/coverage.xml" {
						found = true
						break
					}
				}
				assert.True(t, found, "expected remote source test-alias:~/projects/myapp/coverage.xml in args")
				// Destination should be last
				assert.Equal(t, ".", args[len(args)-1])
			},
		},
		{
			name: "pull with destination directory",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			patterns:  []string{"dist/*.whl"},
			localDest: "/tmp/artifacts",
			checkArgs: func(t *testing.T, args []string) {
				// Destination should end with /
				assert.Equal(t, "/tmp/artifacts/", args[len(args)-1])
				// Source pattern should be preserved
				found := false
				for _, arg := range args {
					if arg == "test-alias:~/projects/myapp/dist/*.whl" {
						found = true
						break
					}
				}
				assert.True(t, found, "expected remote source with glob pattern")
			},
		},
		{
			name: "pull multiple patterns",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			patterns:  []string{"coverage.xml", "htmlcov/"},
			localDest: "./reports",
			checkArgs: func(t *testing.T, args []string) {
				// Should have both patterns as sources
				foundCoverage := false
				foundHtmlcov := false
				for _, arg := range args {
					if arg == "test-alias:~/projects/myapp/coverage.xml" {
						foundCoverage = true
					}
					if arg == "test-alias:~/projects/myapp/htmlcov/" {
						foundHtmlcov = true
					}
				}
				assert.True(t, foundCoverage, "expected coverage.xml source")
				assert.True(t, foundHtmlcov, "expected htmlcov/ source")
			},
		},
		{
			name: "includes SSH ControlMaster options",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			patterns:  []string{"file.txt"},
			localDest: ".",
			checkArgs: func(t *testing.T, args []string) {
				// Find the -e flag and check its value
				foundE := false
				for i, arg := range args {
					if arg == "-e" && i+1 < len(args) {
						sshCmd := args[i+1]
						foundE = true
						assert.Contains(t, sshCmd, "ControlMaster=auto")
						assert.Contains(t, sshCmd, "ControlPath=")
						assert.Contains(t, sshCmd, "BatchMode=yes")
						break
					}
				}
				assert.True(t, foundE, "expected -e flag with SSH command")
			},
		},
		{
			name: "with extra flags",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			patterns:   []string{"file.txt"},
			localDest:  ".",
			extraFlags: []string{"--verbose", "--dry-run"},
			checkArgs: func(t *testing.T, args []string) {
				assert.Contains(t, args, "--verbose")
				assert.Contains(t, args, "--dry-run")
			},
		},
		{
			name:      "nil connection",
			conn:      nil,
			patterns:  []string{"file.txt"},
			localDest: ".",
			wantErr:   true,
		},
		{
			name: "empty patterns",
			conn: &host.Connection{
				Name:  "test-host",
				Alias: "test-alias",
				Host:  config.Host{Dir: "~/projects/myapp"},
			},
			patterns:  []string{},
			localDest: ".",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := BuildPullArgs(tt.conn, tt.patterns, tt.localDest, tt.extraFlags)

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

func TestBuildPullArgs_SSHConfigFile(t *testing.T) {
	// Save and restore the global SSHConfigFile
	origConfigFile := SSHConfigFile
	defer func() { SSHConfigFile = origConfigFile }()

	conn := &host.Connection{
		Name:  "test-host",
		Alias: "test-alias",
		Host:  config.Host{Dir: "~/projects/app"},
	}

	t.Run("with custom config", func(t *testing.T) {
		SSHConfigFile = "/tmp/test-ssh-config"
		args, err := BuildPullArgs(conn, []string{"file.txt"}, ".", nil)
		require.NoError(t, err)

		var sshCmd string
		for i, arg := range args {
			if arg == "-e" && i+1 < len(args) {
				sshCmd = args[i+1]
				break
			}
		}

		assert.Contains(t, sshCmd, `-F "/tmp/test-ssh-config"`)
	})
}

func TestGroupByDest(t *testing.T) {
	tests := []struct {
		name        string
		items       []config.PullItem
		defaultDest string
		expected    map[string][]string
	}{
		{
			name: "all same dest",
			items: []config.PullItem{
				{Src: "a.txt"},
				{Src: "b.txt"},
			},
			defaultDest: "./output",
			expected: map[string][]string{
				"./output": {"a.txt", "b.txt"},
			},
		},
		{
			name: "mixed destinations",
			items: []config.PullItem{
				{Src: "a.txt", Dest: "./dir1"},
				{Src: "b.txt", Dest: "./dir2"},
				{Src: "c.txt"},
			},
			defaultDest: "./default",
			expected: map[string][]string{
				"./dir1":    {"a.txt"},
				"./dir2":    {"b.txt"},
				"./default": {"c.txt"},
			},
		},
		{
			name: "empty default dest uses current dir",
			items: []config.PullItem{
				{Src: "a.txt"},
			},
			defaultDest: "",
			expected: map[string][]string{
				".": {"a.txt"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := groupByDest(tt.items, tt.defaultDest)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPull_SkipsForLocalConnection(t *testing.T) {
	localConn := &host.Connection{
		Name:    "local",
		Alias:   "local",
		IsLocal: true,
		Client:  nil,
		Host:    config.Host{Dir: "/tmp/test"},
	}

	err := Pull(localConn, PullOptions{
		Patterns: []config.PullItem{{Src: "file.txt"}},
	}, nil)
	assert.NoError(t, err)
}

func TestPull_NilConnection(t *testing.T) {
	err := Pull(nil, PullOptions{
		Patterns: []config.PullItem{{Src: "file.txt"}},
	}, nil)
	assert.Error(t, err)
}

func TestPull_EmptyPatterns(t *testing.T) {
	conn := &host.Connection{
		Name:  "test",
		Alias: "test",
		Host:  config.Host{Dir: "~/test"},
	}

	// Empty patterns should return nil (nothing to do)
	err := Pull(conn, PullOptions{Patterns: nil}, nil)
	assert.NoError(t, err)
}

func TestHandlePullError(t *testing.T) {
	tests := []struct {
		name         string
		exitCode     int
		stderr       string
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
			name:         "exit code 3 - file selection error",
			exitCode:     3,
			hostName:     "testhost",
			wantContains: "File selection error",
		},
		{
			name:         "exit code 23 - partial transfer",
			exitCode:     23,
			hostName:     "testhost",
			wantContains: "Partial transfer",
		},
		{
			name:         "exit code 255 - SSH failure",
			exitCode:     255,
			hostName:     "myserver",
			wantContains: "SSH connection to 'myserver' failed",
		},
		{
			name:         "file not found in stderr",
			exitCode:     23,
			hostName:     "testhost",
			stderr:       "rsync: link_stat \"/remote/path/file.txt\" failed: No such file or directory",
			wantContains: "Remote file or pattern not found",
		},
		{
			name:         "rsync version too old",
			exitCode:     1,
			hostName:     "testhost",
			stderr:       "rsync: unrecognized option '--info=progress2'\nusage: rsync [options]",
			wantContains: "rsync version too old",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", "exit "+strconv.Itoa(tt.exitCode))
			err := cmd.Run()
			require.Error(t, err)

			result := handlePullError(err, tt.hostName, tt.stderr)
			assert.Error(t, result)
			assert.Contains(t, result.Error(), tt.wantContains)
		})
	}
}

func TestBuildPullArgs_CreatesDestDir(t *testing.T) {
	// Create a temp directory
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "nested", "dir")

	conn := &host.Connection{
		Name:  "test-host",
		Alias: "test-alias",
		Host:  config.Host{Dir: "~/projects/myapp"},
	}

	// BuildPullArgs should create the destination directory
	_, err := BuildPullArgs(conn, []string{"file.txt"}, destDir, nil)
	require.NoError(t, err)

	// Verify directory was created
	_, statErr := os.Stat(destDir)
	assert.NoError(t, statErr, "destination directory should be created")
}

func TestHandlePullError_NonExitError(t *testing.T) {
	// Test with a non-ExitError (e.g., a generic error)
	genericErr := errors.New("some random error")
	result := handlePullError(genericErr, "testhost", "")
	assert.Error(t, result)
	assert.Contains(t, result.Error(), "rsync pull failed")
}

func TestHandlePullError_MoreExitCodes(t *testing.T) {
	tests := []struct {
		name         string
		exitCode     int
		wantContains string
	}{
		{
			name:         "exit code 2 - protocol incompatibility",
			exitCode:     2,
			wantContains: "protocol incompatibility",
		},
		{
			name:         "exit code 5 - client-server protocol",
			exitCode:     5,
			wantContains: "client-server protocol",
		},
		{
			name:         "exit code 10 - socket I/O",
			exitCode:     10,
			wantContains: "socket I/O",
		},
		{
			name:         "exit code 11 - file I/O",
			exitCode:     11,
			wantContains: "file I/O",
		},
		{
			name:         "exit code 12 - protocol data stream",
			exitCode:     12,
			wantContains: "protocol data stream",
		},
		{
			name:         "unknown exit code",
			exitCode:     99,
			wantContains: "rsync exited with code 99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", "exit "+strconv.Itoa(tt.exitCode))
			err := cmd.Run()
			require.Error(t, err)

			result := handlePullError(err, "testhost", "")
			assert.Error(t, result)
			assert.Contains(t, result.Error(), tt.wantContains)
		})
	}
}

func TestPull_FindsRsync(t *testing.T) {
	// Test that Pull returns error when rsync is not found
	// This is hard to test directly, but we can at least verify
	// the function handles the case where rsync exists (common case)
	conn := &host.Connection{
		Name:  "test-host",
		Alias: "test-alias",
		Host:  config.Host{Dir: "~/projects/myapp"},
	}

	// This will fail because we don't have a real SSH connection,
	// but it will exercise the rsync finding code path
	err := Pull(conn, PullOptions{
		Patterns: []config.PullItem{{Src: "file.txt"}},
	}, nil)

	// We expect an error (SSH connection will fail), but not a "rsync not found" error
	// if rsync is installed on the system
	if err != nil {
		assert.NotContains(t, err.Error(), "rsync not found")
	}
}

func TestGroupByDest_OrderPreserved(t *testing.T) {
	// Test that multiple patterns going to the same dest are preserved in order
	items := []config.PullItem{
		{Src: "first.txt"},
		{Src: "second.txt"},
		{Src: "third.txt"},
	}

	result := groupByDest(items, "./output")

	// All should be grouped under ./output
	require.Len(t, result, 1)
	patterns := result["./output"]
	require.Len(t, patterns, 3)
	assert.Equal(t, "first.txt", patterns[0])
	assert.Equal(t, "second.txt", patterns[1])
	assert.Equal(t, "third.txt", patterns[2])
}

func TestBuildPullArgs_RemoteDirTrailingSlash(t *testing.T) {
	// Test that remote dir without trailing slash gets one added
	conn := &host.Connection{
		Name:  "test-host",
		Alias: "test-alias",
		Host:  config.Host{Dir: "~/projects/myapp"}, // no trailing slash
	}

	args, err := BuildPullArgs(conn, []string{"file.txt"}, ".", nil)
	require.NoError(t, err)

	// Find the remote source argument
	found := false
	for _, arg := range args {
		if arg == "test-alias:~/projects/myapp/file.txt" {
			found = true
			break
		}
	}
	assert.True(t, found, "remote path should include trailing slash before pattern")

	// Also test with trailing slash already present
	conn2 := &host.Connection{
		Name:  "test-host",
		Alias: "test-alias",
		Host:  config.Host{Dir: "~/projects/myapp/"}, // with trailing slash
	}

	args2, err := BuildPullArgs(conn2, []string{"file.txt"}, ".", nil)
	require.NoError(t, err)

	found2 := false
	for _, arg := range args2 {
		// Should not have double slash
		if arg == "test-alias:~/projects/myapp/file.txt" {
			found2 = true
			break
		}
	}
	assert.True(t, found2, "remote path should not have double slashes")
}
