package lock

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
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
