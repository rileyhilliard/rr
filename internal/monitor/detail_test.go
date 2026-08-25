package monitor

import (
	"fmt"
	"strings"
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
			result := m.renderDetailProcessSection(tt.procs, 0, tt.width)
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

func TestModel_renderDetailCPUSection_PerCoreStrip(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	// No per-core data: no heat strip cells (placeholder rows are spaces)
	cpu := CPUMetrics{Percent: 50.0, Cores: 4, LoadAvg: [3]float64{1.0, 2.0, 3.0}}
	result := m.renderDetailCPUSection("server1", cpu, 80)
	assert.NotContains(t, result, "▰")

	// With per-core data: heat strip rendered
	cpu.PerCore = []float64{10, 50, 80, 95}
	result = m.renderDetailCPUSection("server1", cpu, 80)
	assert.Contains(t, result, "▰")
}

func TestModel_renderDetailCPUSection_Temperature(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	// Temp of 0 (unavailable) is hidden
	cpu := CPUMetrics{Percent: 50.0, Cores: 4}
	result := m.renderDetailCPUSection("server1", cpu, 80)
	assert.NotContains(t, result, "45C")

	// Temp > 0 shows in the header
	cpu.TempC = 45.0
	result = m.renderDetailCPUSection("server1", cpu, 80)
	assert.Contains(t, result, "45C")
}

func TestModel_renderDetailCPUSection_WarmingUp(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	// Linux first sample: Cores set but no per-core deltas yet. The bogus 0.0%
	// must not be shown as a real reading.
	cpu := CPUMetrics{Percent: 0, Cores: 8, FirstSample: true}
	result := m.renderDetailCPUSection("server1", cpu, 80)
	assert.Contains(t, result, cpuWarmingUpText)
	assert.NotContains(t, result, "0.0%")

	// Once per-core deltas arrive, the real percentage is shown
	cpu.FirstSample = false
	cpu.Percent = 12.5
	cpu.PerCore = []float64{10, 15, 12, 13, 11, 14, 12, 13}
	result = m.renderDetailCPUSection("server1", cpu, 80)
	assert.NotContains(t, result, cpuWarmingUpText)
	assert.Contains(t, result, "12.5%")
}

func TestModel_renderCardCPUSection_WarmingUp(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 160

	cpu := CPUMetrics{Percent: 0, Cores: 8, LoadAvg: [3]float64{1.5, 1.2, 1.0}, FirstSample: true}
	full := strings.Join(m.renderCardCPUSection("server1", cpu, 60), "\n")
	assert.Contains(t, full, cpuWarmingUpText)

	compact := strings.Join(m.renderCompactCPUSection("server1", cpu, 60), "\n")
	assert.Contains(t, compact, cpuWarmingUpText)

	minimal := strings.Join(m.renderMinimalCPUSection("server1", cpu, 60), "\n")
	assert.Contains(t, minimal, cpuWarmingUpText)
}

func TestRenderPercentStats(t *testing.T) {
	assert.Equal(t, "Waiting for data...", renderPercentStats(nil))

	// Peak and average over the full window
	got := renderPercentStats([]float64{10, 20, 60})
	assert.Contains(t, got, "Peak: 60.0%")
	assert.Contains(t, got, "Avg: 30.0%")
}

func TestModel_renderDetailSections_PeakAvgStats(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	// Push history so the sections have a window to summarize
	for _, pct := range []float64{20, 40, 80} {
		m.history.Push("server1", &HostMetrics{
			CPU: CPUMetrics{Percent: pct, Cores: 4, PerCore: []float64{pct}},
			GPU: &GPUMetrics{Percent: pct},
		})
	}

	cpuSection := m.renderDetailCPUSection("server1", CPUMetrics{Percent: 80, Cores: 4, PerCore: []float64{80}}, 80)
	assert.Contains(t, cpuSection, "Peak:")
	assert.Contains(t, cpuSection, "Avg:")

	gpuSection := m.renderDetailGPUSection("server1", &GPUMetrics{Percent: 80}, 80)
	assert.Contains(t, gpuSection, "Peak:")
	assert.Contains(t, gpuSection, "Avg:")
}

func TestModel_renderDetailProcessSection_SortOrder(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	procs := []ProcessInfo{
		{PID: 1, User: "root", CPU: 90, Memory: 5, Command: "cpuhog"},
		{PID: 2, User: "root", CPU: 5, Memory: 90, Command: "memhog"},
	}

	// Header reflects the active sort, and ordering follows it
	byCPU := m.renderDetailProcessSection(procs, 0, 100)
	assert.Contains(t, byCPU, "by CPU")
	assert.Less(t, strings.Index(byCPU, "cpuhog"), strings.Index(byCPU, "memhog"))

	m.procSortOrder = ProcSortByMemory
	byMem := m.renderDetailProcessSection(procs, 0, 100)
	assert.Contains(t, byMem, "by MEM")
	assert.Less(t, strings.Index(byMem, "memhog"), strings.Index(byMem, "cpuhog"))
}

func TestModel_renderDetailProcessSection_ShowsTenProcesses(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	// Collector gathers 15; the detail view lists 10
	procs := make([]ProcessInfo, 15)
	for i := range procs {
		procs[i] = ProcessInfo{PID: i + 1, User: "root", CPU: float64(15 - i), Command: fmt.Sprintf("proc%d", i)}
	}

	result := m.renderDetailProcessSection(procs, 0, 100)
	for i := 0; i < detailProcessLimit; i++ {
		assert.Contains(t, result, fmt.Sprintf("proc%d", i))
	}
	assert.NotContains(t, result, "proc10")
}

func TestModel_renderDetailDiskSection(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	tests := []struct {
		name     string
		disk     DiskMetrics
		wantRate bool
	}{
		{
			name: "usage only",
			disk: DiskMetrics{
				UsedBytes:  100 * 1024 * 1024 * 1024,
				TotalBytes: 500 * 1024 * 1024 * 1024,
				Percent:    20.0,
			},
			wantRate: false,
		},
		{
			name: "usage with io rates",
			disk: DiskMetrics{
				UsedBytes:        100 * 1024 * 1024 * 1024,
				TotalBytes:       500 * 1024 * 1024 * 1024,
				Percent:          20.0,
				ReadBytesPerSec:  2 * 1024 * 1024,
				WriteBytesPerSec: 512 * 1024,
			},
			wantRate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.renderDetailDiskSection(tt.disk, 80)
			assert.Contains(t, result, "Disk")
			assert.Contains(t, result, "20.0%")
			if tt.wantRate {
				assert.Contains(t, result, "R:")
				assert.Contains(t, result, "W:")
			} else {
				assert.NotContains(t, result, "R:")
				assert.NotContains(t, result, "W:")
			}
		})
	}
}

