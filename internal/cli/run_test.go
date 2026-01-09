package cli

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/stretchr/testify/assert"
)

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

func TestMapProbeErrorToStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ui.ConnectionStatus
	}{
		{
			name: "nil error returns success",
			err:  nil,
			want: ui.StatusSuccess,
		},
		{
			name: "timeout error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailTimeout,
			},
			want: ui.StatusTimeout,
		},
		{
			name: "refused error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailRefused,
			},
			want: ui.StatusRefused,
		},
		{
			name: "unreachable error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnreachable,
			},
			want: ui.StatusUnreachable,
		},
		{
			name: "auth error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailAuth,
			},
			want: ui.StatusAuthFailed,
		},
		{
			name: "unknown probe error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnknown,
			},
			want: ui.StatusFailed,
		},
		{
			name: "host key error maps to failed",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailHostKey,
			},
			want: ui.StatusFailed,
		},
		{
			name: "generic error returns failed",
			err:  assert.AnError,
			want: ui.StatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapProbeErrorToStatus(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunOptions_Defaults(t *testing.T) {
	opts := RunOptions{}

	assert.Empty(t, opts.Command)
	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.SkipSync)
	assert.False(t, opts.SkipLock)
	assert.False(t, opts.DryRun)
	assert.Empty(t, opts.WorkingDir)
	assert.False(t, opts.Quiet)
}

func TestRunOptions_WithValues(t *testing.T) {
	opts := RunOptions{
		Command:    "make test",
		Host:       "remote-dev",
		Tag:        "fast",
		SkipSync:   true,
		SkipLock:   true,
		DryRun:     true,
		WorkingDir: "/custom/dir",
		Quiet:      true,
	}

	assert.Equal(t, "make test", opts.Command)
	assert.Equal(t, "remote-dev", opts.Host)
	assert.Equal(t, "fast", opts.Tag)
	assert.True(t, opts.SkipSync)
	assert.True(t, opts.SkipLock)
	assert.True(t, opts.DryRun)
	assert.Equal(t, "/custom/dir", opts.WorkingDir)
	assert.True(t, opts.Quiet)
}
