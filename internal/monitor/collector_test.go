package monitor

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"user@server1"}},
		"server2": {SSH: []string{"user@server2"}},
	}

	collector := NewCollector(hosts)
	require.NotNil(t, collector)
	assert.NotNil(t, collector.pool)
	assert.Equal(t, 2, len(collector.hosts))
}

func TestCollectorHosts(t *testing.T) {
	hosts := map[string]config.Host{
		"alpha": {SSH: []string{"alpha"}},
		"beta":  {SSH: []string{"beta"}},
		"gamma": {SSH: []string{"gamma"}},
	}

	collector := NewCollector(hosts)
	aliases := collector.Hosts()

	assert.Len(t, aliases, 3)
	assert.Contains(t, aliases, "alpha")
	assert.Contains(t, aliases, "beta")
	assert.Contains(t, aliases, "gamma")
}

func TestCollectorClose(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"user@server1"}},
	}

	collector := NewCollector(hosts)
	require.NotNil(t, collector)

	// Close should not panic
	collector.Close()
}

func TestParseLinuxOutput(t *testing.T) {
	collector := NewCollector(map[string]config.Host{})
	metrics := &HostMetrics{}

	// Sample Linux output sections
	procStat := `cpu  1234567 12345 234567 8901234 12345 0 6789 0 0 0
cpu0 617283 6172 117283 4450617 6172 0 3394 0 0 0
cpu1 617284 6173 117284 4450617 6173 0 3395 0 0 0`

	procLoadavg := "1.23 2.34 3.45 1/234 5678"

	procMeminfo := `MemTotal:       16384000 kB
MemFree:         1234567 kB
MemAvailable:    8765432 kB
Buffers:          123456 kB
Cached:          4567890 kB`

	procNetDev := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234567   12345    0    0    0     0          0         0  1234567   12345    0    0    0     0       0          0
  eth0: 9876543   98765    0    0    0     0          0         0  5678901   56789    0    0    0     0       0          0`

	nvidiaSmi := "NVIDIA GeForce RTX 3080, 45, 2048, 10240, 65, 220"

	sections := []string{procStat, procLoadavg, procMeminfo, procNetDev, nvidiaSmi}

	result, err := collector.parseLinuxOutput("test-host", metrics, sections)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify CPU metrics were parsed
	assert.Equal(t, 2, result.CPU.Cores)
	assert.InDelta(t, 1.23, result.CPU.LoadAvg[0], 0.01)

	// Verify RAM metrics were parsed
	assert.Equal(t, int64(16384000*1024), result.RAM.TotalBytes)
	assert.Greater(t, result.RAM.UsedBytes, int64(0))

	// Verify network metrics were parsed
	assert.Len(t, result.Network, 2)

	// Verify GPU metrics were parsed
	require.NotNil(t, result.GPU)
	assert.Equal(t, "NVIDIA GeForce RTX 3080", result.GPU.Name)
	assert.Equal(t, 45.0, result.GPU.Percent)
}

func TestParseLinuxOutput_PartialSections(t *testing.T) {
	collector := NewCollector(map[string]config.Host{})
	metrics := &HostMetrics{}

	// Only provide CPU section
	procStat := `cpu  1000000 10000 200000 8000000 10000 0 5000 0 0 0
cpu0 500000 5000 100000 4000000 5000 0 2500 0 0 0`
	procLoadavg := "0.5 1.0 1.5"

	sections := []string{procStat, procLoadavg}

	result, err := collector.parseLinuxOutput("test-host", metrics, sections)
	require.NoError(t, err)
	require.NotNil(t, result)

	// CPU should be parsed
	assert.Equal(t, 1, result.CPU.Cores)

	// Other fields should have zero values
	assert.Equal(t, int64(0), result.RAM.TotalBytes)
	assert.Nil(t, result.Network)
	assert.Nil(t, result.GPU)
}

func TestParseLinuxOutput_EmptySections(t *testing.T) {
	collector := NewCollector(map[string]config.Host{})
	metrics := &HostMetrics{}

	sections := []string{}

	result, err := collector.parseLinuxOutput("test-host", metrics, sections)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestParseDarwinOutput(t *testing.T) {
	collector := &Collector{}
	metrics := &HostMetrics{}

	topOutput := `Processes: 385 total, 2 running, 383 sleeping, 1890 threads
Load Avg: 2.45, 3.12, 3.56
CPU usage: 5.26% user, 10.52% sys, 84.21% idle
SharedLibs: 400M resident, 100M data, 50M linkedit.`

	vmStatOutput := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                              123456.
Pages active:                            234567.
Pages inactive:                          345678.
Pages speculative:                        12345.
Pages wired down:                        567890.
Pages occupied by compressor:             89012.
File-backed pages:                       456789.
Pages purgeable:                          23456.`

	netstatOutput := `Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0   16384 <Link#1>                         12345     0    1234567    12345     0    1234567     0
en0   1500  <Link#4>      xx:xx:xx:xx:xx:xx  98765     0    9876543    56789     0    5678901     0`

	sections := []string{topOutput, vmStatOutput, netstatOutput}

	result, err := collector.parseDarwinOutput(metrics, sections)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify CPU metrics
	assert.InDelta(t, 15.78, result.CPU.Percent, 0.1) // 100 - 84.21 idle
	assert.InDelta(t, 2.45, result.CPU.LoadAvg[0], 0.01)

	// Verify RAM metrics
	assert.Greater(t, result.RAM.TotalBytes, int64(0))

	// Verify network metrics
	assert.NotEmpty(t, result.Network)

	// macOS doesn't have GPU in this implementation
	assert.Nil(t, result.GPU)
}

