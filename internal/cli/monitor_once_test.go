package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/monitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotAt is a fixed timestamp so JSON assertions stay stable.
var snapshotAt = time.Date(2026, 1, 12, 15, 4, 5, 0, time.UTC)

// fullHostResult is a host reporting every optional field, so the golden JSON
// covers the whole schema.
func fullHostResult() monitor.HostResult {
	return monitor.HostResult{
		Alias:        "builder",
		ConnectedVia: "builder-tailscale",
		Latency:      12500 * time.Microsecond,
		Platform:     monitor.PlatformLinux,
		Metrics: &monitor.HostMetrics{
			CPU: monitor.CPUMetrics{
				Percent: 42.567,
				Cores:   8,
				LoadAvg: [3]float64{1.5, 1.25, 1},
				PerCore: []float64{40.111, 45.999},
				TempC:   61.5,
			},
			RAM:  monitor.RAMMetrics{UsedBytes: 8 << 30, TotalBytes: 32 << 30},
			GPU:  &monitor.GPUMetrics{Name: "NVIDIA RTX 4090", Percent: 88, MemoryUsed: 4 << 30, MemoryTotal: 24 << 30, Temperature: 71, PowerWatts: 320},
			Disk: monitor.DiskMetrics{UsedBytes: 200 << 30, TotalBytes: 500 << 30, Percent: 40, ReadBytesPerSec: 1024, WriteBytesPerSec: 2048},
			System: monitor.SystemInfo{
				OS:     "Linux",
				Kernel: "6.8.0-64-generic",
				Uptime: 90 * time.Minute,
			},
		},
		NetRates: &monitor.NetRates{RxBytesPerSec: 3000, TxBytesPerSec: 4000},
		LockInfo: &monitor.HostLockInfo{
			IsLocked: true,
			Holder:   "riley@laptop",
			Command:  "go test ./...",
			Started:  time.Date(2026, 1, 12, 15, 0, 0, 0, time.UTC),
		},
	}
}

func TestBuildMonitorOutput_JSONSchema(t *testing.T) {
	out := buildMonitorOutput([]monitor.HostResult{fullHostResult()}, snapshotAt)

	data, err := json.Marshal(out)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "2026-01-12T15:04:05Z", got["timestamp"])

	hosts, ok := got["hosts"].([]any)
	require.True(t, ok)
	require.Len(t, hosts, 1)
	host := hosts[0].(map[string]any)

	// Field names are the contract for scripts and agents: assert them all.
	assert.Equal(t, []string{
		"name", "alias", "online", "latency_ms", "platform",
		"cpu", "ram", "gpu", "disk", "net", "lock", "system",
	}, jsonKeys(t, data, "hosts", 0))

	assert.Equal(t, "builder", host["name"])
	assert.Equal(t, "builder-tailscale", host["alias"])
	assert.Equal(t, true, host["online"])
	assert.Equal(t, 12.5, host["latency_ms"])
	assert.Equal(t, "linux", host["platform"])

	cpu := host["cpu"].(map[string]any)
	assert.Equal(t, 42.57, cpu["percent"], "percentages are rounded to two decimals")
	assert.Equal(t, float64(8), cpu["cores"])
	assert.Equal(t, []any{1.5, 1.25, float64(1)}, cpu["load"])
	assert.Equal(t, []any{40.11, 46.0}, cpu["per_core"])
	assert.Equal(t, 61.5, cpu["temp_c"])

	ram := host["ram"].(map[string]any)
	assert.Equal(t, float64(8<<30), ram["used_bytes"])
	assert.Equal(t, float64(32<<30), ram["total_bytes"])
	assert.Equal(t, float64(25), ram["percent"])

	gpu := host["gpu"].(map[string]any)
	assert.Equal(t, "NVIDIA RTX 4090", gpu["name"])
	assert.Equal(t, float64(88), gpu["percent"])
	assert.Equal(t, float64(4<<30), gpu["memory_used_bytes"])
	assert.Equal(t, float64(24<<30), gpu["memory_total_bytes"])
	assert.Equal(t, float64(71), gpu["temp_c"])
	assert.Equal(t, float64(320), gpu["power_watts"])

	disk := host["disk"].(map[string]any)
	assert.Equal(t, float64(40), disk["percent"])
	assert.Equal(t, float64(1024), disk["read_bytes_per_sec"])
	assert.Equal(t, float64(2048), disk["write_bytes_per_sec"])

	net := host["net"].(map[string]any)
	assert.Equal(t, float64(3000), net["rx_bytes_per_sec"])
	assert.Equal(t, float64(4000), net["tx_bytes_per_sec"])

	lock := host["lock"].(map[string]any)
	assert.Equal(t, "riley@laptop", lock["holder"])
	assert.Equal(t, "go test ./...", lock["command"])
	assert.Equal(t, "2026-01-12T15:00:00Z", lock["started"])

	system := host["system"].(map[string]any)
	assert.Equal(t, "Linux", system["os"])
	assert.Equal(t, "6.8.0-64-generic", system["kernel"])
	assert.Equal(t, float64(5400), system["uptime_seconds"])

	// A fully-populated host has nothing to report as an error.
	assert.NotContains(t, host, "error")
}

