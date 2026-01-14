// Package testing provides test doubles for the lock package.
package testing

import (
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
)

// AcquireCall records a call to Acquire.
type AcquireCall struct {
	Conn    *host.Connection
	Config  config.LockConfig
	Success bool
}

// FakeLock represents a fake acquired lock.
type FakeLock struct {
	Dir      string
	Holder   string
	Acquired time.Time
	Released bool
	mu       sync.Mutex
}

// Release marks the fake lock as released.
func (l *FakeLock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Released = true
	return nil
}

// FakeLockManager simulates distributed locking for testing.
type FakeLockManager struct {
	mu sync.Mutex

	// Configuration
	ShouldFail     bool
	FailError      error
	SimulatedDelay time.Duration // Delay before acquiring
	HeldBy         string        // If set, lock appears held by this holder
	HeldUntil      time.Time     // If HeldBy is set, lock is held until this time

	// Call tracking
	AcquireCalls []AcquireCall
	ReleaseCalls []string // Lock dirs that were released

	// Active locks
	activeLocks map[string]*FakeLock
}

// NewFakeLockManager creates a new fake lock manager that succeeds by default.
func NewFakeLockManager() *FakeLockManager {
	return &FakeLockManager{
		activeLocks: make(map[string]*FakeLock),
	}
}

// Acquire simulates lock acquisition.
func (m *FakeLockManager) Acquire(conn *host.Connection, cfg config.LockConfig) (*FakeLock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	call := AcquireCall{
		Conn:   conn,
		Config: cfg,
	}

	// Simulate delay
	if m.SimulatedDelay > 0 {
		m.mu.Unlock()
		time.Sleep(m.SimulatedDelay)
		m.mu.Lock()
	}

	if m.ShouldFail {
		call.Success = false
		m.AcquireCalls = append(m.AcquireCalls, call)
		if m.FailError != nil {
			return nil, m.FailError
		}
		return nil, errors.New(errors.ErrLock,
			"Lock acquisition failed",
			"Configured to fail in test")
	}

	// Check if lock is held
	if m.HeldBy != "" && time.Now().Before(m.HeldUntil) {
		call.Success = false
		m.AcquireCalls = append(m.AcquireCalls, call)
		return nil, errors.New(errors.ErrLock,
			"Lock held by "+m.HeldBy,
			"Wait for the lock to be released")
	}

	call.Success = true
	m.AcquireCalls = append(m.AcquireCalls, call)

	hostName := "unknown"
	if conn != nil {
		hostName = conn.Name
	}

	fakeLock := &FakeLock{
		Dir:      "/tmp/rr.lock",
		Holder:   hostName,
		Acquired: time.Now(),
	}
	m.activeLocks[fakeLock.Dir] = fakeLock

	return fakeLock, nil
}

// SetFail configures the manager to fail lock acquisition.
func (m *FakeLockManager) SetFail(err error) *FakeLockManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ShouldFail = true
	m.FailError = err
	return m
}

// SetContention simulates lock contention by another holder.
func (m *FakeLockManager) SetContention(holder string, duration time.Duration) *FakeLockManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.HeldBy = holder
	m.HeldUntil = time.Now().Add(duration)
	return m
}

// SetDelay configures a delay before lock acquisition succeeds.
func (m *FakeLockManager) SetDelay(d time.Duration) *FakeLockManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SimulatedDelay = d
	return m
}

// AssertAcquireCalled returns true if Acquire was called at least once.
func (m *FakeLockManager) AssertAcquireCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.AcquireCalls) > 0
}

// AssertAcquireCount returns true if Acquire was called exactly n times.
func (m *FakeLockManager) AssertAcquireCount(n int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.AcquireCalls) == n
}

// SuccessfulAcquires returns the number of successful lock acquisitions.
func (m *FakeLockManager) SuccessfulAcquires() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, call := range m.AcquireCalls {
		if call.Success {
			count++
		}
	}
	return count
}

// Reset clears all state.
func (m *FakeLockManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AcquireCalls = nil
	m.ReleaseCalls = nil
	m.ShouldFail = false
	m.FailError = nil
	m.HeldBy = ""
	m.activeLocks = make(map[string]*FakeLock)
}

// WrapRealLock wraps the real lock.Acquire function for testing.
// This allows using real locks with mock SSH connections.
func WrapRealLock(l *lock.Lock) *FakeLock {
	if l == nil {
		return nil
	}
	return &FakeLock{
		Dir:      l.Dir,
		Holder:   l.Info.Hostname,
		Acquired: l.Info.Started,
	}
}