func TestGenerateDetailContent_DiskSection(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120

	// Without disk data: no Disk section
	m.metrics["server1"] = &HostMetrics{
		CPU: CPUMetrics{Percent: 50.0},
		RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000},
	}
	m.status["server1"] = StatusIdleState
	content := m.generateDetailContent()
	assert.NotContains(t, content, "Disk")

	// With disk data: Disk section present
	m.metrics["server1"].Disk = DiskMetrics{
		UsedBytes:  100 * 1024 * 1024 * 1024,
		TotalBytes: 500 * 1024 * 1024 * 1024,
		Percent:    20.0,
	}
	content = m.generateDetailContent()
	assert.Contains(t, content, "Disk")
}

func TestModel_renderDetailHeader_SystemInfo(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// No metrics: header has no system info line
	result := m.renderDetailHeader("server1", StatusIdleState)
	assert.NotContains(t, result, "kernel")

	// Populated SystemInfo shows OS, kernel, and uptime
	m.metrics["server1"] = &HostMetrics{
		System: SystemInfo{
			OS:     "Linux",
			Kernel: "6.8.0-64-generic",
			Uptime: 49*time.Hour + 30*time.Minute,
		},
	}
	result = m.renderDetailHeader("server1", StatusIdleState)
	assert.Contains(t, result, "Linux")
	assert.Contains(t, result, "kernel 6.8.0-64-generic")
	assert.Contains(t, result, "up 2d 1h")
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"minutes only", 42 * time.Minute, "42m"},
		{"hours and minutes", 3*time.Hour + 15*time.Minute, "3h 15m"},
		{"days and hours", 49*time.Hour + 30*time.Minute, "2d 1h"},
		{"zero", 0, "0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatUptime(tt.d))
		})
	}
}
