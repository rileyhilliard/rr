package require

import "sync"

// Cache stores requirement check results per host for the session lifetime.
// This avoids repeated SSH round-trips when running multiple commands.
type Cache struct {
	mu      sync.Mutex
	results map[string]map[string]CheckResult // hostName -> toolName -> result
}

// globalCache is the package-level cache shared across all workflow invocations.
var globalCache = &Cache{
	results: make(map[string]map[string]CheckResult),
}

// GlobalCache returns the shared session-level cache.
func GlobalCache() *Cache {
	return globalCache
}

// NewCache creates a new cache instance (useful for testing).
func NewCache() *Cache {
	return &Cache{
		results: make(map[string]map[string]CheckResult),
	}
}

// Get retrieves a cached result for a host/tool combination.
func (c *Cache) Get(host, tool string) (CheckResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if hostResults, ok := c.results[host]; ok {
		if result, ok := hostResults[tool]; ok {
			return result, true
		}
	}
	return CheckResult{}, false
}

// Set stores a result for a host/tool combination.
func (c *Cache) Set(host, tool string, result CheckResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.results[host] == nil {
		c.results[host] = make(map[string]CheckResult)
	}
	c.results[host][tool] = result
}

// Clear removes all cached results for a host.
func (c *Cache) Clear(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.results, host)
}

// ClearAll removes all cached results.
func (c *Cache) ClearAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.results = make(map[string]map[string]CheckResult)
}
