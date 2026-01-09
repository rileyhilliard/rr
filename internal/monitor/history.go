package monitor

import "sync"

// DefaultHistorySize is the default number of data points to retain per metric.
const DefaultHistorySize = 60

// History manages metric history for multiple hosts using ring buffers.
// It provides thread-safe access to historical data for sparkline rendering.
type History struct {
	mu    sync.RWMutex
	size  int
	hosts map[string]*hostHistory
}

// hostHistory holds the ring buffers for a single host.
type hostHistory struct {
	cpu     *ringBuffer
	ram     *ringBuffer
	gpu     *ringBuffer // nil if host has no GPU
	network map[string]*networkHistory
}

// networkHistory holds per-interface network metrics history.
type networkHistory struct {
	bytesIn  *ringBuffer
	bytesOut *ringBuffer
}

// ringBuffer is a fixed-size circular buffer for float64 values.
type ringBuffer struct {
	data  []float64
	head  int
	count int
	size  int
}

// NewHistory creates a new history tracker with the specified buffer size.
func NewHistory(size int) *History {
	if size <= 0 {
		size = DefaultHistorySize
	}
	return &History{
		size:  size,
		hosts: make(map[string]*hostHistory),
	}
}

// Push adds a new metrics sample for the specified host.
func (h *History) Push(alias string, metrics *HostMetrics) {
	if metrics == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	hist := h.getOrCreateHost(alias)

	// Push CPU percentage
	hist.cpu.push(metrics.CPU.Percent)

	// Push RAM usage percentage
	if metrics.RAM.TotalBytes > 0 {
		ramPercent := float64(metrics.RAM.UsedBytes) / float64(metrics.RAM.TotalBytes) * 100
		hist.ram.push(ramPercent)
	}

	// Push GPU percentage if available
	if metrics.GPU != nil {
		if hist.gpu == nil {
			hist.gpu = newRingBuffer(h.size)
		}
		hist.gpu.push(metrics.GPU.Percent)
	}

	// Push network metrics per interface
	for _, iface := range metrics.Network {
		netHist, ok := hist.network[iface.Name]
		if !ok {
			netHist = &networkHistory{
				bytesIn:  newRingBuffer(h.size),
				bytesOut: newRingBuffer(h.size),
			}
			hist.network[iface.Name] = netHist
		}
		netHist.bytesIn.push(float64(iface.BytesIn))
		netHist.bytesOut.push(float64(iface.BytesOut))
	}
}

// GetCPUHistory returns the last count CPU percentage values for the specified host.
// Returns fewer values if not enough history is available.
func (h *History) GetCPUHistory(alias string, count int) []float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hist, ok := h.hosts[alias]
	if !ok || hist.cpu == nil {
		return nil
	}

	return hist.cpu.getLast(count)
}

// GetRAMHistory returns the last count RAM percentage values for the specified host.
// Returns fewer values if not enough history is available.
func (h *History) GetRAMHistory(alias string, count int) []float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hist, ok := h.hosts[alias]
	if !ok || hist.ram == nil {
		return nil
	}

	return hist.ram.getLast(count)
}

// GetGPUHistory returns the last count GPU percentage values for the specified host.
// Returns nil if the host has no GPU history.
func (h *History) GetGPUHistory(alias string, count int) []float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hist, ok := h.hosts[alias]
	if !ok || hist.gpu == nil {
		return nil
	}

	return hist.gpu.getLast(count)
}

// GetNetworkHistory returns the last count network bytes values for the specified interface.
// Returns bytesIn and bytesOut slices.
func (h *History) GetNetworkHistory(alias, iface string, count int) (bytesIn, bytesOut []float64) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hist, ok := h.hosts[alias]
	if !ok {
		return nil, nil
	}

	netHist, ok := hist.network[iface]
	if !ok {
		return nil, nil
	}

	return netHist.bytesIn.getLast(count), netHist.bytesOut.getLast(count)
}

