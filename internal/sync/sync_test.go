package sync

import (
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
