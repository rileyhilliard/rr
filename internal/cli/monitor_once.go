package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/monitor"
	"github.com/rileyhilliard/rr/internal/ui"
)

// MonitorOutput is the JSON document emitted by `rr monitor --once --json`.
type MonitorOutput struct {
	Timestamp string              `json:"timestamp"`
	Hosts     []MonitorHostOutput `json:"hosts"`
}

// MonitorHostOutput is a single host's snapshot. Everything past Online is
// omitted when collection failed, so consumers can branch on Online/Error
// without probing for zero values.
type MonitorHostOutput struct {
	Name      string             `json:"name"`
	Alias     string             `json:"alias,omitempty"`
	Online    bool               `json:"online"`
	Error     string             `json:"error,omitempty"`
	LatencyMS float64            `json:"latency_ms"`
	Platform  string             `json:"platform,omitempty"`
	CPU       *MonitorCPUOutput  `json:"cpu,omitempty"`
	RAM       *MonitorRAMOutput  `json:"ram,omitempty"`
	GPU       *MonitorGPUOutput  `json:"gpu,omitempty"`
	Disk      *MonitorDiskOutput `json:"disk,omitempty"`
	Net       *MonitorNetOutput  `json:"net,omitempty"`
	Lock      *MonitorLockOutput `json:"lock,omitempty"`
	System    *MonitorSystemInfo `json:"system,omitempty"`
}

// MonitorCPUOutput reports processor load. PerCore and TempC are Linux-only
// and omitted when the host did not report them.
type MonitorCPUOutput struct {
	Percent float64    `json:"percent"`
	Cores   int        `json:"cores"`
	Load    [3]float64 `json:"load"`
	PerCore []float64  `json:"per_core,omitempty"`
	TempC   float64    `json:"temp_c,omitempty"`
}

// MonitorRAMOutput reports memory usage.
type MonitorRAMOutput struct {
	UsedBytes  int64   `json:"used_bytes"`
	TotalBytes int64   `json:"total_bytes"`
	Percent    float64 `json:"percent"`
}

// MonitorGPUOutput reports GPU usage when a GPU was detected.
type MonitorGPUOutput struct {
	Name             string  `json:"name,omitempty"`
	Percent          float64 `json:"percent"`
	MemoryUsedBytes  int64   `json:"memory_used_bytes"`
	MemoryTotalBytes int64   `json:"memory_total_bytes"`
	TempC            int     `json:"temp_c,omitempty"`
	PowerWatts       int     `json:"power_watts,omitempty"`
}

