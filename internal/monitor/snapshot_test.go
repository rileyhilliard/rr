package monitor

import (
	"strings"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Snapshot command construction ---

func TestBuildSnapshotCommand_Linux(t *testing.T) {
	cmd := BuildSnapshotCommand(PlatformLinux, "/tmp/rr.lock")

	// The delta-based sources must appear twice: once priming, once for real.
	assert.Equal(t, 2, strings.Count(cmd, "cat /proc/stat"),
		"/proc/stat must be sampled twice so CPU%% has a delta")
	assert.Equal(t, 2, strings.Count(cmd, "cat /proc/net/dev"),
		"/proc/net/dev must be sampled twice so network rates have a delta")
	assert.Equal(t, 2, strings.Count(cmd, "/proc/diskstats"),
		"/proc/diskstats must be sampled twice so disk I/O rates have a delta")

	// Exactly one sleep, between the two samples.
	assert.Equal(t, 1, strings.Count(cmd, "sleep 1"))
	assert.Less(t, strings.Index(cmd, "sleep 1"), strings.LastIndex(cmd, "cat /proc/stat"),
		"the sleep must come before the second sample")

	// Everything the regular battery collects is still there, once.
	assert.Equal(t, 1, strings.Count(cmd, "/proc/meminfo"))
	assert.Equal(t, 1, strings.Count(cmd, "nvidia-smi"))
	assert.Equal(t, 1, strings.Count(cmd, "df -P -k /"))

	// The lock read is still the final section.
	assert.True(t, strings.HasSuffix(cmd, `cat '/tmp/rr.lock/info.json' 2>/dev/null || true`),
		"lock section must stay last, got: %s", cmd)
}

func TestBuildSnapshotCommand_Darwin(t *testing.T) {
	cmd := BuildSnapshotCommand(PlatformDarwin, "/tmp/rr.lock")

	// Only netstat needs priming on macOS: top -l 1 is already instantaneous.
	assert.Equal(t, 2, strings.Count(cmd, "netstat -ib"),
		"netstat must be sampled twice so network rates have a delta")
	assert.Equal(t, 1, strings.Count(cmd, "top -l 1"))
	assert.Equal(t, 1, strings.Count(cmd, "sleep 1"))
	assert.Less(t, strings.Index(cmd, "sleep 1"), strings.LastIndex(cmd, "netstat -ib"))

	assert.True(t, strings.HasSuffix(cmd, `cat '/tmp/rr.lock/info.json' 2>/dev/null || true`),
		"lock section must stay last, got: %s", cmd)
}

func TestBuildSnapshotCommand_UnknownFallsBackToLinux(t *testing.T) {
	assert.Equal(t,
		BuildSnapshotCommand(PlatformLinux, "/tmp/rr.lock"),
		BuildSnapshotCommand(PlatformUnknown, "/tmp/rr.lock"))
}

func TestBuildSnapshotCommand_SectionLayout(t *testing.T) {
	tests := []struct {
		name      string
		platform  Platform
		wantPrime int
		wantLock  int
	}{
		{name: "linux", platform: PlatformLinux, wantPrime: 3, wantLock: linuxLockSection},
		{name: "darwin", platform: PlatformDarwin, wantPrime: 1, wantLock: darwinLockSection},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantPrime, SnapshotPrimeSections(tt.platform))

			base := strings.Count(BuildMetricsCommand(tt.platform, "/tmp/rr.lock"), `echo "---"`)
			snap := strings.Count(BuildSnapshotCommand(tt.platform, "/tmp/rr.lock"), `echo "---"`)
			assert.Equal(t, base+tt.wantPrime, snap,
				"snapshot adds exactly one separator per priming section")

			// After dropping the priming sections, the lock index is unchanged,
			// which is what parseSnapshotOutput relies on.
			assert.Equal(t, tt.wantLock, snap-tt.wantPrime)
		})
	}
}

