package monitor

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHistory(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		expected int
	}{
		{"default size", 0, DefaultHistorySize},
		{"negative size", -1, DefaultHistorySize},
		{"custom size", 100, 100},
		{"small size", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHistory(tt.size)
			assert.NotNil(t, h)
			assert.Equal(t, tt.expected, h.size)
			assert.NotNil(t, h.hosts)
		})
	}
}

func TestHistoryPush(t *testing.T) {
	h := NewHistory(10)

	metrics := &HostMetrics{
		CPU: CPUMetrics{Percent: 50.0},
		RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000},
	}

	// Push should not panic
	h.Push("host1", metrics)

	// Verify data was stored
	assert.Equal(t, 1, h.Count("host1"))

	// Push nil should be ignored
	h.Push("host1", nil)
	assert.Equal(t, 1, h.Count("host1"))
}

func TestHistoryPushMultiple(t *testing.T) {
	h := NewHistory(10)

	for i := 0; i < 5; i++ {
		metrics := &HostMetrics{
			CPU: CPUMetrics{Percent: float64(i * 10)},
			RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000},
		}
		h.Push("host1", metrics)
	}

	assert.Equal(t, 5, h.Count("host1"))

	cpu := h.GetCPUHistory("host1", 5)
	require.Len(t, cpu, 5)
	assert.Equal(t, []float64{0, 10, 20, 30, 40}, cpu)
}

func TestHistoryRingBufferOverflow(t *testing.T) {
	h := NewHistory(5) // Small buffer to test overflow

	// Push more values than buffer size
	for i := 0; i < 8; i++ {
		metrics := &HostMetrics{
			CPU: CPUMetrics{Percent: float64(i)},
			RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000},
		}
		h.Push("host1", metrics)
	}

	// Should only have last 5 values
	assert.Equal(t, 5, h.Count("host1"))

	cpu := h.GetCPUHistory("host1", 10) // Request more than available
	require.Len(t, cpu, 5)
	assert.Equal(t, []float64{3, 4, 5, 6, 7}, cpu)
}

func TestGetCPUHistory(t *testing.T) {
	h := NewHistory(10)

	// Empty history
	cpu := h.GetCPUHistory("nonexistent", 5)
	assert.Nil(t, cpu)

	// Push some data
	for i := 0; i < 7; i++ {
		metrics := &HostMetrics{
			CPU: CPUMetrics{Percent: float64(i * 10)},
			RAM: RAMMetrics{TotalBytes: 1},
		}
		h.Push("host1", metrics)
	}

	// Get all
	cpu = h.GetCPUHistory("host1", 10)
	assert.Len(t, cpu, 7)
	assert.Equal(t, []float64{0, 10, 20, 30, 40, 50, 60}, cpu)

	// Get partial
	cpu = h.GetCPUHistory("host1", 3)
	assert.Len(t, cpu, 3)
	assert.Equal(t, []float64{40, 50, 60}, cpu)

	// Get zero
	cpu = h.GetCPUHistory("host1", 0)
	assert.Nil(t, cpu)
}

func TestGetRAMHistory(t *testing.T) {
	h := NewHistory(10)

	// Empty history
	ram := h.GetRAMHistory("nonexistent", 5)
	assert.Nil(t, ram)

	// Push data with varying RAM usage
	for i := 1; i <= 5; i++ {
		metrics := &HostMetrics{
			RAM: RAMMetrics{
				UsedBytes:  int64(i * 1000000000), // 1GB, 2GB, 3GB, 4GB, 5GB
				TotalBytes: 10000000000,           // 10GB
			},
		}
		h.Push("host1", metrics)
	}

	ram = h.GetRAMHistory("host1", 5)
	require.Len(t, ram, 5)

	// Should be percentages: 10%, 20%, 30%, 40%, 50%
	assert.InDelta(t, 10.0, ram[0], 0.1)
	assert.InDelta(t, 20.0, ram[1], 0.1)
	assert.InDelta(t, 30.0, ram[2], 0.1)
	assert.InDelta(t, 40.0, ram[3], 0.1)
	assert.InDelta(t, 50.0, ram[4], 0.1)
}

