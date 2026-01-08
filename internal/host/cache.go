package host

import (
	"sync"
)

// ConnectionCache provides thread-safe caching of SSH connections by host name.
// This allows connection reuse across multiple commands in a session.
type ConnectionCache struct {
	mu    sync.Mutex
	conns map[string]*Connection
}

// NewConnectionCache creates a new empty connection cache.
func NewConnectionCache() *ConnectionCache {
	return &ConnectionCache{
		conns: make(map[string]*Connection),
	}
}

// Get retrieves a cached connection for the given host name.
// Returns nil if no cached connection exists or if the cached connection is dead.
func (c *ConnectionCache) Get(hostName string) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, ok := c.conns[hostName]
	if !ok {
		return nil
	}

	// Verify connection is still alive
	if !c.isAlive(conn) {
		conn.Close() //nolint:errcheck // Cleanup, error not actionable
		delete(c.conns, hostName)
		return nil
	}

	return conn
}

// Set stores a connection in the cache for the given host name.
// If a connection already exists for this host, it is closed and replaced.
func (c *ConnectionCache) Set(hostName string, conn *Connection) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close existing connection if present
	if existing, ok := c.conns[hostName]; ok {
		existing.Close() //nolint:errcheck // Cleanup, error not actionable
	}

	c.conns[hostName] = conn
}

// Clear removes a cached connection for the given host name.
// The connection is closed before removal.
func (c *ConnectionCache) Clear(hostName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if conn, ok := c.conns[hostName]; ok {
		conn.Close() //nolint:errcheck // Cleanup, error not actionable
		delete(c.conns, hostName)
	}
}

// CloseAll closes all cached connections and clears the cache.
// This should be called during application shutdown.
func (c *ConnectionCache) CloseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, conn := range c.conns {
		conn.Close() //nolint:errcheck // Cleanup, error not actionable
		delete(c.conns, name)
	}
}

// Size returns the number of cached connections.
func (c *ConnectionCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.conns)
}

// Hosts returns a list of host names with cached connections.
func (c *ConnectionCache) Hosts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	hosts := make([]string, 0, len(c.conns))
	for name := range c.conns {
		hosts = append(hosts, name)
	}
	return hosts
}

// isAlive checks if a connection is still usable.
func (c *ConnectionCache) isAlive(conn *Connection) bool {
	if conn == nil || conn.Client == nil {
		return false
	}

	// Try to create a session as a quick health check
	session, err := conn.Client.NewSession()
	if err != nil {
		return false
	}
	session.Close() //nolint:errcheck // Session close error is not meaningful in health check
	return true
}

// Global connection cache for use across the application.
// This is initialized lazily and should be closed with CloseAll() on exit.
var globalCache *ConnectionCache
var globalCacheOnce sync.Once

// GlobalCache returns the global connection cache singleton.
func GlobalCache() *ConnectionCache {
	globalCacheOnce.Do(func() {
		globalCache = NewConnectionCache()
	})
	return globalCache
}
