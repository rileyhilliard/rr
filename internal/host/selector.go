package host

import (
	"fmt"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// DefaultProbeTimeout is the default timeout for SSH connection probes.
const DefaultProbeTimeout = 5 * time.Second

// Connection represents an established SSH connection to a host.
type Connection struct {
	Name    string          // The host name from config (e.g., "gpu-box")
	Alias   string          // The SSH alias used to connect (e.g., "gpu-local")
	Client  *sshutil.Client // The active SSH client
	Host    config.Host     // The host configuration
	Latency time.Duration   // Connection latency from probe
}

// Close closes the SSH connection.
func (c *Connection) Close() error {
	if c.Client != nil {
		return c.Client.Close()
	}
	return nil
}

// Selector manages host selection and connection caching.
type Selector struct {
	hosts   map[string]config.Host
	timeout time.Duration

	// Connection cache for session reuse
	mu     sync.Mutex
	cached *Connection
}

// NewSelector creates a new host selector with the given hosts configuration.
func NewSelector(hosts map[string]config.Host) *Selector {
	return &Selector{
		hosts:   hosts,
		timeout: DefaultProbeTimeout,
	}
}

// SetTimeout sets the probe timeout for connection attempts.
func (s *Selector) SetTimeout(timeout time.Duration) {
	s.timeout = timeout
}

// Select chooses and connects to a host.
// If preferred is specified and exists, it tries that host first.
// For Phase 1, this only tries the first SSH alias (no fallback chain).
//
// Returns a cached connection if one exists for the same host.
func (s *Selector) Select(preferred string) (*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If we have a cached connection for the preferred host, return it
	if s.cached != nil {
		if preferred == "" || s.cached.Name == preferred {
			// Verify connection is still alive
			if s.isConnectionAlive(s.cached) {
				return s.cached, nil
			}
			// Connection is dead, clear cache
			s.cached.Close()
			s.cached = nil
		} else {
			// Different host requested, close existing connection
			s.cached.Close()
			s.cached = nil
		}
	}

	// Determine which host to try
	hostName, host, err := s.resolveHost(preferred)
	if err != nil {
		return nil, err
	}

	// For Phase 1: try only the first SSH alias
	if len(host.SSH) == 0 {
		return nil, errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' has no SSH aliases configured", hostName),
			"Add at least one SSH alias to the host configuration")
	}

	sshAlias := host.SSH[0]
	conn, err := s.connect(hostName, sshAlias, host)
	if err != nil {
		return nil, err
	}

	// Cache the connection
	s.cached = conn
	return conn, nil
}

// SelectWithFallback tries to connect to a host, falling back through all
// SSH aliases if the first fails. This is the Phase 2 implementation.
func (s *Selector) SelectWithFallback(preferred string) (*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check cache first
	if s.cached != nil {
		if preferred == "" || s.cached.Name == preferred {
			if s.isConnectionAlive(s.cached) {
				return s.cached, nil
			}
			s.cached.Close()
			s.cached = nil
		} else {
			s.cached.Close()
			s.cached = nil
		}
	}

	hostName, host, err := s.resolveHost(preferred)
	if err != nil {
		return nil, err
	}

	if len(host.SSH) == 0 {
		return nil, errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' has no SSH aliases configured", hostName),
			"Add at least one SSH alias to the host configuration")
	}

	// Try each SSH alias in order
	var lastErr error
	for _, sshAlias := range host.SSH {
		conn, err := s.connect(hostName, sshAlias, host)
		if err == nil {
			s.cached = conn
			return conn, nil
		}
		lastErr = err
	}

	return nil, errors.WrapWithCode(lastErr, errors.ErrSSH,
		fmt.Sprintf("All SSH aliases failed for host '%s'", hostName),
		"Check your network connection and SSH configuration")
}

// Close closes any cached connection.
func (s *Selector) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil {
		err := s.cached.Close()
		s.cached = nil
		return err
	}
	return nil
}

// GetCached returns the currently cached connection, if any.
func (s *Selector) GetCached() *Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cached
}

// resolveHost determines which host to use based on the preferred name.
func (s *Selector) resolveHost(preferred string) (string, config.Host, error) {
	if len(s.hosts) == 0 {
		return "", config.Host{}, errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add host configurations to your .rr.yaml file")
	}

	// If preferred is specified, use that
	if preferred != "" {
		host, ok := s.hosts[preferred]
		if !ok {
			return "", config.Host{}, errors.New(errors.ErrConfig,
				fmt.Sprintf("Host '%s' not found in configuration", preferred),
				fmt.Sprintf("Available hosts: %s", s.hostNames()))
		}
		return preferred, host, nil
	}

	// Otherwise, use the first host (deterministic iteration via sorted keys would be better,
	// but for Phase 1, we just grab the first one from the map)
	for name, host := range s.hosts {
		return name, host, nil
	}

	// Shouldn't reach here if len > 0, but just in case
	return "", config.Host{}, errors.New(errors.ErrConfig,
		"No hosts available",
		"Add host configurations to your .rr.yaml file")
}

// connect establishes an SSH connection to the given alias.
func (s *Selector) connect(hostName, sshAlias string, host config.Host) (*Connection, error) {
	// Probe and connect
	latency, err := Probe(sshAlias, s.timeout)
	if err != nil {
		return nil, err
	}

	// Dial returns a connected client
	client, err := sshutil.Dial(sshAlias, s.timeout)
	if err != nil {
		return nil, err
	}

	return &Connection{
		Name:    hostName,
		Alias:   sshAlias,
		Client:  client,
		Host:    host,
		Latency: latency,
	}, nil
}

// isConnectionAlive checks if the cached connection is still usable.
func (s *Selector) isConnectionAlive(conn *Connection) bool {
	if conn == nil || conn.Client == nil {
		return false
	}

	// Try to create a session as a quick health check
	session, err := conn.Client.NewSession()
	if err != nil {
		return false
	}
	session.Close()
	return true
}

// hostNames returns a comma-separated list of configured host names.
func (s *Selector) hostNames() string {
	names := make([]string, 0, len(s.hosts))
	for name := range s.hosts {
		names = append(names, name)
	}
	if len(names) == 0 {
		return "(none)"
	}
	result := names[0]
	for i := 1; i < len(names); i++ {
		result += ", " + names[i]
	}
	return result
}

// QuickSelect is a convenience function that creates a selector, selects a host,
// and returns the connection. The caller is responsible for closing the connection.
func QuickSelect(hosts map[string]config.Host, preferred string) (*Connection, error) {
	selector := NewSelector(hosts)
	return selector.Select(preferred)
}
