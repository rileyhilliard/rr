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
	client       *sshutil.Client
	platform     Platform
	lastUsed     time.Time
	connectedVia string // SSH alias that successfully connected (e.g., "m4-tailscale")
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

// connectionResult holds the result of a parallel connection attempt.
type connectionResult struct {
	client  *sshutil.Client
	sshAddr string
	index   int // position in the SSH list (lower = preferred)
	err     error
}

// Get retrieves an existing connection for the given alias, or creates a new one.
// Connections are reused without preemptive health checks - if a connection has died,
// the caller will get an error when they try to use it and should call CloseOne() to
// remove it from the pool, then retry.
// The alias is looked up in the hosts config to get the actual SSH addresses to try.
// Multiple SSH addresses are tried in parallel - the first to connect wins, but
// earlier addresses in the list are preferred if they connect within a short window.
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
			client:       client,
			lastUsed:     time.Now(),
			connectedVia: alias, // Used alias directly
		}
		p.mu.Unlock()
		return client, nil
	}

	// Single address - no need for parallel logic
	if len(host.SSH) == 1 {
		client, err := sshutil.Dial(host.SSH[0], p.timeout)
		if err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.connections[alias] = &poolEntry{
			client:       client,
			lastUsed:     time.Now(),
			connectedVia: host.SSH[0],
		}
		p.mu.Unlock()
		return client, nil
	}

	// Multiple addresses - try in parallel, prefer earlier ones
	return p.connectParallel(alias, host.SSH)
}

// connectParallel tries multiple SSH addresses concurrently.
// It prefers earlier addresses in the list (e.g., LAN over VPN) but won't block
// waiting for them if a later address connects first. If a preferred address
// connects within 500ms of a less-preferred one, the preferred one wins.
func (p *Pool) connectParallel(alias string, addresses []string) (*sshutil.Client, error) {
	results := make(chan connectionResult, len(addresses))

	// Start all connection attempts in parallel
	for i, addr := range addresses {
		go func(idx int, sshAddr string) {
			client, err := sshutil.Dial(sshAddr, p.timeout)
			results <- connectionResult{
				client:  client,
				sshAddr: sshAddr,
				index:   idx,
				err:     err,
			}
		}(i, addr)
	}

	// Collect results, preferring earlier addresses
	var bestResult *connectionResult
	var errors []error
	received := 0

	// Grace period to wait for a more-preferred connection after first success
	const preferenceGrace = 500 * time.Millisecond
	var graceTimer <-chan time.Time

	for received < len(addresses) {
		select {
		case r := <-results:
			received++

			if r.err != nil {
				errors = append(errors, r.err)
				continue
			}

			// Got a successful connection
			if bestResult == nil {
				// First success - start grace timer for preferred connections
				bestResult = &r
				if r.index > 0 {
					// Not the most preferred, wait briefly for better options
					graceTimer = time.After(preferenceGrace)
				} else {
					// Got the most preferred, use it immediately
					p.storeConnection(alias, r.client, r.sshAddr)
					// Close any other pending connections as they arrive
					go p.drainAndClose(results, received, len(addresses), r.sshAddr)
					return r.client, nil
				}
			} else if r.index < bestResult.index {
				// Found a more preferred connection, switch to it
				_ = bestResult.client.Close()
				bestResult = &r
				if r.index == 0 {
					// Now have the most preferred, use it
					p.storeConnection(alias, r.client, r.sshAddr)
					go p.drainAndClose(results, received, len(addresses), r.sshAddr)
					return r.client, nil
				}
			} else {
				// Less preferred than current best, close it
				_ = r.client.Close()
			}

		case <-graceTimer:
			// Grace period expired, use what we have
			if bestResult != nil {
				p.storeConnection(alias, bestResult.client, bestResult.sshAddr)
				go p.drainAndClose(results, received, len(addresses), bestResult.sshAddr)
				return bestResult.client, nil
			}
		}
	}

	// All attempts finished
	if bestResult != nil {
		p.storeConnection(alias, bestResult.client, bestResult.sshAddr)
		return bestResult.client, nil
	}

	// All failed
	if len(errors) > 0 {
		return nil, errors[len(errors)-1]
	}
	return nil, nil
}

// storeConnection saves a successful connection to the pool.
func (p *Pool) storeConnection(alias string, client *sshutil.Client, sshAddr string) {
	p.mu.Lock()
	p.connections[alias] = &poolEntry{
		client:       client,
		lastUsed:     time.Now(),
		connectedVia: sshAddr,
	}
	p.mu.Unlock()
}

// drainAndClose closes any remaining connections from parallel attempts.
func (p *Pool) drainAndClose(results chan connectionResult, received, total int, keepAddr string) {
	for received < total {
		r := <-results
		received++
		if r.client != nil && r.sshAddr != keepAddr {
			_ = r.client.Close()
		}
	}
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

// GetConnectedVia returns the SSH alias that was used to connect to the given host.
// Returns empty string if no connection exists for this alias.
func (p *Pool) GetConnectedVia(alias string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.connections[alias]; ok {
		return entry.connectedVia
	}
	return ""
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