// MonitorDiskOutput reports root filesystem usage and I/O rates.
// The rate fields are Linux-only.
type MonitorDiskOutput struct {
	UsedBytes        int64   `json:"used_bytes"`
	TotalBytes       int64   `json:"total_bytes"`
	Percent          float64 `json:"percent"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
}

// MonitorNetOutput reports aggregate network throughput across all
// non-loopback interfaces.
type MonitorNetOutput struct {
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
}

// MonitorLockOutput describes an rr lock currently held on the host.
type MonitorLockOutput struct {
	Holder  string `json:"holder"`
	Command string `json:"command,omitempty"`
	Started string `json:"started,omitempty"`
}

// MonitorSystemInfo reports OS identity and uptime.
type MonitorSystemInfo struct {
	OS            string  `json:"os,omitempty"`
	Kernel        string  `json:"kernel,omitempty"`
	UptimeSeconds float64 `json:"uptime_seconds,omitempty"`
}

// validateMonitorFlags rejects flag combinations the monitor command can't
// honor. The live dashboard is a TUI, so there is nothing to serialize.
func validateMonitorFlags() error {
	if monitorJSONFlag && !monitorOnceFlag {
		return errors.New(errors.ErrConfig,
			"--json only works with --once",
			"Run 'rr monitor --once --json' for a one-shot snapshot; the live dashboard has no JSON form.")
	}
	return nil
}

// monitorOnceCommand collects a single delta-accurate snapshot from every
// in-scope host in parallel and prints it, then exits. It fails only when
// every host failed: a partially reachable fleet is still a useful answer.
func monitorOnceCommand(hostsFilter string, asJSON bool) error {
	scope, err := resolveMonitorScope(hostsFilter)
	if err != nil {
		return err
	}

	collector := scope.newCollector()
	collector.SetTimeout(scope.timeout)
	defer collector.Close()

	// The snapshot command sleeps 1s on the remote, so the per-host timeout
	// must cover that on top of connect and collect time.
	ctx, cancel := context.WithTimeout(context.Background(),
		scope.timeout+monitor.SnapshotSleepSeconds*time.Second)
	defer cancel()

	results := collector.CollectSnapshot(ctx)
	output := buildMonitorOutput(results, time.Now())

	if asJSON {
		if err := writeMonitorJSON(os.Stdout, output); err != nil {
			return err
		}
	} else if err := writeMonitorText(os.Stdout, output, scope.thresholds); err != nil {
		return err
	}

	if allHostsFailed(output.Hosts) {
		return errors.New(errors.ErrSSH,
			"Couldn't reach any host",
			"Run 'rr status' to see which SSH aliases are failing, or 'rr doctor' for a full diagnosis.")
	}
	return nil
}

// allHostsFailed reports whether every host in the snapshot errored out.
// An empty snapshot is not a failure, resolveMonitorScope already rejects that.
func allHostsFailed(hosts []MonitorHostOutput) bool {
	if len(hosts) == 0 {
		return false
	}
	for i := range hosts {
		if hosts[i].Online {
			return false
		}
	}
	return true
}

// buildMonitorOutput converts collector results into the JSON document shape.
func buildMonitorOutput(results []monitor.HostResult, now time.Time) MonitorOutput {
	out := MonitorOutput{
		Timestamp: now.UTC().Format(time.RFC3339),
		Hosts:     make([]MonitorHostOutput, 0, len(results)),
	}
	for i := range results {
		out.Hosts = append(out.Hosts, buildMonitorHost(results[i]))
	}
	return out
}

// buildMonitorHost converts one collector result into its JSON form.
func buildMonitorHost(r monitor.HostResult) MonitorHostOutput {
	h := MonitorHostOutput{
		Name:      r.Alias,
		Alias:     r.ConnectedVia,
		LatencyMS: roundMillis(r.Latency),
	}

	if r.Error != nil {
		h.Error = r.Error.Error()
		return h
	}
	if r.Metrics == nil {
		h.Error = "no metrics returned"
		return h
	}

	h.Online = true
	if r.Platform != "" && r.Platform != monitor.PlatformUnknown {
		h.Platform = string(r.Platform)
	}

	m := r.Metrics
	h.CPU = &MonitorCPUOutput{
		Percent: round2(m.CPU.Percent),
		Cores:   m.CPU.Cores,
		Load:    m.CPU.LoadAvg,
		PerCore: roundAll(m.CPU.PerCore),
		TempC:   round2(m.CPU.TempC),
	}
	h.RAM = &MonitorRAMOutput{
		UsedBytes:  m.RAM.UsedBytes,
		TotalBytes: m.RAM.TotalBytes,
		Percent:    round2(percentOf(m.RAM.UsedBytes, m.RAM.TotalBytes)),
	}
	h.Disk = &MonitorDiskOutput{
		UsedBytes:        m.Disk.UsedBytes,
		TotalBytes:       m.Disk.TotalBytes,
		Percent:          round2(m.Disk.Percent),
		ReadBytesPerSec:  round2(m.Disk.ReadBytesPerSec),
		WriteBytesPerSec: round2(m.Disk.WriteBytesPerSec),
	}

	if m.GPU != nil {
		h.GPU = &MonitorGPUOutput{
			Name:             m.GPU.Name,
			Percent:          round2(m.GPU.Percent),
			MemoryUsedBytes:  m.GPU.MemoryUsed,
			MemoryTotalBytes: m.GPU.MemoryTotal,
			TempC:            m.GPU.Temperature,
			PowerWatts:       m.GPU.PowerWatts,
		}
	}

	if r.NetRates != nil {
		h.Net = &MonitorNetOutput{
			RxBytesPerSec: round2(r.NetRates.RxBytesPerSec),
			TxBytesPerSec: round2(r.NetRates.TxBytesPerSec),
		}
	}

	if r.LockInfo != nil && r.LockInfo.IsLocked {
		lock := &MonitorLockOutput{
			Holder:  r.LockInfo.Holder,
			Command: r.LockInfo.Command,
		}
		if !r.LockInfo.Started.IsZero() {
			lock.Started = r.LockInfo.Started.UTC().Format(time.RFC3339)
		}
		h.Lock = lock
	}

	if m.System.OS != "" || m.System.Kernel != "" || m.System.Uptime > 0 {
		h.System = &MonitorSystemInfo{
			OS:            m.System.OS,
			Kernel:        m.System.Kernel,
			UptimeSeconds: math.Round(m.System.Uptime.Seconds()),
		}
	}

	return h
}

// writeMonitorJSON emits the snapshot document. It follows the status command's
// convention: envelope-wrapped in machine mode, bare indented JSON otherwise.
func writeMonitorJSON(w io.Writer, output MonitorOutput) error {
	if MachineMode() {
		return WriteJSONSuccess(w, output)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// writeMonitorText renders the snapshot as one table row per host.
func writeMonitorText(w io.Writer, output MonitorOutput, thresholds config.ThresholdConfig) error {
	columns := []ui.TableColumn{
		{Title: "HOST", Width: 14},
		{Title: "STATUS", Width: 8},
		{Title: "CPU", Width: 7},
		{Title: "RAM", Width: 7},
		{Title: "GPU", Width: 7},
		{Title: "DISK", Width: 7},
		{Title: "LATENCY", Width: 9},
		{Title: "LOCK", Width: 24},
	}

	rows := make([][]string, 0, len(output.Hosts))
	for i := range output.Hosts {
		rows = append(rows, monitorTextRow(output.Hosts[i], thresholds))
	}

	if _, err := fmt.Fprintln(w, ui.RenderSimpleTable(columns, rows)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "Snapshot at %s\n", output.Timestamp)
	return err
}

// monitorTextRow builds one table row, coloring the percentage cells by the
// configured warning/critical thresholds.
func monitorTextRow(h MonitorHostOutput, thresholds config.ThresholdConfig) []string {
	if !h.Online {
		return []string{h.Name, colorize("offline", ui.ColorError), "-", "-", "-", "-", "-", errorCell(h.Error)}
	}

	gpu := "-"
	if h.GPU != nil {
		gpu = metricCell(h.GPU.Percent, thresholds.GPU)
	}

	lock := "-"
	if h.Lock != nil {
		lock = truncateCell(h.Lock.Holder, 24)
	}

	return []string{
		h.Name,
		colorize("online", ui.ColorSuccess),
		metricCell(h.CPU.Percent, thresholds.CPU),
		metricCell(h.RAM.Percent, thresholds.RAM),
		gpu,
		metricCell(h.Disk.Percent, monitorDiskThresholds),
		formatLatencyMS(h.LatencyMS),
		lock,
	}
}

// monitorDiskThresholds mirrors the dashboard's fixed disk severity pair:
// df capacity sits high in normal operation, so it doesn't use the
// configurable CPU/RAM/GPU thresholds.
var monitorDiskThresholds = config.ThresholdValues{Warning: 80, Critical: 95}

// metricCell formats a percentage and colors it by severity.
func metricCell(percent float64, t config.ThresholdValues) string {
	warning, critical := normalizeThresholdPair(t)
	text := fmt.Sprintf("%.0f%%", percent)
	return lipgloss.NewStyle().
		Foreground(monitor.MetricColorWithThresholds(percent, warning, critical)).
		Render(text)
}

// normalizeThresholdPair fills in the 70/90 defaults for unset values, matching
// the dashboard's behavior.
func normalizeThresholdPair(t config.ThresholdValues) (warning, critical int) {
	warning, critical = t.Warning, t.Critical
	if warning <= 0 {
		warning = int(monitor.WarningThreshold)
	}
	if critical <= 0 {
		critical = int(monitor.CriticalThreshold)
	}
	return warning, critical
}

// colorize applies a foreground color. Rendering is a no-op when colors are
// disabled, so --json and piped output stay clean.
func colorize(text string, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Render(text)
}

// errorCell renders a collection error as a single table cell. SSH errors are
// multi-line and prefixed with a status glyph; only the headline survives here,
// and the STATUS column already says "offline". The full text stays in --json.
func errorCell(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "unreachable"
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	msg = strings.TrimLeftFunc(msg, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return truncateCell(msg, 24)
}

// truncateCell shortens a string to fit a table cell, counting runes so
// multibyte characters in remote hostnames or error text aren't split.
func truncateCell(s string, width int) string {
	runes := []rune(s)
	if width <= 1 || len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}

// formatLatencyMS renders a latency in milliseconds for the table.
func formatLatencyMS(ms float64) string {
	if ms <= 0 {
		return "-"
	}
	if ms < 1 {
		return "<1ms"
	}
	return fmt.Sprintf("%.0fms", ms)
}

// percentOf returns used/total as a percentage, guarding division by zero.
func percentOf(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

// round2 rounds to two decimals so JSON output stays readable and stable.
func round2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*100) / 100
}

// roundAll applies round2 across a slice, preserving nil for omitempty.
func roundAll(vals []float64) []float64 {
	if len(vals) == 0 {
		return nil
	}
	out := make([]float64, len(vals))
	for i, v := range vals {
		out[i] = round2(v)
	}
	return out
}

// roundMillis converts a duration to milliseconds with two decimals.
func roundMillis(d time.Duration) float64 {
	return round2(float64(d) / float64(time.Millisecond))
}
