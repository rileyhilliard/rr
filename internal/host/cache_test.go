package host

import (
	"testing"
)

func TestNewConnectionCache(t *testing.T) {
	cache := NewConnectionCache()
	if cache == nil {
		t.Fatal("NewConnectionCache returned nil")
	}
	if cache.Size() != 0 {
		t.Errorf("new cache size = %d, want 0", cache.Size())
	}
}

func TestConnectionCache_SetAndGet(t *testing.T) {
	cache := NewConnectionCache()

	// Create a mock connection (no actual SSH client)
	conn := &Connection{
		Name:  "test-host",
		Alias: "test-alias",
		// Client is nil, so isAlive will return false
	}

	cache.Set("test-host", conn)

	// Get should return nil because the connection is not alive (nil client)
	result := cache.Get("test-host")
	if result != nil {
		t.Error("Get should return nil for connection with nil client")
	}

	// Cache should have been cleared after failed health check
	if cache.Size() != 0 {
		t.Errorf("cache size = %d, want 0 after failed health check", cache.Size())
	}
}

func TestConnectionCache_Get_NotFound(t *testing.T) {
	cache := NewConnectionCache()

	result := cache.Get("nonexistent")
	if result != nil {
		t.Error("Get should return nil for nonexistent host")
	}
}

func TestConnectionCache_Clear(t *testing.T) {
	cache := NewConnectionCache()

	// Add a connection
	conn := &Connection{Name: "test-host"}
	cache.Set("test-host", conn)

	// Clear it
	cache.Clear("test-host")

	// Should be gone
	if cache.Size() != 0 {
		t.Errorf("cache size = %d after Clear, want 0", cache.Size())
	}
}

func TestConnectionCache_Clear_NotFound(t *testing.T) {
	cache := NewConnectionCache()

	// Should not panic
	cache.Clear("nonexistent")
}

func TestConnectionCache_CloseAll(t *testing.T) {
	cache := NewConnectionCache()

	// Add multiple connections
	cache.Set("host1", &Connection{Name: "host1"})
	cache.Set("host2", &Connection{Name: "host2"})
	cache.Set("host3", &Connection{Name: "host3"})

	// Close all
	cache.CloseAll()

	if cache.Size() != 0 {
		t.Errorf("cache size = %d after CloseAll, want 0", cache.Size())
	}
}

func TestConnectionCache_Hosts(t *testing.T) {
	cache := NewConnectionCache()

	// Empty cache should return empty slice
	hosts := cache.Hosts()
	if len(hosts) != 0 {
		t.Errorf("Hosts() on empty cache returned %d items, want 0", len(hosts))
	}

	// Add some connections (they will be added but Get will remove them on health check)
	cache.mu.Lock()
	cache.conns["host1"] = &Connection{Name: "host1"}
	cache.conns["host2"] = &Connection{Name: "host2"}
	cache.mu.Unlock()

	hosts = cache.Hosts()
	if len(hosts) != 2 {
		t.Errorf("Hosts() returned %d items, want 2", len(hosts))
	}

	// Verify hosts are in the list (order is not guaranteed)
	hostMap := make(map[string]bool)
	for _, h := range hosts {
		hostMap[h] = true
	}
	if !hostMap["host1"] || !hostMap["host2"] {
		t.Errorf("Hosts() = %v, expected [host1, host2]", hosts)
	}
}

func TestConnectionCache_Set_ReplacesExisting(t *testing.T) {
	cache := NewConnectionCache()

	// Add first connection
	conn1 := &Connection{Name: "test-host", Alias: "alias1"}
	cache.mu.Lock()
	cache.conns["test-host"] = conn1
	cache.mu.Unlock()

	// Replace with second connection
	conn2 := &Connection{Name: "test-host", Alias: "alias2"}
	cache.Set("test-host", conn2)

	// Should have replaced (directly check internal state)
	cache.mu.Lock()
	stored := cache.conns["test-host"]
	cache.mu.Unlock()

	if stored != conn2 {
		t.Error("Set should replace existing connection")
	}
}

func TestGlobalCache(t *testing.T) {
	cache1 := GlobalCache()
	if cache1 == nil {
		t.Fatal("GlobalCache returned nil")
	}

	cache2 := GlobalCache()
	if cache1 != cache2 {
		t.Error("GlobalCache should return the same instance")
	}
}

func TestConnectionCache_ThreadSafety(t *testing.T) {
	cache := NewConnectionCache()

	// Run concurrent operations
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			hostName := "host"
			conn := &Connection{Name: hostName}
			cache.Set(hostName, conn)
			cache.Get(hostName)
			cache.Size()
			cache.Hosts()
			cache.Clear(hostName)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we get here without deadlock or panic, the test passes
}
