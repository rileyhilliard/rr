package sshutil

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
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

// matchWarningOnce ensures the SSH config Match directive warning is only shown once per process.
var matchWarningOnce sync.Once

// WarningHandler is a function that handles warning messages.
// If nil, warnings are printed to stderr via log.Printf.
var WarningHandler func(message string)

// emitWarning sends a warning through the configured handler or falls back to log.Printf.
func emitWarning(message string) {
	if WarningHandler != nil {
		WarningHandler(message)
	} else {
		log.Printf("Warning: %s", message)
	}
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
		// If buildSSHConfig already returned a structured error, pass it through
		var rrErr *errors.Error
		if stderrors.As(err, &rrErr) {
			return nil, err
		}
		return nil, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Couldn't set up SSH for '%s'", host),
			"Check your keys are loaded: ssh-add -l")
	}

	// Dial with timeout
	address := settings.address()
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Can't reach '%s' at %s", host, address),
			suggestionForDialError(err))
	}

	// SSH handshake
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		conn.Close()

		// Check for host key mismatch error (provides detailed suggestion)
		var hostKeyErr *HostKeyMismatchError
		if stderrors.As(err, &hostKeyErr) {
			return nil, errors.New(errors.ErrSSH,
				hostKeyErr.Error(),
				hostKeyErr.Suggestion())
		}

		// Build suggestion, with extra context if we found encrypted keys
		suggestion := suggestionForHandshakeError(err, settings.encryptedKeys)

		return nil, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("SSH handshake with '%s' didn't go through", host),
			suggestion)
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

// SendRequest sends a global request on the SSH connection.
// This is a lightweight way to check connection liveness without the overhead
// of creating a new session (~100-200ms savings).
func (c *Client) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	return c.Client.SendRequest(name, wantReply, payload)
}

// newSSHSession creates a new *ssh.Session for internal use by exec methods.
func (c *Client) newSSHSession() (*ssh.Session, error) {
	return c.Client.NewSession()
}

// sshSettings holds resolved SSH connection parameters.
type sshSettings struct {
	hostname      string
	port          string
	user          string
	identityFile  string
	encryptedKeys []string // Keys that exist but are encrypted
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

	// First, try to preprocess the config to handle Match directives
	// The kevinburke/ssh_config library doesn't support Match, so we need to
	// strip them out and only parse content before the first Match block
	content, matchLine, err := preprocessSSHConfig(sshConfigPath)
	if err != nil {
		// Config doesn't exist or can't be read, that's fine
		return settings
	}

	cfg, err := ssh_config.Decode(bytes.NewReader(content))
	if err != nil {
		// Decoding failed even after preprocessing, just return defaults
		return settings
	}

	// Track if we found any config for this host
	hostFound := false

	// Get hostname (could be different from alias)
	if hostname, _ := cfg.Get(host, "HostName"); hostname != "" {
		settings.hostname = hostname
		hostFound = true
	}

	// Get port
	if port, _ := cfg.Get(host, "Port"); port != "" {
		settings.port = port
		hostFound = true
	}

	// Get user
	if user, _ := cfg.Get(host, "User"); user != "" {
		settings.user = user
		hostFound = true
	}

	// Get identity file
	if identity, _ := cfg.Get(host, "IdentityFile"); identity != "" {
		settings.identityFile = expandPath(identity)
		hostFound = true
	}

	// Only warn about Match block if host wasn't found - it might be defined after the Match
	if matchLine > 0 && !hostFound {
		matchWarningOnce.Do(func() {
			emitWarning(fmt.Sprintf(
				"Host '%s' not found in SSH config (config has a Match block at line %d that may hide later entries). "+
					"If this host is defined after line %d, move it earlier in ~/.ssh/config.",
				host, matchLine, matchLine))
		})
	}

	return settings
}

// StrictHostKeyChecking controls host key verification behavior.
// When true (default), host keys are verified against ~/.ssh/known_hosts.
// When false, host key verification is skipped (insecure, for CI/automation).
var StrictHostKeyChecking = true

