package sshutil

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kevinburke/ssh_config"
	"github.com/rileyhilliard/rr/internal/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Client wraps an SSH connection with additional metadata.
type Client struct {
	*ssh.Client
	Host    string // The original host/alias used to connect
	Address string // The resolved address (host:port)
}

// Dial establishes an SSH connection to the specified host.
// The host can be:
//   - An SSH config alias (e.g., "myserver")
//   - A hostname (e.g., "192.168.1.100")
//   - A user@hostname (e.g., "user@192.168.1.100")
//   - A hostname:port (e.g., "192.168.1.100:2222")
//
// Connection settings are resolved from ~/.ssh/config when available.
func Dial(host string, timeout time.Duration) (*Client, error) {
	// Resolve connection settings from SSH config
	settings := resolveSSHSettings(host)

	// Build SSH client config
	config, err := buildSSHConfig(settings)
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Failed to configure SSH for '%s'", host),
			"Check your SSH keys and SSH agent: ssh-add -l")
	}

	// Dial with timeout
	address := settings.address()
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Failed to connect to '%s' (%s)", host, address),
			suggestionForDialError(err))
	}

	// SSH handshake
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		conn.Close()
		return nil, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("SSH handshake failed for '%s'", host),
			suggestionForHandshakeError(err))
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	return &Client{
		Client:  client,
		Host:    host,
		Address: address,
	}, nil
}

// Close closes the SSH connection.
func (c *Client) Close() error {
	if c.Client == nil {
		return nil
	}
	return c.Client.Close()
}

// GetHost returns the original host/alias used to connect.
func (c *Client) GetHost() string {
	return c.Host
}

// GetAddress returns the resolved host:port address.
func (c *Client) GetAddress() string {
	return c.Address
}

// NewSession creates a new SSH session.
// This satisfies the SSHClient interface for liveness checks.
// Note: The exec methods use c.Client.NewSession() directly to get the full *ssh.Session.
func (c *Client) NewSession() (Session, error) {
	return c.Client.NewSession()
}

// newSSHSession creates a new *ssh.Session for internal use by exec methods.
func (c *Client) newSSHSession() (*ssh.Session, error) {
	return c.Client.NewSession()
}

// sshSettings holds resolved SSH connection parameters.
type sshSettings struct {
	hostname     string
	port         string
	user         string
	identityFile string
}

// address returns the host:port string for dialing.
func (s *sshSettings) address() string {
	return net.JoinHostPort(s.hostname, s.port)
}

// resolveSSHSettings parses the host string and resolves settings from ~/.ssh/config.
func resolveSSHSettings(host string) *sshSettings {
	settings := &sshSettings{
		port: "22",
		user: currentUser(),
	}

	// Parse user@host:port format first (explicit user takes precedence)
	explicitUser := false
	if atIdx := strings.Index(host, "@"); atIdx != -1 {
		settings.user = host[:atIdx]
		host = host[atIdx+1:]
		explicitUser = true
	}

	// Check for test user override (for CI environments)
	// Only applies when no explicit user@host format was used
	if !explicitUser {
		if testUser := os.Getenv("RR_TEST_SSH_USER"); testUser != "" {
			settings.user = testUser
		}
	}

	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		// Check if this looks like a port (all digits after colon)
		potentialPort := host[colonIdx+1:]
		isPort := true
		for _, c := range potentialPort {
			if c < '0' || c > '9' {
				isPort = false
				break
			}
		}
		if isPort && len(potentialPort) > 0 {
			settings.port = potentialPort
			host = host[:colonIdx]
		}
	}

	settings.hostname = host

	// Try to load from SSH config
	sshConfigPath := filepath.Join(homeDir(), ".ssh", "config")
	f, err := os.Open(sshConfigPath)
	if err == nil {
		defer f.Close()
		cfg, err := ssh_config.Decode(f)
		if err == nil {
			// Get hostname (could be different from alias)
			if hostname, _ := cfg.Get(host, "HostName"); hostname != "" {
				settings.hostname = hostname
			}

			// Get port
			if port, _ := cfg.Get(host, "Port"); port != "" {
				settings.port = port
			}

			// Get user
			if user, _ := cfg.Get(host, "User"); user != "" {
				settings.user = user
			}

			// Get identity file
			if identity, _ := cfg.Get(host, "IdentityFile"); identity != "" {
				settings.identityFile = expandPath(identity)
			}
		}
	}

	return settings
}

// StrictHostKeyChecking controls host key verification behavior.
// When true (default), host keys are verified against ~/.ssh/known_hosts.
// When false, host key verification is skipped (insecure, for CI/automation).
var StrictHostKeyChecking = true

// hostKeyCallback is cached to avoid re-parsing known_hosts on every connection.
var (
	hostKeyCallback     ssh.HostKeyCallback
	hostKeyCallbackOnce sync.Once
	hostKeyCallbackErr  error
)

