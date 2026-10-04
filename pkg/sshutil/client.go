package sshutil

import (
	"bytes"
	"crypto/ed25519"
	stderrors "errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

	// Dial with timeout, using ProxyCommand if configured
	address := settings.address()
	var conn net.Conn
	if settings.hasProxy() {
		conn, err = startProxy(proxyCmd(host, settings), settings)
		if err != nil {
			var startErr *proxyStartError
			if stderrors.As(err, &startErr) {
				return nil, proxyStartFailure(host, settings, startErr)
			}
			var stderr string
			var exitErr *proxyExitError
			if stderrors.As(err, &exitErr) {
				stderr = exitErr.stderr
				// A proxy that exits cleanly without a word has relayed a
				// connection the target closed.
				if stderr == "" && exitErr.err == nil {
					return nil, targetClosed(host, settings, err)
				}
			}
			return nil, proxyFailure(host, settings, timeout, &ProxyError{Stderr: stderr, Err: err})
		}
	} else {
		conn, err = net.DialTimeout("tcp", address, timeout)
		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrSSH,
				fmt.Sprintf("Can't reach '%s' at %s", host, address),
				suggestionForDialError(err))
		}
	}

	// SSH handshake with timeout for proxy connections.
	// For direct TCP, net.DialTimeout already enforces the timeout on the TCP connection.
	// For proxy connections, we need our own timeout since the proxy may connect but the
	// SSH handshake could stall (e.g., hung bastion host).
	var proxyTimedOut atomic.Bool
	if settings.hasProxy() {
		timer := time.AfterFunc(timeout, func() {
			proxyTimedOut.Store(true)
			conn.Close()
		})
		defer timer.Stop()
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		conn.Close()

		// Through a proxy, a timeout, or a dropped connection the proxy
		// explained, is the proxy failing to reach the target. Close has
		// waited for it to exit, so what it printed is complete. A connection
		// dropped without a word from the proxy is the target closing it (ssh
		// always says why when its own leg fails); that, and any other
		// handshake error, is handled below as the target's.
		if settings.hasProxy() {
			var stderr string
			if pc, ok := conn.(*proxyConn); ok {
				stderr = proxyStderr(pc.stderr.String())
			}
			if proxyTimedOut.Load() || (isConnClosed(err) && stderr != "") {
				pe := &ProxyError{TimedOut: proxyTimedOut.Load(), Stderr: stderr, Err: err}
				return nil, proxyFailure(host, settings, timeout, pe)
			}
			if isConnClosed(err) {
				return nil, targetClosed(host, settings, err)
			}
		}

		// Check for host key mismatch error (provides detailed suggestion)
		var hostKeyErr *HostKeyMismatchError
		if stderrors.As(err, &hostKeyErr) {
			// Wrap (not replace) so the original error is preserved for errors.As
			return nil, errors.WrapWithCode(hostKeyErr, errors.ErrSSH,
				hostKeyErr.Error(),
				hostKeyErr.Suggestion())
		}

		// Build suggestion, with extra context if we found encrypted keys
		suggestion := suggestionForHandshakeError(err, settings.encryptedKeys, host)

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
	proxyCommand  string   // ProxyCommand from SSH config, if any
	jumpHosts     string   // ProxyJump from SSH config, if any and there's no ProxyCommand
	identityAgent string   // IdentityAgent socket path from SSH config (if any)
	encryptedKeys []string // Keys that exist but are encrypted
}

// hasProxy reports whether the connection goes through a ProxyJump or
// ProxyCommand rather than straight to the target.
func (s *sshSettings) hasProxy() bool {
	return s.proxyCommand != "" || s.jumpHosts != ""
}

