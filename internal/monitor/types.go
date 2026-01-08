package monitor

import "time"

// HostMetrics contains all collected metrics from a remote host.
type HostMetrics struct {
	Timestamp time.Time
	CPU       CPUMetrics
	RAM       RAMMetrics
	GPU       *GPUMetrics // nil if no GPU
	Network   []NetworkInterface
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

// SystemInfo contains general system information.
type SystemInfo struct {
	Hostname string
	OS       string
	Kernel   string
	Uptime   time.Duration
}
