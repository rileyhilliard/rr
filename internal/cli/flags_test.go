package cli

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProbeTimeout(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		want    time.Duration
		wantErr bool
	}{
		{
			name:    "empty string returns zero",
			flag:    "",
			want:    0,
			wantErr: false,
		},
		{
			name:    "valid seconds",
			flag:    "5s",
			want:    5 * time.Second,
			wantErr: false,
		},
		{
			name:    "valid minutes",
			flag:    "2m",
			want:    2 * time.Minute,
			wantErr: false,
		},
		{
			name:    "valid milliseconds",
			flag:    "500ms",
			want:    500 * time.Millisecond,
			wantErr: false,
		},
		{
			name:    "valid complex duration",
			flag:    "1m30s",
			want:    90 * time.Second,
			wantErr: false,
		},
		{
			name:    "invalid format returns error",
			flag:    "5",
			wantErr: true,
		},
		{
			name:    "invalid string returns error",
			flag:    "fast",
			wantErr: true,
		},
		{
			name:    "negative duration",
			flag:    "-5s",
			want:    -5 * time.Second,
			wantErr: false, // Go allows negative durations
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProbeTimeout(tt.flag)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAddCommonFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	flags := &CommonFlags{}

	AddCommonFlags(cmd, flags)

	// Verify flags are registered
	hostFlag := cmd.Flags().Lookup("host")
	require.NotNil(t, hostFlag, "host flag should be registered")
	assert.Equal(t, "", hostFlag.DefValue)

	tagFlag := cmd.Flags().Lookup("tag")
	require.NotNil(t, tagFlag, "tag flag should be registered")
	assert.Equal(t, "", tagFlag.DefValue)

	probeTimeoutFlag := cmd.Flags().Lookup("probe-timeout")
	require.NotNil(t, probeTimeoutFlag, "probe-timeout flag should be registered")
	assert.Equal(t, "", probeTimeoutFlag.DefValue)
}

func TestAddCommonFlags_Values(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	flags := &CommonFlags{}

	AddCommonFlags(cmd, flags)

	// Set flag values
	err := cmd.Flags().Set("host", "myhost")
	require.NoError(t, err)
	assert.Equal(t, "myhost", flags.Host)

	err = cmd.Flags().Set("tag", "gpu")
	require.NoError(t, err)
	assert.Equal(t, "gpu", flags.Tag)

	err = cmd.Flags().Set("probe-timeout", "10s")
	require.NoError(t, err)
	assert.Equal(t, "10s", flags.ProbeTimeout)
}

func TestHashProject(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantLen int
	}{
		{
			name:    "simple path",
			path:    "/home/user/project",
			wantLen: 16,
		},
		{
			name:    "empty path",
			path:    "",
			wantLen: 16,
		},
		{
			name:    "path with special chars",
			path:    "/home/user/my-project_v2.0",
			wantLen: 16,
		},
		{
			name:    "windows-style path",
			path:    "C:\\Users\\project",
			wantLen: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hashProject(tt.path)
			assert.Len(t, got, tt.wantLen, "hash should be 16 hex characters")
		})
	}
}

func TestHashProject_Deterministic(t *testing.T) {
	path := "/home/user/myproject"

	hash1 := hashProject(path)
	hash2 := hashProject(path)

	assert.Equal(t, hash1, hash2, "same path should produce same hash")
}

func TestHashProject_UniqueForDifferentPaths(t *testing.T) {
	paths := []string{
		"/home/user/project1",
		"/home/user/project2",
		"/home/other/project1",
		"/tmp/test",
	}

	hashes := make(map[string]string)
	for _, path := range paths {
		hash := hashProject(path)
		if existing, ok := hashes[hash]; ok {
			t.Errorf("hash collision: %q and %q both produce %s", path, existing, hash)
		}
		hashes[hash] = path
	}
}

func TestHashProject_HexFormat(t *testing.T) {
	hash := hashProject("/some/path")

	// Should only contain valid hex characters
	for _, c := range hash {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		assert.True(t, isHex, "hash should only contain hex characters, got: %c", c)
	}
}
