package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const syncStateDir = ".rr"
const syncStateFile = "sync-state.json"

// SyncState tracks the last successful sync context for git-aware sync.
// Used to detect branch or host changes that require a full sync.
type SyncState struct {
	Branch string `json:"branch"`
	Host   string `json:"host"`
	Alias  string `json:"alias"`
}

// LoadSyncState reads the sync state from <projectRoot>/.rr/sync-state.json.
// Returns nil, nil if the file doesn't exist (first sync).
// Returns nil, error if the file exists but can't be parsed.
func LoadSyncState(projectRoot string) (*SyncState, error) {
	path := filepath.Join(projectRoot, syncStateDir, syncStateFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// SaveSyncState writes the sync state to <projectRoot>/.rr/sync-state.json.
// Creates the .rr/ directory if it doesn't exist.
func SaveSyncState(projectRoot string, state *SyncState) error {
	dir := filepath.Join(projectRoot, syncStateDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, syncStateFile), data, 0644)
}

// SyncStateChanged returns true if the branch, host, or alias differs
// between current and previous state.
func SyncStateChanged(current, previous *SyncState) bool {
	if current == nil || previous == nil {
		return true
	}
	return current.Branch != previous.Branch ||
		current.Host != previous.Host ||
		current.Alias != previous.Alias
}
