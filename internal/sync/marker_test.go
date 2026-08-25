package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func markerTestConn(mock *sshtesting.MockClient) *host.Connection {
	return &host.Connection{
		Name:   "test-host",
		Alias:  "test-host",
		Client: mock,
		Host:   config.Host{Dir: "/root/rr/myapp"},
	}
}

func TestMarkerRemotePath(t *testing.T) {
	conn := &host.Connection{Host: config.Host{Dir: "~/rr/myapp/"}}
	assert.Equal(t, "~/rr/myapp/.rr-source", markerRemotePath(conn))
}

func TestBuildArgs_ProtectsSourceMarker(t *testing.T) {
	conn := &host.Connection{
		Name:  "test-host",
		Alias: "test-alias",
		Host:  config.Host{Dir: "~/rr/myapp"},
	}
	args, err := BuildArgs(conn, t.TempDir(), config.SyncConfig{})
	require.NoError(t, err)
	assert.Contains(t, args, "--filter=P /.rr-source")
}

func TestWriteSourceMarker(t *testing.T) {
	mock := sshtesting.NewMockClient("test-host")
	conn := markerTestConn(mock)
	localDir := t.TempDir()

	writeSourceMarker(conn, localDir)

	data, err := mock.GetFS().ReadFile("/root/rr/myapp/.rr-source")
	require.NoError(t, err)

	var marker SourceMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, filepath.Clean(localDir), marker.SourcePath)
	hostname, _ := os.Hostname()
	assert.Equal(t, hostname, marker.Hostname)
	assert.False(t, marker.SyncedAt.IsZero())
}

func TestWriteSourceMarker_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		writeSourceMarker(nil, "/tmp/x")
		writeSourceMarker(&host.Connection{}, "/tmp/x")
	})
}

func TestCheckSourceMarker(t *testing.T) {
	collect := func() (*SyncOptions, *[]SyncWarning) {
		var warnings []SyncWarning
		opts := &SyncOptions{Warn: func(w SyncWarning) { warnings = append(warnings, w) }}
		return opts, &warnings
	}

	t.Run("no marker means no warning", func(t *testing.T) {
		mock := sshtesting.NewMockClient("test-host")
		opts, warnings := collect()
		checkSourceMarker(markerTestConn(mock), t.TempDir(), opts)
		assert.Empty(t, *warnings)
	})

	t.Run("same source and hostname means no warning", func(t *testing.T) {
		mock := sshtesting.NewMockClient("test-host")
		conn := markerTestConn(mock)
		localDir := t.TempDir()

		writeSourceMarker(conn, localDir)

		opts, warnings := collect()
		checkSourceMarker(conn, localDir, opts)
		assert.Empty(t, *warnings)
	})

	t.Run("different source path warns with details", func(t *testing.T) {
		mock := sshtesting.NewMockClient("test-host")
		conn := markerTestConn(mock)
		hostname, _ := os.Hostname()

		prev := SourceMarker{
			SourcePath: "/Users/other/projects/myapp-worktree",
			Hostname:   hostname,
			Branch:     "feature-x",
			Worktree:   "myapp-worktree",
		}
		data, err := json.Marshal(prev)
		require.NoError(t, err)
		require.NoError(t, mock.GetFS().WriteFile("/root/rr/myapp/.rr-source", data))

		opts, warnings := collect()
		checkSourceMarker(conn, t.TempDir(), opts)

		require.Len(t, *warnings, 1)
		w := (*warnings)[0]
		assert.Equal(t, "source_mismatch", w.Code)
		assert.Contains(t, w.Message, "myapp-worktree")
		assert.Contains(t, w.Message, "feature-x")
		assert.Equal(t, "/Users/other/projects/myapp-worktree", w.Details["previous_source"])
		assert.Equal(t, "feature-x", w.Details["previous_branch"])
	})

	t.Run("different hostname warns", func(t *testing.T) {
		mock := sshtesting.NewMockClient("test-host")
		conn := markerTestConn(mock)
		localDir := t.TempDir()

		prev := SourceMarker{
			SourcePath: filepath.Clean(localDir),
			Hostname:   "someone-elses-laptop",
		}
		data, err := json.Marshal(prev)
		require.NoError(t, err)
		require.NoError(t, mock.GetFS().WriteFile("/root/rr/myapp/.rr-source", data))

		opts, warnings := collect()
		checkSourceMarker(conn, localDir, opts)

		require.Len(t, *warnings, 1)
		assert.Equal(t, "source_mismatch", (*warnings)[0].Code)
	})

	t.Run("unparseable marker is ignored", func(t *testing.T) {
		mock := sshtesting.NewMockClient("test-host")
		conn := markerTestConn(mock)
		require.NoError(t, mock.GetFS().WriteFile("/root/rr/myapp/.rr-source", []byte("not json")))

		opts, warnings := collect()
		checkSourceMarker(conn, t.TempDir(), opts)
		assert.Empty(t, *warnings)
	})

	t.Run("nil options are safe", func(t *testing.T) {
		mock := sshtesting.NewMockClient("test-host")
		assert.NotPanics(t, func() {
			checkSourceMarker(markerTestConn(mock), "/tmp/x", nil)
			checkSourceMarker(nil, "/tmp/x", &SyncOptions{Warn: func(SyncWarning) {}})
		})
	})
}
