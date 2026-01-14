package testing

import (
	"bytes"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeSyncer_Success(t *testing.T) {
	syncer := NewFakeSyncer()

	conn := &host.Connection{Name: "test-host"}
	err := syncer.Sync(conn, "/local/path", config.SyncConfig{}, nil)

	require.NoError(t, err)
	assert.True(t, syncer.AssertSyncCalled())
	assert.True(t, syncer.AssertSyncCount(1))
}

func TestFakeSyncer_Failure(t *testing.T) {
	expectedErr := errors.New(errors.ErrSync, "sync failed", "try again")
	syncer := NewFakeSyncer().SetFail(expectedErr)

	conn := &host.Connection{Name: "test-host"}
	err := syncer.Sync(conn, "/local/path", config.SyncConfig{}, nil)

	assert.Equal(t, expectedErr, err)
}

func TestFakeSyncer_Progress(t *testing.T) {
	syncer := NewFakeSyncer().SetProgress(
		"sending incremental file list",
		"100%  10.5MB/s",
		"sent 1024 bytes",
	)

	var buf bytes.Buffer
	conn := &host.Connection{Name: "test-host"}
	err := syncer.Sync(conn, "/local/path", config.SyncConfig{}, &buf)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "100%")
	assert.Contains(t, buf.String(), "sent 1024 bytes")
}

func TestFakeSyncer_RecordsCalls(t *testing.T) {
	syncer := NewFakeSyncer()

	conn := &host.Connection{Name: "host-1"}
	cfg := config.SyncConfig{
		Exclude: []string{".git/"},
	}

	err := syncer.Sync(conn, "/project", cfg, nil)
	require.NoError(t, err)

	call := syncer.LastCall()
	require.NotNil(t, call)
	assert.Equal(t, "host-1", call.Conn.Name)
	assert.Equal(t, "/project", call.LocalDir)
	assert.Equal(t, []string{".git/"}, call.Config.Exclude)
}

func TestFakeSyncer_Reset(t *testing.T) {
	syncer := NewFakeSyncer()
	conn := &host.Connection{Name: "test"}

	syncer.Sync(conn, "/path", config.SyncConfig{}, nil)
	assert.True(t, syncer.AssertSyncCalled())

	syncer.Reset()
	assert.False(t, syncer.AssertSyncCalled())
}
