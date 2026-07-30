package monitor

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderCardDivider(t *testing.T) {
	tests := []struct {
		name  string
		width int
	}{
		{"normal width", 40},
		{"narrow", 10},
		{"very narrow", 1},
		{"zero width", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderCardDivider(tt.width)
			// Just verify it doesn't panic
			_ = result
		})
	}
}

func TestTruncateErrorMsg(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		maxLen int
		expect string
	}{
		{
			name:   "short message",
			errMsg: "Error",
			maxLen: 20,
			expect: "Error",
		},
		{
			name:   "message with colon",
			errMsg: "connection: refused",
			maxLen: 20,
			expect: "refused",
		},
		{
			name:   "long message truncated",
			errMsg: "This is a very long error message that should be truncated",
			maxLen: 20,
			expect: "This is a very lo...",
		},
		{
			name:   "very short max",
			errMsg: "Error message",
			maxLen: 4,
			expect: "E...",
		},
		{
			name:   "max length too short for ellipsis",
			errMsg: "Error",
			maxLen: 3,
			expect: "Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateErrorMsg(tt.errMsg, tt.maxLen)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestParseErrorParts(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		wantCore string
		wantSugg string
	}{
		{
			name:     "structured error with suggestion",
			errMsg:   "✗ Can't reach 'host' at 192.168.1.1:22\n\n  connect: connection refused\n\n  Check if SSH is running",
			wantCore: "connect: connection refused",
			wantSugg: "Check if SSH is running",
		},
		{
			name:     "simple error message",
			errMsg:   "connection refused",
			wantCore: "connection refused",
			wantSugg: "",
		},
		{
			name:     "error with Try suggestion",
			errMsg:   "dial tcp: timeout\n\nTry: ssh host",
			wantCore: "dial tcp: timeout",
			wantSugg: "Try: ssh host",
		},
		{
			name:     "error with Check suggestion",
			errMsg:   "network unreachable\n\nCheck your connection",
			wantCore: "network unreachable",
			wantSugg: "Check your connection",
		},
		{
			name:     "empty message",
			errMsg:   "",
			wantCore: "",
			wantSugg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, sugg := parseErrorParts(tt.errMsg)
			assert.Equal(t, tt.wantCore, core)
			assert.Equal(t, tt.wantSugg, sugg)
		})
	}
}

func TestTruncateWithEllipsis(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "Hello", 10, "Hello"},
		{"exact length", "Hello", 5, "Hello"},
		{"needs truncation", "Hello World", 8, "Hello..."},
		{"max len too short", "Hello", 2, "Hello"},
		{"empty string", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateWithEllipsis(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestRenderCardLine(t *testing.T) {
	tests := []struct {
		name    string
		content string
		width   int
	}{
		{"normal content", "Hello", 20},
		{"empty content", "", 10},
		{"content wider than width", "Very long content here", 5},
		{"exact width", "Test", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderCardLine(tt.content, tt.width)
			assert.NotEmpty(t, result)
		})
	}
}

func TestRenderLoadAvg(t *testing.T) {
	loadAvg := [3]float64{1.23, 2.34, 3.45}
	result := RenderLoadAvg(loadAvg)

	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Load")
	assert.Contains(t, result, "1.23")
	assert.Contains(t, result, "2.34")
	assert.Contains(t, result, "3.45")
}

func TestModel_renderHostLine(t *testing.T) {
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
			result := m.renderHostLine(tt.host, tt.status)
			assert.NotEmpty(t, result)
			assert.Contains(t, result, tt.host)
		})
	}
}

func TestModel_renderCard(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.height = 40

	tests := []struct {
		name       string
		host       string
		width      int
		selected   bool
		hasMetrics bool
	}{
		{"unselected no metrics", "server1", 40, false, false},
		{"selected no metrics", "server1", 40, true, false},
		{"unselected with metrics", "server1", 40, false, true},
		{"selected with metrics", "server1", 40, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasMetrics {
				m.metrics[tt.host] = &HostMetrics{
					CPU: CPUMetrics{Percent: 50.0, LoadAvg: [3]float64{1.0, 2.0, 3.0}},
					RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000},
					Processes: []ProcessInfo{
						{PID: 1234, User: "root", CPU: 25.0, Memory: 10.0, Command: "/usr/bin/process"},
					},
				}
				m.status[tt.host] = StatusIdleState
			}

			result := m.renderCard(tt.host, tt.width, tt.selected)
			assert.NotEmpty(t, result)
		})
	}
}

func TestModel_renderCompactCard(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 100
	m.height = 40

	tests := []struct {
		name       string
		hasMetrics bool
		selected   bool
	}{
		{"no metrics", false, false},
		{"with metrics", true, false},
		{"selected with metrics", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasMetrics {
				m.metrics["server1"] = &HostMetrics{
					CPU: CPUMetrics{Percent: 50.0},
					RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000},
				}
				m.status["server1"] = StatusIdleState
			}

			result := m.renderCompactCard("server1", 80, tt.selected)
			assert.NotEmpty(t, result)
		})
	}
}

func TestModel_renderMinimalCard(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 60
	m.height = 40

	tests := []struct {
		name       string
		hasMetrics bool
	}{
		{"no metrics", false},
		{"with metrics", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasMetrics {
				m.metrics["server1"] = &HostMetrics{
					CPU: CPUMetrics{Percent: 50.0},
					RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000},
				}
				m.status["server1"] = StatusIdleState
			}

			result := m.renderMinimalCard("server1", 50, false)
			assert.NotEmpty(t, result)
		})
	}
}

