package monitor

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestCenterText(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		width  int
		expect string
	}{
		{"shorter text", "hi", 10, "    hi    "},
		{"exact width", "test", 4, "test"},
		{"longer text", "hello world", 5, "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := centerText(tt.text, tt.width)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestPadToWidth(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
	}{
		{"shorter text", "hi", 10},
		{"exact width", "test", 4},
		{"longer text", "hello world", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := padToWidth(tt.text, tt.width)
			// Just verify it doesn't panic
			_ = result
		})
	}
}

func TestProcSortOrderConstants(t *testing.T) {
	assert.Equal(t, ProcSortOrder(0), ProcSortByCPU)
	assert.Equal(t, ProcSortOrder(1), ProcSortByMemory)
	assert.Equal(t, ProcSortOrder(2), ProcSortByPID)
}

func TestModel_renderDetailHeader(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	tests := []struct {
		name   string
		host   string
		status HostStatus
	}{
		{"idle", "server1", StatusIdleState},
		{"slow", "server1", StatusSlowState},
		{"unreachable", "server1", StatusUnreachableState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.renderDetailHeader(tt.host, tt.status)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, tt.host)
		})
	}
}

func TestModel_renderDetailCPUSection(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	cpu := CPUMetrics{
		Percent: 50.0,
		Cores:   8,
		LoadAvg: [3]float64{1.0, 2.0, 3.0},
	}

	tests := []struct {
		name       string
		hasHistory bool
		width      int
	}{
		{"no history", false, 80},
		{"with history", true, 80},
		{"narrow width", false, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasHistory {
				for i := 0; i < 10; i++ {
					m.history.Push("server1", &HostMetrics{
						CPU: CPUMetrics{Percent: float64(i * 10)},
						RAM: RAMMetrics{TotalBytes: 1},
					})
				}
			}

			result := m.renderDetailCPUSection("server1", cpu, tt.width)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, "CPU")
		})
	}
}

func TestModel_renderDetailRAMSection(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	ram := RAMMetrics{
		UsedBytes:  4000000000,
		TotalBytes: 8000000000,
		Available:  4000000000,
		Cached:     1000000000,
	}

	tests := []struct {
		name       string
		hasHistory bool
		width      int
	}{
		{"no history", false, 80},
		{"with history", true, 80},
		{"narrow width", false, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasHistory {
				for i := 0; i < 10; i++ {
					m.history.Push("server1", &HostMetrics{
						CPU: CPUMetrics{Percent: 50},
						RAM: RAMMetrics{UsedBytes: int64(i * 1000000000), TotalBytes: 8000000000},
					})
				}
			}

			result := m.renderDetailRAMSection("server1", ram, tt.width)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, "Memory")
		})
	}
}

func TestModel_renderDetailGPUSection(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	tests := []struct {
		name  string
		gpu   *GPUMetrics
		width int
	}{
		{"basic GPU", &GPUMetrics{
			Name:    "NVIDIA RTX 3080",
			Percent: 75.0,
		}, 80},
		{"GPU with all stats", &GPUMetrics{
			Name:        "NVIDIA RTX 3080",
			Percent:     75.0,
			MemoryUsed:  4000000000,
			MemoryTotal: 10000000000,
			Temperature: 65,
			PowerWatts:  250,
		}, 80},
		{"GPU high temp", &GPUMetrics{
			Name:        "GPU",
			Percent:     90.0,
			Temperature: 85,
		}, 80},
		{"GPU warning temp", &GPUMetrics{
			Name:        "GPU",
			Percent:     70.0,
			Temperature: 72,
		}, 80},
		{"narrow width", &GPUMetrics{
			Name:    "GPU",
			Percent: 50.0,
		}, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.renderDetailGPUSection("server1", tt.gpu, tt.width)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, "GPU")
		})
	}
}

func TestModel_renderDetailNetworkSection(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.interval = time.Second

	tests := []struct {
		name       string
		hasHistory bool
		width      int
	}{
		{"no history", false, 80},
		{"with history", true, 80},
		{"narrow width", false, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasHistory {
				for i := 1; i <= 10; i++ {
					m.history.Push("server1", &HostMetrics{
						RAM: RAMMetrics{TotalBytes: 1},
						Network: []NetworkInterface{
							{Name: "eth0", BytesIn: int64(i * 10000), BytesOut: int64(i * 5000)},
						},
					})
				}
			}

			result := m.renderDetailNetworkSection("server1", tt.width)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, "Network")
		})
	}
}

func TestModel_renderDetailProcessSection(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	tests := []struct {
		name  string
		procs []ProcessInfo
		width int
	}{
		{"empty procs", []ProcessInfo{}, 80},
		{"single proc", []ProcessInfo{
			{PID: 1234, User: "root", CPU: 50.0, Memory: 25.0, Command: "/usr/bin/test"},
		}, 80},
		{"multiple procs", []ProcessInfo{
			{PID: 1, User: "root", CPU: 90.0, Memory: 50.0, Command: "/usr/bin/high"},
			{PID: 2, User: "user", CPU: 50.0, Memory: 25.0, Command: "/usr/bin/medium"},
			{PID: 3, User: "user", CPU: 10.0, Memory: 5.0, Command: "/usr/bin/low"},
		}, 80},
		{"long username", []ProcessInfo{
			{PID: 1, User: "very_long_username", CPU: 50.0, Memory: 25.0, Command: "/cmd"},
		}, 80},
		{"long command", []ProcessInfo{
			{PID: 1, User: "root", CPU: 50.0, Memory: 25.0, Command: "/very/long/command/path/that/should/be/truncated/in/the/output"},
		}, 80},
		{"narrow width", []ProcessInfo{
			{PID: 1, User: "root", CPU: 50.0, Memory: 25.0, Command: "/cmd"},
		}, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.renderDetailProcessSection(tt.procs, tt.width)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, "Processes")
		})
	}
}

func TestModel_renderDetailFooter(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	result := m.renderDetailFooter()
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Esc")
	assert.Contains(t, result, "quit")
}

func TestModel_renderDetailFooterWithScroll(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// Without viewport ready
	result := m.renderDetailFooterWithScroll()
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Esc")
}

func TestModel_renderDetailViewWithViewport(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.height = 40

	tests := []struct {
		name       string
		hasMetrics bool
		hasGPU     bool
		wideLayout bool
	}{
		{"no metrics", false, false, false},
		{"with metrics", true, false, false},
		{"with GPU", true, true, false},
		{"wide layout", true, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasMetrics {
				metrics := &HostMetrics{
					CPU: CPUMetrics{Percent: 50.0, LoadAvg: [3]float64{1.0, 2.0, 3.0}, Cores: 8},
					RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000, Available: 4000000000},
					Processes: []ProcessInfo{
						{PID: 1, User: "root", CPU: 50.0, Memory: 25.0, Command: "/cmd"},
					},
				}
				if tt.hasGPU {
					metrics.GPU = &GPUMetrics{
						Name:        "NVIDIA RTX 3080",
						Percent:     75.0,
						MemoryUsed:  4000000000,
						MemoryTotal: 10000000000,
						Temperature: 65,
					}
				}
				m.metrics["server1"] = metrics
				m.status["server1"] = StatusIdleState
			}

			if tt.wideLayout {
				m.width = 200
			}

			result := m.renderDetailViewWithViewport()
			assert.NotEmpty(t, result)
		})
	}
}

func TestModel_renderDetailViewWithViewport_NoHost(t *testing.T) {
	m := Model{
		hosts:    []string{},
		selected: -1,
	}

	result := m.renderDetailViewWithViewport()
	assert.Contains(t, result, "No host selected")
}