// NetworkRate represents calculated network throughput for an interface.
type NetworkRate struct {
	Interface      string
	BytesInPerSec  float64
	BytesOutPerSec float64
}

// GetNetworkRates calculates network throughput rates for all interfaces of a host.
// The intervalSec parameter is the time between samples (typically the refresh interval).
// Returns rates in bytes per second.
func (h *History) GetNetworkRates(alias string, intervalSec float64) []NetworkRate {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hist, ok := h.hosts[alias]
	if !ok || intervalSec <= 0 {
		return nil
	}

	var rates []NetworkRate
	for ifaceName, netHist := range hist.network {
		// Get last 2 samples to calculate delta
		inHist := netHist.bytesIn.getLast(2)
		outHist := netHist.bytesOut.getLast(2)

		// Need at least 2 samples to calculate rate
		if len(inHist) < 2 || len(outHist) < 2 {
			continue
		}

		// Calculate bytes per second from delta
		// inHist[0] is older, inHist[1] is newer
		inDelta := inHist[1] - inHist[0]
		outDelta := outHist[1] - outHist[0]

		// Handle counter wraparound or reset (negative delta)
		if inDelta < 0 {
			inDelta = 0
		}
		if outDelta < 0 {
			outDelta = 0
		}

		rates = append(rates, NetworkRate{
			Interface:      ifaceName,
			BytesInPerSec:  inDelta / intervalSec,
			BytesOutPerSec: outDelta / intervalSec,
		})
	}

	return rates
}

// GetTotalNetworkRate returns the combined throughput across all non-loopback interfaces.
func (h *History) GetTotalNetworkRate(alias string, intervalSec float64) (bytesInPerSec, bytesOutPerSec float64) {
	rates := h.GetNetworkRates(alias, intervalSec)
	for _, r := range rates {
		// Skip loopback
		if r.Interface == "lo" || r.Interface == "lo0" {
			continue
		}
		bytesInPerSec += r.BytesInPerSec
		bytesOutPerSec += r.BytesOutPerSec
	}
	return
}

// Clear removes all history for the specified host.
func (h *History) Clear(alias string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.hosts, alias)
}

// ClearAll removes all history.
func (h *History) ClearAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hosts = make(map[string]*hostHistory)
}

// Count returns the number of data points stored for a host's CPU metric.
func (h *History) Count(alias string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hist, ok := h.hosts[alias]
	if !ok || hist.cpu == nil {
		return 0
	}

	return hist.cpu.count
}

// getOrCreateHost returns the history for a host, creating it if needed.
// Must be called with h.mu held.
func (h *History) getOrCreateHost(alias string) *hostHistory {
	hist, ok := h.hosts[alias]
	if !ok {
		hist = &hostHistory{
			cpu:     newRingBuffer(h.size),
			ram:     newRingBuffer(h.size),
			network: make(map[string]*networkHistory),
		}
		h.hosts[alias] = hist
	}
	return hist
}

// newRingBuffer creates a new ring buffer with the specified capacity.
func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		data: make([]float64, size),
		size: size,
	}
}

// push adds a value to the ring buffer.
func (r *ringBuffer) push(value float64) {
	r.data[r.head] = value
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// getLast returns the last count values in chronological order (oldest first).
func (r *ringBuffer) getLast(count int) []float64 {
	if count <= 0 || r.count == 0 {
		return nil
	}

	if count > r.count {
		count = r.count
	}

	result := make([]float64, count)

	// Calculate starting position
	// head points to the next write position, so the most recent value is at head-1
	// We want 'count' values ending at head-1
	start := (r.head - count + r.size) % r.size

	for i := 0; i < count; i++ {
		idx := (start + i) % r.size
		result[i] = r.data[idx]
	}

	return result
}

// getAll returns all stored values in chronological order.
func (r *ringBuffer) getAll() []float64 {
	return r.getLast(r.count)
}
