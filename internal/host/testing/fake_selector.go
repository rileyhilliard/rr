// Package testing provides test doubles for the host package.
package testing

import (
	"sort"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	sstesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
)

// FakeHost configures a fake host for testing.
type FakeHost struct {
	Config     config.Host
	Client     sshutil.SSHClient // Optional pre-configured client
	ShouldFail bool              // If true, connection attempts fail
	FailError  error             // Error to return when ShouldFail is true
	Latency    time.Duration     // Simulated connection latency
}

// FakeSelector simulates host selection for testing.
// It allows tests to configure which hosts succeed/fail without real SSH.
type FakeSelector struct {
	mu            sync.Mutex
	hosts         map[string]*FakeHost
	hostOrder     []string
	eventHandler  host.EventHandler
	localFallback bool
	cached        *host.Connection

	// Tracking for assertions
	SelectCalls        []string // Names of hosts requested via Select
	SelectHostCalls    []string // Names of hosts requested via SelectHost
	ConnectionAttempts int      // Total connection attempts
}

// NewFakeSelector creates a new fake selector for testing.
func NewFakeSelector() *FakeSelector {
	return &FakeSelector{
		hosts: make(map[string]*FakeHost),
	}
}

// AddHost adds a fake host that will succeed.
func (s *FakeSelector) AddHost(name string, cfg config.Host) *FakeSelector {
	s.mu.Lock()
	defer s.mu.Unlock()

	client := sstesting.NewMockClient(name)
	s.hosts[name] = &FakeHost{
		Config: cfg,
		Client: client,
	}
	return s
}

// AddHostWithClient adds a fake host with a custom mock client.
func (s *FakeSelector) AddHostWithClient(name string, cfg config.Host, client sshutil.SSHClient) *FakeSelector {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.hosts[name] = &FakeHost{
		Config: cfg,
		Client: client,
	}
	return s
}

// AddFailingHost adds a fake host that will fail to connect.
func (s *FakeSelector) AddFailingHost(name string, cfg config.Host, err error) *FakeSelector {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.hosts[name] = &FakeHost{
		Config:     cfg,
		ShouldFail: true,
		FailError:  err,
	}
	return s
}

// SetHostOrder sets the priority order for host selection.
func (s *FakeSelector) SetHostOrder(order []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostOrder = order
}

// SetEventHandler sets a callback for connection events.
func (s *FakeSelector) SetEventHandler(handler host.EventHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventHandler = handler
}

// SetLocalFallback enables or disables local fallback mode.
func (s *FakeSelector) SetLocalFallback(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localFallback = enabled
}

// emit sends an event to the handler if one is configured.
func (s *FakeSelector) emit(event host.ConnectionEvent) {
	if s.eventHandler != nil {
		s.eventHandler(event)
	}
}

// Select chooses and connects to a host.
func (s *FakeSelector) Select(preferred string) (*host.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SelectCalls = append(s.SelectCalls, preferred)
	s.ConnectionAttempts++

	// Check for cached connection
	if s.cached != nil {
		if preferred == "" || s.cached.Name == preferred || s.cached.IsLocal {
			s.emit(host.ConnectionEvent{
				Type:    host.EventCacheHit,
				Alias:   s.cached.Alias,
				Message: "reusing cached connection",
			})
			return s.cached, nil
		}
		// Different host, close cached
		if err := s.cached.Close(); err != nil {
			s.emit(host.ConnectionEvent{
				Type:    host.EventFailed,
				Alias:   s.cached.Alias,
				Message: "failed to close cached connection",
				Error:   err,
			})
		}
		s.cached = nil
	}

	// Handle no hosts configured
	if len(s.hosts) == 0 {
		if s.localFallback {
			return s.createLocalConnection(), nil
		}
		return nil, errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add hosts to your configuration.")
	}

	// Resolve which host to try
	hostName, fakeHost, err := s.resolveHost(preferred)
	if err != nil {
		return nil, err
	}

	// Try to connect
	conn, err := s.connect(hostName, fakeHost)
	if err != nil {
		if s.localFallback {
			return s.createLocalConnection(), nil
		}
		return nil, err
	}

	s.cached = conn
	return conn, nil
}

// SelectHost connects to a specific host by name (no caching).
func (s *FakeSelector) SelectHost(hostName string) (*host.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SelectHostCalls = append(s.SelectHostCalls, hostName)
	s.ConnectionAttempts++

	fakeHost, ok := s.hosts[hostName]
	if !ok {
		return nil, errors.New(errors.ErrConfig,
			"Host not found: "+hostName,
			"Check your host configuration.")
	}

	return s.connect(hostName, fakeHost)
}

// SelectByTag selects a host that has the specified tag.
func (s *FakeSelector) SelectByTag(tag string) (*host.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, fakeHost := range s.hosts {
		for _, t := range fakeHost.Config.Tags {
			if t == tag {
				return s.connect(name, fakeHost)
			}
		}
	}

	return nil, errors.New(errors.ErrConfig,
		"No hosts have tag: "+tag,
		"Add tags to your hosts.")
}

