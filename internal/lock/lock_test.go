package lock

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockInfo_NewLockInfo(t *testing.T) {
	info, err := NewLockInfo()
	if err != nil {
		t.Fatalf("NewLockInfo() returned error: %v", err)
	}

	if info.User == "" {
		t.Error("Expected User to be set")
	}

	if info.Hostname == "" {
		t.Error("Expected Hostname to be set")
	}

	if info.PID == 0 {
		t.Error("Expected PID to be non-zero")
	}

	if info.Started.IsZero() {
		t.Error("Expected Started to be set")
	}

	// Started should be recent (within last second)
	if time.Since(info.Started) > time.Second {
		t.Error("Expected Started to be within the last second")
	}
}

func TestLockInfo_Age(t *testing.T) {
	info := &LockInfo{
		User:     "testuser",
		Hostname: "testhost",
		Started:  time.Now().Add(-5 * time.Minute),
		PID:      12345,
	}

	age := info.Age()

	// Should be approximately 5 minutes (allow 1 second tolerance)
	if age < 5*time.Minute-time.Second || age > 5*time.Minute+time.Second {
		t.Errorf("Expected age around 5 minutes, got %v", age)
	}
}

func TestLockInfo_Marshal(t *testing.T) {
	info := &LockInfo{
		User:     "testuser",
		Hostname: "testhost",
		Started:  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		PID:      12345,
	}

	data, err := info.Marshal()
	if err != nil {
		t.Fatalf("Marshal() returned error: %v", err)
	}

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Marshal() produced invalid JSON: %v", err)
	}

	// Verify fields are present
	if parsed["user"] != "testuser" {
		t.Errorf("Expected user=testuser, got %v", parsed["user"])
	}
	if parsed["hostname"] != "testhost" {
		t.Errorf("Expected hostname=testhost, got %v", parsed["hostname"])
	}
	if parsed["pid"].(float64) != 12345 {
		t.Errorf("Expected pid=12345, got %v", parsed["pid"])
	}
}

func TestParseLockInfo(t *testing.T) {
	original := &LockInfo{
		User:     "testuser",
		Hostname: "testhost",
		Started:  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		PID:      12345,
	}

	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal() returned error: %v", err)
	}

	parsed, err := ParseLockInfo(data)
	if err != nil {
		t.Fatalf("ParseLockInfo() returned error: %v", err)
	}

	if parsed.User != original.User {
		t.Errorf("User mismatch: got %s, want %s", parsed.User, original.User)
	}
	if parsed.Hostname != original.Hostname {
		t.Errorf("Hostname mismatch: got %s, want %s", parsed.Hostname, original.Hostname)
	}
	if parsed.PID != original.PID {
		t.Errorf("PID mismatch: got %d, want %d", parsed.PID, original.PID)
	}
	if !parsed.Started.Equal(original.Started) {
		t.Errorf("Started mismatch: got %v, want %v", parsed.Started, original.Started)
	}
}