func TestBuildMonitorOutput_OmitsEmptyOptionals(t *testing.T) {
	minimal := monitor.HostResult{
		Alias:    "bare",
		Platform: monitor.PlatformUnknown,
		Metrics: &monitor.HostMetrics{
			CPU: monitor.CPUMetrics{Percent: 10, Cores: 4},
			RAM: monitor.RAMMetrics{UsedBytes: 1, TotalBytes: 2},
		},
	}

	out := buildMonitorOutput([]monitor.HostResult{minimal}, snapshotAt)
	data, err := json.Marshal(out)
	require.NoError(t, err)

	assert.Equal(t, []string{"name", "online", "latency_ms", "cpu", "ram", "disk"},
		jsonKeys(t, data, "hosts", 0))

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	cpu := got["hosts"].([]any)[0].(map[string]any)["cpu"].(map[string]any)
	// per_core and temp_c are Linux extras; they must not show up as zeros.
	assert.NotContains(t, cpu, "per_core")
	assert.NotContains(t, cpu, "temp_c")
}

func TestBuildMonitorOutput_FailedHost(t *testing.T) {
	out := buildMonitorOutput([]monitor.HostResult{
		{Alias: "dead", Error: errors.New("dial tcp: connection refused")},
		{Alias: "empty"},
	}, snapshotAt)

	require.Len(t, out.Hosts, 2)

	assert.False(t, out.Hosts[0].Online)
	assert.Equal(t, "dial tcp: connection refused", out.Hosts[0].Error)
	// No metrics blocks are fabricated for an unreachable host.
	assert.Nil(t, out.Hosts[0].CPU)
	assert.Nil(t, out.Hosts[0].RAM)
	assert.Nil(t, out.Hosts[0].Disk)

	// A nil-metrics result without an error still reads as offline.
	assert.False(t, out.Hosts[1].Online)
	assert.Equal(t, "no metrics returned", out.Hosts[1].Error)
}

func TestBuildMonitorOutput_StaleLockIsOmitted(t *testing.T) {
	r := fullHostResult()
	r.LockInfo = &monitor.HostLockInfo{IsLocked: false, Holder: "someone"}

	out := buildMonitorOutput([]monitor.HostResult{r}, snapshotAt)
	assert.Nil(t, out.Hosts[0].Lock, "an unheld lock must not appear in the output")
}

func TestAllHostsFailed(t *testing.T) {
	tests := []struct {
		name  string
		hosts []MonitorHostOutput
		want  bool
	}{
		{name: "empty snapshot is not a failure", hosts: nil, want: false},
		{name: "all offline", hosts: []MonitorHostOutput{{Online: false}, {Online: false}}, want: true},
		{name: "one online is enough", hosts: []MonitorHostOutput{{Online: false}, {Online: true}}, want: false},
		{name: "all online", hosts: []MonitorHostOutput{{Online: true}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, allHostsFailed(tt.hosts))
		})
	}
}

func TestWriteMonitorText(t *testing.T) {
	out := buildMonitorOutput([]monitor.HostResult{
		fullHostResult(),
		{Alias: "dead", Error: errors.New("connection refused")},
	}, snapshotAt)

	var buf bytes.Buffer
	require.NoError(t, writeMonitorText(&buf, out, config.ThresholdConfig{}))
	text := buf.String()

	for _, want := range []string{
		"HOST", "STATUS", "CPU", "RAM", "GPU", "DISK", "LATENCY", "LOCK",
		"builder", "online", "43%", "25%", "88%", "40%", "12ms", "riley@laptop",
		"dead", "offline", "connection refused",
		"Snapshot at 2026-01-12T15:04:05Z",
	} {
		assert.Contains(t, text, want)
	}
}