// proxyDirective names the SSH config setting the proxy came from, for errors.
func (s *sshSettings) proxyDirective() string {
	if s.jumpHosts != "" {
		return "ProxyJump"
	}
	return "ProxyCommand"
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

	// Get ProxyCommand. "none" turns off one set by an earlier block.
	if proxyCmd, _ := cfg.Get(host, "ProxyCommand"); proxyCmd != "" {
		if !strings.EqualFold(strings.TrimSpace(proxyCmd), "none") {
			settings.proxyCommand = proxyCmd
		}
		hostFound = true
	}

	// Get IdentityAgent (e.g. 1Password, YubiKey agent sockets)
	if identityAgent, _ := cfg.Get(host, "IdentityAgent"); identityAgent != "" {
		// Expand env vars first ($SSH_AUTH_SOCK), then tilde (~/)
		expanded := os.ExpandEnv(identityAgent)
		settings.identityAgent = expandPath(expanded)
		hostFound = true
	}

	// ProxyJump runs the system ssh to the jump host, as OpenSSH does. A
	// ProxyCommand set for the host wins.
	if settings.proxyCommand == "" {
		if proxyJump, _ := cfg.Get(host, "ProxyJump"); proxyJump != "" {
			settings.jumpHosts = proxyJumpValue(proxyJump)
			hostFound = true
		}
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

	// Check for test key override (for CI environments)
	// When set, ONLY use this key - do NOT fall back to agent or other keys
	testKey := os.Getenv("RR_TEST_SSH_KEY")
	if testKey != "" {
		keyAuth, err := keyFileAuth(testKey)
		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrSSH,
				fmt.Sprintf("failed to load RR_TEST_SSH_KEY: %s", testKey),
				"Check that the key file exists and is valid")
		}
		authMethods = append(authMethods, keyAuth)
	} else {
		// Normal mode: try multiple auth methods
		// Try SSH agent first (most common and convenient)
		if agentAuth := sshAgentAuth(settings.identityAgent); agentAuth != nil {
			authMethods = append(authMethods, agentAuth)
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
			sb.WriteString("\nNot sure which key? Check with: ssh -v " + settings.hostname)
			suggestion = sb.String()
		}

		return nil, errors.New(errors.ErrSSH, msg, suggestion)
	}

	// Determine host key callback
	var hostKeyCallback ssh.HostKeyCallback
	var hostKeyAlgorithms []string
	if StrictHostKeyChecking {
		knownHostsPath := filepath.Join(homeDir(), ".ssh", "known_hosts")
		var err error
		hostKeyCallback, err = createHostKeyCallback(knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load known_hosts: %w", err)
		}
		hostKeyAlgorithms = knownHostKeyAlgorithms(knownHostsPath, settings.address())
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec // User explicitly disabled host key checking
	}

	return &ssh.ClientConfig{
		User:              settings.user,
		Auth:              authMethods,
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: hostKeyAlgorithms,
		Timeout:           10 * time.Second,
	}, nil
}

// agentConn holds the reusable SSH agent connection for the default SSH_AUTH_SOCK.
var (
	agentConn     net.Conn
	agentClient   agent.ExtendedAgent
	agentConnOnce sync.Once
)

// perHostAgentConns tracks agent connections opened for per-host IdentityAgent sockets.
var (
	perHostAgentConns   []net.Conn
	perHostAgentConnsMu sync.Mutex
)