func TestGetGPUHistory(t *testing.T) {
	h := NewHistory(10)

	// No GPU history initially
	gpu := h.GetGPUHistory("host1", 5)
	assert.Nil(t, gpu)

	// Push metrics without GPU
	metrics := &HostMetrics{
		CPU: CPUMetrics{Percent: 50},
		RAM: RAMMetrics{TotalBytes: 1},
		GPU: nil,
	}
	h.Push("host1", metrics)
	gpu = h.GetGPUHistory("host1", 5)
	assert.Nil(t, gpu)

	// Push metrics with GPU
	for i := 0; i < 3; i++ {
		metrics := &HostMetrics{
			GPU: &GPUMetrics{Percent: float64(i * 25)},
			RAM: RAMMetrics{TotalBytes: 1},
		}
		h.Push("host1", metrics)
	}

	gpu = h.GetGPUHistory("host1", 5)
	require.Len(t, gpu, 3)
	assert.Equal(t, []float64{0, 25, 50}, gpu)
}

func TestGetNetworkHistory(t *testing.T) {
	h := NewHistory(10)

	// No network history
	bytesIn, bytesOut := h.GetNetworkHistory("host1", "eth0", 5)
	assert.Nil(t, bytesIn)
	assert.Nil(t, bytesOut)

	// Push network data
	for i := 1; i <= 4; i++ {
		metrics := &HostMetrics{
			RAM: RAMMetrics{TotalBytes: 1},
			Network: []NetworkInterface{
				{
					Name:     "eth0",
					BytesIn:  int64(i * 1000),
					BytesOut: int64(i * 500),
				},
			},
		}
		h.Push("host1", metrics)
	}

	bytesIn, bytesOut = h.GetNetworkHistory("host1", "eth0", 4)
	require.Len(t, bytesIn, 4)
	require.Len(t, bytesOut, 4)
	assert.Equal(t, []float64{1000, 2000, 3000, 4000}, bytesIn)
	assert.Equal(t, []float64{500, 1000, 1500, 2000}, bytesOut)

	// Non-existent interface
	bytesIn, bytesOut = h.GetNetworkHistory("host1", "wlan0", 5)
	assert.Nil(t, bytesIn)
	assert.Nil(t, bytesOut)
}

func TestHistoryClear(t *testing.T) {
	h := NewHistory(10)

	metrics := &HostMetrics{
		CPU: CPUMetrics{Percent: 50},
		RAM: RAMMetrics{TotalBytes: 1},
	}
	h.Push("host1", metrics)
	h.Push("host2", metrics)

	assert.Equal(t, 1, h.Count("host1"))
	assert.Equal(t, 1, h.Count("host2"))

	h.Clear("host1")
	assert.Equal(t, 0, h.Count("host1"))
	assert.Equal(t, 1, h.Count("host2"))
}

func TestHistoryClearAll(t *testing.T) {
	h := NewHistory(10)

	metrics := &HostMetrics{
		CPU: CPUMetrics{Percent: 50},
		RAM: RAMMetrics{TotalBytes: 1},
	}
	h.Push("host1", metrics)
	h.Push("host2", metrics)
	h.Push("host3", metrics)

	h.ClearAll()
	assert.Equal(t, 0, h.Count("host1"))
	assert.Equal(t, 0, h.Count("host2"))
	assert.Equal(t, 0, h.Count("host3"))
}

func TestHistoryConcurrency(t *testing.T) {
	h := NewHistory(100)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				metrics := &HostMetrics{
					CPU: CPUMetrics{Percent: float64(j)},
					RAM: RAMMetrics{TotalBytes: 1},
				}
				h.Push("host1", metrics)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				h.GetCPUHistory("host1", 10)
				h.GetRAMHistory("host1", 10)
				h.Count("host1")
			}
		}()
	}

	wg.Wait()

	// Should have some data (exact count depends on timing)
	assert.Greater(t, h.Count("host1"), 0)
}

func TestRingBuffer(t *testing.T) {
	t.Run("basic operations", func(t *testing.T) {
		rb := newRingBuffer(5)
		assert.Equal(t, 0, rb.count)

		rb.push(1.0)
		rb.push(2.0)
		rb.push(3.0)

		assert.Equal(t, 3, rb.count)

		all := rb.getAll()
		assert.Equal(t, []float64{1.0, 2.0, 3.0}, all)
	})

	t.Run("overflow wrapping", func(t *testing.T) {
		rb := newRingBuffer(3)

		rb.push(1.0)
		rb.push(2.0)
		rb.push(3.0)
		rb.push(4.0) // Overwrites 1.0
		rb.push(5.0) // Overwrites 2.0

		assert.Equal(t, 3, rb.count)

		all := rb.getAll()
		assert.Equal(t, []float64{3.0, 4.0, 5.0}, all)
	})

	t.Run("getLast partial", func(t *testing.T) {
		rb := newRingBuffer(10)

		for i := 1; i <= 7; i++ {
			rb.push(float64(i))
		}

		last3 := rb.getLast(3)
		assert.Equal(t, []float64{5.0, 6.0, 7.0}, last3)

		last5 := rb.getLast(5)
		assert.Equal(t, []float64{3.0, 4.0, 5.0, 6.0, 7.0}, last5)
	})

	t.Run("getLast more than available", func(t *testing.T) {
		rb := newRingBuffer(10)

		rb.push(1.0)
		rb.push(2.0)

		last10 := rb.getLast(10)
		assert.Equal(t, []float64{1.0, 2.0}, last10)
	})

	t.Run("getLast zero or negative", func(t *testing.T) {
		rb := newRingBuffer(5)
		rb.push(1.0)

		assert.Nil(t, rb.getLast(0))
		assert.Nil(t, rb.getLast(-1))
	})

	t.Run("empty buffer", func(t *testing.T) {
		rb := newRingBuffer(5)

		assert.Nil(t, rb.getLast(1))
		assert.Nil(t, rb.getAll())
	})
}

