package host

import (
	"os"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
)

// skipIfNoSSH skips the test if SSH tests are disabled.
// Tests are skipped by default unless RR_TEST_SSH_HOST is explicitly set.
func skipIfNoSSH(t *testing.T) {
	t.Helper()
	if os.Getenv("RR_TEST_SKIP_SSH") == "1" {
		t.Skip("Skipping SSH test: RR_TEST_SKIP_SSH=1")
	}
	// Also skip if no explicit SSH host is configured (most CI environments)
	if os.Getenv("RR_TEST_SSH_HOST") == "" {
		t.Skip("Skipping SSH test: RR_TEST_SSH_HOST not set")
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

func TestSelector_Select_FallbackOnFirstAliasFail(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"test": {
			// First alias is unreachable, second should work
			SSH: []string{"192.0.2.1", host},
			Dir: "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(2 * time.Second) // Short timeout for unreachable host
	defer selector.Close()

	// Track events
	var events []ConnectionEvent
	selector.SetEventHandler(func(event ConnectionEvent) {
		events = append(events, event)
	})

	conn, err := selector.Select("test")
	if err != nil {
		t.Fatalf("Select with fallback failed: %v", err)
	}

	// Should have connected via the second alias
	if conn.Alias != host {
		t.Errorf("conn.Alias = %q, want %q (fallback)", conn.Alias, host)
	}

	// Verify events were emitted
	if len(events) < 3 {
		t.Errorf("expected at least 3 events (try, fail, try, connect), got %d", len(events))
	}

	// First event should be trying the first alias
	if len(events) > 0 && events[0].Type != EventTrying {
		t.Errorf("first event type = %v, want EventTrying", events[0].Type)
	}

	// Should have a failure event for the unreachable host
	var sawFailure bool
	for _, e := range events {
		if e.Type == EventFailed && e.Alias == "192.0.2.1" {
			sawFailure = true
			break
		}
	}
	if !sawFailure {
		t.Error("expected EventFailed for unreachable host")
	}

	// Should have a connected event for the working host
	var sawConnected bool
	for _, e := range events {
		if e.Type == EventConnected && e.Alias == host {
			sawConnected = true
			break
		}
	}
	if !sawConnected {
		t.Error("expected EventConnected for working host")
	}
}

func TestSelector_Select_AllAliasesFail(t *testing.T) {
	hosts := map[string]config.Host{
		"test": {
			// All aliases are unreachable
			SSH: []string{"192.0.2.1", "192.0.2.2"},
			Dir: "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(1 * time.Second) // Short timeout
	defer selector.Close()

	// Track events
	var failCount int
	selector.SetEventHandler(func(event ConnectionEvent) {
		if event.Type == EventFailed {
			failCount++
		}
	})

	_, err := selector.Select("test")
	if err == nil {
		t.Fatal("Select should fail when all aliases are unreachable")
	}

	// Should have tried both aliases
	if failCount != 2 {
		t.Errorf("expected 2 failure events, got %d", failCount)
	}
}

func TestSelector_EventHandler_CacheHit(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"test": {SSH: []string{host}},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(10 * time.Second)
	defer selector.Close()

	// First call to establish connection
	_, err := selector.Select("test")
	if err != nil {
		t.Fatalf("First Select failed: %v", err)
	}

	// Track events for second call
	var events []ConnectionEvent
	selector.SetEventHandler(func(event ConnectionEvent) {
		events = append(events, event)
	})

	// Second call should hit cache
	_, err = selector.Select("test")
	if err != nil {
		t.Fatalf("Second Select failed: %v", err)
	}

	// Should have a cache hit event
	var sawCacheHit bool
	for _, e := range events {
		if e.Type == EventCacheHit {
			sawCacheHit = true
			break
		}
	}
	if !sawCacheHit {
		t.Error("expected EventCacheHit for cached connection")
	}
}

func TestConnectionEventType_String(t *testing.T) {
	tests := []struct {
		eventType ConnectionEventType
		expected  string
	}{
		{EventTrying, "trying"},
		{EventFailed, "failed"},
		{EventConnected, "connected"},
		{EventCacheHit, "cache_hit"},
		{ConnectionEventType(99), "unknown"},
	}

	for _, tt := range tests {
		result := tt.eventType.String()
		if result != tt.expected {
			t.Errorf("ConnectionEventType(%d).String() = %q, want %q", tt.eventType, result, tt.expected)
		}
	}
}

func TestSelector_SetEventHandler(t *testing.T) {
	selector := NewSelector(nil)

	var called bool
	selector.SetEventHandler(func(event ConnectionEvent) {
		called = true
	})

	// Emit an event manually to test the handler is set
	selector.emit(ConnectionEvent{Type: EventTrying, Alias: "test"})

	if !called {
		t.Error("event handler was not called")
	}
}

func TestSelector_emit_NoHandler(t *testing.T) {
	selector := NewSelector(nil)
	// Should not panic when no handler is set
	selector.emit(ConnectionEvent{Type: EventTrying, Alias: "test"})
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

func TestSelector_LocalFallback_Enabled(t *testing.T) {
	hosts := map[string]config.Host{
		"test": {
			// All aliases are unreachable
			SSH: []string{"192.0.2.1", "192.0.2.2"},
			Dir: "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(1 * time.Second) // Short timeout
	selector.SetLocalFallback(true)      // Enable local fallback
	defer selector.Close()

	// Track events
	var sawLocalFallback bool
	selector.SetEventHandler(func(event ConnectionEvent) {
		if event.Type == EventLocalFallback {
			sawLocalFallback = true
		}
	})

	conn, err := selector.Select("test")

	// Should succeed with local fallback
	if err != nil {
		t.Fatalf("Select with local fallback should succeed: %v", err)
	}

	// Connection should be local
	if !conn.IsLocal {
		t.Error("expected conn.IsLocal to be true")
	}

	if conn.Name != "local" {
		t.Errorf("conn.Name = %q, want 'local'", conn.Name)
	}

	if conn.Alias != "local" {
		t.Errorf("conn.Alias = %q, want 'local'", conn.Alias)
	}

	if conn.Client != nil {
		t.Error("expected conn.Client to be nil for local connection")
	}

	if !sawLocalFallback {
		t.Error("expected EventLocalFallback event")
	}
}

func TestSelector_LocalFallback_Disabled(t *testing.T) {
	hosts := map[string]config.Host{
		"test": {
			// All aliases are unreachable
			SSH: []string{"192.0.2.1"},
			Dir: "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(1 * time.Second) // Short timeout
	selector.SetLocalFallback(false)     // Disabled (default)
	defer selector.Close()

	_, err := selector.Select("test")

	// Should fail since local fallback is disabled
	if err == nil {
		t.Fatal("Select without local fallback should fail when all hosts unreachable")
	}

	// Error message should suggest enabling local_fallback
	if !containsString(err.Error(), "local_fallback") {
		t.Errorf("error should mention local_fallback: %v", err)
	}
}

func TestSelector_LocalFallback_CachesConnection(t *testing.T) {
	hosts := map[string]config.Host{
		"test": {
			SSH: []string{"192.0.2.1"},
			Dir: "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(1 * time.Second)
	selector.SetLocalFallback(true)
	defer selector.Close()

	// First call
	conn1, err := selector.Select("test")
	if err != nil {
		t.Fatalf("First Select failed: %v", err)
	}

	// Set up event handler for second call
	var sawCacheHit bool
	selector.SetEventHandler(func(event ConnectionEvent) {
		if event.Type == EventCacheHit {
			sawCacheHit = true
		}
	})

	// Second call should return cached local connection
	conn2, err := selector.Select("test")
	if err != nil {
		t.Fatalf("Second Select failed: %v", err)
	}

	if conn1 != conn2 {
		t.Error("Second Select should return cached connection")
	}

	if !sawCacheHit {
		t.Error("expected EventCacheHit for cached local connection")
	}
}

func TestSelector_SetLocalFallback(t *testing.T) {
	selector := NewSelector(nil)

	// Default should be false
	if selector.localFallback {
		t.Error("localFallback should be false by default")
	}

	selector.SetLocalFallback(true)
	if !selector.localFallback {
		t.Error("SetLocalFallback(true) should enable local fallback")
	}

	selector.SetLocalFallback(false)
	if selector.localFallback {
		t.Error("SetLocalFallback(false) should disable local fallback")
	}
}

func TestConnectionEventType_LocalFallback_String(t *testing.T) {
	result := EventLocalFallback.String()
	if result != "local_fallback" {
		t.Errorf("EventLocalFallback.String() = %q, want 'local_fallback'", result)
	}
}

func TestConnection_IsLocal_Field(t *testing.T) {
	// Remote connection
	remoteConn := &Connection{
		Name:    "test",
		Alias:   "test-alias",
		IsLocal: false,
	}
	if remoteConn.IsLocal {
		t.Error("remote connection should have IsLocal = false")
	}

	// Local connection
	localConn := &Connection{
		Name:    "local",
		Alias:   "local",
		IsLocal: true,
	}
	if !localConn.IsLocal {
		t.Error("local connection should have IsLocal = true")
	}
}

func TestSelector_isConnectionAlive_LocalConnection(t *testing.T) {
	selector := NewSelector(nil)

	// Local connection should always be considered alive
	localConn := &Connection{
		Name:    "local",
		Alias:   "local",
		IsLocal: true,
		Client:  nil, // No client for local
	}

	if !selector.isConnectionAlive(localConn) {
		t.Error("local connection should be considered alive")
	}
}

func TestFormatFailedAliases(t *testing.T) {
	tests := []struct {
		aliases  []string
		expected string
	}{
		{[]string{}, "(none)"},
		{[]string{"host1"}, "host1"},
		{[]string{"host1", "host2"}, "host1, host2"},
		{[]string{"a", "b", "c"}, "a, b, c"},
	}

	for _, tt := range tests {
		result := formatFailedAliases(tt.aliases)
		if result != tt.expected {
			t.Errorf("formatFailedAliases(%v) = %q, want %q", tt.aliases, result, tt.expected)
		}
	}
}

func TestHasTag(t *testing.T) {
	tests := []struct {
		tags     []string
		tag      string
		expected bool
	}{
		{[]string{"gpu", "fast", "dev"}, "gpu", true},
		{[]string{"gpu", "fast", "dev"}, "fast", true},
		{[]string{"gpu", "fast", "dev"}, "slow", false},
		{[]string{}, "anything", false},
		{[]string{"production"}, "production", true},
		{[]string{"prod"}, "production", false}, // Exact match required
	}

	for _, tt := range tests {
		result := hasTag(tt.tags, tt.tag)
		if result != tt.expected {
			t.Errorf("hasTag(%v, %q) = %v, want %v", tt.tags, tt.tag, result, tt.expected)
		}
	}
}

func TestFormatTags(t *testing.T) {
	tests := []struct {
		tags     []string
		expected string
	}{
		{[]string{}, "(none)"},
		{[]string{"gpu"}, "gpu"},
		{[]string{"gpu", "fast"}, "gpu, fast"},
		{[]string{"a", "b", "c"}, "a, b, c"},
	}

	for _, tt := range tests {
		result := formatTags(tt.tags)
		if result != tt.expected {
			t.Errorf("formatTags(%v) = %q, want %q", tt.tags, result, tt.expected)
		}
	}
}

func TestSelector_collectTags(t *testing.T) {
	hosts := map[string]config.Host{
		"host1": {SSH: []string{"localhost"}, Tags: []string{"gpu", "fast"}},
		"host2": {SSH: []string{"localhost"}, Tags: []string{"cpu", "fast"}},
		"host3": {SSH: []string{"localhost"}, Tags: []string{}},
	}

	selector := NewSelector(hosts)
	tags := selector.collectTags()

	// Should have all unique tags
	tagMap := make(map[string]bool)
	for _, tag := range tags {
		tagMap[tag] = true
	}

	expectedTags := []string{"gpu", "fast", "cpu"}
	for _, expected := range expectedTags {
		if !tagMap[expected] {
			t.Errorf("collectTags() missing expected tag %q, got %v", expected, tags)
		}
	}

	if len(tags) != 3 {
		t.Errorf("collectTags() returned %d tags, want 3 unique tags", len(tags))
	}
}

func TestSelector_SelectByTag_NoMatchingHosts(t *testing.T) {
	hosts := map[string]config.Host{
		"host1": {SSH: []string{"localhost"}, Tags: []string{"gpu", "fast"}},
		"host2": {SSH: []string{"localhost"}, Tags: []string{"cpu", "slow"}},
	}

	selector := NewSelector(hosts)

	_, err := selector.SelectByTag("nonexistent")
	if err == nil {
		t.Fatal("SelectByTag should fail when no hosts have the requested tag")
	}

	// Error message should mention the tag
	if !containsString(err.Error(), "nonexistent") {
		t.Errorf("error should mention the missing tag: %v", err)
	}
}

func TestSelector_SelectByTag_NoTagsConfigured(t *testing.T) {
	hosts := map[string]config.Host{
		"host1": {SSH: []string{"localhost"}, Tags: []string{}},
		"host2": {SSH: []string{"localhost"}}, // Tags is nil
	}

	selector := NewSelector(hosts)

	_, err := selector.SelectByTag("any")
	if err == nil {
		t.Fatal("SelectByTag should fail when no hosts have tags configured")
	}
}

func TestSelector_SelectByTag_Success(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"gpu-host": {
			SSH:  []string{host},
			Tags: []string{"gpu", "fast"},
			Dir:  "/tmp/test",
		},
		"cpu-host": {
			SSH:  []string{"192.0.2.1"}, // Unreachable
			Tags: []string{"cpu", "slow"},
			Dir:  "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(10 * time.Second)
	defer selector.Close()

	conn, err := selector.SelectByTag("gpu")
	if err != nil {
		t.Fatalf("SelectByTag(\"gpu\") failed: %v", err)
	}

	if conn.Name != "gpu-host" {
		t.Errorf("conn.Name = %q, want 'gpu-host'", conn.Name)
	}

	// Verify the connection works
	if conn.Client == nil {
		t.Error("conn.Client should not be nil")
	}
}

func TestSelector_SelectByTag_MultipleHostsWithTag(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"host1": {
			SSH:  []string{"192.0.2.1"}, // Unreachable
			Tags: []string{"fast"},
			Dir:  "/tmp/test",
		},
		"host2": {
			SSH:  []string{host}, // Reachable
			Tags: []string{"fast"},
			Dir:  "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(2 * time.Second)
	defer selector.Close()

	conn, err := selector.SelectByTag("fast")
	if err != nil {
		t.Fatalf("SelectByTag(\"fast\") failed: %v", err)
	}

	// Should connect to whichever host is reachable
	if conn.Name != "host2" {
		// If host1 was tried first and failed, that's OK
		// The point is we got a connection
		t.Logf("Connected to %s", conn.Name)
	}

	if conn.Client == nil {
		t.Error("conn.Client should not be nil")
	}
}

func TestSelector_SelectByTag_CachesConnection(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	hosts := map[string]config.Host{
		"tagged": {
			SSH:  []string{host},
			Tags: []string{"gpu"},
			Dir:  "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(10 * time.Second)
	defer selector.Close()

	// First call
	conn1, err := selector.SelectByTag("gpu")
	if err != nil {
		t.Fatalf("First SelectByTag failed: %v", err)
	}

	// Second call should return cached connection
	var sawCacheHit bool
	selector.SetEventHandler(func(event ConnectionEvent) {
		if event.Type == EventCacheHit {
			sawCacheHit = true
		}
	})

	conn2, err := selector.Select("") // Use regular Select to check cache
	if err != nil {
		t.Fatalf("Second Select failed: %v", err)
	}

	if conn1 != conn2 {
		t.Error("Second call should return cached connection")
	}

	if !sawCacheHit {
		t.Error("expected EventCacheHit for cached connection")
	}
}

func TestSelector_SelectByTag_LocalFallback(t *testing.T) {
	hosts := map[string]config.Host{
		"unreachable": {
			SSH:  []string{"192.0.2.1"}, // Unreachable
			Tags: []string{"test"},
			Dir:  "/tmp/test",
		},
	}

	selector := NewSelector(hosts)
	selector.SetTimeout(1 * time.Second)
	selector.SetLocalFallback(true) // Enable local fallback
	defer selector.Close()

	conn, err := selector.SelectByTag("test")
	if err != nil {
		t.Fatalf("SelectByTag with local fallback should succeed: %v", err)
	}

	if !conn.IsLocal {
		t.Error("expected conn.IsLocal to be true")
	}

	if conn.Name != "local" {
		t.Errorf("conn.Name = %q, want 'local'", conn.Name)
	}
}
