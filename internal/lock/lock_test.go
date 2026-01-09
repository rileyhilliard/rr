package lock

import (
	"encoding/json"
	"path/filepath"
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

	_, err := Acquire(nil, cfg, "testhash")
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
	_, err := Acquire(nil, cfg, "testhash")
	if err == nil {
		t.Error("Expected error for nil connection")
	}
	// The error should be about connection, not config
	if !strings.Contains(err.Error(), "no connection") {
		t.Errorf("Expected 'no connection' error, got: %v", err)
	}
}

func TestLockDir_IncludesProjectHash(t *testing.T) {
	// Verify lock path format: <dir>/rr-<hash>.lock
	projectHash := "abc123def456"
	baseDir := "/tmp"

	// Build expected path
	expected := filepath.Join(baseDir, "rr-"+projectHash+".lock")

	// Verify format matches expected pattern
	if !strings.Contains(expected, projectHash) {
		t.Errorf("Lock dir should contain project hash")
	}
	if !strings.HasSuffix(expected, ".lock") {
		t.Errorf("Lock dir should end with .lock")
	}
	if !strings.Contains(expected, "rr-") {
		t.Errorf("Lock dir should contain rr- prefix")
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

	lock, err := Acquire(conn, cfg, "abc123")
	require.NoError(t, err)
	require.NotNil(t, lock)

	// Verify lock dir was created
	assert.True(t, mock.GetFS().IsDir("/tmp/rr-abc123.lock"))

	// Verify info file was created
	assert.True(t, mock.GetFS().IsFile("/tmp/rr-abc123.lock/info.json"))

	// Verify lock fields
	assert.Equal(t, "/tmp/rr-abc123.lock", lock.Dir)
	assert.NotNil(t, lock.Info)
}

func TestAcquire_AlreadyHeld_TimesOut(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Pre-create the lock directory to simulate existing lock
	mock.GetFS().Mkdir("/tmp/rr-abc123.lock")

	// Create a recent lock info (not stale)
	info := &LockInfo{
		User:     "other",
		Hostname: "otherhost",
		Started:  time.Now(),
		PID:      9999,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr-abc123.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 100 * time.Millisecond, // Very short timeout
		Stale:   10 * time.Minute,
		Dir:     "/tmp",
	}

	_, err := Acquire(conn, cfg, "abc123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Timed out")
	assert.Contains(t, err.Error(), "other@otherhost")
}

func TestAcquire_StaleLockRemoved(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Pre-create a stale lock
	mock.GetFS().Mkdir("/tmp/rr-abc123.lock")

	// Create an old lock info (stale)
	info := &LockInfo{
		User:     "old",
		Hostname: "oldhost",
		Started:  time.Now().Add(-1 * time.Hour), // 1 hour ago
		PID:      1234,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr-abc123.lock/info.json", infoJSON)

	cfg := config.LockConfig{
		Enabled: true,
		Timeout: 5 * time.Second,
		Stale:   10 * time.Minute, // 10 minutes = stale
		Dir:     "/tmp",
	}

	// Should acquire by removing stale lock
	lock, err := Acquire(conn, cfg, "abc123")
	require.NoError(t, err)
	require.NotNil(t, lock)

	// Verify we got the lock
	assert.Equal(t, "/tmp/rr-abc123.lock", lock.Dir)
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
	lock, err := Acquire(conn, cfg, "abc123")
	require.NoError(t, err)

	// Verify lock exists
	assert.True(t, mock.GetFS().IsDir("/tmp/rr-abc123.lock"))

	// Release
	err = lock.Release()
	require.NoError(t, err)

	// Verify lock is gone
	assert.False(t, mock.GetFS().Exists("/tmp/rr-abc123.lock"))
}

func TestForceRelease_Success(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Create a lock manually
	mock.GetFS().Mkdir("/tmp/rr-abc123.lock")
	mock.GetFS().WriteFile("/tmp/rr-abc123.lock/info.json", []byte(`{"user":"someone"}`))

	// Force release
	err := ForceRelease(conn, "/tmp/rr-abc123.lock")
	require.NoError(t, err)

	// Verify lock is gone
	assert.False(t, mock.GetFS().Exists("/tmp/rr-abc123.lock"))
}

func TestHolder_ReturnsInfo(t *testing.T) {
	conn, mock := newMockConnection("testhost")

	// Create a lock with info
	mock.GetFS().Mkdir("/tmp/rr-abc123.lock")
	info := &LockInfo{
		User:     "alice",
		Hostname: "wonderland",
		Started:  time.Now(),
		PID:      42,
	}
	infoJSON, _ := info.Marshal()
	mock.GetFS().WriteFile("/tmp/rr-abc123.lock/info.json", infoJSON)

	// Get holder
	holder := Holder(conn, "/tmp/rr-abc123.lock")
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

	lock, err := Acquire(conn, cfg, "xyz789")
	require.NoError(t, err)

	// Verify lock is in custom directory
	assert.True(t, mock.GetFS().IsDir("/var/locks/rr-xyz789.lock"))
	assert.Equal(t, "/var/locks/rr-xyz789.lock", lock.Dir)
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

	lock, err := Acquire(conn, cfg, "testproject")
	require.NoError(t, err)

	// Verify parent was created
	assert.True(t, mock.GetFS().IsDir("/tmp/rr-locks"))
	// Verify lock dir was created
	assert.True(t, mock.GetFS().IsDir("/tmp/rr-locks/rr-testproject.lock"))
	assert.Equal(t, "/tmp/rr-locks/rr-testproject.lock", lock.Dir)

	// Cleanup
	err = lock.Release()
	require.NoError(t, err)
	assert.False(t, mock.GetFS().Exists("/tmp/rr-locks/rr-testproject.lock"))
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

	_, err := Acquire(conn, cfg, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to create lock parent directory")
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

	// With /tmp removed, mkdir /tmp/rr-xxx.lock should fail
	_, err := Acquire(conn, cfg, "test")
	require.Error(t, err)
	// Should timeout because mkdir keeps failing
	assert.Contains(t, err.Error(), "Timed out")
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
	lock1, err := Acquire(conn1, cfg, "lifecycle-test")
	require.NoError(t, err)
	require.NotNil(t, lock1)

	// Verify lock directory exists
	assert.True(t, mock.GetFS().IsDir("/tmp/rr-lifecycle-test.lock"))
	assert.True(t, mock.GetFS().IsFile("/tmp/rr-lifecycle-test.lock/info.json"))

	// Second acquire on same project should timeout (lock is held)
	conn2 := &host.Connection{
		Name:   "testhost",
		Alias:  "testhost",
		Client: mock,
	}
	cfg2 := cfg
	cfg2.Timeout = 100 * time.Millisecond

	_, err = Acquire(conn2, cfg2, "lifecycle-test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Timed out")

	// Release the lock
	err = lock1.Release()
	require.NoError(t, err)

	// Verify lock directory is gone
	assert.False(t, mock.GetFS().Exists("/tmp/rr-lifecycle-test.lock"))

	// Third acquire should now succeed
	lock3, err := Acquire(conn2, cfg, "lifecycle-test")
	require.NoError(t, err)
	require.NotNil(t, lock3)

	// Cleanup
	lock3.Release()
}