// TestBuildMetricsCommand_UnaffectedBySnapshot guards the TUI path: adding the
// snapshot builder must not change a single byte of the regular command.
func TestBuildMetricsCommand_UnaffectedBySnapshot(t *testing.T) {
	linux := BuildMetricsCommand(PlatformLinux, "/tmp/rr.lock")
	assert.False(t, strings.Contains(linux, "sleep"), "TUI command must not sleep")
	assert.Equal(t, 1, strings.Count(linux, "cat /proc/stat"), "TUI command samples once")

	darwin := BuildMetricsCommand(PlatformDarwin, "/tmp/rr.lock")
	assert.False(t, strings.Contains(darwin, "sleep"), "TUI command must not sleep")
	assert.Equal(t, 1, strings.Count(darwin, "netstat -ib"), "TUI command samples once")
}

// --- Snapshot output parsing ---

// snapshotStatFixture builds a /proc/stat sample. busy jiffies land in the
// user column and idle in the idle column, so the delta math is predictable.
func snapshotStatFixture(busy, idle int64) string {
	return "cpu  " + itoa(busy) + " 0 0 " + itoa(idle) + " 0 0 0 0 0 0\n" +
		"cpu0 " + itoa(busy/2) + " 0 0 " + itoa(idle/2) + " 0 0 0 0 0 0\n" +
		"cpu1 " + itoa(busy/2) + " 0 0 " + itoa(idle/2) + " 0 0 0 0 0 0"
}

func itoa(n int64) string {
	return formatInt(int(n))
}

// snapshotNetFixture builds a /proc/net/dev sample with the given eth0 counters.
func snapshotNetFixture(rx, tx int64) string {
	return "Inter-|   Receive                                                |  Transmit\n" +
		" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed\n" +
		"    lo: 500 5 0 0 0 0 0 0 500 5 0 0 0 0 0 0\n" +
		"  eth0: " + itoa(rx) + " 10 0 0 0 0 0 0 " + itoa(tx) + " 20 0 0 0 0 0 0"
}

// snapshotDiskFixture builds a /proc/diskstats sample for a single nvme device
// with the given read/write sector counts.
func snapshotDiskFixture(readSectors, writeSectors int64) string {
	return " 259       0 nvme0n1 100 0 " + itoa(readSectors) + " 10 200 0 " + itoa(writeSectors) + " 20 0 30 40 0 0 0 0 0 0"
}

// buildLinuxSnapshotFixture assembles a full snapshot payload: three priming
// sections, then the eleven regular Linux sections.
func buildLinuxSnapshotFixture() string {
	sep := "\n" + OutputSeparator + "\n"

	prime := snapshotStatFixture(1_000, 9_000) + sep +
		snapshotNetFixture(1_000_000, 2_000_000) + sep +
		snapshotDiskFixture(1_000, 2_000)

	// Second sample: 500 busy jiffies and 500 idle jiffies later => 50% CPU.
	regular := strings.Join([]string{
		snapshotStatFixture(1_500, 9_500),
		"1.23 2.34 3.45",
		"MemTotal:       16000000 kB\nMemFree:         4000000 kB\nMemAvailable:    8000000 kB\nBuffers:          500000 kB\nCached:          2000000 kB",
		snapshotNetFixture(1_003_000, 2_004_000),
		"", // nvidia-smi: no GPU
		"USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND\nroot 1 0.1 0.2 100 200 ? Ss 10:00 0:01 /sbin/init",
		"Filesystem     1024-blocks      Used Available Capacity Mounted on\n/dev/nvme0n1p2   479079112 202230300 252440264      45% /",
		snapshotDiskFixture(3_000, 6_000),
		"coretemp:42000",
		"1520410.85 11421484.42\n6.8.0-64-generic",
		`{"host":"builder","user":"riley","pid":123,"started":"` + time.Now().UTC().Format(time.RFC3339) + `","command":"go test ./..."}`,
	}, sep)

	return prime + sep + regular
}