func TestMultipleHosts(t *testing.T) {
	h := NewHistory(10)

	// Push data for different hosts
	for i := 0; i < 5; i++ {
		h.Push("host1", &HostMetrics{
			CPU: CPUMetrics{Percent: float64(i * 10)},
			RAM: RAMMetrics{TotalBytes: 1},
		})
		h.Push("host2", &HostMetrics{
			CPU: CPUMetrics{Percent: float64(i * 20)},
			RAM: RAMMetrics{TotalBytes: 1},
		})
	}

	// Verify hosts have independent histories
	cpu1 := h.GetCPUHistory("host1", 5)
	cpu2 := h.GetCPUHistory("host2", 5)

	assert.Equal(t, []float64{0, 10, 20, 30, 40}, cpu1)
	assert.Equal(t, []float64{0, 20, 40, 60, 80}, cpu2)
}

func TestNetworkRateToPercent(t *testing.T) {
	tests := []struct {
		name        string
		bytesPerSec float64
		wantMin     float64
		wantMax     float64
	}{
		{
			name:        "zero rate",
			bytesPerSec: 0,
			wantMin:     0,
			wantMax:     0,
		},
		{
			name:        "negative rate",
			bytesPerSec: -100,
			wantMin:     0,
			wantMax:     0,
		},
		{
			name:        "very high rate over 100MB/s",
			bytesPerSec: 150 * 1024 * 1024,
			wantMin:     100,
			wantMax:     100,
		},
		{
			name:        "high rate 10-100MB/s",
			bytesPerSec: 50 * 1024 * 1024,
			wantMin:     80,
			wantMax:     100,
		},
		{
			name:        "medium rate 1-10MB/s",
			bytesPerSec: 5 * 1024 * 1024,
			wantMin:     60,
			wantMax:     80,
		},
		{
			name:        "low rate 100KB-1MB/s",
			bytesPerSec: 500 * 1024,
			wantMin:     40,
			wantMax:     60,
		},
		{
			name:        "very low rate 10-100KB/s",
			bytesPerSec: 50 * 1024,
			wantMin:     20,
			wantMax:     40,
		},
		{
			name:        "tiny rate 1-10KB/s",
			bytesPerSec: 5 * 1024,
			wantMin:     5,
			wantMax:     20,
		},
		{
			name:        "minimal rate under 1KB/s",
			bytesPerSec: 500,
			wantMin:     0,
			wantMax:     5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := networkRateToPercent(tt.bytesPerSec)
			assert.GreaterOrEqual(t, result, tt.wantMin)
			assert.LessOrEqual(t, result, tt.wantMax)
		})
	}
}

func TestGetNetworkRates(t *testing.T) {
	h := NewHistory(10)

	// No history for host
	rates := h.GetNetworkRates("nonexistent", 1.0)
	assert.Nil(t, rates)

	// Zero interval
	rates = h.GetNetworkRates("host1", 0)
	assert.Nil(t, rates)

	// Push network data (need at least 2 samples to calculate rate)
	for i := 1; i <= 3; i++ {
		h.Push("host1", &HostMetrics{
			RAM: RAMMetrics{TotalBytes: 1},
			Network: []NetworkInterface{
				{
					Name:     "eth0",
					BytesIn:  int64(i * 1000),
					BytesOut: int64(i * 500),
				},
			},
		})
	}

	rates = h.GetNetworkRates("host1", 1.0)
	assert.NotEmpty(t, rates)
	assert.Equal(t, "eth0", rates[0].Interface)
	assert.InDelta(t, 1000, rates[0].BytesInPerSec, 0.1)
	assert.InDelta(t, 500, rates[0].BytesOutPerSec, 0.1)
}

