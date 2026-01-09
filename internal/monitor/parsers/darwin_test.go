package parsers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDarwinCPU(t *testing.T) {
	tests := []struct {
		name        string
		topOutput   string
		wantPercent float64
		wantLoadAvg [3]float64
		wantErr     bool
	}{
		{
			name: "typical top output",
			topOutput: `Processes: 450 total, 2 running, 448 sleeping, 2345 threads
2025/01/08 10:30:45
Load Avg: 2.50, 3.25, 2.75
CPU usage: 15.79% user, 10.52% sys, 73.69% idle
SharedLibs: 350M resident, 75M data, 25M linkedit.
MemRegions: 123456 total, 2345M resident, 125M private, 900M shared.
PhysMem: 16G used (2500M wired), 500M unused.
VM: 2T vsize, 1234M framework vsize, 12345678(0) swapins, 23456789(0) swapouts.
Networks: packets: 1234567/1G in, 987654/500M out.
Disks: 2345678/50G read, 1234567/25G written.`,
			wantPercent: 26.31, // 100 - 73.69
			wantLoadAvg: [3]float64{2.50, 3.25, 2.75},
			wantErr:     false,
		},
		{
			name: "high CPU usage",
			topOutput: `Processes: 300 total, 5 running, 295 sleeping
Load Avg: 8.50, 6.25, 4.75
CPU usage: 45.00% user, 35.00% sys, 20.00% idle`,
			wantPercent: 80.0, // 100 - 20
			wantLoadAvg: [3]float64{8.50, 6.25, 4.75},
			wantErr:     false,
		},
		{
			name: "idle system",
			topOutput: `Processes: 200 total, 1 running, 199 sleeping
Load Avg: 0.25, 0.50, 0.75
CPU usage: 2.00% user, 3.00% sys, 95.00% idle`,
			wantPercent: 5.0, // 100 - 95
			wantLoadAvg: [3]float64{0.25, 0.50, 0.75},
			wantErr:     false,
		},
		{
			name:        "empty output",
			topOutput:   "",
			wantPercent: 0,
			wantLoadAvg: [3]float64{0, 0, 0},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := ParseDarwinCPU(tt.topOutput)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, metrics)
			assert.InDelta(t, tt.wantPercent, metrics.Percent, 0.01)
			assert.Equal(t, tt.wantLoadAvg, metrics.LoadAvg)
		})
	}
}

func TestParseDarwinMemory(t *testing.T) {
	tests := []struct {
		name       string
		vmStatOut  string
		wantUsed   int64
		wantTotal  int64
		wantCached int64
		wantErr    bool
	}{
		{
			name: "typical vm_stat output - Apple Silicon with sysctl",
			vmStatOut: `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               50000.
Pages active:                            200000.
Pages inactive:                          100000.
Pages speculative:                        10000.
Pages throttled:                              0.
Pages wired down:                        150000.
Pages purgeable:                          20000.
"Translation faults":                 500000000.
Pages copy-on-write:                   10000000.
Pages zero filled:                    200000000.
Pages reactivated:                      5000000.
Pages purged:                           1000000.
File-backed pages:                        80000.
Anonymous pages:                         180000.
Pages stored in compressor:               30000.
Pages occupied by compressor:             25000.
hw.memsize: 17179869184`,
			// Used = active(200000) + wired(150000) + compressor(25000) = 375000 pages
			// (speculative is now part of available, not used)
			wantUsed:   375000 * 16384, // pages * page_size
			wantTotal:  17179869184,    // 16 GB from sysctl
			wantCached: 80000 * 16384,  // file-backed pages
			wantErr:    false,
		},
		{
			name: "vm_stat output - Intel Mac without sysctl",
			vmStatOut: `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                              100000.
Pages active:                            500000.
Pages inactive:                          200000.
Pages speculative:                        50000.
Pages wired down:                        300000.
Pages purgeable:                          30000.
File-backed pages:                       100000.
Pages occupied by compressor:             50000.`,
			// Used = 500000 + 300000 + 50000 = 850000 pages (no speculative)
			// Available = 100000 + 200000 + 30000 + 50000 = 380000 pages
			// Total fallback = (850000 + 380000) * 4096 = 5,038,080,000
			wantUsed:   850000 * 4096,
			wantTotal:  (850000 + 380000) * 4096, // fallback calculation
			wantCached: 100000 * 4096,
			wantErr:    false,
		},
		{
			name:      "empty output",
			vmStatOut: "",
			wantUsed:  0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := ParseDarwinMemory(tt.vmStatOut)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, metrics)
			assert.Equal(t, tt.wantUsed, metrics.UsedBytes)
			assert.Equal(t, tt.wantCached, metrics.Cached)
			if tt.wantTotal > 0 {
				assert.Equal(t, tt.wantTotal, metrics.TotalBytes)
			}
			// Available should be positive (or zero for empty)
			assert.GreaterOrEqual(t, metrics.Available, int64(0))
		})
	}
}