// getHostKeyCallback returns a cached host key callback that reads from known_hosts.
func getHostKeyCallback() (ssh.HostKeyCallback, error) {
	hostKeyCallbackOnce.Do(func() {
		knownHostsPath := filepath.Join(homeDir(), ".ssh", "known_hosts")

		// Check if known_hosts exists
		if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
			// Create an empty known_hosts file if it doesn't exist
			dir := filepath.Dir(knownHostsPath)
			if err := os.MkdirAll(dir, 0700); err != nil {
				hostKeyCallbackErr = fmt.Errorf("failed to create .ssh directory: %w", err)
				return
			}
			if err := os.WriteFile(knownHostsPath, []byte{}, 0600); err != nil {
				hostKeyCallbackErr = fmt.Errorf("failed to create known_hosts: %w", err)
				return
			}
		}

		hostKeyCallback, hostKeyCallbackErr = knownhosts.New(knownHostsPath)
	})

	return hostKeyCallback, hostKeyCallbackErr
}

// buildSSHConfig creates an SSH client config with authentication methods.
func buildSSHConfig(settings *sshSettings) (*ssh.ClientConfig, error) {
	var authMethods []ssh.AuthMethod

	// Try SSH agent first (most common and convenient)
	if agentAuth := sshAgentAuth(); agentAuth != nil {
		authMethods = append(authMethods, agentAuth)
	}

	// Check for test key override (for CI environments)
	if testKey := os.Getenv("RR_TEST_SSH_KEY"); testKey != "" {
		if keyAuth, err := keyFileAuth(testKey); err == nil {
			authMethods = append(authMethods, keyAuth)
		}
	}

	// Try specific identity file from SSH config
	if settings.identityFile != "" {
		if keyAuth, err := keyFileAuth(settings.identityFile); err == nil {
			authMethods = append(authMethods, keyAuth)
		}
	}

	// Try default key files
	defaultKeys := []string{
		filepath.Join(homeDir(), ".ssh", "id_ed25519"),
		filepath.Join(homeDir(), ".ssh", "id_rsa"),
		filepath.Join(homeDir(), ".ssh", "id_ecdsa"),
	}

	for _, keyPath := range defaultKeys {
		if keyPath == settings.identityFile {
			continue // Already tried this one
		}
		if keyAuth, err := keyFileAuth(keyPath); err == nil {
			authMethods = append(authMethods, keyAuth)
		}
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no SSH authentication methods available")
	}

	// Determine host key callback
	var hostKeyCallback ssh.HostKeyCallback
	if StrictHostKeyChecking {
		var err error
		hostKeyCallback, err = getHostKeyCallback()
		if err != nil {
			return nil, fmt.Errorf("failed to load known_hosts: %w", err)
		}
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec // User explicitly disabled host key checking
	}

	return &ssh.ClientConfig{
		User:            settings.user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}, nil
}

// agentConn holds the reusable SSH agent connection.
var (
	agentConn     net.Conn
	agentClient   agent.ExtendedAgent
	agentConnOnce sync.Once
)

// sshAgentAuth returns an auth method using the SSH agent if available.
// The agent connection is reused across multiple SSH connections.
// Returns nil if the agent has no keys loaded.
func sshAgentAuth() ssh.AuthMethod {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil
	}

	agentConnOnce.Do(func() {
		conn, err := net.Dial("unix", socket)
		if err != nil {
			return
		}
		agentConn = conn
		agentClient = agent.NewClient(conn)
	})

	if agentClient == nil {
		return nil
	}

	// Only return agent auth if the agent actually has keys.
	// An empty agent causes auth failures when placed before other methods.
	signers, err := agentClient.Signers()
	if err != nil || len(signers) == 0 {
		return nil
	}

	return ssh.PublicKeysCallback(agentClient.Signers)
}

// CloseAgent closes the SSH agent connection if one is open.
// This should be called when the application is shutting down.
func CloseAgent() {
	if agentConn != nil {
		agentConn.Close()
	}
}

// keyFileAuth returns an auth method using a private key file.
func keyFileAuth(keyPath string) (ssh.AuthMethod, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		// Key might be encrypted - passphrase-protected keys must be loaded via ssh-agent
		if strings.Contains(err.Error(), "encrypted") || strings.Contains(err.Error(), "passphrase") {
			return nil, fmt.Errorf("encrypted key at %s requires ssh-agent: ssh-add %s", keyPath, keyPath)
		}
		return nil, err
	}

	return ssh.PublicKeys(signer), nil
}

// Helper functions

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("HOME")
	}
	return home
}

func currentUser() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	return "root"
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}

func suggestionForDialError(err error) string {
	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") {
		return "Is the SSH server running? Check: ssh <host>"
	}
	if strings.Contains(errStr, "no route to host") || strings.Contains(errStr, "network is unreachable") {
		return "Host is not reachable. Check your network connection."
	}
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "i/o timeout") {
		return "Connection timed out. Check if host is online and firewall allows SSH."
	}
	return "Check if the host is reachable: ping <host>"
}

func suggestionForHandshakeError(err error) string {
	errStr := err.Error()
	if strings.Contains(errStr, "unable to authenticate") || strings.Contains(errStr, "no supported methods") {
		return "Authentication failed. Check your SSH keys: ssh-add -l"
	}
	if strings.Contains(errStr, "host key") {
		return "Host key verification failed. Run: ssh <host> to accept the key."
	}
	return "SSH handshake failed. Try connecting manually: ssh <host>"
}
