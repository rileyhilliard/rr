package monitor

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rileyhilliard/rr/internal/config"
)

// AlertMetric identifies which metric an alert is about.
type AlertMetric string

const (
	AlertMetricCPU AlertMetric = "cpu"
	AlertMetricRAM AlertMetric = "ram"
	AlertMetricGPU AlertMetric = "gpu"
)

// defaultAlertCooldown is the minimum time between re-fires for the same
// host+metric when monitor.alerts.cooldown isn't configured.
const defaultAlertCooldown = 60 * time.Second

// onAlertTimeout bounds each on_alert hook run. Without it, a hanging hook
// (network call, `read`, `sleep 1d`) pins a goroutine plus a child process
// per alert for the life of the TUI session.
const onAlertTimeout = 10 * time.Second

// alertState is the per-host+metric state machine.
//
// A metric fires once when it crosses its critical threshold. It stays
// "firing" (no re-fire) until it drops back below the WARNING threshold, which
// re-arms it. Hysteresis on the warning threshold keeps a metric hovering
// around critical from firing on every sample.
type alertState struct {
	firing   bool      // currently above critical, already fired
	lastFire time.Time // when this host+metric last fired
}

// alertKey identifies a single host+metric alert slot.
type alertKey struct {
	host   string
	metric AlertMetric
}

// alertEvent describes a newly fired alert.
type alertEvent struct {
	Host   string
	Metric AlertMetric
	Value  float64
}

// alertTracker holds the alert state machine for every host+metric pair.
// It is shared across Model copies; Bubble Tea calls Update on one goroutine.
type alertTracker struct {
	cfg      config.AlertsConfig
	cooldown time.Duration
	now      func() time.Time
	states   map[alertKey]*alertState
}

// newAlertTracker builds a tracker from the alerts config. An invalid or unset
// cooldown falls back to the 60s default; validation already rejects malformed
// values on the config path, so this is just belt-and-braces for direct callers.
func newAlertTracker(cfg config.AlertsConfig) *alertTracker {
	cooldown := defaultAlertCooldown
	if cfg.Cooldown != "" {
		if parsed, err := time.ParseDuration(cfg.Cooldown); err == nil && parsed >= 0 {
			cooldown = parsed
		}
	}

	return &alertTracker{
		cfg:      cfg,
		cooldown: cooldown,
		now:      time.Now,
		states:   make(map[alertKey]*alertState),
	}
}

// Enabled reports whether alerting is turned on.
func (t *alertTracker) Enabled() bool {
	return t != nil && t.cfg.Enabled
}

// Flashing reports whether the host's card should render its alert border.
// Separate from Firing so `flash: false` keeps the header badge and the bell
// while leaving card borders alone.
func (t *alertTracker) Flashing(host string) bool {
	if !t.Enabled() || !t.cfg.Flash {
		return false
	}
	return t.Firing(host)
}

// Firing reports whether any metric on the host is currently alerting.
func (t *alertTracker) Firing(host string) bool {
	if !t.Enabled() {
		return false
	}
	for _, metric := range []AlertMetric{AlertMetricCPU, AlertMetricRAM, AlertMetricGPU} {
		if state, ok := t.states[alertKey{host: host, metric: metric}]; ok && state.firing {
			return true
		}
	}
	return false
}

// FiringCount returns the number of host+metric pairs currently alerting.
func (t *alertTracker) FiringCount() int {
	if !t.Enabled() {
		return 0
	}
	count := 0
	for _, state := range t.states {
		if state.firing {
			count++
		}
	}
	return count
}

// Clear drops all alert state for a host. Called when a host goes unreachable
// so its card stops flashing on stale data, and so recovery re-fires cleanly.
func (t *alertTracker) Clear(host string) {
	if t == nil {
		return
	}
	for _, metric := range []AlertMetric{AlertMetricCPU, AlertMetricRAM, AlertMetricGPU} {
		delete(t.states, alertKey{host: host, metric: metric})
	}
}

