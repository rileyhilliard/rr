package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// RequireSSH skips the test if SSH environment variables are not set.
// This is stricter than SkipIfNoSSH - it requires the test SSH server to be available.
func RequireSSH(t *testing.T) {
	t.Helper()
	if os.Getenv("RR_TEST_SSH_HOST") == "" {
		t.Skip("Skipping: RR_TEST_SSH_HOST not set (SSH test server not available)")
	}
	if os.Getenv("RR_TEST_SSH_KEY") == "" {
		t.Skip("Skipping: RR_TEST_SSH_KEY not set (SSH test key not available)")
	}
}

// GetSSHConnection establishes a real SSH connection for integration tests.
// Returns a connection that the caller must close.
func GetSSHConnection(t *testing.T) *host.Connection {
	t.Helper()
	RequireSSH(t)

	// Disable strict host key checking for tests
	sshutil.StrictHostKeyChecking = false
	t.Cleanup(func() {
		sshutil.StrictHostKeyChecking = true
	})

	hostAddr := GetTestSSHHost()
	client, err := sshutil.Dial(hostAddr, 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect to test SSH server: %v", err)
	}

	t.Cleanup(func() {
		client.Close()
	})

	// Parse host and port for rsync configuration
	sshHost, sshPort := parseHostPort(hostAddr)
	sshUser := GetTestSSHUser()
	sshKey := GetTestSSHKey()

	// Build the SSH alias for rsync (user@host format)
	sshAlias := sshHost
	if sshUser != "" {
		sshAlias = sshUser + "@" + sshHost
	}

	// If using non-standard port or explicit key, create SSH config file
	if sshPort != "" || sshKey != "" {
		configPath := setupSSHConfigForTests(t, sshHost, sshPort, sshUser, sshKey)
		sync.SSHConfigFile = configPath
		t.Cleanup(func() {
			sync.SSHConfigFile = ""
		})
	}

	remoteDir := fmt.Sprintf("/tmp/rr-test-%d", time.Now().UnixNano())

	conn := &host.Connection{
		Name:   "test-host",
		Alias:  sshAlias,
		Client: client,
		Host: config.Host{
			Dir: remoteDir,
			SSH: []string{hostAddr},
		},
	}

	return conn
}

// parseHostPort splits a host:port string into its components.
// Returns (host, port) where port may be empty for default port 22.
func parseHostPort(addr string) (string, string) {
	// Handle user@host:port format - strip user first
	if idx := strings.Index(addr, "@"); idx != -1 {
		addr = addr[idx+1:]
	}

	// Split host:port
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx], addr[idx+1:]
	}
	return addr, ""
}

// setupSSHConfigForTests creates a temporary SSH config file for rsync.
// This allows rsync to connect using non-standard ports and explicit keys.
func setupSSHConfigForTests(t *testing.T, sshHost, port, user, keyPath string) string {
	t.Helper()

	// Create temp directory for SSH config
	tmpDir, err := os.MkdirTemp("", "rr-ssh-config-")
	if err != nil {
		t.Fatalf("Failed to create temp dir for SSH config: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	// Build SSH config content
	var config strings.Builder
	config.WriteString(fmt.Sprintf("Host %s\n", sshHost))
	config.WriteString(fmt.Sprintf("  HostName %s\n", sshHost))
	if port != "" {
		config.WriteString(fmt.Sprintf("  Port %s\n", port))
	}
	if user != "" {
		config.WriteString(fmt.Sprintf("  User %s\n", user))
	}
	if keyPath != "" {
		config.WriteString(fmt.Sprintf("  IdentityFile %s\n", keyPath))
	}
	config.WriteString("  StrictHostKeyChecking no\n")
	config.WriteString("  UserKnownHostsFile /dev/null\n")

	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte(config.String()), 0600); err != nil {
		t.Fatalf("Failed to write SSH config: %v", err)
	}

	return configPath
}

// CleanupRemoteDir removes a directory on the remote host.
// Safe to call even if the directory doesn't exist.
func CleanupRemoteDir(t *testing.T, conn *host.Connection, dir string) {
	t.Helper()
	if conn == nil || conn.Client == nil {
		return
	}
	// Use rm -rf to clean up, ignore errors
	_, _, _, _ = conn.Client.Exec(fmt.Sprintf("rm -rf %q", dir))
}

// CreateRemoteFile creates a file with content on the remote host.
func CreateRemoteFile(t *testing.T, conn *host.Connection, path, content string) {
	t.Helper()
	cmd := fmt.Sprintf("mkdir -p \"$(dirname %q)\" && cat > %q << 'EOF'\n%s\nEOF", path, path, content)
	_, stderr, exitCode, err := conn.Client.Exec(cmd)
	if err != nil {
		t.Fatalf("Failed to create remote file %s: %v", path, err)
	}
	if exitCode != 0 {
		t.Fatalf("Failed to create remote file %s: %s", path, string(stderr))
	}
}

// ReadRemoteFile reads a file from the remote host.
func ReadRemoteFile(t *testing.T, conn *host.Connection, path string) string {
	t.Helper()
	stdout, stderr, exitCode, err := conn.Client.Exec(fmt.Sprintf("cat %q", path))
	if err != nil {
		t.Fatalf("Failed to read remote file %s: %v", path, err)
	}
	if exitCode != 0 {
		t.Fatalf("Failed to read remote file %s: %s", path, string(stderr))
	}
	return string(stdout)
}

// RemoteFileExists checks if a file exists on the remote host.
func RemoteFileExists(t *testing.T, conn *host.Connection, path string) bool {
	t.Helper()
	_, _, exitCode, err := conn.Client.Exec(fmt.Sprintf("test -e %q", path))
	if err != nil {
		return false
	}
	return exitCode == 0
}

// RemoteDirExists checks if a directory exists on the remote host.
func RemoteDirExists(t *testing.T, conn *host.Connection, path string) bool {
	t.Helper()
	_, _, exitCode, err := conn.Client.Exec(fmt.Sprintf("test -d %q", path))
	if err != nil {
		return false
	}
	return exitCode == 0
}

// ListRemoteDir lists files in a remote directory.
func ListRemoteDir(t *testing.T, conn *host.Connection, path string) []string {
	t.Helper()
	stdout, _, exitCode, err := conn.Client.Exec(fmt.Sprintf("ls -1 %q 2>/dev/null", path))
	if err != nil || exitCode != 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
