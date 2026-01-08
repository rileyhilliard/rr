package host

import (
	"os"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
)

// skipIfNoSSH skips the test if SSH tests are disabled.
func skipIfNoSSH(t *testing.T) {
	t.Helper()
	if os.Getenv("RR_TEST_SKIP_SSH") == "1" {
		t.Skip("Skipping SSH test: RR_TEST_SKIP_SSH=1")
	}
}

// getTestSSHHost returns the SSH host for testing.
func getTestSSHHost() string {
	host := os.Getenv("RR_TEST_SSH_HOST")
	if host == "" {
		return "localhost"
	}
	return host
}

func TestProbeFailReason_String(t *testing.T) {
	tests := []struct {
		reason   ProbeFailReason
		expected string
	}{
		{ProbeFailTimeout, "connection timed out"},
		{ProbeFailRefused, "connection refused"},
		{ProbeFailUnreachable, "host unreachable"},
		{ProbeFailAuth, "authentication failed"},
		{ProbeFailHostKey, "host key verification failed"},
		{ProbeFailUnknown, "unknown error"},
	}

	for _, tt := range tests {
		result := tt.reason.String()
		if result != tt.expected {
			t.Errorf("ProbeFailReason(%d).String() = %q, want %q", tt.reason, result, tt.expected)
		}
	}
}

func TestProbeError_Error(t *testing.T) {
	err := &ProbeError{
		SSHAlias: "test-host",
		Reason:   ProbeFailTimeout,
		Cause:    nil,
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("ProbeError.Error() returned empty string")
	}

	if !containsString(errStr, "test-host") {
		t.Errorf("error string should contain host name: %q", errStr)
	}

	if !containsString(errStr, "timed out") {
		t.Errorf("error string should contain reason: %q", errStr)
	}
}

func TestProbe_Success(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	latency, err := Probe(host, 10*time.Second)
	if err != nil {
		t.Fatalf("Probe(%q) failed: %v", host, err)
	}

	if latency <= 0 {
		t.Errorf("latency = %v, want > 0", latency)
	}

	t.Logf("Probe latency: %v", latency)
}

func TestProbe_InvalidHost(t *testing.T) {
	skipIfNoSSH(t)

	// Use a non-routable IP to ensure failure
	_, err := Probe("192.0.2.1", 1*time.Second)
	if err == nil {
		t.Fatal("Probe to invalid host should fail")
	}

	// Should be a ProbeError
	probeErr, ok := err.(*ProbeError)
	if !ok {
		t.Fatalf("error should be *ProbeError, got %T", err)
	}

	if probeErr.SSHAlias != "192.0.2.1" {
		t.Errorf("ProbeError.SSHAlias = %q, want '192.0.2.1'", probeErr.SSHAlias)
	}
}

func TestFirstReachable_Success(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	// First alias is unreachable, second should work
	aliases := []string{"192.0.2.1", host}

	result, err := FirstReachable(aliases, 2*time.Second)
	if err != nil {
		t.Fatalf("FirstReachable failed: %v", err)
	}

	if result.SSHAlias != host {
		t.Errorf("result.SSHAlias = %q, want %q", result.SSHAlias, host)
	}

	if !result.Success {
		t.Error("result.Success should be true")
	}
}

func TestFirstReachable_AllFail(t *testing.T) {
	skipIfNoSSH(t)

	// All aliases are unreachable
	aliases := []string{"192.0.2.1", "192.0.2.2"}

	_, err := FirstReachable(aliases, 1*time.Second)
	if err == nil {
		t.Fatal("FirstReachable should fail when all aliases are unreachable")
	}
}

func TestFirstReachable_EmptyList(t *testing.T) {
	_, err := FirstReachable([]string{}, 1*time.Second)
	if err == nil {
		t.Fatal("FirstReachable should fail with empty list")
	}
}

func TestNewSelector(t *testing.T) {
	hosts := map[string]config.Host{
		"test": {SSH: []string{"localhost"}},
	}

	selector := NewSelector(hosts)
	if selector == nil {
		t.Fatal("NewSelector returned nil")
	}

	if selector.timeout != DefaultProbeTimeout {
		t.Errorf("timeout = %v, want %v", selector.timeout, DefaultProbeTimeout)
	}
}

