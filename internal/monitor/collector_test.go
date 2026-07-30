package monitor

import (
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
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

	result := collector.parseLinuxOutput("test-host", metrics, sections)
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

	result := collector.parseLinuxOutput("test-host", metrics, sections)
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

	result := collector.parseLinuxOutput("test-host", metrics, sections)
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

	gpuOutput := `  |   "PerformanceStatistics" = {"In use system memory (driver)"=0,"Alloc system memory"=635551744,"Tiler Utilization %"=5,"recoveryCount"=0,"lastRecoveryTime"=0,"Renderer Utilization %"=10,"TiledSceneBytes"=0,"Device Utilization %"=25,"SplitSceneCount"=0,"Allocated PB Size"=11534336,"In use system memory"=33390592}
  |   "model" = "Apple M4"
  |   "gpu-core-count" = 10`

	psOutput := `USER               PID  %CPU %MEM      VSZ    RSS   TT  STAT STARTED      TIME COMMAND
root                 1   0.0  0.1  5134736  21456   ??  Ss   Mon09AM  12:34.56 /sbin/launchd`

	sections := []string{topOutput, vmStatOutput, netstatOutput, gpuOutput, psOutput}

	result := collector.parseDarwinOutput(metrics, sections)
	require.NotNil(t, result)

	// Verify CPU metrics
	assert.InDelta(t, 15.78, result.CPU.Percent, 0.1) // 100 - 84.21 idle
	assert.InDelta(t, 2.45, result.CPU.LoadAvg[0], 0.01)

	// Verify RAM metrics
	assert.Greater(t, result.RAM.TotalBytes, int64(0))

	// Verify network metrics
	assert.NotEmpty(t, result.Network)

	// Verify Apple Silicon GPU metrics
	require.NotNil(t, result.GPU)
	assert.Equal(t, "Apple M4", result.GPU.Name)
	assert.InDelta(t, 25.0, result.GPU.Percent, 0.1) // Device Utilization %
	assert.Equal(t, int64(33390592), result.GPU.MemoryUsed)
	assert.Equal(t, int64(635551744), result.GPU.MemoryTotal)
}

func TestParseDarwinOutput_NoGPU(t *testing.T) {
	collector := &Collector{}
	metrics := &HostMetrics{}

	topOutput := `Load Avg: 1.0, 2.0, 3.0
CPU usage: 10.0% user, 20.0% sys, 70.0% idle`

	vmStatOutput := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                              123456.`

	netstatOutput := `Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0   16384 <Link#1>                         12345     0    1234567    12345     0    1234567     0`

	// Empty GPU output (no Apple Silicon GPU or ioreg failed)
	gpuOutput := ``

	psOutput := `USER               PID  %CPU %MEM      VSZ    RSS   TT  STAT STARTED      TIME COMMAND
root                 1   0.0  0.1  5134736  21456   ??  Ss   Mon09AM  12:34.56 /sbin/launchd`

	sections := []string{topOutput, vmStatOutput, netstatOutput, gpuOutput, psOutput}

	result := collector.parseDarwinOutput(metrics, sections)
	require.NotNil(t, result)

	// GPU should be nil when no data
	assert.Nil(t, result.GPU)
}

func TestParseDarwinOutput_PartialSections(t *testing.T) {
	collector := &Collector{}
	metrics := &HostMetrics{}

	topOutput := `Load Avg: 1.0, 2.0, 3.0
CPU usage: 10.0% user, 20.0% sys, 70.0% idle`

	sections := []string{topOutput}

	result := collector.parseDarwinOutput(metrics, sections)
	require.NotNil(t, result)

	// CPU should be parsed
	assert.InDelta(t, 30.0, result.CPU.Percent, 0.1)

	// Other fields should have zero values
	assert.Equal(t, int64(0), result.RAM.TotalBytes)
	assert.Nil(t, result.Network)
}

// Note: Tests that require actual SSH connections are integration tests.

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
	result, lockInfo := c.parseOutput("test-host", PlatformLinux, linuxOutput)
	require.NotNil(t, result)
	assert.Nil(t, lockInfo)

	// Test Darwin parsing
	darwinOutput := "top data\n---\nvm_stat data\n---\nnetstat data\n---\nps data"
	result, lockInfo = c.parseOutput("test-host", PlatformDarwin, darwinOutput)
	require.NotNil(t, result)
	assert.Nil(t, lockInfo)

	// Test unknown platform (defaults to Linux)
	result, lockInfo = c.parseOutput("test-host", PlatformUnknown, linuxOutput)
	require.NotNil(t, result)
	assert.Nil(t, lockInfo)
}