// SelectNextHost returns the next available host after skipping specified hosts.
func (s *FakeSelector) SelectNextHost(skipHosts []string) (*host.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	skipSet := make(map[string]bool)
	for _, name := range skipHosts {
		skipSet[name] = true
	}

	for _, name := range s.orderedHostNames() {
		if skipSet[name] {
			continue
		}
		fakeHost := s.hosts[name]
		conn, err := s.connect(name, fakeHost)
		if err == nil {
			return conn, nil
		}
	}

	return nil, errors.New(errors.ErrSSH,
		"No available hosts",
		"All hosts either skipped or failed to connect.")
}

// Close closes any cached connection.
func (s *FakeSelector) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil {
		err := s.cached.Close()
		s.cached = nil
		return err
	}
	return nil
}

// GetHostNames returns all configured host names.
func (s *FakeSelector) GetHostNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.orderedHostNames()
}

// HostCount returns the number of configured hosts.
func (s *FakeSelector) HostCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hosts)
}

// GetCached returns the currently cached connection, if any.
func (s *FakeSelector) GetCached() *host.Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cached
}

// HostInfo returns information about all configured hosts.
func (s *FakeSelector) HostInfo() []host.HostInfoItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]host.HostInfoItem, 0, len(s.hosts))
	for name, fakeHost := range s.hosts {
		items = append(items, host.HostInfoItem{
			Name: name,
			SSH:  fakeHost.Config.SSH,
			Dir:  fakeHost.Config.Dir,
			Tags: fakeHost.Config.Tags,
		})
	}
	return items
}

// resolveHost determines which host to use based on the preferred name.
func (s *FakeSelector) resolveHost(preferred string) (string, *FakeHost, error) {
	if preferred != "" {
		fakeHost, ok := s.hosts[preferred]
		if !ok {
			return "", nil, errors.New(errors.ErrConfig,
				"Host not found: "+preferred,
				"Check your host configuration.")
		}
		return preferred, fakeHost, nil
	}

	// Use first host in order
	names := s.orderedHostNames()
	if len(names) == 0 {
		return "", nil, errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add hosts to your configuration.")
	}

	return names[0], s.hosts[names[0]], nil
}

// connect creates a connection to the fake host.
func (s *FakeSelector) connect(name string, fakeHost *FakeHost) (*host.Connection, error) {
	if fakeHost.ShouldFail {
		s.emit(host.ConnectionEvent{
			Type:    host.EventFailed,
			Alias:   name,
			Message: "connection failed",
			Error:   fakeHost.FailError,
		})
		if fakeHost.FailError != nil {
			return nil, fakeHost.FailError
		}
		return nil, errors.New(errors.ErrSSH,
			"Connection failed to "+name,
			"Host configured to fail in test.")
	}

	// Simulate latency
	if fakeHost.Latency > 0 {
		time.Sleep(fakeHost.Latency)
	}

	// Create client if not provided
	client := fakeHost.Client
	if client == nil {
		client = sstesting.NewMockClient(name)
	}

	alias := name
	if len(fakeHost.Config.SSH) > 0 {
		alias = fakeHost.Config.SSH[0]
	}

	s.emit(host.ConnectionEvent{
		Type:    host.EventConnected,
		Alias:   alias,
		Message: "connected",
		Latency: fakeHost.Latency,
	})

	return &host.Connection{
		Name:    name,
		Alias:   alias,
		Client:  client,
		Host:    fakeHost.Config,
		Latency: fakeHost.Latency,
		IsLocal: false,
	}, nil
}

// createLocalConnection creates a local fallback connection.
func (s *FakeSelector) createLocalConnection() *host.Connection {
	s.emit(host.ConnectionEvent{
		Type:    host.EventLocalFallback,
		Alias:   "local",
		Message: "falling back to local execution",
	})

	conn := &host.Connection{
		Name:    "local",
		Alias:   "local",
		Client:  nil,
		Host:    config.Host{},
		IsLocal: true,
	}
	s.cached = conn
	return conn
}

// orderedHostNames returns host names in priority order.
func (s *FakeSelector) orderedHostNames() []string {
	if len(s.hostOrder) > 0 {
		names := make([]string, 0, len(s.hostOrder))
		for _, name := range s.hostOrder {
			if _, ok := s.hosts[name]; ok {
				names = append(names, name)
			}
		}
		return names
	}

	// Alphabetical order for deterministic iteration
	names := make([]string, 0, len(s.hosts))
	for name := range s.hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AssertHostSelected verifies the expected host was selected.
func (s *FakeSelector) AssertHostSelected(expected string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, name := range s.SelectCalls {
		if name == expected {
			return true
		}
	}
	return false
}

// AssertConnectionAttempts verifies the expected number of connection attempts.
func (s *FakeSelector) AssertConnectionAttempts(expected int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ConnectionAttempts == expected
}

// Reset clears all tracking data for fresh assertions.
func (s *FakeSelector) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SelectCalls = nil
	s.SelectHostCalls = nil
	s.ConnectionAttempts = 0
	if s.cached != nil {
		_ = s.cached.Close() // Ignore error during reset
		s.cached = nil
	}
}