// sshAgentAuth returns an auth method using the SSH agent if available.
// If identityAgent is set, it uses that socket path instead of SSH_AUTH_SOCK.
// If identityAgent is "none", agent auth is explicitly disabled.
// The default SSH_AUTH_SOCK connection is cached; per-host connections are tracked for cleanup.
func sshAgentAuth(identityAgent string) ssh.AuthMethod {
	if identityAgent == "none" {
		return nil
	}

	// Per-host IdentityAgent: open a dedicated connection (not cached globally
	// since different hosts may use different agent sockets)
	if identityAgent != "" {
		conn, err := net.Dial("unix", identityAgent)
		if err != nil {
			return nil
		}

		client := agent.NewClient(conn)
		signers, err := client.Signers()
		if err != nil || len(signers) == 0 {
			conn.Close()
			return nil
		}

		// Only track the connection for cleanup if we're actually using it
		perHostAgentConnsMu.Lock()
		perHostAgentConns = append(perHostAgentConns, conn)
		perHostAgentConnsMu.Unlock()

		return ssh.PublicKeysCallback(client.Signers)
	}

	// Default: use SSH_AUTH_SOCK with cached connection
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

// CloseAgent closes all SSH agent connections (both default and per-host).
// This should be called when the application is shutting down.
func CloseAgent() {
	if agentConn != nil {
		agentConn.Close()
	}
	perHostAgentConnsMu.Lock()
	for _, conn := range perHostAgentConns {
		conn.Close()
	}
	perHostAgentConns = nil
	perHostAgentConnsMu.Unlock()
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

	// Check for an accompanying SSH certificate file (e.g. id_ed25519-cert.pub).
	// If found, combine it with the private key for certificate-based auth.
	certPath := keyPath + "-cert.pub"
	if certData, err := os.ReadFile(certPath); err == nil {
		pubKey, _, _, _, parseErr := ssh.ParseAuthorizedKey(certData)
		if parseErr != nil {
			emitWarning(fmt.Sprintf("Found certificate %s but couldn't parse it: %v", certPath, parseErr))
		} else if cert, ok := pubKey.(*ssh.Certificate); !ok {
			emitWarning(fmt.Sprintf("Found %s but it's not a certificate", certPath))
		} else if certSigner, certErr := ssh.NewCertSigner(cert, signer); certErr != nil {
			emitWarning(fmt.Sprintf("Found certificate %s but couldn't use it: %v", certPath, certErr))
		} else {
			return ssh.PublicKeys(certSigner), nil
		}
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
		return "Check if SSH is running, or if the host is on a different network"
	}
	if strings.Contains(errStr, "no route to host") || strings.Contains(errStr, "network is unreachable") {
		return "Host may be offline or on a different network"
	}
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "i/o timeout") {
		return "Check if host is powered on and reachable from this network"
	}
	return "Check network connectivity to this host"
}

// proxyFailure turns a connection that failed in its ProxyJump or
// ProxyCommand into an error naming the jump host (or ProxyCommand), with
// what the proxy printed as the cause and a way to check each leg.
func proxyFailure(host string, settings *sshSettings, timeout time.Duration, pe *ProxyError) error {
	pe.Host = host
	pe.Directive = settings.proxyDirective()
	pe.JumpHost = settings.jumpHosts

	waited := ""
	if timeout > 0 {
		waited = " within " + timeout.String()
	}

	if pe.JumpHost != "" {
		check := fmt.Sprintf("Check the jump host on its own (%s), then that it can reach %s, the HostName and Port for '%s' in ~/.ssh/config.",
			jumpCheckCommand(pe.JumpHost), settings.address(), host)
		if pe.TimedOut {
			return errors.WrapWithCode(pe, errors.ErrSSH,
				fmt.Sprintf("Timed out reaching '%s' through jump host '%s'", host, pe.JumpHost),
				"Nothing answered"+waited+". "+check)
		}
		return errors.WrapWithCode(pe, errors.ErrSSH,
			fmt.Sprintf("Couldn't reach '%s' through jump host '%s'", host, pe.JumpHost),
			check)
	}

	check := fmt.Sprintf("Check the ProxyCommand for '%s' in ~/.ssh/config, then try: ssh %s", host, host)
	if pe.TimedOut {
		return errors.WrapWithCode(pe, errors.ErrSSH,
			fmt.Sprintf("Timed out reaching '%s' through its ProxyCommand", host),
			"Nothing answered"+waited+". "+check)
	}
	return errors.WrapWithCode(pe, errors.ErrSSH,
		fmt.Sprintf("Couldn't reach '%s' through its ProxyCommand", host),
		check)
}

// targetClosed is a connection through a proxy that the target hung up on
// before the SSH handshake finished: the proxy got there, so it isn't at fault.
func targetClosed(host string, settings *sshSettings, err error) error {
	return errors.WrapWithCode(err, errors.ErrSSH,
		fmt.Sprintf("'%s' closed the connection before the SSH handshake", host),
		fmt.Sprintf("The %s reached %s, but its SSH server hung up. Check sshd is running there and isn't refusing this connection (MaxStartups, fail2ban, hosts.deny), then try: ssh %s",
			settings.proxyDirective(), settings.address(), host))
}

