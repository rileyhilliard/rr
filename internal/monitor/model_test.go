package monitor

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModel(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"user@server1"}},
		"server2": {SSH: []string{"user@server2"}},
	}
	collector := NewCollector(hosts)

	m := NewModel(collector, 5*time.Second, 0, nil)

	// Should have hosts sorted alphabetically
	assert.Equal(t, []string{"server1", "server2"}, m.hosts)

	// Should initialize maps
	assert.NotNil(t, m.metrics)
	assert.NotNil(t, m.status)
	assert.NotNil(t, m.errors)

	// All hosts should start as connecting (not yet determined)
	for _, status := range m.status {
		assert.Equal(t, StatusConnectingState, status)
	}

	// Should have the collector
	assert.Equal(t, collector, m.collector)

	// Should have the interval
	assert.Equal(t, 5*time.Second, m.interval)
}

func TestHostStatus_String(t *testing.T) {
	tests := []struct {
		status HostStatus
		expect string
	}{
		{StatusConnectingState, "connecting"},
		{StatusIdleState, "idle"},
		{StatusRunningState, "running"},
		{StatusUnreachableState, "offline"},
		{HostStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			result := tt.status.String()
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestModel_OnlineCount(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
		"server2": {SSH: []string{"server2"}},
		"server3": {SSH: []string{"server3"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// Initially all unreachable
	assert.Equal(t, 0, m.OnlineCount())

	// Mark one as connected
	m.status["server1"] = StatusIdleState
	assert.Equal(t, 1, m.OnlineCount())

	// Mark another as connected
	m.status["server2"] = StatusIdleState
	assert.Equal(t, 2, m.OnlineCount())

	// Mark one as unreachable (not counted as online)
	m.status["server3"] = StatusUnreachableState
	assert.Equal(t, 2, m.OnlineCount())
}

func TestModel_SelectedHost(t *testing.T) {
	hosts := map[string]config.Host{
		"alpha": {SSH: []string{"alpha"}},
		"beta":  {SSH: []string{"beta"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// First host selected by default
	assert.Equal(t, "alpha", m.SelectedHost())

	// Change selection
	m.selected = 1
	assert.Equal(t, "beta", m.SelectedHost())

	// Invalid selection
	m.selected = 99
	assert.Equal(t, "", m.SelectedHost())

	m.selected = -1
	assert.Equal(t, "", m.SelectedHost())
}

func TestModel_SecondsSinceUpdate(t *testing.T) {
	m := Model{}

	// Zero time should return 0
	assert.Equal(t, 0, m.SecondsSinceUpdate())

	// Set last update to now
	m.lastUpdate = time.Now()
	assert.LessOrEqual(t, m.SecondsSinceUpdate(), 1)

	// Set last update to 5 seconds ago
	m.lastUpdate = time.Now().Add(-5 * time.Second)
	assert.GreaterOrEqual(t, m.SecondsSinceUpdate(), 5)
}

func TestModel_sortHosts_ByName(t *testing.T) {
	hosts := map[string]config.Host{
		"zebra":  {SSH: []string{"zebra"}},
		"alpha":  {SSH: []string{"alpha"}},
		"middle": {SSH: []string{"middle"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	m.sortOrder = SortByName
	m.sortHosts()

	assert.Equal(t, []string{"alpha", "middle", "zebra"}, m.hosts)
}

func TestModel_sortHosts_ByCPU(t *testing.T) {
	hosts := map[string]config.Host{
		"low":    {SSH: []string{"low"}},
		"high":   {SSH: []string{"high"}},
		"medium": {SSH: []string{"medium"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// Add metrics
	m.metrics["low"] = &HostMetrics{CPU: CPUMetrics{Percent: 10.0}}
	m.metrics["high"] = &HostMetrics{CPU: CPUMetrics{Percent: 90.0}}
	m.metrics["medium"] = &HostMetrics{CPU: CPUMetrics{Percent: 50.0}}

	m.sortOrder = SortByCPU
	m.sortHosts()

	// Should be sorted descending by CPU
	assert.Equal(t, "high", m.hosts[0])
	assert.Equal(t, "medium", m.hosts[1])
	assert.Equal(t, "low", m.hosts[2])
}

func TestModel_sortHosts_ByRAM(t *testing.T) {
	hosts := map[string]config.Host{
		"low":  {SSH: []string{"low"}},
		"high": {SSH: []string{"high"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// Add metrics
	m.metrics["low"] = &HostMetrics{
		RAM: RAMMetrics{UsedBytes: 1000, TotalBytes: 10000}, // 10%
	}
	m.metrics["high"] = &HostMetrics{
		RAM: RAMMetrics{UsedBytes: 9000, TotalBytes: 10000}, // 90%
	}

	m.sortOrder = SortByRAM
	m.sortHosts()

	// Should be sorted descending by RAM usage percentage
	assert.Equal(t, "high", m.hosts[0])
	assert.Equal(t, "low", m.hosts[1])
}

func TestModel_sortHosts_ByGPU(t *testing.T) {
	hosts := map[string]config.Host{
		"no_gpu":   {SSH: []string{"no_gpu"}},
		"gpu_low":  {SSH: []string{"gpu_low"}},
		"gpu_high": {SSH: []string{"gpu_high"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// Add metrics
	m.metrics["no_gpu"] = &HostMetrics{GPU: nil}
	m.metrics["gpu_low"] = &HostMetrics{GPU: &GPUMetrics{Percent: 20.0}}
	m.metrics["gpu_high"] = &HostMetrics{GPU: &GPUMetrics{Percent: 80.0}}

	m.sortOrder = SortByGPU
	m.sortHosts()

	// Should be sorted: high GPU, low GPU, no GPU
	assert.Equal(t, "gpu_high", m.hosts[0])
	assert.Equal(t, "gpu_low", m.hosts[1])
	assert.Equal(t, "no_gpu", m.hosts[2])
}

func TestModel_sortHosts_PreservesSelection(t *testing.T) {
	hosts := map[string]config.Host{
		"alpha": {SSH: []string{"alpha"}},
		"beta":  {SSH: []string{"beta"}},
		"gamma": {SSH: []string{"gamma"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// Select "beta"
	m.selected = 1 // beta

	// Add metrics to change sort order
	m.metrics["alpha"] = &HostMetrics{CPU: CPUMetrics{Percent: 10.0}}
	m.metrics["beta"] = &HostMetrics{CPU: CPUMetrics{Percent: 90.0}}
	m.metrics["gamma"] = &HostMetrics{CPU: CPUMetrics{Percent: 50.0}}

	m.sortOrder = SortByCPU
	m.sortHosts()

	// "beta" should still be selected even though its index changed
	assert.Equal(t, "beta", m.SelectedHost())
}

func TestModel_sortHosts_EmptyHosts(t *testing.T) {
	m := Model{
		hosts: []string{},
	}

	// Should not panic on empty hosts
	m.sortOrder = SortByName
	m.sortHosts()

	assert.Empty(t, m.hosts)
}

func TestModel_sortHosts_NilMetrics(t *testing.T) {
	hosts := map[string]config.Host{
		"with_metrics":    {SSH: []string{"with_metrics"}},
		"without_metrics": {SSH: []string{"without_metrics"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// Only one host has metrics
	m.metrics["with_metrics"] = &HostMetrics{CPU: CPUMetrics{Percent: 50.0}}
	// "without_metrics" has no entry in metrics map

	m.sortOrder = SortByCPU
	m.sortHosts()

	// Host with metrics should come first
	assert.Equal(t, "with_metrics", m.hosts[0])
	assert.Equal(t, "without_metrics", m.hosts[1])
}

func TestModel_View_Quitting(t *testing.T) {
	m := Model{quitting: true}

	view := m.View()
	assert.Equal(t, "", view)
}

func TestModel_Init(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	cmd := m.Init()

	// Should return a batch command
	require.NotNil(t, cmd)
}

func TestNewModelWithThresholds(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"user@server1"}},
	}

	t.Run("custom thresholds shift class boundaries per metric", func(t *testing.T) {
		thresholds := config.ThresholdConfig{
			CPU: config.ThresholdValues{Warning: 50, Critical: 80},
			RAM: config.ThresholdValues{Warning: 60, Critical: 85},
			GPU: config.ThresholdValues{Warning: 40, Critical: 75},
		}
		m := NewModelWithThresholds(NewCollector(hosts), time.Second, 0, nil, thresholds)

		cpuColor := thresholdColorFunc(m.thresholds.CPU)
		assert.Equal(t, ColorHealthy, cpuColor(49.9))
		assert.Equal(t, ColorWarning, cpuColor(50))
		assert.Equal(t, ColorWarning, cpuColor(79.9))
		assert.Equal(t, ColorCritical, cpuColor(80))

		ramColor := thresholdColorFunc(m.thresholds.RAM)
		assert.Equal(t, ColorHealthy, ramColor(59.9))
		assert.Equal(t, ColorWarning, ramColor(60))
		assert.Equal(t, ColorCritical, ramColor(85))

		gpuColor := thresholdColorFunc(m.thresholds.GPU)
		assert.Equal(t, ColorHealthy, gpuColor(39.9))
		assert.Equal(t, ColorWarning, gpuColor(40))
		assert.Equal(t, ColorCritical, gpuColor(75))

		// The same value classifies differently across metrics with different thresholds
		assert.Equal(t, ColorHealthy, cpuColor(45))
		assert.Equal(t, ColorWarning, gpuColor(45))
	})

	t.Run("zero thresholds fall back to 70/90 defaults", func(t *testing.T) {
		m := NewModelWithThresholds(NewCollector(hosts), time.Second, 0, nil, config.ThresholdConfig{})

		for _, values := range []config.ThresholdValues{m.thresholds.CPU, m.thresholds.RAM, m.thresholds.GPU} {
			color := thresholdColorFunc(values)
			assert.Equal(t, ColorHealthy, color(69.9))
			assert.Equal(t, ColorWarning, color(70))
			assert.Equal(t, ColorWarning, color(89.9))
			assert.Equal(t, ColorCritical, color(90))
		}
	})

	t.Run("NewModel uses default thresholds", func(t *testing.T) {
		m := NewModel(NewCollector(hosts), time.Second, 0, nil)

		color := thresholdColorFunc(m.thresholds.CPU)
		assert.Equal(t, ColorHealthy, color(69.9))
		assert.Equal(t, ColorWarning, color(70))
		assert.Equal(t, ColorCritical, color(90))
	})
}

func TestNormalizeThresholdValues(t *testing.T) {
	tests := []struct {
		name     string
		input    config.ThresholdValues
		expected config.ThresholdValues
	}{
		{
			name:     "zero values get defaults",
			input:    config.ThresholdValues{},
			expected: config.ThresholdValues{Warning: 70, Critical: 90},
		},
		{
			name:     "partial config keeps set value",
			input:    config.ThresholdValues{Warning: 55},
			expected: config.ThresholdValues{Warning: 55, Critical: 90},
		},
		{
			name:     "negative values get defaults",
			input:    config.ThresholdValues{Warning: -1, Critical: -5},
			expected: config.ThresholdValues{Warning: 70, Critical: 90},
		},
		{
			name:     "fully set config is unchanged",
			input:    config.ThresholdValues{Warning: 40, Critical: 60},
			expected: config.ThresholdValues{Warning: 40, Critical: 60},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeThresholdValues(tt.input))
		})
	}
}
