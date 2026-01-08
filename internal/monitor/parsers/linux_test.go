package parsers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			name: "valid two core system",
			procStat: `cpu  1234567 12345 234567 8901234 12345 0 6789 0 0 0
cpu0 617283 6172 117283 4450617 6172 0 3394 0 0 0
cpu1 617284 6173 117284 4450617 6173 0 3395 0 0 0`,
			procLoadavg: "1.23 2.34 3.45 1/234 5678",
			wantCores:   2,
			wantLoadAvg: [3]float64{1.23, 2.34, 3.45},
			wantErr:     false,
		},
		{
			name: "four core system",
			procStat: `cpu  2000000 20000 400000 16000000 20000 0 10000 0 0 0
cpu0 500000 5000 100000 4000000 5000 0 2500 0 0 0
cpu1 500000 5000 100000 4000000 5000 0 2500 0 0 0
cpu2 500000 5000 100000 4000000 5000 0 2500 0 0 0
cpu3 500000 5000 100000 4000000 5000 0 2500 0 0 0`,
			procLoadavg: "0.50 1.00 1.50 2/100 1234",
			wantCores:   4,
			wantLoadAvg: [3]float64{0.50, 1.00, 1.50},
			wantErr:     false,
		},
		{
			name: "no loadavg",
			procStat: `cpu  1234567 12345 234567 8901234 12345 0 6789 0 0 0
cpu0 617283 6172 117283 4450617 6172 0 3394 0 0 0`,
			procLoadavg: "",
			wantCores:   1,
			wantLoadAvg: [3]float64{0, 0, 0},
			wantErr:     false,
		},
		{
			name:        "invalid cpu line",
			procStat:    "cpu  invalid data",
			procLoadavg: "1.0 2.0 3.0",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := ParseLinuxCPU(tt.procStat, tt.procLoadavg)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, metrics)
			assert.Equal(t, tt.wantCores, metrics.Cores)
			assert.Equal(t, tt.wantLoadAvg, metrics.LoadAvg)
			// CPU percent should be between 0 and 100
			assert.GreaterOrEqual(t, metrics.Percent, 0.0)
			assert.LessOrEqual(t, metrics.Percent, 100.0)
		})
	}
}

func TestParseLinuxCPU_PercentCalculation(t *testing.T) {
	// Test specific CPU percentage calculation
	// Total jiffies: 1234567 + 12345 + 234567 + 8901234 + 12345 + 0 + 6789 + 0 + 0 + 0 = 10,401,847
	// Idle jiffies (idle + iowait): 8901234 + 12345 = 8,913,579
	// Non-idle: 10,401,847 - 8,913,579 = 1,488,268
	// Percent: 1,488,268 / 10,401,847 * 100 = ~14.3%
	procStat := `cpu  1234567 12345 234567 8901234 12345 0 6789 0 0 0
cpu0 617283 6172 117283 4450617 6172 0 3394 0 0 0`

	metrics, err := ParseLinuxCPU(procStat, "")
	require.NoError(t, err)

	// Check that the calculation is approximately correct
	assert.InDelta(t, 14.3, metrics.Percent, 0.5)
}

func TestParseLinuxMemory(t *testing.T) {
	tests := []struct {
		name          string
		procMeminfo   string
		wantTotal     int64
		wantAvailable int64
		wantCached    int64
		wantUsedRange [2]int64 // min, max expected
		wantErr       bool
	}{
		{
			name: "valid meminfo",
			procMeminfo: `MemTotal:       16384000 kB
MemFree:         1234567 kB
MemAvailable:    8765432 kB
Buffers:          123456 kB
Cached:          4567890 kB
SwapCached:        12345 kB
Active:          5000000 kB
Inactive:        4000000 kB`,
			wantTotal:     16384000 * 1024,
			wantAvailable: 8765432 * 1024,
			wantCached:    (4567890 + 123456) * 1024, // Cached + Buffers
			wantUsedRange: [2]int64{10000000000, 11000000000},
			wantErr:       false,
		},
		{
			name: "minimal meminfo",
			procMeminfo: `MemTotal:       8000000 kB
MemFree:         500000 kB
MemAvailable:   2000000 kB
Buffers:          50000 kB
Cached:         1000000 kB`,
			wantTotal:     8000000 * 1024,
			wantAvailable: 2000000 * 1024,
			wantCached:    (1000000 + 50000) * 1024,
			wantUsedRange: [2]int64{6000000000, 7000000000},
			wantErr:       false,
		},
		{
			name:        "insufficient fields",
			procMeminfo: `MemTotal:       16384000 kB`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := ParseLinuxMemory(tt.procMeminfo)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, metrics)
			assert.Equal(t, tt.wantTotal, metrics.TotalBytes)
			assert.Equal(t, tt.wantAvailable, metrics.Available)
			assert.Equal(t, tt.wantCached, metrics.Cached)
			assert.GreaterOrEqual(t, metrics.UsedBytes, tt.wantUsedRange[0])
			assert.LessOrEqual(t, metrics.UsedBytes, tt.wantUsedRange[1])
		})
	}
}

func TestParseLinuxNetwork(t *testing.T) {
	tests := []struct {
		name       string
		procNetDev string
		wantCount  int
		wantIfaces map[string]struct {
			bytesIn, bytesOut     int64
			packetsIn, packetsOut int64
		}
		wantErr bool
	}{
		{
			name: "valid network interfaces",
			procNetDev: `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234567   12345    0    0    0     0          0         0  1234567   12345    0    0    0     0       0          0
  eth0: 9876543   98765    0    0    0     0          0         0  5678901   56789    0    0    0     0       0          0`,
			wantCount: 2,
			wantIfaces: map[string]struct {
				bytesIn, bytesOut     int64
				packetsIn, packetsOut int64
			}{
				"lo":   {1234567, 1234567, 12345, 12345},
				"eth0": {9876543, 5678901, 98765, 56789},
			},
			wantErr: false,
		},
		{
			name: "multiple interfaces",
			procNetDev: `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  1000000   10000    0    0    0     0          0         0   1000000   10000    0    0    0     0       0          0
  eth0: 50000000  500000    0    0    0     0          0         0  25000000  250000    0    0    0     0       0          0
 wlan0: 30000000  300000   10    5    0     0          0         0  15000000  150000    5    0    0     0       0          0`,
			wantCount: 3,
			wantIfaces: map[string]struct {
				bytesIn, bytesOut     int64
				packetsIn, packetsOut int64
			}{
				"lo":    {1000000, 1000000, 10000, 10000},
				"eth0":  {50000000, 25000000, 500000, 250000},
				"wlan0": {30000000, 15000000, 300000, 150000},
			},
			wantErr: false,
		},
		{
			name:       "empty output",
			procNetDev: "",
			wantCount:  0,
			wantIfaces: nil,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interfaces, err := ParseLinuxNetwork(tt.procNetDev)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, interfaces, tt.wantCount)

			for name, expected := range tt.wantIfaces {
				var found bool
				for _, iface := range interfaces {
					if iface.Name == name {
						found = true
						assert.Equal(t, expected.bytesIn, iface.BytesIn, "BytesIn for %s", name)
						assert.Equal(t, expected.bytesOut, iface.BytesOut, "BytesOut for %s", name)
						assert.Equal(t, expected.packetsIn, iface.PacketsIn, "PacketsIn for %s", name)
						assert.Equal(t, expected.packetsOut, iface.PacketsOut, "PacketsOut for %s", name)
						break
					}
				}
				assert.True(t, found, "interface %s not found", name)
			}
		})
	}
}