// proxyStartFailure is a proxy process that couldn't be started, so nothing
// was dialed. For a ProxyJump that's the system ssh missing.
func proxyStartFailure(host string, settings *sshSettings, startErr *proxyStartError) error {
	if settings.jumpHosts != "" {
		return errors.WrapWithCode(startErr, errors.ErrSSH,
			fmt.Sprintf("Couldn't run ssh to reach '%s' through jump host '%s'", host, settings.jumpHosts),
			"rr runs the system ssh for ProxyJump. Install the OpenSSH client and make sure ssh is on PATH, or set a ProxyCommand for the host in ~/.ssh/config.")
	}
	return errors.WrapWithCode(startErr, errors.ErrSSH,
		fmt.Sprintf("Couldn't run the ProxyCommand for '%s'", host),
		"rr runs ProxyCommand with sh. Make sure sh is on PATH.")
}

// isConnClosed reports whether a handshake error is the connection going
// away under it: the proxy exited, or rr closed it at the timeout.
func isConnClosed(err error) bool {
	if stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrUnexpectedEOF) ||
		stderrors.Is(err, net.ErrClosed) || stderrors.Is(err, os.ErrClosed) {
		return true
	}
	s := err.Error()
	return strings.HasSuffix(s, ": EOF") || strings.Contains(s, "file already closed") ||
		strings.Contains(s, "broken pipe") || strings.Contains(s, "use of closed")
}

// jumpCheckCommand is the ssh command that connects to the last jump host in
// a ProxyJump value, through any before it.
func jumpCheckCommand(jumpHosts string) string {
	hops := strings.Split(jumpHosts, ",")
	for i := range hops {
		hops[i] = strings.TrimSpace(hops[i])
	}
	parts := []string{"ssh"}
	if len(hops) > 1 {
		parts = append(parts, "-J", strings.Join(hops[:len(hops)-1], ","))
	}
	if dest, port := splitJumpPort(hops[len(hops)-1]); port != "" {
		parts = append(parts, "-p", port, dest)
	} else {
		parts = append(parts, dest)
	}
	return strings.Join(parts, " ")
}

func suggestionForHandshakeError(err error, encryptedKeys []string, host string) string {
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
			sb.WriteString("\nNot sure which key? Check with: ssh -v " + host)
			return sb.String()
		}
		return "Auth failed. Check your keys are loaded: ssh-add -l"
	}
	if strings.Contains(errStr, "host key") || strings.Contains(errStr, "knownhosts:") {
		return "Host key not trusted. rr checks ~/.ssh/known_hosts and never adds keys. Accept the key once: ssh -o StrictHostKeyChecking=accept-new " + host + " exit"
	}
	return "Something went wrong during SSH setup. Try: ssh " + host
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

// preprocessSSHConfig reads the SSH config and strips out Match blocks.
// Match blocks are not supported by the ssh_config library, so we skip them
// but continue parsing Host blocks that come after.
// Returns the line number of the first Match directive found (0 if none).
func preprocessSSHConfig(configPath string) ([]byte, int, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, 0, err
	}

	lines := strings.Split(string(content), "\n")
	var result []string
	matchLine := 0
	inMatchBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Match directive starts a block we need to skip
		if strings.HasPrefix(lower, "match ") {
			if matchLine == 0 {
				matchLine = i + 1 // Record first Match line (1-indexed)
			}
			inMatchBlock = true
			continue
		}

		// Host directive ends a Match block
		if strings.HasPrefix(lower, "host ") {
			inMatchBlock = false
		}

		// Skip lines inside Match blocks
		if inMatchBlock {
			continue
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

// knownHostKeyAlgorithms returns the host key algorithms to offer for
// address: those of the keys known_hosts has for it, as OpenSSH orders them.
// Otherwise the server picks from Go's default order, and a host known only
// by its ed25519 key (what `ssh` records) fails as a mismatch when the server
// offers ecdsa first. Nil when known_hosts has no key for the address, which
// leaves the default order.
func knownHostKeyAlgorithms(knownHostsPath, address string) []string {
	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil
	}
	// A key no host has: the lookup fails and lists the keys it wanted.
	probe, err := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	if err != nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if !stderrors.As(callback(address, proxyAddr{addr: address}, probe), &keyErr) {
		return nil
	}
	var algos []string
	seen := map[string]bool{}
	for _, known := range keyErr.Want {
		keyType := known.Key.Type()
		names := []string{keyType}
		if keyType == ssh.KeyAlgoRSA {
			names = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
		}
		for _, name := range names {
			if !seen[name] {
				seen[name] = true
				algos = append(algos, name)
			}
		}
	}
	return algos
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
