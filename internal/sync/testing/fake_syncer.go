// Package testing provides test doubles for the sync package.
package testing

import (
	"io"
	"sync"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
)

// SyncCall records a call to the syncer.
type SyncCall struct {
	Conn     *host.Connection
	LocalDir string
	Config   config.SyncConfig
}

// FakeSyncer simulates rsync operations for testing.
// It records calls and returns configured results.
type FakeSyncer struct {
	mu sync.Mutex

	// Configuration
	ShouldFail  bool
	FailError   error
	BytesSynced int64
	FilesCount  int

	// Progress simulation
	ProgressLines []string // Lines to write to progress writer

	// Call tracking
	Calls []SyncCall
}

// NewFakeSyncer creates a new fake syncer that succeeds by default.
func NewFakeSyncer() *FakeSyncer {
	return &FakeSyncer{}
}

// Sync simulates a sync operation.
func (f *FakeSyncer) Sync(conn *host.Connection, localDir string, cfg config.SyncConfig, progress io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Calls = append(f.Calls, SyncCall{
		Conn:     conn,
		LocalDir: localDir,
		Config:   cfg,
	})

	// Write progress if configured
	if progress != nil && len(f.ProgressLines) > 0 {
		for _, line := range f.ProgressLines {
			_, _ = progress.Write([]byte(line + "\n"))
		}
	}

	if f.ShouldFail {
		return f.FailError
	}
	return nil
}

// SetFail configures the syncer to fail with the given error.
func (f *FakeSyncer) SetFail(err error) *FakeSyncer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ShouldFail = true
	f.FailError = err
	return f
}

// SetProgress configures progress lines to emit during sync.
func (f *FakeSyncer) SetProgress(lines ...string) *FakeSyncer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ProgressLines = lines
	return f
}

// AssertSyncCalled returns true if Sync was called at least once.
func (f *FakeSyncer) AssertSyncCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls) > 0
}

// AssertSyncCount returns true if Sync was called exactly n times.
func (f *FakeSyncer) AssertSyncCount(n int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls) == n
}

// LastCall returns the most recent sync call, or nil if none.
func (f *FakeSyncer) LastCall() *SyncCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Calls) == 0 {
		return nil
	}
	call := f.Calls[len(f.Calls)-1]
	return &call
}

// Reset clears all recorded calls.
func (f *FakeSyncer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = nil
}