// buildSSHConfig creates an SSH client config with authentication methods.
// It also populates settings.encryptedKeys with any keys that exist but are encrypted.
func buildSSHConfig(settings *sshSettings) (*ssh.ClientConfig, error) {
	var authMethods []ssh.AuthMethod

	// Helper to try loading a key and track encrypted keys
	tryKeyFile := func(keyPath string) {
		keyAuth, err := keyFileAuth(keyPath)
		if err != nil {
			var encErr *EncryptedKeyError
			if stderrors.As(err, &encErr) {
				settings.encryptedKeys = append(settings.encryptedKeys, keyPath)
			}
			// Other errors (file not found, etc.) are silently ignored
			return
		}
		authMethods = append(authMethods, keyAuth)
	}

	// Try SSH agent first (most common and convenient)
	if agentAuth := sshAgentAuth(); agentAuth != nil {
		authMethods = append(authMethods, agentAuth)
	}

	// Check for test key override (for CI environments)
	if testKey := os.Getenv("RR_TEST_SSH_KEY"); testKey != "" {
		tryKeyFile(testKey)
	}

	// Try specific identity file from SSH config
	if settings.identityFile != "" {
		tryKeyFile(settings.identityFile)
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
		tryKeyFile(keyPath)
	}

	if len(authMethods) == 0 {
		msg := "No SSH auth methods available"
		suggestion := "Check your keys are loaded: ssh-add -l"

		if len(settings.encryptedKeys) > 0 {
			msg = fmt.Sprintf("Found SSH key(s) but they're encrypted: %s", strings.Join(settings.encryptedKeys, ", "))
			var sb strings.Builder
			sb.WriteString("Add your key(s) to the agent:\n")
			for _, key := range settings.encryptedKeys {
				if runtime.GOOS == "darwin" {
					sb.WriteString(fmt.Sprintf("  ssh-add --apple-use-keychain %s\n", key))
				} else {
					sb.WriteString(fmt.Sprintf("  ssh-add %s\n", key))
				}
			}
			sb.WriteString("\nNot sure which key? Check with: ssh -v <host>")
			suggestion = sb.String()
		}

		return nil, errors.New(errors.ErrSSH, msg, suggestion)
	}

	// Determine host key callback
	var hostKeyCallback ssh.HostKeyCallback
	if StrictHostKeyChecking {
		knownHostsPath := filepath.Join(homeDir(), ".ssh", "known_hosts")
		var err error
		hostKeyCallback, err = createHostKeyCallback(knownHostsPath)
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
// Returns EncryptedKeyError if the key requires a passphrase.
func keyFileAuth(keyPath string) (ssh.AuthMethod, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		// Check if key is encrypted (requires passphrase)
		// This can be detected either from the error message or by checking PEM headers
		if strings.Contains(err.Error(), "encrypted") ||
			strings.Contains(err.Error(), "passphrase") ||
			isEncryptedPEM(key) {
			return nil, &EncryptedKeyError{Path: keyPath}
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
		return "Is SSH running on that box? Try: ssh <host>"
	}
	if strings.Contains(errStr, "no route to host") || strings.Contains(errStr, "network is unreachable") {
		return "Can't route to the host. Check your network connection."
	}
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "i/o timeout") {
		return "Connection timed out. Host might be offline or blocked by a firewall."
	}
	return "Make sure the host is reachable: ping <host>"
}

func suggestionForHandshakeError(err error, encryptedKeys []string) string {
	errStr := err.Error()
	if strings.Contains(errStr, "unable to authenticate") || strings.Contains(errStr, "no supported methods") {
		// If we found encrypted keys, suggest adding them to the agent
		if len(encryptedKeys) > 0 {
			var sb strings.Builder
			sb.WriteString("Your key(s) are encrypted. Add them to the agent:\n")
			for _, key := range encryptedKeys {
				if runtime.GOOS == "darwin" {
					sb.WriteString(fmt.Sprintf("  ssh-add --apple-use-keychain %s\n", key))
				} else {
					sb.WriteString(fmt.Sprintf("  ssh-add %s\n", key))
				}
			}
			sb.WriteString("\nNot sure which key? Check with: ssh -v <host>")
			return sb.String()
		}
		return "Auth failed. Check your keys are loaded: ssh-add -l"
	}
	if strings.Contains(errStr, "host key") {
		return "Host key issue. Try connecting manually first: ssh <host>"
	}
	return "Something went wrong during SSH setup. Try: ssh <host>"
}

// EncryptedKeyError is returned when an SSH key requires a passphrase.
type EncryptedKeyError struct {
	Path string
}

func (e *EncryptedKeyError) Error() string {
	return fmt.Sprintf("SSH key at %s is encrypted (passphrase protected)", e.Path)
}

// HostKeyMismatchError provides helpful context when known_hosts verification fails.
type HostKeyMismatchError struct {
	Hostname     string
	ReceivedType string
	KnownHosts   string
	Want         []knownhosts.KnownKey
}

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("host key mismatch for %s: server sent %s key", e.Hostname, e.ReceivedType)
}

