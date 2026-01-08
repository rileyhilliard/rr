package sshutil

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kevinburke/ssh_config"
	"github.com/rileyhilliard/rr/internal/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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
		conn.Close() //nolint:errcheck // Best-effort cleanup on error path
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

	// Parse user@host:port format
	if atIdx := strings.Index(host, "@"); atIdx != -1 {
		settings.user = host[:atIdx]
		host = host[atIdx+1:]
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

// buildSSHConfig creates an SSH client config with authentication methods.
func buildSSHConfig(settings *sshSettings) (*ssh.ClientConfig, error) {
	var authMethods []ssh.AuthMethod

	// Try SSH agent first (most common and convenient)
	if agentAuth := sshAgentAuth(); agentAuth != nil {
		authMethods = append(authMethods, agentAuth)
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

	return &ssh.ClientConfig{
		User:            settings.user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: proper host key verification
		Timeout:         10 * time.Second,
	}, nil
}

// sshAgentAuth returns an auth method using the SSH agent if available.
func sshAgentAuth() ssh.AuthMethod {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil
	}

	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers)
}

// keyFileAuth returns an auth method using a private key file.
func keyFileAuth(keyPath string) (ssh.AuthMethod, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		// Key might be encrypted, try without passphrase handling for now
		// TODO: prompt for passphrase or use agent
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