// lockInfoJSON builds a lock info.json payload for tests.
func lockInfoJSON(t *testing.T, started time.Time, command string) string {
	t.Helper()
	info := lock.LockInfo{
		User:     "testuser",
		Hostname: "testhost",
		Started:  started,
		PID:      1234,
		Command:  command,
	}
	data, err := info.Marshal()
	require.NoError(t, err)
	return string(data)
}

func TestCollector_parseOutput_LockSection(t *testing.T) {
	tests := []struct {
		name       string
		platform   Platform
		lockAge    time.Duration // 0 = no lock payload (empty section)
		wantLocked bool
	}{
		{
			name:       "linux with lock payload",
			platform:   PlatformLinux,
			lockAge:    2 * time.Minute,
			wantLocked: true,
		},
		{
			name:       "darwin with lock payload",
			platform:   PlatformDarwin,
			lockAge:    2 * time.Minute,
			wantLocked: true,
		},
		{
			name:       "linux without lock payload",
			platform:   PlatformLinux,
			lockAge:    0, // empty section
			wantLocked: false,
		},
		{
			name:       "darwin without lock payload",
			platform:   PlatformDarwin,
			lockAge:    0, // empty section
			wantLocked: false,
		},
		{
			name:       "linux stale lock is ignored",
			platform:   PlatformLinux,
			lockAge:    45 * time.Minute, // beyond default 30m stale threshold
			wantLocked: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollector(map[string]config.Host{})

			lockPayload := ""
			if tt.lockAge > 0 {
				lockPayload = lockInfoJSON(t, time.Now().Add(-tt.lockAge), "rr test")
			}

			var sections []string
			if tt.platform == PlatformDarwin {
				sections = []string{"top data", "vm_stat data", "netstat data", "gpu data", "ps data", lockPayload}
			} else {
				sections = []string{"cpu data", "loadavg data", "meminfo data", "net data", "gpu data", "ps data", lockPayload}
			}
			output := strings.Join(sections, "\n---\n")

			result, lockInfo := c.parseOutput("test-host", tt.platform, output)
			require.NotNil(t, result)

			if tt.wantLocked {
				require.NotNil(t, lockInfo)
				assert.True(t, lockInfo.IsLocked)
				assert.Equal(t, "testuser@testhost (pid 1234)", lockInfo.Holder)
				assert.Equal(t, "rr test", lockInfo.Command)
				assert.WithinDuration(t, time.Now().Add(-tt.lockAge), lockInfo.Started, 5*time.Second)
			} else {
				assert.Nil(t, lockInfo)
			}
		})
	}
}

func TestCollector_parseLockSection_StaleThresholdFromConfig(t *testing.T) {
	c := NewCollector(map[string]config.Host{})
	c.SetLockConfig(config.LockConfig{Stale: 5 * time.Minute})

	// Lock aged 10 minutes: fresh under the 30m default, stale under the 5m config
	payload := lockInfoJSON(t, time.Now().Add(-10*time.Minute), "rr build")
	assert.Nil(t, c.parseLockSection(payload))

	// Lock aged 2 minutes: within the configured threshold
	payload = lockInfoJSON(t, time.Now().Add(-2*time.Minute), "rr build")
	info := c.parseLockSection(payload)
	require.NotNil(t, info)
	assert.True(t, info.IsLocked)
}

func TestCollector_parseLockSection_InvalidPayload(t *testing.T) {
	c := NewCollector(map[string]config.Host{})

	assert.Nil(t, c.parseLockSection(""))
	assert.Nil(t, c.parseLockSection("   \n"))
	assert.Nil(t, c.parseLockSection("not valid json"))
}

func TestCollector_lockDir(t *testing.T) {
	c := NewCollector(map[string]config.Host{})

	// Default when lock checking is unconfigured
	assert.Equal(t, "/tmp/rr.lock", c.lockDir())

	// Configured lock dir
	c.SetLockConfig(config.LockConfig{Dir: "/var/lock"})
	assert.Equal(t, "/var/lock/rr.lock", c.lockDir())

	// Configured but empty dir falls back to default
	c.SetLockConfig(config.LockConfig{})
	assert.Equal(t, "/tmp/rr.lock", c.lockDir())
}