func TestParseDarwinOutput_PartialSections(t *testing.T) {
	collector := &Collector{}
	metrics := &HostMetrics{}

	topOutput := `Load Avg: 1.0, 2.0, 3.0
CPU usage: 10.0% user, 20.0% sys, 70.0% idle`

	sections := []string{topOutput}

	result, err := collector.parseDarwinOutput(metrics, sections)
	require.NoError(t, err)
	require.NotNil(t, result)

	// CPU should be parsed
	assert.InDelta(t, 30.0, result.CPU.Percent, 0.1)

	// Other fields should have zero values
	assert.Equal(t, int64(0), result.RAM.TotalBytes)
	assert.Nil(t, result.Network)
}

// Note: Tests that require actual SSH connections are integration tests.
// The Collect and CollectOne methods need real hosts or proper mocking.

func TestParseLinuxCPU(t *testing.T) {
	tests := []struct {
		name        string
		procStat    string
		procLoadavg string
		wantCores   int
		wantLoadAvg [3]float64
		wantErr     bool
	}{
		{
			name: "valid input",
			procStat: `cpu  1000000 10000 200000 8000000 10000 0 5000 0 0 0
cpu0 500000 5000 100000 4000000 5000 0 2500 0 0 0
cpu1 500000 5000 100000 4000000 5000 0 2500 0 0 0`,
			procLoadavg: "1.50 2.25 3.00 1/234 5678",
			wantCores:   2,
			wantLoadAvg: [3]float64{1.50, 2.25, 3.00},
			wantErr:     false,
		},
		{
			name:        "empty loadavg",
			procStat:    "cpu  1000000 10000 200000 8000000 10000 0 5000 0 0 0",
			procLoadavg: "",
			wantCores:   0,
			wantLoadAvg: [3]float64{0, 0, 0},
			wantErr:     false,
		},
		{
			name:        "invalid cpu line",
			procStat:    "cpu  invalid",
			procLoadavg: "1.0 2.0 3.0",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLinuxCPU(tt.procStat, tt.procLoadavg)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.wantCores, result.Cores)
			assert.InDelta(t, tt.wantLoadAvg[0], result.LoadAvg[0], 0.01)
			assert.InDelta(t, tt.wantLoadAvg[1], result.LoadAvg[1], 0.01)
			assert.InDelta(t, tt.wantLoadAvg[2], result.LoadAvg[2], 0.01)
		})
	}
}

func TestParseLinuxMemory(t *testing.T) {
	tests := []struct {
		name        string
		procMeminfo string
		wantTotal   int64
		wantErr     bool
	}{
		{
			name: "valid input",
			procMeminfo: `MemTotal:       16384000 kB
MemFree:         1234567 kB
MemAvailable:    8765432 kB
Buffers:          123456 kB
Cached:          4567890 kB`,
			wantTotal: 16384000 * 1024,
			wantErr:   false,
		},
		{
			name:        "insufficient fields",
			procMeminfo: "MemTotal: 1000 kB",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLinuxMemory(tt.procMeminfo)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.wantTotal, result.TotalBytes)
		})
	}
}

func TestParseLinuxNetwork(t *testing.T) {
	tests := []struct {
		name       string
		procNetDev string
		wantCount  int
		wantErr    bool
	}{
		{
			name: "valid input",
			procNetDev: `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234567   12345    0    0    0     0          0         0  1234567   12345    0    0    0     0       0          0
  eth0: 9876543   98765    0    0    0     0          0         0  5678901   56789    0    0    0     0       0          0`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:       "empty input",
			procNetDev: "",
			wantCount:  0,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLinuxNetwork(tt.procNetDev)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, result, tt.wantCount)
		})
	}
}

func TestParseNvidiaSMI(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantNil bool
		wantErr bool
	}{
		{
			name:    "valid output",
			output:  "NVIDIA GeForce RTX 3080, 45, 2048, 10240, 65, 220",
			wantNil: false,
			wantErr: false,
		},
		{
			name:    "empty output",
			output:  "",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "no devices found",
			output:  "No devices found",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "command not found",
			output:  "nvidia-smi: command not found",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "insufficient fields",
			output:  "GPU, 50, 1000",
			wantNil: false,
			wantErr: true,
		},
		{
			name:    "N/A values",
			output:  "GPU, [N/A], [N/A], [N/A], [N/A], [N/A]",
			wantNil: false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNvidiaSMI(tt.output)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
			}
		})
	}
}

