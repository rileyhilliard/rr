package monitor

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewPool(t *testing.T) {
	hosts := map[string]config.Host{
		"test": {SSH: []string{"localhost"}},
	}

	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{
			name:    "default timeout",
			timeout: 0,
		},
		{
			name:    "custom timeout",
			timeout: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewPool(hosts, tt.timeout)
			assert.NotNil(t, pool)
			assert.NotNil(t, pool.connections)
			assert.NotNil(t, pool.hosts)
			assert.Empty(t, pool.connections)

			if tt.timeout == 0 {
				assert.Equal(t, 10*time.Second, pool.timeout)
			} else {
				assert.Equal(t, tt.timeout, pool.timeout)
			}
		})
	}
}

func TestPoolClose(t *testing.T) {
	hosts := map[string]config.Host{}
	pool := NewPool(hosts, 10*time.Second)
	assert.NotNil(t, pool)

	// Close empty pool should not panic
	pool.Close()
	assert.Empty(t, pool.connections)
}

func TestPoolCloseOne(t *testing.T) {
	hosts := map[string]config.Host{}
	pool := NewPool(hosts, 10*time.Second)
	assert.NotNil(t, pool)

	// CloseOne on non-existent alias should not panic
	pool.CloseOne("nonexistent")
	assert.Empty(t, pool.connections)
}

// Note: Tests that require actual SSH connections are integration tests
// and would need real hosts or mocking. The following tests verify
// the pool behavior without actual connections.

func TestPoolConcurrency(t *testing.T) {
	hosts := map[string]config.Host{}
	pool := NewPool(hosts, 10*time.Second)
	done := make(chan bool)

	// Concurrent access should not race
	for i := 0; i < 10; i++ {
		go func() {
			pool.GetConnectedVia("test")
			pool.CloseOne("test")
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	pool.Close()
}