func TestSelector_SetTimeout(t *testing.T) {
	selector := NewSelector(nil)
	newTimeout := 30 * time.Second

	selector.SetTimeout(newTimeout)

	if selector.timeout != newTimeout {
		t.Errorf("timeout = %v, want %v", selector.timeout, newTimeout)
	}
}

func TestSelector_Select_NoHosts(t *testing.T) {
	selector := NewSelector(map[string]config.Host{})

	_, err := selector.Select("")
	if err == nil {
		t.Fatal("Select should fail with no hosts configured")
	}
}

func TestSelector_Select_HostNotFound(t *testing.T) {
	hosts := map[string]config.Host{
		"existing": {SSH: []string{"localhost"}},
	}

	selector := NewSelector(hosts)

	_, err := selector.Select("nonexistent")
	if err == nil {
		t.Fatal("Select should fail when host not found")
	}
}

func TestSelector_Select_NoSSHAliases(t *testing.T) {
	hosts := map[string]config.Host{
		"empty": {SSH: []string{}},
	}

	selector := NewSelector(hosts)

	_, err := selector.Select("empty")
	if err == nil {
		t.Fatal("Select should fail when host has no SSH aliases")
	}
}

func TestSelector_Select_Success(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"test": {
			SSH: []string{host},
			Dir: "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(10 * time.Second)
	defer selector.Close()

	conn, err := selector.Select("test")
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if conn.Name != "test" {
		t.Errorf("conn.Name = %q, want 'test'", conn.Name)
	}

	if conn.Alias != host {
		t.Errorf("conn.Alias = %q, want %q", conn.Alias, host)
	}

	if conn.Client == nil {
		t.Error("conn.Client is nil")
	}

	if conn.Latency <= 0 {
		t.Errorf("conn.Latency = %v, want > 0", conn.Latency)
	}
}

func TestSelector_Select_CachesConnection(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"test": {SSH: []string{host}},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(10 * time.Second)
	defer selector.Close()

	// First call
	conn1, err := selector.Select("test")
	if err != nil {
		t.Fatalf("First Select failed: %v", err)
	}

	// Second call should return cached connection
	conn2, err := selector.Select("test")
	if err != nil {
		t.Fatalf("Second Select failed: %v", err)
	}

	if conn1 != conn2 {
		t.Error("Second Select should return cached connection")
	}
}

func TestSelector_Select_DefaultHost(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"default": {SSH: []string{host}},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(10 * time.Second)
	defer selector.Close()

	// Select without specifying a host should use first available
	conn, err := selector.Select("")
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if conn.Name != "default" {
		t.Errorf("conn.Name = %q, want 'default'", conn.Name)
	}
}

func TestSelector_Close(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"test": {SSH: []string{host}},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(10 * time.Second)

	// Create a connection
	_, err := selector.Select("test")
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	// Close should not return an error
	err = selector.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Cache should be cleared
	if selector.GetCached() != nil {
		t.Error("cached connection should be nil after Close")
	}
}

func TestSelector_GetCached(t *testing.T) {
	hosts := map[string]config.Host{
		"test": {SSH: []string{"localhost"}},
	}

	selector := NewSelector(hosts)

	// Before any connection
	if selector.GetCached() != nil {
		t.Error("GetCached should return nil before any connection")
	}
}

func TestConnection_Close(t *testing.T) {
	// Test with nil client
	conn := &Connection{
		Name:   "test",
		Client: nil,
	}

	err := conn.Close()
	if err != nil {
		t.Errorf("Close with nil client should not return error: %v", err)
	}
}

func TestQuickSelect_Success(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"test": {SSH: []string{host}},
	}

	conn, err := QuickSelect(hosts, "test")
	if err != nil {
		t.Fatalf("QuickSelect failed: %v", err)
	}
	defer conn.Close()

	if conn.Name != "test" {
		t.Errorf("conn.Name = %q, want 'test'", conn.Name)
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsBytes([]byte(s), []byte(substr)))
}

func containsBytes(s, substr []byte) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if string(s[i:i+len(substr)]) == string(substr) {
			return true
		}
	}
	return false
}