// Suggestion returns actionable steps to fix the host key mismatch.
func (e *HostKeyMismatchError) Suggestion() string {
	host := e.Hostname
	// Strip port if present (e.g., "host:22" -> "host")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	var wantTypes []string
	for _, k := range e.Want {
		wantTypes = append(wantTypes, k.Key.Type())
	}
	wantStr := "unknown"
	if len(wantTypes) > 0 {
		wantStr = strings.Join(wantTypes, ", ")
	}

	return fmt.Sprintf(
		"The server's host key doesn't match what's in known_hosts.\n"+
			"  Known types: %s\n"+
			"  Server sent: %s\n\n"+
			"  To update known_hosts with all key types:\n"+
			"    ssh-keyscan -t rsa,ecdsa,ed25519 %s >> %s\n\n"+
			"  Or remove the old entry:\n"+
			"    ssh-keygen -R %s",
		wantStr, e.ReceivedType, host, e.KnownHosts, host)
}

// preprocessSSHConfig reads the SSH config and returns content up to the first Match directive.
// Returns the original content if no Match directive is found.
// Also returns the line number where Match was found (0 if not found).
func preprocessSSHConfig(configPath string) ([]byte, int, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, 0, err
	}

	lines := strings.Split(string(content), "\n")
	var result []string
	matchLine := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Match directive check (case insensitive)
		if strings.HasPrefix(strings.ToLower(trimmed), "match ") {
			matchLine = i + 1 // 1-indexed line number
			break
		}
		result = append(result, line)
	}

	return []byte(strings.Join(result, "\n")), matchLine, nil
}

// isEncryptedPEM checks if PEM data contains encryption markers.
func isEncryptedPEM(data []byte) bool {
	return bytes.Contains(data, []byte("ENCRYPTED")) ||
		bytes.Contains(data, []byte("Proc-Type: 4,ENCRYPTED"))
}

// createHostKeyCallback wraps the knownhosts callback to provide better error messages.
func createHostKeyCallback(knownHostsPath string) (ssh.HostKeyCallback, error) {
	// Check if known_hosts exists, create if it doesn't
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		dir := filepath.Dir(knownHostsPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create .ssh directory: %w", err)
		}
		if err := os.WriteFile(knownHostsPath, []byte{}, 0600); err != nil {
			return nil, fmt.Errorf("failed to create known_hosts: %w", err)
		}
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		if err != nil {
			// Check if this is a key mismatch error from knownhosts
			var keyErr *knownhosts.KeyError
			if stderrors.As(err, &keyErr) && len(keyErr.Want) > 0 {
				return &HostKeyMismatchError{
					Hostname:     hostname,
					ReceivedType: key.Type(),
					KnownHosts:   knownHostsPath,
					Want:         keyErr.Want,
				}
			}
		}
		return err
	}, nil
}
