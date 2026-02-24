package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncState_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	state := &SyncState{
		Branch: "feat",
		Host:   "m4-mini",
		Alias:  "mini-lan",
	}

	err := SaveSyncState(dir, state)
	require.NoError(t, err)

	loaded, err := LoadSyncState(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "feat", loaded.Branch)
	assert.Equal(t, "m4-mini", loaded.Host)
	assert.Equal(t, "mini-lan", loaded.Alias)
}

func TestSyncState_LoadMissing(t *testing.T) {
	dir := t.TempDir()

	loaded, err := LoadSyncState(dir)
	assert.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestSyncState_LoadCorrupt(t *testing.T) {
	dir := t.TempDir()

	// Create the .rr directory and write garbage
	rrDir := filepath.Join(dir, syncStateDir)
	require.NoError(t, os.MkdirAll(rrDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(rrDir, syncStateFile), []byte("not json{{{"), 0644))

	loaded, err := LoadSyncState(dir)
	assert.Error(t, err)
	assert.Nil(t, loaded)
}

func TestSyncState_CreatesDotRRDir(t *testing.T) {
	dir := t.TempDir()

	// Verify .rr/ doesn't exist yet
	rrDir := filepath.Join(dir, syncStateDir)
	_, err := os.Stat(rrDir)
	assert.True(t, os.IsNotExist(err))

	state := &SyncState{Branch: "main", Host: "test", Alias: "test-lan"}
	require.NoError(t, SaveSyncState(dir, state))

	// Verify .rr/ was created
	info, err := os.Stat(rrDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify file exists inside it
	_, err = os.Stat(filepath.Join(rrDir, syncStateFile))
	assert.NoError(t, err)
}

func TestSyncStateChanged_BranchChange(t *testing.T) {
	prev := &SyncState{Branch: "feat-a", Host: "m4-mini", Alias: "mini-lan"}
	curr := &SyncState{Branch: "feat-b", Host: "m4-mini", Alias: "mini-lan"}
	assert.True(t, SyncStateChanged(curr, prev))
}

func TestSyncStateChanged_HostChange(t *testing.T) {
	prev := &SyncState{Branch: "feat", Host: "m4-mini", Alias: "mini-lan"}
	curr := &SyncState{Branch: "feat", Host: "m1-mini", Alias: "m1-vpn"}
	assert.True(t, SyncStateChanged(curr, prev))
}

func TestSyncStateChanged_AliasChangeOnly(t *testing.T) {
	prev := &SyncState{Branch: "feat", Host: "m4-mini", Alias: "mini-lan"}
	curr := &SyncState{Branch: "feat", Host: "m4-mini", Alias: "mini-vpn"}
	assert.True(t, SyncStateChanged(curr, prev))
}

func TestSyncStateChanged_NoChange(t *testing.T) {
	prev := &SyncState{Branch: "feat", Host: "m4-mini", Alias: "mini-lan"}
	curr := &SyncState{Branch: "feat", Host: "m4-mini", Alias: "mini-lan"}
	assert.False(t, SyncStateChanged(curr, prev))
}

func TestSyncStateChanged_NilPrevious(t *testing.T) {
	curr := &SyncState{Branch: "feat", Host: "m4-mini", Alias: "mini-lan"}
	assert.True(t, SyncStateChanged(curr, nil))
}

func TestSyncStateChanged_NilCurrent(t *testing.T) {
	prev := &SyncState{Branch: "feat", Host: "m4-mini", Alias: "mini-lan"}
	assert.True(t, SyncStateChanged(nil, prev))
}