func TestNormalizeThresholdPair(t *testing.T) {
	tests := []struct {
		name         string
		in           config.ThresholdValues
		wantWarning  int
		wantCritical int
	}{
		{name: "unset falls back to 70/90", in: config.ThresholdValues{}, wantWarning: 70, wantCritical: 90},
		{name: "configured values win", in: config.ThresholdValues{Warning: 50, Critical: 80}, wantWarning: 50, wantCritical: 80},
		{name: "negative values fall back", in: config.ThresholdValues{Warning: -1, Critical: -1}, wantWarning: 70, wantCritical: 90},
		{name: "partial config keeps the default for the other", in: config.ThresholdValues{Warning: 60}, wantWarning: 60, wantCritical: 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warning, critical := normalizeThresholdPair(tt.in)
			assert.Equal(t, tt.wantWarning, warning)
			assert.Equal(t, tt.wantCritical, critical)
		})
	}
}

func TestFormatLatencyMS(t *testing.T) {
	tests := []struct {
		name string
		ms   float64
		want string
	}{
		{name: "no measurement", ms: 0, want: "-"},
		{name: "sub-millisecond", ms: 0.4, want: "<1ms"},
		{name: "rounded", ms: 12.6, want: "13ms"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatLatencyMS(tt.ms))
		})
	}
}

func TestErrorCell(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{name: "empty falls back", msg: "", want: "unreachable"},
		{name: "single line passes through", msg: "connection refused", want: "connection refused"},
		{
			name: "multi-line SSH error collapses to its headline",
			msg:  "✗ Couldn't connect to 'm1-mini'\n\n  probe m1-local failed: connection timed out",
			want: "Couldn't connect to 'm1…",
		},
		{name: "leading glyph is stripped", msg: "✕ nope", want: "nope"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errorCell(tt.msg))
		})
	}
}

func TestTruncateCell(t *testing.T) {
	assert.Equal(t, "short", truncateCell("short", 10))
	assert.Equal(t, "exactfit", truncateCell("exactfit", 8))
	assert.Equal(t, "waytoolo…", truncateCell("waytoolongvalue", 9))
	// Truncation counts runes, so multibyte text is never cut mid-character.
	assert.Equal(t, "日本語のホスト…", truncateCell("日本語のホスト名がとても長い", 8))
}

func TestPercentOf(t *testing.T) {
	assert.InDelta(t, 50.0, percentOf(1, 2), 0.001)
	assert.Zero(t, percentOf(1, 0), "zero total must not divide by zero")
	assert.Zero(t, percentOf(1, -5))
}

func TestRound2(t *testing.T) {
	assert.Equal(t, 1.23, round2(1.2345))
	assert.Equal(t, 1.24, round2(1.2355))
	assert.Zero(t, round2(math.NaN()), "NaN must not leak into JSON")
	assert.Zero(t, round2(math.Inf(1)), "Inf must not leak into JSON")
	assert.Nil(t, roundAll(nil), "nil stays nil so omitempty applies")
	assert.Equal(t, []float64{1.11, 2.22}, roundAll([]float64{1.111, 2.222}))
}

// --- Flag validation ---

func TestMonitorCmd_JSONRequiresOnce(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "--json alone is rejected", args: []string{"--json"}, wantErr: true},
		{name: "--json with --once is accepted", args: []string{"--json", "--once"}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetMonitorFlags)
			resetMonitorFlags()
			require.NoError(t, monitorCmd.ParseFlags(tt.args))

			err := validateMonitorFlags()
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--json only works with --once")
			assert.Contains(t, err.Error(), "rr monitor --once --json",
				"the error must suggest the working invocation")
		})
	}
}

func TestMonitorCmd_FlagsRegistered(t *testing.T) {
	for _, name := range []string{"once", "json", "hosts", "interval"} {
		assert.NotNil(t, monitorCmd.Flags().Lookup(name), "monitor should have --%s", name)
	}
}

func TestMonitorCmd_LongHelpDocumentsSnapshotMode(t *testing.T) {
	assert.Contains(t, monitorCmd.Long, "rr monitor --once")
	assert.Contains(t, monitorCmd.Long, "rr monitor --once --json")
}

// resetMonitorFlags restores the package-level monitor flag vars so table
// entries don't leak into each other.
func resetMonitorFlags() {
	monitorOnceFlag = false
	monitorJSONFlag = false
	monitorHostsFlag = ""
	monitorIntervalFlag = "1s"
}

// jsonKeys returns the key order of hosts[idx] as it appears in the encoded
// document, which is the struct field order.
func jsonKeys(t *testing.T, data []byte, arrayField string, idx int) []string {
	t.Helper()

	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &doc))

	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(doc[arrayField], &arr))
	require.Greater(t, len(arr), idx)

	dec := json.NewDecoder(bytes.NewReader(arr[idx]))
	tok, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), tok)

	var keys []string
	for dec.More() {
		key, err := dec.Token()
		require.NoError(t, err)
		keys = append(keys, key.(string))

		var skip json.RawMessage
		require.NoError(t, dec.Decode(&skip))
	}
	return keys
}