func TestModel_renderMinimalHostLine(t *testing.T) {
	hosts := map[string]config.Host{
		"very-long-hostname-that-needs-truncation": {SSH: []string{"host"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	tests := []struct {
		name     string
		host     string
		maxWidth int
	}{
		{"short host", "srv1", 20},
		{"long host", "very-long-hostname-that-needs-truncation", 20},
		{"very narrow", "hostname", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.renderMinimalHostLine(tt.host, StatusIdleState, tt.maxWidth)
			assert.NotEmpty(t, result)
		})
	}
}

func TestModel_renderMinimalMetricsLine(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	metrics := &HostMetrics{
		CPU: CPUMetrics{Percent: 45.0},
		RAM: RAMMetrics{UsedBytes: 6700000000, TotalBytes: 10000000000},
	}

	tests := []struct {
		name  string
		width int
	}{
		{"wide", 40},
		{"narrow", 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.renderMinimalMetricsLine(metrics, tt.width)
			assert.NotEmpty(t, result)
		})
	}
}

func TestModel_renderCardTopProcesses(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	tests := []struct {
		name     string
		procs    []ProcessInfo
		maxWidth int
	}{
		{"empty procs", []ProcessInfo{}, 40},
		{"single proc", []ProcessInfo{
			{PID: 1, User: "root", CPU: 50.0, Command: "/usr/bin/test"},
		}, 40},
		{"proc with path", []ProcessInfo{
			{PID: 1, User: "root", CPU: 50.0, Command: "/very/long/path/to/command"},
		}, 40},
		{"proc with args", []ProcessInfo{
			{PID: 1, User: "root", CPU: 50.0, Command: "python script.py --flag"},
		}, 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.renderCardTopProcesses(tt.procs, tt.maxWidth)
			if len(tt.procs) == 0 {
				assert.Empty(t, result)
			} else {
				assert.Len(t, result, 1) // short terminal: one process
				assert.NotEmpty(t, result[0])
			}
		})
	}
}

// TestModel_renderCardTopProcesses_Extended verifies tall terminals list more
// of the already-collected process data.
func TestModel_renderCardTopProcesses_Extended(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	procs := []ProcessInfo{
		{PID: 1, User: "root", CPU: 90.0, Command: "one"},
		{PID: 2, User: "root", CPU: 50.0, Command: "two"},
		{PID: 3, User: "root", CPU: 10.0, Command: "three"},
		{PID: 4, User: "root", CPU: 5.0, Command: "four"},
	}

	m.height = HeightStandard - 1
	require.False(t, m.CanShowExtendedInfo())
	assert.Len(t, m.renderCardTopProcesses(procs, 60), cardTopProcs)

	m.height = HeightStandard
	require.True(t, m.CanShowExtendedInfo())
	assert.Len(t, m.renderCardTopProcesses(procs, 60), cardTopProcsExtended)
}

// TestModel_cardGraphRows verifies the card graph grows on tall terminals.
func TestModel_cardGraphRows(t *testing.T) {
	m := Model{height: HeightStandard - 1}
	assert.Equal(t, cardGraphHeight, m.cardGraphRows())

	m.height = HeightStandard
	assert.Equal(t, cardGraphHeightExtended, m.cardGraphRows())
}

func TestModel_renderCardNetworkLine(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.interval = time.Second

	// No network history yet
	result := m.renderCardNetworkLine("server1", 40)
	assert.Empty(t, result)

	// Push some network data
	for i := 1; i <= 3; i++ {
		m.history.Push("server1", &HostMetrics{
			RAM: RAMMetrics{TotalBytes: 1},
			Network: []NetworkInterface{
				{Name: "eth0", BytesIn: int64(i * 1000), BytesOut: int64(i * 500)},
			},
		})
	}

	result = m.renderCardNetworkLine("server1", 40)
	assert.NotEmpty(t, result)
}

func TestModel_renderCardDiskLine(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)

	// No disk data: hidden
	result := m.renderCardDiskLine(DiskMetrics{}, 40)
	assert.Empty(t, result)

	// With disk data: DISK label plus usage
	disk := DiskMetrics{
		UsedBytes:  200 * 1024 * 1024 * 1024,
		TotalBytes: 500 * 1024 * 1024 * 1024,
		Percent:    42.0,
	}
	result = m.renderCardDiskLine(disk, 40)
	assert.Contains(t, result, "DISK")
	assert.Contains(t, result, "42.0%")
}

func TestModel_renderCard_DiskLine(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.height = 40

	// Full card without disk data has no DISK line
	m.metrics["server1"] = &HostMetrics{
		CPU: CPUMetrics{Percent: 50.0},
		RAM: RAMMetrics{UsedBytes: 4000000000, TotalBytes: 8000000000},
	}
	m.status["server1"] = StatusIdleState
	result := m.renderCard("server1", 40, false)
	assert.NotContains(t, result, "DISK")

	// Full card with disk data shows the DISK line (fresh cache: new result
	// invalidates the body in the real flow)
	delete(m.cardBodyCache, "server1")
	m.metrics["server1"].Disk = DiskMetrics{
		UsedBytes:  100 * 1024 * 1024 * 1024,
		TotalBytes: 500 * 1024 * 1024 * 1024,
		Percent:    20.0,
	}
	result = m.renderCard("server1", 40, false)
	assert.Contains(t, result, "DISK")
	assert.Contains(t, result, "20.0%")
}

func TestCardConstants(t *testing.T) {
	assert.Equal(t, 2, cardGraphHeight)
	assert.Equal(t, 10, cardMinBarWidth)
}
