package sshutil

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// skipIfNoSSH skips the test if SSH tests are disabled.
// Tests are skipped by default unless RR_TEST_SSH_HOST is explicitly set.
// Also configures test-friendly settings (e.g., disables strict host key checking
// when RR_TEST_SSH_KEY is set, since CI containers regenerate keys each run).
func skipIfNoSSH(t *testing.T) {
	t.Helper()
	if os.Getenv("RR_TEST_SKIP_SSH") == "1" {
		t.Skip("Skipping SSH test: RR_TEST_SKIP_SSH=1")
	}
	// Also skip if no explicit SSH host is configured (most CI environments)
	if os.Getenv("RR_TEST_SSH_HOST") == "" {
		t.Skip("Skipping SSH test: RR_TEST_SSH_HOST not set")
	}
	// When using a test key (CI environment), disable strict host key checking
	// because Docker containers regenerate host keys on each start
	if os.Getenv("RR_TEST_SSH_KEY") != "" {
		originalValue := StrictHostKeyChecking
		StrictHostKeyChecking = false
		t.Cleanup(func() {
			StrictHostKeyChecking = originalValue
		})
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

func TestDial_Success(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	client, err := Dial(host, 10*time.Second)
	if err != nil {
		t.Fatalf("Dial(%q) failed: %v", host, err)
	}
	defer client.Close()

	if client.Host != host {
		t.Errorf("client.Host = %q, want %q", client.Host, host)
	}

	if client.Address == "" {
		t.Error("client.Address is empty")
	}
}

func TestDial_InvalidHost(t *testing.T) {
	skipIfNoSSH(t)

	// Use a non-routable IP to ensure connection failure
	_, err := Dial("192.0.2.1", 1*time.Second)
	if err == nil {
		t.Fatal("Dial to invalid host should fail")
	}
}

func TestExec_SimpleCommand(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	client, err := Dial(host, 10*time.Second)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer client.Close()

	stdout, stderr, exitCode, err := client.Exec("echo hello")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}

	if !bytes.Contains(stdout, []byte("hello")) {
		t.Errorf("stdout = %q, want to contain 'hello'", stdout)
	}

	if len(stderr) > 0 {
		t.Logf("stderr (possibly expected): %q", stderr)
	}
}

func TestExec_NonZeroExit(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	client, err := Dial(host, 10*time.Second)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer client.Close()

	_, _, exitCode, err := client.Exec("exit 42")
	if err != nil {
		t.Fatalf("Exec failed unexpectedly: %v", err)
	}

	if exitCode != 42 {
		t.Errorf("exitCode = %d, want 42", exitCode)
	}
}

func TestExec_StderrOutput(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	client, err := Dial(host, 10*time.Second)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer client.Close()

	stdout, stderr, exitCode, err := client.Exec("echo out; echo err >&2")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}

	if !bytes.Contains(stdout, []byte("out")) {
		t.Errorf("stdout = %q, want to contain 'out'", stdout)
	}

	if !bytes.Contains(stderr, []byte("err")) {
		t.Errorf("stderr = %q, want to contain 'err'", stderr)
	}
}

