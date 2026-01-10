package monitor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHostMetrics_Struct(t *testing.T) {
	now := time.Now()
	metrics := HostMetrics{
		Timestamp: now,
		CPU: CPUMetrics{
			Percent: 50.5,
			Cores:   8,
			LoadAvg: [3]float64{1.0, 2.0, 3.0},
		},
		RAM: RAMMetrics{
			UsedBytes:  1024 * 1024 * 1024 * 8,
			TotalBytes: 1024 * 1024 * 1024 * 16,
			Cached:     1024 * 1024 * 512,
			Available:  1024 * 1024 * 1024 * 8,
		},
		GPU: &GPUMetrics{
			Name:        "NVIDIA GeForce RTX 3080",
			Percent:     75.0,
			MemoryUsed:  1024 * 1024 * 1024 * 4,
			MemoryTotal: 1024 * 1024 * 1024 * 10,
			Temperature: 65,
			PowerWatts:  250,
		},
		Network: []NetworkInterface{
			{
				Name:       "eth0",
				BytesIn:    1000000,
				BytesOut:   500000,
				PacketsIn:  10000,
				PacketsOut: 5000,
			},
		},
		Processes: []ProcessInfo{
			{
				PID:     1234,
				User:    "root",
				CPU:     5.5,
				Memory:  2.3,
				Time:    "00:10:00",
				Command: "/usr/bin/process",
			},
		},
		System: SystemInfo{
			Hostname: "server1",
			OS:       "Linux",
			Kernel:   "5.15.0",
			Uptime:   time.Hour * 24 * 7,
		},
	}

	// Verify all fields are set correctly
	assert.Equal(t, now, metrics.Timestamp)
	assert.Equal(t, 50.5, metrics.CPU.Percent)
	assert.Equal(t, 8, metrics.CPU.Cores)
	assert.Equal(t, 1.0, metrics.CPU.LoadAvg[0])
	assert.Equal(t, 2.0, metrics.CPU.LoadAvg[1])
	assert.Equal(t, 3.0, metrics.CPU.LoadAvg[2])

	assert.Equal(t, int64(1024*1024*1024*8), metrics.RAM.UsedBytes)
	assert.Equal(t, int64(1024*1024*1024*16), metrics.RAM.TotalBytes)

	assert.NotNil(t, metrics.GPU)
	assert.Equal(t, "NVIDIA GeForce RTX 3080", metrics.GPU.Name)
	assert.Equal(t, 75.0, metrics.GPU.Percent)
	assert.Equal(t, 65, metrics.GPU.Temperature)

	assert.Len(t, metrics.Network, 1)
	assert.Equal(t, "eth0", metrics.Network[0].Name)

	assert.Len(t, metrics.Processes, 1)
	assert.Equal(t, 1234, metrics.Processes[0].PID)
	assert.Equal(t, "root", metrics.Processes[0].User)

	assert.Equal(t, "server1", metrics.System.Hostname)
	assert.Equal(t, "Linux", metrics.System.OS)
}

func TestCPUMetrics_Struct(t *testing.T) {
	cpu := CPUMetrics{
		Percent: 95.5,
		Cores:   16,
		LoadAvg: [3]float64{4.5, 3.2, 2.1},
	}

	assert.Equal(t, 95.5, cpu.Percent)
	assert.Equal(t, 16, cpu.Cores)
	assert.Equal(t, [3]float64{4.5, 3.2, 2.1}, cpu.LoadAvg)
}

func TestRAMMetrics_Struct(t *testing.T) {
	ram := RAMMetrics{
		UsedBytes:  1024 * 1024 * 1024 * 4,
		TotalBytes: 1024 * 1024 * 1024 * 8,
		Cached:     1024 * 1024 * 512,
		Available:  1024 * 1024 * 1024 * 4,
	}

	assert.Equal(t, int64(1024*1024*1024*4), ram.UsedBytes)
	assert.Equal(t, int64(1024*1024*1024*8), ram.TotalBytes)
	assert.Equal(t, int64(1024*1024*512), ram.Cached)
	assert.Equal(t, int64(1024*1024*1024*4), ram.Available)
}

func TestGPUMetrics_Struct(t *testing.T) {
	gpu := GPUMetrics{
		Name:        "Tesla V100",
		Percent:     100.0,
		MemoryUsed:  1024 * 1024 * 1024 * 16,
		MemoryTotal: 1024 * 1024 * 1024 * 32,
		Temperature: 80,
		PowerWatts:  300,
	}

	assert.Equal(t, "Tesla V100", gpu.Name)
	assert.Equal(t, 100.0, gpu.Percent)
	assert.Equal(t, int64(1024*1024*1024*16), gpu.MemoryUsed)
	assert.Equal(t, int64(1024*1024*1024*32), gpu.MemoryTotal)
	assert.Equal(t, 80, gpu.Temperature)
	assert.Equal(t, 300, gpu.PowerWatts)
}

func TestNetworkInterface_Struct(t *testing.T) {
	iface := NetworkInterface{
		Name:       "enp0s3",
		BytesIn:    1234567890,
		BytesOut:   987654321,
		PacketsIn:  1000000,
		PacketsOut: 900000,
	}

	assert.Equal(t, "enp0s3", iface.Name)
	assert.Equal(t, int64(1234567890), iface.BytesIn)
	assert.Equal(t, int64(987654321), iface.BytesOut)
	assert.Equal(t, int64(1000000), iface.PacketsIn)
	assert.Equal(t, int64(900000), iface.PacketsOut)
}

func TestProcessInfo_Struct(t *testing.T) {
	proc := ProcessInfo{
		PID:     9999,
		User:    "admin",
		CPU:     99.9,
		Memory:  50.5,
		Time:    "1-12:34:56",
		Command: "/usr/bin/heavy-process --option value",
	}

	assert.Equal(t, 9999, proc.PID)
	assert.Equal(t, "admin", proc.User)
	assert.Equal(t, 99.9, proc.CPU)
	assert.Equal(t, 50.5, proc.Memory)
	assert.Equal(t, "1-12:34:56", proc.Time)
	assert.Equal(t, "/usr/bin/heavy-process --option value", proc.Command)
}

func TestSystemInfo_Struct(t *testing.T) {
	sys := SystemInfo{
		Hostname: "production-server-01",
		OS:       "Ubuntu 22.04",
		Kernel:   "5.15.0-91-generic",
		Uptime:   time.Hour*24*365 + time.Hour*6,
	}

	assert.Equal(t, "production-server-01", sys.Hostname)
	assert.Equal(t, "Ubuntu 22.04", sys.OS)
	assert.Equal(t, "5.15.0-91-generic", sys.Kernel)
	assert.Equal(t, time.Hour*24*365+time.Hour*6, sys.Uptime)
}

func TestHostMetrics_NilGPU(t *testing.T) {
	metrics := HostMetrics{
		Timestamp: time.Now(),
		CPU:       CPUMetrics{Percent: 25.0},
		RAM:       RAMMetrics{TotalBytes: 1024 * 1024},
		GPU:       nil, // No GPU
	}

	assert.Nil(t, metrics.GPU)
}

func TestHostMetrics_EmptyNetwork(t *testing.T) {
	metrics := HostMetrics{
		Timestamp: time.Now(),
		Network:   nil,
	}

	assert.Nil(t, metrics.Network)
	assert.Len(t, metrics.Network, 0)
}

func TestHostMetrics_EmptyProcesses(t *testing.T) {
	metrics := HostMetrics{
		Timestamp: time.Now(),
		Processes: nil,
	}

	assert.Nil(t, metrics.Processes)
	assert.Len(t, metrics.Processes, 0)
}