// Evaluate feeds one host's fresh metrics through the state machine and
// returns the alerts that newly fired on this sample.
func (t *alertTracker) Evaluate(host string, metrics *HostMetrics, thresholds config.ThresholdConfig) []alertEvent {
	if !t.Enabled() || metrics == nil {
		return nil
	}

	var events []alertEvent

	// CPU: skip samples the collector flagged as having no delta baseline,
	// where Percent misleadingly reads 0.
	if metrics.CPU.Valid() {
		if ev, ok := t.evaluateMetric(host, AlertMetricCPU, metrics.CPU.Percent, thresholds.CPU); ok {
			events = append(events, ev)
		}
	}

	if metrics.RAM.TotalBytes > 0 {
		ramPercent := float64(metrics.RAM.UsedBytes) / float64(metrics.RAM.TotalBytes) * 100
		if ev, ok := t.evaluateMetric(host, AlertMetricRAM, ramPercent, thresholds.RAM); ok {
			events = append(events, ev)
		}
	}

	if metrics.GPU != nil {
		if ev, ok := t.evaluateMetric(host, AlertMetricGPU, metrics.GPU.Percent, thresholds.GPU); ok {
			events = append(events, ev)
		}
	}

	return events
}

// evaluateMetric runs the crossing/hysteresis/cooldown logic for one
// host+metric and reports whether this sample fired a new alert.
func (t *alertTracker) evaluateMetric(host string, metric AlertMetric, value float64, thresh config.ThresholdValues) (alertEvent, bool) {
	key := alertKey{host: host, metric: metric}
	state, ok := t.states[key]
	if !ok {
		state = &alertState{}
		t.states[key] = state
	}

	critical := float64(thresh.Critical)
	warning := float64(thresh.Warning)

	// Below warning: re-arm. The metric has recovered far enough that the next
	// critical crossing counts as a new alert.
	if value < warning {
		state.firing = false
		return alertEvent{}, false
	}

	// Between warning and critical: hold whatever state we're in. A firing
	// metric stays firing (no re-fire), an armed metric stays armed.
	if value < critical {
		return alertEvent{}, false
	}

	// At or above critical. Only a metric that isn't already firing can fire.
	if state.firing {
		return alertEvent{}, false
	}

	// Cooldown: suppress the fire (and the effects) but still mark it firing so
	// the card keeps flashing and hysteresis stays consistent.
	now := t.now()
	if !state.lastFire.IsZero() && now.Sub(state.lastFire) < t.cooldown {
		state.firing = true
		return alertEvent{}, false
	}

	state.firing = true
	state.lastFire = now

	return alertEvent{Host: host, Metric: metric, Value: value}, true
}

// alertEffectsCmd builds the side effects for newly fired alerts: the terminal
// bell and the on_alert hook. Both are gated on config and run outside View so
// Bubble Tea's frame diffing never swallows or repeats them.
func (t *alertTracker) alertEffectsCmd(events []alertEvent) tea.Cmd {
	if !t.Enabled() || len(events) == 0 {
		return nil
	}

	var cmds []tea.Cmd

	// One bell per batch, no matter how many metrics crossed at once.
	if t.cfg.Bell {
		cmds = append(cmds, bellCmd)
	}

	if t.cfg.OnAlert != "" {
		for _, ev := range events {
			cmds = append(cmds, onAlertCmd(t.cfg.OnAlert, ev))
		}
	}

	if len(cmds) == 0 {
		return nil
	}

	return tea.Batch(cmds...)
}

// bellCmd writes BEL straight to stderr. It must not go through View: Bubble
// Tea diffs frames, so a bell embedded in rendered output would be dropped on
// unchanged frames and repeated on changed ones.
func bellCmd() tea.Msg {
	_, _ = os.Stderr.WriteString("\a")
	return nil
}

// onAlertCmd runs the configured hook locally without blocking the TUI.
// Hook failures are intentionally swallowed: a broken hook must never take
// down the dashboard, and there's no safe place to print inside the alt screen.
func onAlertCmd(command string, ev alertEvent) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), onAlertTimeout)
		defer cancel()

		// The command string is user-authored config (monitor.alerts.on_alert in
		// the project-local .rr.yaml), same trust boundary as tasks and
		// setup_commands. Event values go through cmd.Env, never into the string,
		// so remote host data cannot inject into the shell.
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Env = append(os.Environ(),
			"RR_HOST="+ev.Host,
			"RR_METRIC="+string(ev.Metric),
			"RR_VALUE="+strconv.FormatFloat(ev.Value, 'f', 1, 64),
		)
		_ = cmd.Run()
		return nil
	}
}