func TestParseDarwinCPU(t *testing.T) {
	tests := []struct {
		name        string
		topOutput   string
		wantPercent float64
		wantLoad    [3]float64
	}{
		{
			name: "valid output",
			topOutput: `Processes: 385 total, 2 running, 383 sleeping, 1890 threads
Load Avg: 2.45, 3.12, 3.56
CPU usage: 5.26% user, 10.52% sys, 84.21% idle`,
			wantPercent: 15.79, // 100 - 84.21
			wantLoad:    [3]float64{2.45, 3.12, 3.56},
		},
		{
			name:        "empty output",
			topOutput:   "",
			wantPercent: 0,
			wantLoad:    [3]float64{0, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDarwinCPU(tt.topOutput)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.InDelta(t, tt.wantPercent, result.Percent, 0.1)
			assert.InDelta(t, tt.wantLoad[0], result.LoadAvg[0], 0.01)
		})
	}
}

func TestParseDarwinCPUUsage(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		expect float64
	}{
		{
			name:   "standard format",
			line:   "CPU usage: 5.26% user, 10.52% sys, 84.21% idle",
			expect: 15.79,
		},
		{
			name:   "no idle field",
			line:   "CPU usage: 50% user, 50% sys",
			expect: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDarwinCPUUsage(tt.line)
			assert.InDelta(t, tt.expect, result, 0.1)
		})
	}
}

func TestParseDarwinLoadAvg(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		expect [3]float64
	}{
		{
			name:   "valid load avg",
			line:   "Load Avg: 1.50, 2.25, 3.00",
			expect: [3]float64{1.50, 2.25, 3.00},
		},
		{
			name:   "no colon",
			line:   "Load Avg 1.0 2.0 3.0",
			expect: [3]float64{0, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDarwinLoadAvg(tt.line)
			assert.InDelta(t, tt.expect[0], result[0], 0.01)
			assert.InDelta(t, tt.expect[1], result[1], 0.01)
			assert.InDelta(t, tt.expect[2], result[2], 0.01)
		})
	}
}

func TestParseDarwinMemory(t *testing.T) {
	vmStatOutput := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                              123456.
Pages active:                            234567.
Pages inactive:                          345678.
Pages speculative:                        12345.
Pages wired down:                        567890.
Pages occupied by compressor:             89012.
File-backed pages:                       456789.
Pages purgeable:                          23456.`

	result, err := parseDarwinMemory(vmStatOutput)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Greater(t, result.TotalBytes, int64(0))
	assert.Greater(t, result.UsedBytes, int64(0))
}

func TestParseDarwinNetwork(t *testing.T) {
	netstatOutput := `Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0   16384 <Link#1>                         12345     0    1234567    12345     0    1234567     0
en0   1500  <Link#4>      xx:xx:xx:xx:xx:xx  98765     0    9876543    56789     0    5678901     0`

	result, err := parseDarwinNetwork(netstatOutput)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestParseProcesses(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantCount int
	}{
		{
			name: "valid output",
			output: `USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND
root         1  0.5  0.1 123456 12345 ?        Ss   Jan01   1:23 /sbin/init
user      1234 25.5  2.3 234567 23456 pts/0    S+   10:00   0:45 /usr/bin/python script.py
user      5678 50.0  5.0 345678 34567 pts/1    R+   10:30   2:30 /very/long/command/path/that/should/be/truncated/here`,
			wantCount: 3,
		},
		{
			name:      "header only",
			output:    "USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND",
			wantCount: 0,
		},
		{
			name:      "empty output",
			output:    "",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseProcesses(tt.output)
			require.NoError(t, err)
			assert.Len(t, result, tt.wantCount)
		})
	}
}

func TestCollector_SetTimeout(t *testing.T) {
	hosts := map[string]config.Host{
		"test": {SSH: []string{"localhost"}},
	}
	c := NewCollector(hosts)
	assert.Equal(t, 30*time.Second, c.timeout)

	c.SetTimeout(5 * time.Second)
	assert.Equal(t, 5*time.Second, c.timeout)
}

func TestCollector_parseOutput(t *testing.T) {
	c := NewCollector(map[string]config.Host{})

	// Test Linux parsing
	linuxOutput := "cpu data\n---\nloadavg data\n---\nmeminfo data\n---\nnet data\n---\ngpu data\n---\nps data"
	result, err := c.parseOutput("test-host", PlatformLinux, linuxOutput)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Test Darwin parsing
	darwinOutput := "top data\n---\nvm_stat data\n---\nnetstat data\n---\nps data"
	result, err = c.parseOutput("test-host", PlatformDarwin, darwinOutput)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Test unknown platform (defaults to Linux)
	result, err = c.parseOutput("test-host", PlatformUnknown, linuxOutput)
	require.NoError(t, err)
	require.NotNil(t, result)
}
