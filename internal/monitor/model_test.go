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

	m := NewModel(collector, 5*time.Second, nil)

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
		{StatusSlowState, "slow"},
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
	m := NewModel(collector, time.Second, nil)

	// Initially all unreachable
	assert.Equal(t, 0, m.OnlineCount())

	// Mark one as connected
	m.status["server1"] = StatusIdleState
	assert.Equal(t, 1, m.OnlineCount())

	// Mark another as connected
	m.status["server2"] = StatusIdleState
	assert.Equal(t, 2, m.OnlineCount())

	// Mark one as slow (not counted as online)
	m.status["server3"] = StatusSlowState
	assert.Equal(t, 2, m.OnlineCount())
}

func TestModel_SelectedHost(t *testing.T) {
	hosts := map[string]config.Host{
		"alpha": {SSH: []string{"alpha"}},
		"beta":  {SSH: []string{"beta"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, nil)

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

func TestModel_updateMetrics(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
		"server2": {SSH: []string{"server2"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, nil)

	// Create some metrics
	metrics := map[string]*HostMetrics{
		"server1": {
			Timestamp: time.Now(),
			CPU:       CPUMetrics{Percent: 50.0},
		},
		"server2": nil, // Server2 unreachable
	}
	errors := map[string]string{
		"server2": "connection refused",
	}

	m.updateMetrics(metrics, errors, nil)

	// Server1 should be connected
	assert.Equal(t, StatusIdleState, m.status["server1"])
	assert.NotNil(t, m.metrics["server1"])
	_, hasError := m.errors["server1"]
	assert.False(t, hasError)

	// Server2 should be unreachable with error
	assert.Equal(t, StatusUnreachableState, m.status["server2"])
	assert.Equal(t, "connection refused", m.errors["server2"])
}

func TestModel_sortHosts_ByName(t *testing.T) {
	hosts := map[string]config.Host{
		"zebra":  {SSH: []string{"zebra"}},
		"alpha":  {SSH: []string{"alpha"}},
		"middle": {SSH: []string{"middle"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, nil)

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
	m := NewModel(collector, time.Second, nil)

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
	m := NewModel(collector, time.Second, nil)

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
	m := NewModel(collector, time.Second, nil)

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
	m := NewModel(collector, time.Second, nil)

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
	m := NewModel(collector, time.Second, nil)

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
	m := NewModel(collector, time.Second, nil)

	cmd := m.Init()

	// Should return a batch command
	require.NotNil(t, cmd)
}
