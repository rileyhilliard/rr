package monitor

import "time"

// HostMetrics contains all collected metrics from a remote host.
type HostMetrics struct {
	Timestamp time.Time
	CPU       CPUMetrics
	RAM       RAMMetrics
	GPU       *GPUMetrics // nil if no GPU
	Network   []NetworkInterface
	Processes []ProcessInfo
	System    SystemInfo
}

// CPUMetrics contains CPU usage information.
type CPUMetrics struct {
	Percent float64
	Cores   int
	LoadAvg [3]float64
}

// RAMMetrics contains memory usage information.
type RAMMetrics struct {
	UsedBytes  int64
	TotalBytes int64
	Cached     int64
	Available  int64
}

// GPUMetrics contains GPU usage information (typically from nvidia-smi).
type GPUMetrics struct {
	Name        string
	Percent     float64
	MemoryUsed  int64
	MemoryTotal int64
	Temperature int
	PowerWatts  int
}

// NetworkInterface contains network I/O statistics for a single interface.
type NetworkInterface struct {
	Name       string
	BytesIn    int64
	BytesOut   int64
	PacketsIn  int64
	PacketsOut int64
}

// ProcessInfo contains information about a running process.
type ProcessInfo struct {
	PID     int
	User    string
	CPU     float64 // percentage
	Memory  float64 // percentage
	Time    string  // elapsed time (format varies by platform)
	Command string  // truncated command line
}

// SystemInfo contains general system information.
type SystemInfo struct {
	Hostname string
	OS       string
	Kernel   string
	Uptime   time.Duration
}

// HostLockInfo contains lock status for a host.
type HostLockInfo struct {
	IsLocked bool      // True if a lock is held on this host
	Holder   string    // Description of who holds the lock (user@host)
	Started  time.Time // When the lock was acquired
}

// HostResult is the result of collecting metrics from a single host.
// Used for streaming results from CollectStreaming.
type HostResult struct {
	Alias        string        // Host alias
	Metrics      *HostMetrics  // Collected metrics (nil on error)
	Error        error         // Error if collection failed
	LockInfo     *HostLockInfo // Lock status (nil if not checked or error)
	ConnectedVia string        // SSH alias used to connect (e.g., "m4-tailscale")
	Latency      time.Duration // Round-trip time for metrics collection
}

// Duration returns how long the lock has been held.
func (l HostLockInfo) Duration() time.Duration {
	if l.Started.IsZero() {
		return 0
	}
	return time.Since(l.Started)
}

// FormatDuration returns a human-readable duration string (e.g., "2m30s").
func (l HostLockInfo) FormatDuration() string {
	d := l.Duration()
	if d < time.Second {
		return "0s"
	}
	// Format as Xm or XmYs
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	if minutes > 0 {
		if seconds > 0 {
			return formatInt(minutes) + "m" + formatInt(seconds) + "s"
		}
		return formatInt(minutes) + "m"
	}
	return formatInt(seconds) + "s"
}

// formatInt converts an integer to a string without importing strconv.
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