func TestParseLockInfo_InvalidJSON(t *testing.T) {
	_, err := ParseLockInfo([]byte("not valid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestParseLockInfo_EmptyData(t *testing.T) {
	_, err := ParseLockInfo([]byte(""))
	if err == nil {
		t.Error("Expected error for empty data")
	}
}

func TestLockInfo_String(t *testing.T) {
	info := &LockInfo{
		User:     "alice",
		Hostname: "workstation",
		Started:  time.Now(),
		PID:      9876,
	}

	str := info.String()

	expected := "alice@workstation (pid 9876)"
	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{12345, "12345"},
		{-1, "-1"},
		{-123, "-123"},
	}

	for _, tc := range tests {
		result := itoa(tc.input)
		if result != tc.expected {
			t.Errorf("itoa(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestLock_ReleaseNil(t *testing.T) {
	// Release on nil lock should not panic
	var l *Lock
	err := l.Release()
	if err != nil {
		t.Errorf("Release() on nil lock returned error: %v", err)
	}
}

func TestLock_ReleaseEmptyConn(t *testing.T) {
	// Release with nil connection should not panic
	l := &Lock{
		Dir:  "/tmp/test.lock",
		Info: &LockInfo{},
		conn: nil,
	}

	err := l.Release()
	if err != nil {
		t.Errorf("Release() on lock with nil conn returned error: %v", err)
	}
}

// TestStaleDetection tests the stale lock detection logic conceptually.
// The actual isLockStale function requires an SSH connection, so we test
// the LockInfo.Age() function which it relies on.
func TestStaleDetection(t *testing.T) {
	// A lock started 15 minutes ago
	oldInfo := &LockInfo{
		User:     "someone",
		Hostname: "somewhere",
		Started:  time.Now().Add(-15 * time.Minute),
		PID:      1234,
	}

	// With a 10-minute stale threshold, this should be stale
	staleThreshold := 10 * time.Minute
	if oldInfo.Age() <= staleThreshold {
		t.Errorf("Expected age %v to exceed stale threshold %v", oldInfo.Age(), staleThreshold)
	}

	// A lock started 5 minutes ago should not be stale
	recentInfo := &LockInfo{
		User:     "someone",
		Hostname: "somewhere",
		Started:  time.Now().Add(-5 * time.Minute),
		PID:      1234,
	}

	if recentInfo.Age() > staleThreshold {
		t.Errorf("Expected age %v to not exceed stale threshold %v", recentInfo.Age(), staleThreshold)
	}
}

// TestAcquireNoConnection tests that Acquire returns an error when there's no connection.
func TestAcquireNoConnection(t *testing.T) {
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: time.Second,
		Stale:   time.Minute,
		Dir:     "/tmp",
	}

	_, err := Acquire(nil, cfg)
	if err == nil {
		t.Error("Expected error when Acquire called with nil connection")
	}
}

// TestForceReleaseNoConnection tests that ForceRelease returns an error when there's no connection.
func TestForceReleaseNoConnection(t *testing.T) {
	err := ForceRelease(nil, "/tmp/test.lock")
	if err == nil {
		t.Error("Expected error when ForceRelease called with nil connection")
	}
}

// TestHolderNoConnection tests that Holder returns a sensible string when there's no connection.
func TestHolderNoConnection(t *testing.T) {
	result := Holder(nil, "/tmp/test.lock")
	if result != "unknown (no connection)" {
		t.Errorf("Holder() with nil connection = %q, want %q", result, "unknown (no connection)")
	}
}

func TestLockConfig_DefaultDir(t *testing.T) {
	// When Dir is empty, Acquire should default to /tmp
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: time.Second,
		Stale:   time.Minute,
		Dir:     "", // Empty should default to /tmp
	}

	// Can't actually acquire without connection, but we can verify the config is accepted
	_, err := Acquire(nil, cfg)
	if err == nil {
		t.Error("Expected error for nil connection")
	}
	// The error should be about connection, not config
	if !strings.Contains(err.Error(), "Can't grab the lock") {
		t.Errorf("Expected 'Can't grab the lock' error, got: %v", err)
	}
}

func TestLockDir_PerHost(t *testing.T) {
	// Verify lock path format: <dir>/rr.lock (per-host, not per-project)
	cfg := config.LockConfig{
		Dir: "/tmp",
	}

	lockDir := LockDir(cfg)
	expected := "/tmp/rr.lock"

	if lockDir != expected {
		t.Errorf("LockDir() = %q, want %q", lockDir, expected)
	}

	// Test with empty dir (should default to /tmp)
	cfg2 := config.LockConfig{}
	lockDir2 := LockDir(cfg2)
	if lockDir2 != "/tmp/rr.lock" {
		t.Errorf("LockDir() with empty dir = %q, want %q", lockDir2, "/tmp/rr.lock")
	}

	// Test with custom dir
	cfg3 := config.LockConfig{Dir: "/var/locks"}
	lockDir3 := LockDir(cfg3)
	if lockDir3 != "/var/locks/rr.lock" {
		t.Errorf("LockDir() with custom dir = %q, want %q", lockDir3, "/var/locks/rr.lock")
	}
}

func TestLock_Struct(t *testing.T) {
	info := &LockInfo{
		User:     "testuser",
		Hostname: "testhost",
		Started:  time.Now(),
		PID:      12345,
	}

	l := &Lock{
		Dir:  "/tmp/rr-test.lock",
		Info: info,
		conn: nil,
	}

	if l.Dir != "/tmp/rr-test.lock" {
		t.Errorf("Lock.Dir = %q, want %q", l.Dir, "/tmp/rr-test.lock")
	}
	if l.Info != info {
		t.Error("Lock.Info not set correctly")
	}
}

func TestLockInfo_ZeroStartedAge(t *testing.T) {
	// A lock with zero Started time should have large age
	info := &LockInfo{
		User:     "test",
		Hostname: "host",
		Started:  time.Time{}, // zero time
		PID:      1,
	}

	age := info.Age()
	// Zero time is year 1, so age should be huge (many years)
	if age < time.Hour*24*365 {
		t.Errorf("Expected very large age for zero Started time, got %v", age)
	}
}

func TestParseLockInfo_MissingFields(t *testing.T) {
	// JSON with missing fields should still parse
	data := []byte(`{"user": "test"}`)
	info, err := ParseLockInfo(data)
	if err != nil {
		t.Fatalf("ParseLockInfo() returned error: %v", err)
	}

	if info.User != "test" {
		t.Errorf("User = %q, want %q", info.User, "test")
	}
	// Other fields should be zero values
	if info.Hostname != "" {
		t.Errorf("Hostname should be empty, got %q", info.Hostname)
	}
	if info.PID != 0 {
		t.Errorf("PID should be 0, got %d", info.PID)
	}
}

// ============================================================================
// Mock-based tests for SSH-dependent functionality
// ============================================================================

// newMockConnection creates a host.Connection with a mock SSH client.
func newMockConnection(hostName string) (*host.Connection, *sshtesting.MockClient) {
	mock := sshtesting.NewMockClient(hostName)
	conn := &host.Connection{
		Name:   hostName,
		Alias:  hostName,
		Client: mock,
	}
	return conn, mock
}

func TestAcquire_Success(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	lock, err := Acquire(conn, cfg)
	require.NoError(t, err)
	require.NotNil(t, lock)

	// Verify lock dir was created
	assert.True(t, mock.GetFS().IsDir("/tmp/rr.lock"))

	// Verify info file was created
	assert.True(t, mock.GetFS().IsFile("/tmp/rr.lock/info.json"))

	// Verify lock fields
	assert.Equal(t, "/tmp/rr.lock", lock.Dir)
	assert.NotNil(t, lock.Info)
}

func TestAcquire_AlreadyHeld_TimesOut(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Pre-create the lock directory to simulate existing lock
	mock.GetFS().Mkdir("/tmp/rr.lock")

	// Create a recent lock info (not stale)
	info := &LockInfo{
		User:     "other",
		Hostname: "otherhost",
		Started:  time.Now(),
		PID:      9999,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 100 * time.Millisecond, // Very short timeout
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	_, err := Acquire(conn, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Lock timeout")
	assert.Contains(t, err.Error(), "other@otherhost")
}

func TestAcquire_StaleLockRemoved(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Pre-create a stale lock
	mock.GetFS().Mkdir("/tmp/rr.lock")

	// Create an old lock info (stale)
	info := &LockInfo{
		User:     "old",
		Hostname: "oldhost",
		Started:  time.Now().Add(-1 * time.Hour), // 1 hour ago
		PID:      1234,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute, // 10 minutes = stale
		Dir:     "/tmp",
	}

	// Should acquire by removing stale lock
	lock, err := Acquire(conn, cfg)
	require.NoError(t, err)
	require.NotNil(t, lock)

	// Verify we got the lock
	assert.Equal(t, "/tmp/rr.lock", lock.Dir)
}

func TestRelease_Success(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// Acquire first
	lock, err := Acquire(conn, cfg)
	require.NoError(t, err)

	// Verify lock exists
	assert.True(t, mock.GetFS().IsDir("/tmp/rr.lock"))

	// Release
	err = lock.Release()
	require.NoError(t, err)

	// Verify lock is gone
	assert.False(t, mock.GetFS().Exists("/tmp/rr.lock"))
}

func TestForceRelease_Success(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Create a lock manually
	mock.GetFS().Mkdir("/tmp/rr.lock")
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", []byte(`{"user":"someone"}`))

	// Force release
	err := ForceRelease(conn, "/tmp/rr.lock")
	require.NoError(t, err)

	// Verify lock is gone
	assert.False(t, mock.GetFS().Exists("/tmp/rr.lock"))
}

func TestHolder_ReturnsInfo(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Create a lock with info
	mock.GetFS().Mkdir("/tmp/rr.lock")
	info := &LockInfo{
		User:     "alice",
		Hostname: "wonderland",
		Started:  time.Now(),
		PID:      42,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	// Get holder
	holder := Holder(conn, "/tmp/rr.lock")
	assert.Contains(t, holder, "alice")
	assert.Contains(t, holder, "wonderland")
}

func TestHolder_UnknownWhenNoFile(t *testing.T) {
	conn, _ := newMockConnection("testhost")

	holder := Holder(conn, "/tmp/nonexistent.lock")
	assert.Equal(t, "unknown", holder)
}

func TestIsLockStale_True(t *testing.T) {
	mock := sshtesting.NewMockClient("testhost")

	// Create old lock info
	info := &LockInfo{
		User:     "old",
		Hostname: "oldhost",
		Started:  time.Now().Add(-1 * time.Hour),
		PID:      1234,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/info.json", infoJSON)

	// Check staleness with 10-minute threshold
	stale := isLockStale(mock, "/tmp/info.json", 10*time.Minute)
	assert.True(t, stale)
}

func TestIsLockStale_False(t *testing.T) {
	mock := sshtesting.NewMockClient("testhost")

	// Create recent lock info
	info := &LockInfo{
		User:     "recent",
		Hostname: "host",
		Started:  time.Now().Add(-1 * time.Minute),
		PID:      1234,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/info.json", infoJSON)

	// Check staleness with 10-minute threshold
	stale := isLockStale(mock, "/tmp/info.json", 10*time.Minute)
	assert.False(t, stale)
}

func TestIsLockStale_ZeroThreshold(t *testing.T) {
	mock := sshtesting.NewMockClient("testhost")

	// With zero threshold, should always return false
	stale := isLockStale(mock, "/tmp/info.json", 0)
	assert.False(t, stale)
}

func TestIsLockStale_FileNotFound(t *testing.T) {
	mock := sshtesting.NewMockClient("testhost")

	// When file doesn't exist, should return false (not stale)
	stale := isLockStale(mock, "/tmp/nonexistent.json", 10*time.Minute)
	assert.False(t, stale)
}

func TestReadLockHolder_InvalidJSON(t *testing.T) {
	mock := sshtesting.NewMockClient("testhost")

	// Write invalid JSON
	mock.GetFS().WriteFile("/tmp/info.json", []byte("not json {"))

	// Should fall back to raw content
	holder := readLockHolder(mock, "/tmp/info.json")
	assert.Contains(t, holder, "not json")
}

func TestAcquire_CustomDir(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/var/locks", // Custom directory
	}

	lock, err := Acquire(conn, cfg)
	require.NoError(t, err)

	// Verify lock is in custom directory
	assert.True(t, mock.GetFS().IsDir("/var/locks/rr.lock"))
	assert.Equal(t, "/var/locks/rr.lock", lock.Dir)
}

// TestAcquire_CreatesParentDirectory tests that lock acquisition creates
// the parent directory if it doesn't exist (e.g., /tmp/rr-locks).
func TestAcquire_CreatesParentDirectory(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Use a nested custom directory that doesn't exist
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp/rr-locks", // This directory doesn't exist initially
	}

	// Verify parent doesn't exist before acquire
	assert.False(t, mock.GetFS().IsDir("/tmp/rr-locks"))

	lock, err := Acquire(conn, cfg)
	require.NoError(t, err)

	// Verify parent was created
	assert.True(t, mock.GetFS().IsDir("/tmp/rr-locks"))
	// Verify lock dir was created
	assert.True(t, mock.GetFS().IsDir("/tmp/rr-locks/rr.lock"))
	assert.Equal(t, "/tmp/rr-locks/rr.lock", lock.Dir)

	// Cleanup
	err = lock.Release()
	require.NoError(t, err)
	assert.False(t, mock.GetFS().Exists("/tmp/rr-locks/rr.lock"))
}

// TestAcquire_ParentDirFailure tests error handling when parent directory
// creation fails (e.g., permission denied).
func TestAcquire_ParentDirFailure(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Set up mock to fail on mkdir -p for the parent
	mock.SetCommandResponse(`mkdir -p "/nonexistent/path"`, sshtesting.CommandResponse{
		Stderr:   []byte("mkdir: cannot create directory: Permission denied"),
		ExitCode: 1,
	})

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 100 * time.Millisecond,
		Stale:   10 * time.Minute,
		Dir:     "/nonexistent/path",
	}

	_, err := Acquire(conn, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Couldn't create lock directory")
}

// TestAcquire_MkdirFailsWithoutParent tests that mkdir fails correctly
// when the parent directory doesn't exist (simulating real mkdir behavior).
func TestAcquire_MkdirFailsWithoutParent(t *testing.T) {
	// Create a mock that doesn't auto-create /tmp
	mock := sshtesting.NewMockClient("testhost")
	// Manually clear /tmp to simulate a system without it
	mock.GetFS().Remove("/tmp")

	conn := &host.Connection{
		Name:   "testhost",
		Alias:  "testhost",
		Client: mock,
	}

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 100 * time.Millisecond,
		Stale:   10 * time.Minute,
		Dir:     "/tmp", // /tmp doesn't exist in this mock
	}

	// With /tmp removed, mkdir /tmp/rr.lock should fail
	_, err := Acquire(conn, cfg)
	require.Error(t, err)
	// Should timeout because mkdir keeps failing
	assert.Contains(t, err.Error(), "Lock timeout")
}

// TestAcquire_LockLifecycle tests the complete lock lifecycle:
// acquire -> verify -> release -> verify released
func TestAcquire_LockLifecycle(t *testing.T) {
	conn1, mock := newMockConnection("testhost")

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// First acquire should succeed
	lock1, err := Acquire(conn1, cfg)
	require.NoError(t, err)
	require.NotNil(t, lock1)

	// Verify lock directory exists
	assert.True(t, mock.GetFS().IsDir("/tmp/rr.lock"))
	assert.True(t, mock.GetFS().IsFile("/tmp/rr.lock/info.json"))

	// Second acquire on same host should timeout (lock is held)
	conn2 := &host.Connection{
		Name:   "testhost",
		Alias:  "testhost",
		Client: mock,
	}
	cfg2 := cfg
	cfg2.Timeout = 100 * time.Millisecond

	_, err = Acquire(conn2, cfg2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Lock timeout")

	// Release the lock
	err = lock1.Release()
	require.NoError(t, err)

	// Verify lock directory is gone
	assert.False(t, mock.GetFS().Exists("/tmp/rr.lock"))

	// Third acquire should now succeed
	lock3, err := Acquire(conn2, cfg)
	require.NoError(t, err)
	require.NotNil(t, lock3)

	// Cleanup
	lock3.Release()
}

// ============================================================================
// TryAcquire tests
// ============================================================================

func TestTryAcquire_Success(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	lock, err := TryAcquire(conn, cfg)
	require.NoError(t, err)
	require.NotNil(t, lock)

	// Verify lock dir was created
	assert.True(t, mock.GetFS().IsDir("/tmp/rr.lock"))

	// Verify info file was created
	assert.True(t, mock.GetFS().IsFile("/tmp/rr.lock/info.json"))

	// Cleanup
	lock.Release()
}

func TestTryAcquire_ReturnsErrLocked(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Pre-create the lock directory to simulate existing lock
	mock.GetFS().Mkdir("/tmp/rr.lock")

	// Create a recent lock info (not stale)
	info := &LockInfo{
		User:     "other",
		Hostname: "otherhost",
		Started:  time.Now(),
		PID:      9999,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	_, err := TryAcquire(conn, cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLocked)
}

func TestTryAcquire_RemovesStaleLock(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Pre-create a stale lock
	mock.GetFS().Mkdir("/tmp/rr.lock")

	// Create an old lock info (stale)
	info := &LockInfo{
		User:     "old",
		Hostname: "oldhost",
		Started:  time.Now().Add(-1 * time.Hour), // 1 hour ago
		PID:      1234,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute, // 10 minutes = stale
		Dir:     "/tmp",
	}

	// Should acquire by removing stale lock
	lock, err := TryAcquire(conn, cfg)
	require.NoError(t, err)
	require.NotNil(t, lock)

	// Verify we got the lock
	assert.Equal(t, "/tmp/rr.lock", lock.Dir)

	// Cleanup
	lock.Release()
}

func TestTryAcquire_NoConnection(t *testing.T) {
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: time.Second,
		Stale:   time.Minute,
		Dir:     "/tmp",
	}

	_, err := TryAcquire(nil, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Can't grab the lock")
}

func TestTryAcquire_DoesNotBlock(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Pre-create lock
	mock.GetFS().Mkdir("/tmp/rr.lock")
	info := &LockInfo{
		User:     "other",
		Hostname: "otherhost",
		Started:  time.Now(),
		PID:      9999,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Minute, // Long timeout - but TryAcquire should ignore this
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// Measure how long TryAcquire takes
	start := time.Now()
	_, err := TryAcquire(conn, cfg)
	elapsed := time.Since(start)

	// Should return ErrLocked almost immediately (not wait for timeout)
	require.ErrorIs(t, err, ErrLocked)
	assert.Less(t, elapsed, 1*time.Second, "TryAcquire should return immediately, not wait")
}

// ============================================================================
// IsLocked tests
// ============================================================================

func TestIsLocked_True(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Create a lock
	mock.GetFS().Mkdir("/tmp/rr.lock")
	info := &LockInfo{
		User:     "holder",
		Hostname: "holderhost",
		Started:  time.Now(),
		PID:      1234,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	locked := IsLocked(conn, cfg)
	assert.True(t, locked)
}

func TestIsLocked_False_NoLock(t *testing.T) {
	conn, _ := newMockConnection("testhost")

	cfg := config.LockConfig{
		Enabled: true,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	locked := IsLocked(conn, cfg)
	assert.False(t, locked)
}

func TestIsLocked_False_StaleLock(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Create a stale lock
	mock.GetFS().Mkdir("/tmp/rr.lock")
	info := &LockInfo{
		User:     "old",
		Hostname: "oldhost",
		Started:  time.Now().Add(-1 * time.Hour), // 1 hour ago
		PID:      1234,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Stale:   10 * time.Minute, // Lock is older than 10 minutes = stale
		Dir:     "/tmp",
	}

	// Stale locks should not count as locked
	locked := IsLocked(conn, cfg)
	assert.False(t, locked)
}

func TestIsLocked_NoConnection(t *testing.T) {
	cfg := config.LockConfig{
		Enabled: true,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// With nil connection, should return false
	locked := IsLocked(nil, cfg)
	assert.False(t, locked)
}

// ============================================================================
// GetLockHolder tests
// ============================================================================

func TestGetLockHolder_ReturnsHolder(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Create a lock
	mock.GetFS().Mkdir("/tmp/rr.lock")
	info := &LockInfo{
		User:     "alice",
		Hostname: "wonderland",
		Started:  time.Now(),
		PID:      42,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	holder := GetLockHolder(conn, cfg)
	assert.Contains(t, holder, "alice")
	assert.Contains(t, holder, "wonderland")
}

func TestGetLockHolder_EmptyWhenNoLock(t *testing.T) {
	conn, _ := newMockConnection("testhost")

	cfg := config.LockConfig{
		Enabled: true,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	holder := GetLockHolder(conn, cfg)
	assert.Empty(t, holder)
}

func TestGetLockHolder_EmptyWhenStale(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Create a stale lock
	mock.GetFS().Mkdir("/tmp/rr.lock")
	info := &LockInfo{
		User:     "old",
		Hostname: "oldhost",
		Started:  time.Now().Add(-1 * time.Hour),
		PID:      1234,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	// Stale lock should return empty holder
	holder := GetLockHolder(conn, cfg)
	assert.Empty(t, holder)
}
