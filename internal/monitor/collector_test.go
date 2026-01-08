package monitor

import (
	"testing"

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
	collector := &Collector{}
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

	result, err := collector.parseLinuxOutput(metrics, sections)
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
	collector := &Collector{}
	metrics := &HostMetrics{}

	// Only provide CPU section
	procStat := `cpu  1000000 10000 200000 8000000 10000 0 5000 0 0 0
cpu0 500000 5000 100000 4000000 5000 0 2500 0 0 0`
	procLoadavg := "0.5 1.0 1.5"

	sections := []string{procStat, procLoadavg}

	result, err := collector.parseLinuxOutput(metrics, sections)
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
	collector := &Collector{}
	metrics := &HostMetrics{}

	sections := []string{}

	result, err := collector.parseLinuxOutput(metrics, sections)
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