func TestExecStream_Success(t *testing.T) {
	skipIfNoSSH(t)

	host := getTestSSHHost()
	client, err := Dial(host, 10*time.Second)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer client.Close()

	var stdout, stderr bytes.Buffer
	exitCode, err := client.ExecStream("echo hello; echo error >&2", &stdout, &stderr)
	if err != nil {
		t.Fatalf("ExecStream failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}

	if !bytes.Contains(stdout.Bytes(), []byte("hello")) {
		t.Errorf("stdout = %q, want to contain 'hello'", stdout.String())
	}

	if !bytes.Contains(stderr.Bytes(), []byte("error")) {
		t.Errorf("stderr = %q, want to contain 'error'", stderr.String())
	}
}

func TestResolveSSHSettings_SimpleHost(t *testing.T) {
	settings := resolveSSHSettings("example.com")

	if settings.hostname != "example.com" {
		t.Errorf("hostname = %q, want 'example.com'", settings.hostname)
	}

	if settings.port != "22" {
		t.Errorf("port = %q, want '22'", settings.port)
	}
}

func TestResolveSSHSettings_UserAtHost(t *testing.T) {
	settings := resolveSSHSettings("testuser@example.com")

	if settings.hostname != "example.com" {
		t.Errorf("hostname = %q, want 'example.com'", settings.hostname)
	}

	if settings.user != "testuser" {
		t.Errorf("user = %q, want 'testuser'", settings.user)
	}
}

func TestResolveSSHSettings_HostWithPort(t *testing.T) {
	settings := resolveSSHSettings("example.com:2222")

	if settings.hostname != "example.com" {
		t.Errorf("hostname = %q, want 'example.com'", settings.hostname)
	}

	if settings.port != "2222" {
		t.Errorf("port = %q, want '2222'", settings.port)
	}
}

func TestResolveSSHSettings_FullFormat(t *testing.T) {
	settings := resolveSSHSettings("admin@server.example.com:2222")

	if settings.hostname != "server.example.com" {
		t.Errorf("hostname = %q, want 'server.example.com'", settings.hostname)
	}

	if settings.user != "admin" {
		t.Errorf("user = %q, want 'admin'", settings.user)
	}

	if settings.port != "2222" {
		t.Errorf("port = %q, want '2222'", settings.port)
	}
}

func TestExpandPath(t *testing.T) {
	home := homeDir()

	tests := []struct {
		input    string
		expected string
	}{
		{"~/test", home + "/test"},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		result := expandPath(tt.input)
		if result != tt.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSuggestionForDialError(t *testing.T) {
	tests := []struct {
		errMsg   string
		contains string
	}{
		{"connection refused", "Is the SSH server running"},
		{"no route to host", "not reachable"},
		{"i/o timeout", "timed out"},
		{"random error", "Check if the host is reachable"},
	}

	for _, tt := range tests {
		suggestion := suggestionForDialError(errFromString(tt.errMsg))
		if suggestion == "" {
			t.Errorf("suggestionForDialError(%q) returned empty string", tt.errMsg)
		}
		if !containsSubstring(suggestion, tt.contains) {
			t.Errorf("suggestionForDialError(%q) = %q, want to contain %q", tt.errMsg, suggestion, tt.contains)
		}
	}
}

func TestSuggestionForHandshakeError(t *testing.T) {
	tests := []struct {
		errMsg   string
		contains string
	}{
		{"unable to authenticate", "Authentication failed"},
		{"host key verification", "Host key verification failed"},
		{"random error", "SSH handshake failed"},
	}

	for _, tt := range tests {
		suggestion := suggestionForHandshakeError(errFromString(tt.errMsg))
		if suggestion == "" {
			t.Errorf("suggestionForHandshakeError(%q) returned empty string", tt.errMsg)
		}
		if !containsSubstring(suggestion, tt.contains) {
			t.Errorf("suggestionForHandshakeError(%q) = %q, want to contain %q", tt.errMsg, suggestion, tt.contains)
		}
	}
}

// Helper to create an error from a string for testing
type stringError string

func (e stringError) Error() string { return string(e) }

func errFromString(s string) error {
	return stringError(s)
}

func containsSubstring(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

func TestClient_Close_NilClient(t *testing.T) {
	c := &Client{
		Client:  nil,
		Host:    "test",
		Address: "test:22",
	}

	err := c.Close()
	if err != nil {
		t.Errorf("Close() on nil client returned error: %v", err)
	}
}

func TestHomeDir(t *testing.T) {
	home := homeDir()

	// Should return a non-empty string
	if home == "" {
		t.Error("homeDir() returned empty string")
	}

	// Should be an absolute path or start with /
	if home[0] != '/' && home[1] != ':' { // Unix or Windows
		t.Errorf("homeDir() returned relative path: %s", home)
	}
}

func TestCurrentUser(t *testing.T) {
	user := currentUser()

	// Should return a non-empty string
	if user == "" {
		t.Error("currentUser() returned empty string")
	}
}

func TestCurrentUser_Fallback(t *testing.T) {
	// Save and clear USER env var
	orig := os.Getenv("USER")
	os.Unsetenv("USER")
	defer os.Setenv("USER", orig)

	user := currentUser()

	// Should fall back to "root"
	if user != "root" {
		t.Errorf("currentUser() with no USER env = %q, want 'root'", user)
	}
}

func TestSshSettings_Address(t *testing.T) {
	tests := []struct {
		name     string
		settings sshSettings
		want     string
	}{
		{
			name:     "default port",
			settings: sshSettings{hostname: "example.com", port: "22"},
			want:     "example.com:22",
		},
		{
			name:     "custom port",
			settings: sshSettings{hostname: "example.com", port: "2222"},
			want:     "example.com:2222",
		},
		{
			name:     "ip address",
			settings: sshSettings{hostname: "192.168.1.1", port: "22"},
			want:     "192.168.1.1:22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.settings.address()
			if got != tt.want {
				t.Errorf("address() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSSHSettings_IPv6Host(t *testing.T) {
	// IPv6 addresses have colons in them, test that port parsing handles this
	settings := resolveSSHSettings("user@::1")

	// Should not confuse IPv6 colons with port separator
	if settings.user != "user" {
		t.Errorf("user = %q, want 'user'", settings.user)
	}
}

func TestExpandPath_Various(t *testing.T) {
	home := homeDir()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "tilde path",
			input: "~/test/path",
			want:  home + "/test/path",
		},
		{
			name:  "absolute path unchanged",
			input: "/absolute/path",
			want:  "/absolute/path",
		},
		{
			name:  "relative path unchanged",
			input: "relative/path",
			want:  "relative/path",
		},
		{
			name:  "just tilde",
			input: "~",
			want:  "~", // Only ~/xxx is expanded
		},
		{
			name:  "tilde in middle unchanged",
			input: "/path/~/test",
			want:  "/path/~/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandPath(tt.input)
			if got != tt.want {
				t.Errorf("expandPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClient_Struct(t *testing.T) {
	c := &Client{
		Client:  nil, // Would be ssh.Client in real use
		Host:    "myserver",
		Address: "192.168.1.100:22",
	}

	if c.Host != "myserver" {
		t.Errorf("Host = %q, want 'myserver'", c.Host)
	}
	if c.Address != "192.168.1.100:22" {
		t.Errorf("Address = %q, want '192.168.1.100:22'", c.Address)
	}
}