func TestParseSnapshotOutput_LinuxRatesAreRealOnFirstRound(t *testing.T) {
	c := NewCollector(map[string]config.Host{})
	c.SetLockConfig(config.LockConfig{Enabled: true})

	metrics, lockInfo, rates := c.parseSnapshotOutput("builder", PlatformLinux, buildLinuxSnapshotFixture())
	require.NotNil(t, metrics)

	// CPU: 500 busy / 1000 total delta = 50%. The whole point of double
	// sampling; a single round would report 0 and flag itself as warming up.
	assert.InDelta(t, 50.0, metrics.CPU.Percent, 0.01)
	assert.False(t, metrics.CPU.FirstSample, "the priming sample supplies the baseline")
	assert.True(t, metrics.CPU.Valid())
	assert.Equal(t, 2, metrics.CPU.Cores)
	require.Len(t, metrics.CPU.PerCore, 2)
	assert.InDelta(t, 50.0, metrics.CPU.PerCore[0], 0.01)
	assert.InDelta(t, 50.0, metrics.CPU.PerCore[1], 0.01)

	// Disk I/O: 2000 read sectors and 4000 written sectors over ~1s. The rate
	// divides by real elapsed time (parse overhead included), so allow 5% for
	// scheduling slack on loaded CI runners.
	assert.InDelta(t, float64(2_000*512), metrics.Disk.ReadBytesPerSec, float64(2_000*512)*0.05)
	assert.InDelta(t, float64(4_000*512), metrics.Disk.WriteBytesPerSec, float64(4_000*512)*0.05)

	// Network rates come from the two counter samples, loopback excluded.
	require.NotNil(t, rates)
	assert.InDelta(t, 3_000.0, rates.RxBytesPerSec, 0.01)
	assert.InDelta(t, 4_000.0, rates.TxBytesPerSec, 0.01)

	// The rest of the battery still parses off the second sample.
	assert.InDelta(t, 1.23, metrics.CPU.LoadAvg[0], 0.001)
	assert.InDelta(t, 42.0, metrics.CPU.TempC, 0.001)
	assert.Equal(t, int64(16000000*1024), metrics.RAM.TotalBytes)
	assert.InDelta(t, 45, metrics.Disk.Percent, 0.01)
	assert.Equal(t, "6.8.0-64-generic", metrics.System.Kernel)
	assert.Len(t, metrics.Processes, 1)

	// Lock still resolves from the final section.
	require.NotNil(t, lockInfo)
	assert.True(t, lockInfo.IsLocked)
	assert.Equal(t, "go test ./...", lockInfo.Command)
}

func TestParseSnapshotOutput_DarwinNetRates(t *testing.T) {
	c := NewCollector(map[string]config.Host{})
	sep := "\n" + OutputSeparator + "\n"

	netstat := func(ibytes, obytes int64) string {
		return "Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll\n" +
			"en0   1500  <Link#4>    aa:bb:cc:dd:ee:ff      100     0 " + itoa(ibytes) + "      200     0 " + itoa(obytes) + "     0"
	}

	payload := netstat(1_000_000, 2_000_000) + sep + strings.Join([]string{
		"CPU usage: 10.0% user, 5.0% sys, 85.0% idle\nLoad Avg: 1.50, 1.20, 1.00",
		"Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages active:  100000.\nPages wired down: 50000.\nPages free: 20000.\nhw.memsize: 17179869184",
		netstat(1_005_000, 2_002_500),
		"", // no GPU
		"USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND\nroot 1 0.1 0.2 100 200 ? Ss 10:00 0:01 /sbin/launchd",
		"Filesystem     1024-blocks      Used Available Capacity Mounted on\n/dev/disk3s1   970000000 300000000 670000000      31% /",
		"10",
		"{ sec = 1753837432, usec = 0 } Tue Jul 29 16:03:52 2026\n24.5.0",
		"", // no lock
	}, sep)

	metrics, lockInfo, rates := c.parseSnapshotOutput("m4", PlatformDarwin, payload)
	require.NotNil(t, metrics)
	assert.Nil(t, lockInfo)

	// macOS CPU is not delta-based, so it is real from the single top sample.
	assert.InDelta(t, 15.0, metrics.CPU.Percent, 0.01)
	assert.Equal(t, 10, metrics.CPU.Cores)

	require.NotNil(t, rates)
	assert.InDelta(t, 5_000.0, rates.RxBytesPerSec, 0.01)
	assert.InDelta(t, 2_500.0, rates.TxBytesPerSec, 0.01)
}

