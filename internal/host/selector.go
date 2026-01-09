package host

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// ConnectionEvent represents an event during connection attempts.
type ConnectionEvent struct {
	Type    ConnectionEventType
	Alias   string
	Message string
	Error   error
	Latency time.Duration
}

// ConnectionEventType categorizes connection events.
type ConnectionEventType int

const (
	// EventTrying indicates an alias connection attempt is starting.
	EventTrying ConnectionEventType = iota
	// EventFailed indicates an alias connection attempt failed.
	EventFailed
	// EventConnected indicates a successful connection.
	EventConnected
	// EventCacheHit indicates a cached connection was reused.
	EventCacheHit
	// EventLocalFallback indicates falling back to local execution.
	EventLocalFallback
)

// String returns a human-readable description of the event type.
func (t ConnectionEventType) String() string {
	switch t {
	case EventTrying:
		return "trying"
	case EventFailed:
		return "failed"
	case EventConnected:
		return "connected"
	case EventCacheHit:
		return "cache_hit"
	case EventLocalFallback:
		return "local_fallback"
	default:
		return "unknown"
	}
}

// EventHandler is a callback for connection events.
type EventHandler func(event ConnectionEvent)

// DefaultProbeTimeout is the default timeout for SSH connection probes.
const DefaultProbeTimeout = 5 * time.Second

// Connection represents an established SSH connection to a host,
// or a local execution context when IsLocal is true.
type Connection struct {
	Name    string            // The host name from config (e.g., "gpu-box")
	Alias   string            // The SSH alias used to connect (e.g., "gpu-local")
	Client  sshutil.SSHClient // The active SSH client (nil for local connections)
	Host    config.Host       // The host configuration
	Latency time.Duration     // Connection latency from probe
	IsLocal bool              // True when falling back to local execution
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
	hosts         map[string]config.Host
	timeout       time.Duration
	eventHandler  EventHandler
	localFallback bool // Whether to fall back to local execution when all hosts fail

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

// SetEventHandler sets a callback for connection events.
// Events are emitted during Select/SelectWithFallback to report progress.
func (s *Selector) SetEventHandler(handler EventHandler) {
	s.eventHandler = handler
}

// SetLocalFallback enables or disables local fallback mode.
// When enabled, if all remote hosts fail, Select returns a local Connection.
func (s *Selector) SetLocalFallback(enabled bool) {
	s.localFallback = enabled
}

// emit sends an event to the handler if one is configured.
func (s *Selector) emit(event ConnectionEvent) {
	if s.eventHandler != nil {
		s.eventHandler(event)
	}
}

// Select chooses and connects to a host, trying each SSH alias in order
// until one succeeds (fallback behavior).
// If preferred is specified and exists, it tries that host.
//
// Returns a cached connection if one exists for the same host.
// Emits ConnectionEvents to report progress through the event handler.
func (s *Selector) Select(preferred string) (*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectUnlocked(preferred)
}

// SelectWithFallback is an alias for Select, which now includes fallback behavior.
//
// Deprecated: Use Select instead. This method exists for backward compatibility.
func (s *Selector) SelectWithFallback(preferred string) (*Connection, error) {
	return s.Select(preferred)
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
			"No hosts set up yet",
			"You need at least one remote machine. Add one under 'hosts:' in .rr.yaml or run 'rr init'.")
	}

	// If preferred is specified, use that
	if preferred != "" {
		host, ok := s.hosts[preferred]
		if !ok {
			return "", config.Host{}, errors.New(errors.ErrConfig,
				fmt.Sprintf("Host '%s' doesn't exist", preferred),
				fmt.Sprintf("Did you mean one of these? %s", s.hostNames()))
		}
		return preferred, host, nil
	}

	// Use the first host alphabetically for deterministic selection
	names := make([]string, 0, len(s.hosts))
	for name := range s.hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	firstName := names[0]
	return firstName, s.hosts[firstName], nil
}

