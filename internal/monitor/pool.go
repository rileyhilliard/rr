package monitor

import (
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// Pool manages a pool of SSH connections for reuse between refresh cycles.
// It keeps connections alive to avoid the overhead of reconnecting on each metrics collection.
type Pool struct {
	mu          sync.Mutex
	connections map[string]*poolEntry
	hosts       map[string]config.Host
	timeout     time.Duration
}

// poolEntry holds a connection and its metadata.
type poolEntry struct {
	client   *sshutil.Client
	platform Platform
	lastUsed time.Time
}

// NewPool creates a new SSH connection pool with host configurations.
func NewPool(hosts map[string]config.Host, timeout time.Duration) *Pool {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Pool{
		connections: make(map[string]*poolEntry),
		hosts:       hosts,
		timeout:     timeout,
	}
}

// Get retrieves an existing connection for the given alias, or creates a new one.
// Connections are reused without preemptive health checks - if a connection has died,
// the caller will get an error when they try to use it and should call CloseOne() to
// remove it from the pool, then retry.
// The alias is looked up in the hosts config to get the actual SSH addresses to try.
func (p *Pool) Get(alias string) (*sshutil.Client, error) {
	p.mu.Lock()
	entry, exists := p.connections[alias]
	if exists && entry.client != nil {
		entry.lastUsed = time.Now()
		p.mu.Unlock()
		return entry.client, nil
	}
	p.mu.Unlock()

	// Look up the host config to get SSH addresses
	host, ok := p.hosts[alias]
	if !ok || len(host.SSH) == 0 {
		// Fall back to using alias directly (for backwards compatibility or simple configs)
		client, err := sshutil.Dial(alias, p.timeout)
		if err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.connections[alias] = &poolEntry{
			client:   client,
			lastUsed: time.Now(),
		}
		p.mu.Unlock()
		return client, nil
	}

	// Try each SSH address in order until one succeeds
	var lastErr error
	for _, sshAddr := range host.SSH {
		client, err := sshutil.Dial(sshAddr, p.timeout)
		if err == nil {
			p.mu.Lock()
			p.connections[alias] = &poolEntry{
				client:   client,
				lastUsed: time.Now(),
			}
			p.mu.Unlock()
			return client, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

// GetWithPlatform retrieves a connection and its detected platform.
// If the platform hasn't been detected yet, it detects it and caches the result.
func (p *Pool) GetWithPlatform(alias string) (*sshutil.Client, Platform, error) {
	client, err := p.Get(alias)
	if err != nil {
		return nil, PlatformUnknown, err
	}

	p.mu.Lock()
	entry := p.connections[alias]
	platform := entry.platform
	p.mu.Unlock()

	if platform == "" || platform == PlatformUnknown {
		// Detect platform
		detected, err := p.detectPlatform(client)
		if err != nil {
			// Continue with unknown platform, don't fail the whole operation
			detected = PlatformUnknown
		}

		p.mu.Lock()
		if e, ok := p.connections[alias]; ok {
			e.platform = detected
		}
		p.mu.Unlock()

		platform = detected
	}

	return client, platform, nil
}

// Return returns a connection to the pool for reuse.
// This is a no-op since we keep connections in the map, but it updates last used time.
func (p *Pool) Return(alias string, _ *sshutil.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.connections[alias]; ok {
		entry.lastUsed = time.Now()
	}
}

// Close closes all connections in the pool and clears it.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for alias, entry := range p.connections {
		if entry.client != nil {
			_ = entry.client.Close()
		}
		delete(p.connections, alias)
	}
}

// CloseOne closes and removes a specific connection from the pool.
func (p *Pool) CloseOne(alias string) {
	p.remove(alias)
}

// Size returns the number of connections in the pool.
func (p *Pool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.connections)
}

// remove closes and removes a connection from the pool.
func (p *Pool) remove(alias string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.connections[alias]; ok {
		if entry.client != nil {
			_ = entry.client.Close()
		}
		delete(p.connections, alias)
	}
}

// detectPlatform runs uname to determine the OS type.
func (p *Pool) detectPlatform(client *sshutil.Client) (Platform, error) {
	// Use embedded ssh.Client's NewSession directly for full session capabilities
	session, err := client.Client.NewSession()
	if err != nil {
		return PlatformUnknown, err
	}
	defer session.Close()

	output, err := session.Output(PlatformDetectCommand())
	if err != nil {
		return PlatformUnknown, err
	}

	// Trim whitespace from output
	result := string(output)
	for len(result) > 0 && (result[len(result)-1] == '\n' || result[len(result)-1] == '\r') {
		result = result[:len(result)-1]
	}

	return ParsePlatform(result), nil
}