func TestParseSnapshotOutput_TruncatedFallsBackToSingleSample(t *testing.T) {
	c := NewCollector(map[string]config.Host{})

	// Host died after the first sample, before the sleep produced a second.
	// What's left looks like a plain single-sample payload.
	truncated := snapshotStatFixture(1_000, 9_000) + "\n" + OutputSeparator + "\n" +
		"1.23 2.34 3.45"

	metrics, _, rates := c.parseSnapshotOutput("flaky", PlatformLinux, truncated)
	require.NotNil(t, metrics)

	// Salvage what's parseable rather than dropping the host entirely.
	assert.Equal(t, 2, metrics.CPU.Cores)
	assert.InDelta(t, 1.23, metrics.CPU.LoadAvg[0], 0.001)

	// But no delta baseline exists, so the 0% is flagged as warming up rather
	// than passed off as a real reading, and no rates are fabricated.
	assert.Zero(t, metrics.CPU.Percent)
	assert.True(t, metrics.CPU.FirstSample)
	assert.False(t, metrics.CPU.Valid())
	assert.Nil(t, rates)
}

// --- Network rate math ---

func TestNetworkRatesFromSamples(t *testing.T) {
	tests := []struct {
		name     string
		prev     []NetworkInterface
		cur      []NetworkInterface
		interval time.Duration
		wantNil  bool
		wantRx   float64
		wantTx   float64
	}{
		{
			name:     "sums non-loopback interfaces",
			prev:     []NetworkInterface{{Name: "eth0", BytesIn: 100, BytesOut: 200}, {Name: "wlan0", BytesIn: 10, BytesOut: 20}},
			cur:      []NetworkInterface{{Name: "eth0", BytesIn: 1100, BytesOut: 2200}, {Name: "wlan0", BytesIn: 510, BytesOut: 20}},
			interval: time.Second,
			wantRx:   1500,
			wantTx:   2000,
		},
		{
			name:     "loopback is excluded",
			prev:     []NetworkInterface{{Name: "lo", BytesIn: 0, BytesOut: 0}, {Name: "lo0", BytesIn: 0}, {Name: "eth0", BytesIn: 0}},
			cur:      []NetworkInterface{{Name: "lo", BytesIn: 9999, BytesOut: 9999}, {Name: "lo0", BytesIn: 9999}, {Name: "eth0", BytesIn: 100}},
			interval: time.Second,
			wantRx:   100,
		},
		{
			name:     "half-second interval doubles the rate",
			prev:     []NetworkInterface{{Name: "eth0", BytesIn: 0}},
			cur:      []NetworkInterface{{Name: "eth0", BytesIn: 500}},
			interval: 500 * time.Millisecond,
			wantRx:   1000,
		},
		{
			name:     "counter reset contributes zero, not a negative rate",
			prev:     []NetworkInterface{{Name: "eth0", BytesIn: 10_000, BytesOut: 10_000}},
			cur:      []NetworkInterface{{Name: "eth0", BytesIn: 5, BytesOut: 5}},
			interval: time.Second,
		},
		{
			name:     "interfaces absent from the first sample are skipped",
			prev:     []NetworkInterface{{Name: "eth0", BytesIn: 0}},
			cur:      []NetworkInterface{{Name: "eth0", BytesIn: 100}, {Name: "docker0", BytesIn: 99_999}},
			interval: time.Second,
			wantRx:   100,
		},
		{
			name:     "no priming sample yields nil",
			cur:      []NetworkInterface{{Name: "eth0", BytesIn: 100}},
			interval: time.Second,
			wantNil:  true,
		},
		{
			name:     "zero interval yields nil",
			prev:     []NetworkInterface{{Name: "eth0"}},
			cur:      []NetworkInterface{{Name: "eth0", BytesIn: 100}},
			interval: 0,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := networkRatesFromSamples(tt.prev, tt.cur, tt.interval)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.InDelta(t, tt.wantRx, got.RxBytesPerSec, 0.01)
			assert.InDelta(t, tt.wantTx, got.TxBytesPerSec, 0.01)
		})
	}
}

func TestCollectSnapshot_NoHostsReturnsEmpty(t *testing.T) {
	c := NewCollector(map[string]config.Host{})
	assert.Empty(t, c.CollectSnapshot(t.Context()))
}