// connect establishes an SSH connection to the given alias.
func (s *Selector) connect(hostName, sshAlias string, host config.Host) (*Connection, error) {
	// ProbeAndConnect does a single SSH handshake and returns both the client
	// and the measured latency, avoiding the previous double-handshake overhead.
	client, latency, err := ProbeAndConnect(sshAlias, s.timeout)
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
// Uses a lightweight keep-alive request instead of creating a full session,
// which avoids the ~100-200ms overhead of session creation.
func (s *Selector) isConnectionAlive(conn *Connection) bool {
	if conn == nil {
		return false
	}

	// Local connections are always "alive"
	if conn.IsLocal {
		return true
	}

	if conn.Client == nil {
		return false
	}

	// Use SendRequest with "keepalive@openssh.com" for a lightweight check.
	// This is much faster than NewSession() because it doesn't create a
	// new channel - it just sends a global request on the existing connection.
	// The wantReply=true ensures we get a response confirming the connection works.
	_, _, err := conn.Client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

// hostNames returns a comma-separated list of configured host names.
func (s *Selector) hostNames() string {
	names := make([]string, 0, len(s.hosts))
	for name := range s.hosts {
		names = append(names, name)
	}
	return joinOrNone(names)
}

// formatFailedAliases returns a comma-separated list of failed aliases.
func formatFailedAliases(aliases []string) string {
	return joinOrNone(aliases)
}

// QuickSelect is a convenience function that creates a selector, selects a host,
// and returns the connection. The caller is responsible for closing the connection.
func QuickSelect(hosts map[string]config.Host, preferred string) (*Connection, error) {
	selector := NewSelector(hosts)
	return selector.Select(preferred)
}

// SelectByTag selects a host that has the specified tag.
// It filters the configured hosts to those containing the tag, then performs
// normal selection from the filtered set. If no hosts match the tag, returns an error.
func (s *Selector) SelectByTag(tag string) (*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Filter hosts to those with the matching tag
	matchingHosts := make(map[string]config.Host)
	for name, host := range s.hosts {
		if hasTag(host.Tags, tag) {
			matchingHosts[name] = host
		}
	}

	if len(matchingHosts) == 0 {
		availableTags := s.collectTags()
		hint := "Add tags to your hosts in .rr.yaml."
		if len(availableTags) > 0 {
			hint = fmt.Sprintf("Available tags: %s", formatTags(availableTags))
		}
		return nil, errors.New(errors.ErrConfig,
			fmt.Sprintf("No hosts have the '%s' tag", tag),
			hint)
	}

	// Create a temporary selector with filtered hosts
	filteredSelector := &Selector{
		hosts:         matchingHosts,
		timeout:       s.timeout,
		eventHandler:  s.eventHandler,
		localFallback: s.localFallback,
	}

	// Use Select logic without preferred host (will use first available from filtered set)
	conn, err := filteredSelector.selectUnlocked("")
	if err != nil {
		return nil, err
	}

	// Cache the connection in the original selector
	s.cached = conn
	return conn, nil
}

// selectUnlocked performs selection without locking (called when lock is already held).
func (s *Selector) selectUnlocked(preferred string) (*Connection, error) {
	// If we have a cached connection for the preferred host, return it
	if s.cached != nil {
		// Local fallback connections are reused regardless of preferred host
		if preferred == "" || s.cached.Name == preferred || s.cached.IsLocal {
			// Verify connection is still alive
			if s.isConnectionAlive(s.cached) {
				s.emit(ConnectionEvent{
					Type:    EventCacheHit,
					Alias:   s.cached.Alias,
					Message: fmt.Sprintf("reusing cached connection to %s", s.cached.Alias),
				})
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

	if len(host.SSH) == 0 {
		return nil, errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' needs at least one SSH connection", hostName),
			"Add something like 'user@hostname' under the 'ssh:' section for this host.")
	}

	// Try each SSH alias in order (fallback chain)
	var lastErr error
	var failedAliases []string
	for i, sshAlias := range host.SSH {
		s.emit(ConnectionEvent{
			Type:    EventTrying,
			Alias:   sshAlias,
			Message: fmt.Sprintf("trying alias %s", sshAlias),
		})

		conn, err := s.connect(hostName, sshAlias, host)
		if err == nil {
			// Emit success event, noting if this was a fallback
			msg := fmt.Sprintf("connected via %s", sshAlias)
			if i > 0 {
				msg = fmt.Sprintf("connected via %s (fallback)", sshAlias)
			}
			s.emit(ConnectionEvent{
				Type:    EventConnected,
				Alias:   sshAlias,
				Message: msg,
				Latency: conn.Latency,
			})
			s.cached = conn
			return conn, nil
		}

		// Emit failure event
		errMsg := "connection failed"
		if probeErr, ok := err.(*ProbeError); ok {
			errMsg = probeErr.Reason.String()
		}
		s.emit(ConnectionEvent{
			Type:    EventFailed,
			Alias:   sshAlias,
			Message: errMsg,
			Error:   err,
		})
		failedAliases = append(failedAliases, sshAlias)
		lastErr = err
	}

	// All remote hosts failed - check if local fallback is enabled
	if s.localFallback {
		s.emit(ConnectionEvent{
			Type:    EventLocalFallback,
			Alias:   "local",
			Message: "All remote hosts unreachable, falling back to local execution",
		})
		localConn := &Connection{
			Name:    "local",
			Alias:   "local",
			Client:  nil, // No SSH client for local execution
			Host:    host,
			IsLocal: true,
		}
		s.cached = localConn
		return localConn, nil
	}

	// Build detailed error message listing all failed aliases
	return nil, errors.WrapWithCode(lastErr, errors.ErrSSH,
		fmt.Sprintf("Couldn't connect to '%s' - tried: %s", hostName, formatFailedAliases(failedAliases)),
		"The remote might be offline, or there could be a network/firewall issue. You can also set 'local_fallback: true' in .rr.yaml to run locally when remotes are down.")
}

// hasTag checks if the tags slice contains the specified tag.
func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// collectTags gathers all unique tags from configured hosts.
func (s *Selector) collectTags() []string {
	tagSet := make(map[string]struct{})
	for _, host := range s.hosts {
		for _, tag := range host.Tags {
			tagSet[tag] = struct{}{}
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	return tags
}

// formatTags returns a comma-separated list of tags.
func formatTags(tags []string) string {
	return joinOrNone(tags)
}

// joinOrNone joins strings with ", " or returns "(none)" for empty slices.
func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}

// HostInfo returns information about all configured hosts.
// This is useful for interactive host selection UIs.
func (s *Selector) HostInfo(defaultHost string) []HostInfoItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]HostInfoItem, 0, len(s.hosts))
	for name, host := range s.hosts {
		items = append(items, HostInfoItem{
			Name:    name,
			SSH:     host.SSH,
			Dir:     host.Dir,
			Tags:    host.Tags,
			Default: name == defaultHost,
		})
	}

	// Sort by name for consistent ordering
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	return items
}

// HostInfoItem contains information about a host for display purposes.
type HostInfoItem struct {
	Name    string   // Host name from config
	SSH     []string // SSH aliases
	Dir     string   // Remote directory
	Tags    []string // Tags for filtering
	Default bool     // Whether this is the default host
}

// HostCount returns the number of configured hosts.
func (s *Selector) HostCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hosts)
}
