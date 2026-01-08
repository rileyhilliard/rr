package monitor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewPool(t *testing.T) {
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
			pool := NewPool(tt.timeout)
			assert.NotNil(t, pool)
			assert.NotNil(t, pool.connections)
			assert.Equal(t, 0, pool.Size())

			if tt.timeout == 0 {
				assert.Equal(t, 10*time.Second, pool.timeout)
			} else {
				assert.Equal(t, tt.timeout, pool.timeout)
			}
		})
	}
}

func TestPoolClose(t *testing.T) {
	pool := NewPool(10 * time.Second)
	assert.NotNil(t, pool)

	// Close empty pool should not panic
	pool.Close()
	assert.Equal(t, 0, pool.Size())
}

func TestPoolCloseOne(t *testing.T) {
	pool := NewPool(10 * time.Second)
	assert.NotNil(t, pool)

	// CloseOne on non-existent alias should not panic
	pool.CloseOne("nonexistent")
	assert.Equal(t, 0, pool.Size())
}

func TestPoolReturn(t *testing.T) {
	pool := NewPool(10 * time.Second)
	assert.NotNil(t, pool)

	// Return on non-existent alias should not panic
	pool.Return("nonexistent", nil)
	assert.Equal(t, 0, pool.Size())
}

func TestPoolSize(t *testing.T) {
	pool := NewPool(10 * time.Second)
	assert.Equal(t, 0, pool.Size())
}

func TestPoolIsAlive_NilClient(t *testing.T) {
	pool := NewPool(10 * time.Second)

	// Nil client should return false
	assert.False(t, pool.isAlive(nil))
}

// Note: Tests that require actual SSH connections are integration tests
// and would need real hosts or mocking. The following tests verify
// the pool behavior without actual connections.

func TestPoolConcurrency(t *testing.T) {
	pool := NewPool(10 * time.Second)
	done := make(chan bool)

	// Concurrent access should not race
	for i := 0; i < 10; i++ {
		go func() {
			pool.Size()
			pool.Return("test", nil)
			pool.CloseOne("test")
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	pool.Close()
}
