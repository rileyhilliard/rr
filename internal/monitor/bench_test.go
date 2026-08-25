package monitor

import (
	"fmt"
	"math"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rileyhilliard/rr/internal/config"
)

// benchHistoryData generates n percentage samples with a wavy shape so
// rendered graphs exercise multiple colors and dot heights.
func benchHistoryData(n int) []float64 {
	data := make([]float64, n)
	for i := range data {
		data[i] = 50 + 45*math.Sin(float64(i)/9.0)
	}
	return data
}

// benchMetrics builds a full metrics sample for benchmarks.
func benchMetrics(percent float64, tick int) *HostMetrics {
	return &HostMetrics{
		Timestamp: time.Now(),
		CPU:       CPUMetrics{Percent: percent, Cores: 8, LoadAvg: [3]float64{1.5, 1.2, 1.0}},
		RAM:       RAMMetrics{UsedBytes: int64(percent) * 1024 * 1024 * 100, TotalBytes: 16 * 1024 * 1024 * 1024},
		GPU:       &GPUMetrics{Name: "bench-gpu", Percent: percent, Temperature: 55},
		Network: []NetworkInterface{
			{Name: "eth0", BytesIn: int64(tick) * 250_000, BytesOut: int64(tick) * 120_000},
		},
		Processes: []ProcessInfo{
			{PID: 1234, User: "bench", CPU: percent, Memory: 12.5, Command: "/usr/bin/benchproc --work"},
		},
	}
}

// newBenchModel builds a ready-to-render model with numHosts hosts, each with
// points samples of CPU/RAM/GPU/latency/network history.
func newBenchModel(numHosts, points int) Model {
	hostCfgs := make(map[string]config.Host, numHosts)
	order := make([]string, 0, numHosts)
	for i := 0; i < numHosts; i++ {
		alias := fmt.Sprintf("bench-host-%d", i)
		hostCfgs[alias] = config.Host{SSH: []string{alias}}
		order = append(order, alias)
	}

	collector := NewCollector(hostCfgs)
	m := NewModel(collector, time.Second, time.Second, order)

	// Initialize viewports at a wide layout (2 cards per row)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 166, Height: 50})
	m = nm.(Model)

	data := benchHistoryData(points)
	for _, alias := range order {
		var metrics *HostMetrics
		for i, v := range data {
			metrics = benchMetrics(v, i)
			m.history.Push(alias, metrics)
			m.history.PushLatency(alias, 20+v)
		}
		m.metrics[alias] = metrics
		m.status[alias] = StatusIdleState
		m.latency[alias] = 45 * time.Millisecond
		if state, ok := m.connState[alias]; ok {
			state.Connected = true
		}
	}
	m.sortHosts()
	m.updateListViewportContent()
	return m
}

// BenchmarkRenderBrailleSparkline measures the detail-view sized graph render:
// 100 chars wide, 8 rows tall, fed from a 600-point history.
func BenchmarkRenderBrailleSparkline(b *testing.B) {
	data := benchHistoryData(600)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RenderBrailleSparklineWithOptions(data, 100, 8, ColorGraph, nil, false)
	}
}

// BenchmarkRenderBrailleSparklineColorFunc measures the same graph with a
// custom per-column color function (the threshold-colored card path).
func BenchmarkRenderBrailleSparklineColorFunc(b *testing.B) {
	data := benchHistoryData(600)
	colorFunc := thresholdColorFunc(config.ThresholdValues{Warning: 70, Critical: 90})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RenderBrailleSparklineWithOptions(data, 100, 8, ColorGraph, colorFunc, false)
	}
}

// BenchmarkRenderGradientBar measures the gradient bar fallback renderer.
func BenchmarkRenderGradientBar(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RenderGradientBar(100, 65, ColorGraph)
	}
}

// BenchmarkDashboardView measures the realistic per-result render cost: a
// 4-host model with 600-point histories receiving one host result, then
// rendering the full dashboard view.
func BenchmarkDashboardView(b *testing.B) {
	m := newBenchModel(4, 600)
	aliases := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		aliases = append(aliases, fmt.Sprintf("bench-host-%d", i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		alias := aliases[i%len(aliases)]
		msg := hostResultMsg{
			alias:   alias,
			metrics: benchMetrics(float64((i*7)%100), i),
			latency: 45 * time.Millisecond,
			time:    time.Now(),
		}
		nm, _ := m.Update(msg)
		m = nm.(Model)
		_ = m.View()
	}
}