func TestParseDarwinNetwork(t *testing.T) {
	tests := []struct {
		name       string
		netstatOut string
		wantCount  int
		wantIfaces map[string]struct {
			bytesIn, bytesOut     int64
			packetsIn, packetsOut int64
		}
		wantErr bool
	}{
		{
			name: "typical netstat -ib output",
			netstatOut: `Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0   16384 <Link#1>                         123456     0   12345678   123456     0   12345678     0
lo0   16384 127           127.0.0.1          123456     -   12345678   123456     -   12345678     -
en0   1500  <Link#4>      aa:bb:cc:dd:ee:ff  987654     0   98765432   654321     0   65432109     0
en0   1500  192.168.1     192.168.1.100      500000     -   50000000   400000     -   40000000     -`,
			wantCount: 2,
			wantIfaces: map[string]struct {
				bytesIn, bytesOut     int64
				packetsIn, packetsOut int64
			}{
				"lo0": {12345678, 12345678, 123456, 123456},
				"en0": {98765432, 65432109, 987654, 654321},
			},
			wantErr: false,
		},
		{
			name: "multiple physical interfaces",
			netstatOut: `Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0   16384 <Link#1>                         10000     0    1000000    10000     0    1000000     0
en0   1500  <Link#4>      00:11:22:33:44:55  50000     0    5000000    40000     0    4000000     0
en1   1500  <Link#5>      66:77:88:99:aa:bb  30000     0    3000000    20000     0    2000000     0`,
			wantCount: 3,
			wantIfaces: map[string]struct {
				bytesIn, bytesOut     int64
				packetsIn, packetsOut int64
			}{
				"lo0": {1000000, 1000000, 10000, 10000},
				"en0": {5000000, 4000000, 50000, 40000},
				"en1": {3000000, 2000000, 30000, 20000},
			},
			wantErr: false,
		},
		{
			name:       "empty output",
			netstatOut: "",
			wantCount:  0,
			wantIfaces: nil,
			wantErr:    false,
		},
		{
			name:       "header only",
			netstatOut: `Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll`,
			wantCount:  0,
			wantIfaces: nil,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interfaces, err := ParseDarwinNetwork(tt.netstatOut)

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

func TestParseDarwinCPUUsage(t *testing.T) {
	tests := []struct {
		line        string
		wantPercent float64
	}{
		{"CPU usage: 5.26% user, 10.52% sys, 84.21% idle", 15.79},
		{"CPU usage: 50.00% user, 30.00% sys, 20.00% idle", 80.0},
		{"CPU usage: 0.00% user, 0.00% sys, 100.00% idle", 0.0},
		{"CPU usage: 100.00% user, 0.00% sys, 0.00% idle", 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			result := parseDarwinCPUUsage(tt.line)
			assert.InDelta(t, tt.wantPercent, result, 0.01)
		})
	}
}

func TestParseDarwinLoadAvg(t *testing.T) {
	tests := []struct {
		line string
		want [3]float64
	}{
		{"Load Avg: 1.23, 2.34, 3.45", [3]float64{1.23, 2.34, 3.45}},
		{"Load Avg: 0.00, 0.00, 0.00", [3]float64{0.00, 0.00, 0.00}},
		{"Load Avg: 10.50, 8.25, 6.75", [3]float64{10.50, 8.25, 6.75}},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			result := parseDarwinLoadAvg(tt.line)
			assert.Equal(t, tt.want, result)
		})
	}
}