func TestGetNetworkRates_CounterWrap(t *testing.T) {
	h := NewHistory(10)

	// Simulate counter wraparound (value decreases)
	h.Push("host1", &HostMetrics{
		RAM: RAMMetrics{TotalBytes: 1},
		Network: []NetworkInterface{
			{Name: "eth0", BytesIn: 10000, BytesOut: 5000},
		},
	})
	h.Push("host1", &HostMetrics{
		RAM: RAMMetrics{TotalBytes: 1},
		Network: []NetworkInterface{
			{Name: "eth0", BytesIn: 1000, BytesOut: 500}, // Counter reset
		},
	})

	rates := h.GetNetworkRates("host1", 1.0)
	assert.NotEmpty(t, rates)
	// Should handle negative delta gracefully (clamp to 0)
	assert.Equal(t, 0.0, rates[0].BytesInPerSec)
	assert.Equal(t, 0.0, rates[0].BytesOutPerSec)
}

func TestGetTotalNetworkRate(t *testing.T) {
	h := NewHistory(10)

	// No history
	inRate, outRate := h.GetTotalNetworkRate("nonexistent", 1.0)
	assert.Equal(t, 0.0, inRate)
	assert.Equal(t, 0.0, outRate)

	// Push data for multiple interfaces including loopback
	for i := 1; i <= 3; i++ {
		h.Push("host1", &HostMetrics{
			RAM: RAMMetrics{TotalBytes: 1},
			Network: []NetworkInterface{
				{Name: "lo", BytesIn: int64(i * 100), BytesOut: int64(i * 100)},
				{Name: "eth0", BytesIn: int64(i * 1000), BytesOut: int64(i * 500)},
				{Name: "wlan0", BytesIn: int64(i * 2000), BytesOut: int64(i * 1000)},
			},
		})
	}

	inRate, outRate = h.GetTotalNetworkRate("host1", 1.0)
	// Should skip loopback (lo) and sum eth0 + wlan0
	assert.InDelta(t, 3000, inRate, 0.1)  // 1000 + 2000
	assert.InDelta(t, 1500, outRate, 0.1) // 500 + 1000
}

func TestGetNetworkRateHistory(t *testing.T) {
	h := NewHistory(20)

	// No history
	result := h.GetNetworkRateHistory("nonexistent", 10, 1.0)
	assert.Nil(t, result)

	// Zero interval
	result = h.GetNetworkRateHistory("host1", 10, 0)
	assert.Nil(t, result)

	// Push sufficient network data
	for i := 1; i <= 10; i++ {
		h.Push("host1", &HostMetrics{
			RAM: RAMMetrics{TotalBytes: 1},
			Network: []NetworkInterface{
				{Name: "eth0", BytesIn: int64(i * 1000), BytesOut: int64(i * 500)},
			},
		})
	}

	result = h.GetNetworkRateHistory("host1", 5, 1.0)
	assert.NotNil(t, result)
	// Should return rate values as percentages (0-100)
	for _, v := range result {
		assert.GreaterOrEqual(t, v, 0.0)
		assert.LessOrEqual(t, v, 100.0)
	}
}

func TestGetNetworkRateHistory_SkipsLoopback(t *testing.T) {
	h := NewHistory(20)

	// Push data only for loopback
	for i := 1; i <= 5; i++ {
		h.Push("host1", &HostMetrics{
			RAM: RAMMetrics{TotalBytes: 1},
			Network: []NetworkInterface{
				{Name: "lo0", BytesIn: int64(i * 1000), BytesOut: int64(i * 1000)},
			},
		})
	}

	// Should return nil since only loopback exists
	result := h.GetNetworkRateHistory("host1", 3, 1.0)
	assert.Nil(t, result)
}

func TestDefaultHistorySize(t *testing.T) {
	assert.Equal(t, 600, DefaultHistorySize)
}

func TestRingBuffer_getAll(t *testing.T) {
	rb := newRingBuffer(5)

	// Empty buffer
	all := rb.getAll()
	assert.Nil(t, all)

	// Partial fill
	rb.push(1.0)
	rb.push(2.0)
	rb.push(3.0)
	all = rb.getAll()
	assert.Equal(t, []float64{1.0, 2.0, 3.0}, all)

	// Full buffer
	rb.push(4.0)
	rb.push(5.0)
	all = rb.getAll()
	assert.Equal(t, []float64{1.0, 2.0, 3.0, 4.0, 5.0}, all)

	// Overflow
	rb.push(6.0)
	all = rb.getAll()
	assert.Equal(t, []float64{2.0, 3.0, 4.0, 5.0, 6.0}, all)
}
