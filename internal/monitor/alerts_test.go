package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testThresholds is the 70/90 warning/critical pair used across alert tests.
func testThresholds() config.ThresholdConfig {
	return config.ThresholdConfig{
		CPU: config.ThresholdValues{Warning: 70, Critical: 90},
		RAM: config.ThresholdValues{Warning: 70, Critical: 90},
		GPU: config.ThresholdValues{Warning: 70, Critical: 90},
	}
}

// enabledAlerts returns an alerts config with effects off, so state-machine
// tests don't ring bells or shell out.
func enabledAlerts() config.AlertsConfig {
	return config.AlertsConfig{Enabled: true, Cooldown: "60s"}
}

// cpuMetrics builds a valid (non-first-sample) metrics payload at the given
// CPU percentage.
func cpuMetrics(percent float64) *HostMetrics {
	return &HostMetrics{CPU: CPUMetrics{Percent: percent, Cores: 8}}
}

// ramMetrics builds a metrics payload whose RAM sits at the given percentage.
func ramMetrics(percent float64) *HostMetrics {
	const total = 1000
	return &HostMetrics{
		CPU: CPUMetrics{Percent: 1},
		RAM: RAMMetrics{TotalBytes: total, UsedBytes: int64(percent * total / 100)},
	}
}

// newTestTracker builds a tracker with a controllable clock.
func newTestTracker(cfg config.AlertsConfig, clock *time.Time) *alertTracker {
	t := newAlertTracker(cfg)
	t.now = func() time.Time { return *clock }
	return t
}

func TestAlertTracker_StateMachine(t *testing.T) {
	thresholds := testThresholds()

	t.Run("fires when the metric crosses critical", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)

		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(50), thresholds))
		assert.False(t, tr.Firing("h1"))

		events := tr.Evaluate("h1", cpuMetrics(93.4), thresholds)
		require.Len(t, events, 1)
		assert.Equal(t, "h1", events[0].Host)
		assert.Equal(t, AlertMetricCPU, events[0].Metric)
		assert.InDelta(t, 93.4, events[0].Value, 0.001)
		assert.True(t, tr.Firing("h1"))
	})

	t.Run("does not re-fire while the metric stays above warning", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)

		require.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)

		// Still critical: no repeat.
		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(97), thresholds))

		// Dipped below critical but still above warning: not re-armed, and
		// crossing critical again must not fire.
		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(85), thresholds))
		assert.True(t, tr.Firing("h1"), "stays firing between warning and critical")

		now = now.Add(time.Hour) // past any cooldown
		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(95), thresholds),
			"re-crossing critical without dropping below warning must not fire")
	})

	t.Run("re-arms once the metric drops below warning", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)

		require.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)

		// Drop below warning re-arms and stops the flash.
		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(69), thresholds))
		assert.False(t, tr.Firing("h1"))

		// Past the cooldown, a fresh critical crossing fires again.
		now = now.Add(2 * time.Minute)
		assert.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
	})

	t.Run("cooldown suppresses a re-armed re-fire", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)

		require.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)

		// Re-arm, then cross critical again well inside the 60s cooldown.
		require.Empty(t, tr.Evaluate("h1", cpuMetrics(10), thresholds))
		now = now.Add(30 * time.Second)
		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(95), thresholds),
			"re-fire inside the cooldown window is suppressed")
		assert.True(t, tr.Firing("h1"), "suppressed alert still flashes the card")

		// Re-arm again and wait out the cooldown.
		require.Empty(t, tr.Evaluate("h1", cpuMetrics(10), thresholds))
		now = now.Add(61 * time.Second)
		assert.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
	})

	t.Run("zero cooldown re-fires on every re-armed crossing", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(config.AlertsConfig{Enabled: true, Cooldown: "0s"}, &now)

		require.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
		require.Empty(t, tr.Evaluate("h1", cpuMetrics(10), thresholds))
		assert.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
	})

	t.Run("host and metric slots are independent", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)

		// h1 CPU fires; h2 is untouched.
		require.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
		assert.True(t, tr.Firing("h1"))
		assert.False(t, tr.Firing("h2"))

		// h2 crossing fires on its own slot despite h1's cooldown.
		assert.Len(t, tr.Evaluate("h2", cpuMetrics(95), thresholds), 1)

		// RAM on h1 is a separate slot from the already-firing CPU.
		ram := ramMetrics(95)
		ram.CPU.Percent = 95 // CPU already firing, must not repeat
		events := tr.Evaluate("h1", ram, thresholds)
		require.Len(t, events, 1)
		assert.Equal(t, AlertMetricRAM, events[0].Metric)
		assert.Equal(t, 3, tr.FiringCount(), "h1 cpu + h1 ram + h2 cpu")
	})

	t.Run("GPU fires only when a GPU is present", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)

		// No GPU on the payload: nothing to evaluate.
		require.Empty(t, tr.Evaluate("h1", cpuMetrics(10), thresholds))

		withGPU := cpuMetrics(10)
		withGPU.GPU = &GPUMetrics{Name: "test", Percent: 99}
		events := tr.Evaluate("h1", withGPU, thresholds)
		require.Len(t, events, 1)
		assert.Equal(t, AlertMetricGPU, events[0].Metric)
	})

	t.Run("invalid first CPU sample is skipped", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)

		// A first Linux sample reads 0% but is meaningless; a 95% one is too.
		warming := &HostMetrics{CPU: CPUMetrics{Percent: 95, FirstSample: true}}
		assert.Empty(t, tr.Evaluate("h1", warming, thresholds))
		assert.False(t, tr.Firing("h1"))

		// The next valid sample is evaluated normally.
		assert.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
	})

	t.Run("custom thresholds drive the crossings", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)
		custom := config.ThresholdConfig{
			CPU: config.ThresholdValues{Warning: 30, Critical: 50},
			RAM: config.ThresholdValues{Warning: 70, Critical: 90},
			GPU: config.ThresholdValues{Warning: 70, Critical: 90},
		}

		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(49), custom))
		assert.Len(t, tr.Evaluate("h1", cpuMetrics(51), custom), 1)

		// Re-arm boundary now sits at 30, not 70.
		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(40), custom))
		assert.True(t, tr.Firing("h1"))
		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(20), custom))
		assert.False(t, tr.Firing("h1"))
	})

	t.Run("disabled tracker never fires", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(config.AlertsConfig{Enabled: false}, &now)

		assert.Empty(t, tr.Evaluate("h1", cpuMetrics(99), thresholds))
		assert.False(t, tr.Firing("h1"))
		assert.Zero(t, tr.FiringCount())
	})

	t.Run("Clear drops all of a host's state", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(enabledAlerts(), &now)

		require.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
		require.Len(t, tr.Evaluate("h2", cpuMetrics(95), thresholds), 1)

		tr.Clear("h1")
		assert.False(t, tr.Firing("h1"))
		assert.True(t, tr.Firing("h2"), "clearing one host leaves others alone")

		// Cooldown is cleared too, so the host re-fires immediately on recovery.
		assert.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
	})
}

// TestAlertTracker_NilSafe covers the zero-value Model, which has no tracker.
// Every alert entry point must tolerate it rather than panicking mid-render.
func TestAlertTracker_NilSafe(t *testing.T) {
	var m Model // zero value: m.alerts is nil

	assert.False(t, m.AlertsEnabled())
	assert.Zero(t, m.AlertCount())
	assert.False(t, m.IsAlerting("h1"))
	assert.False(t, m.IsFlashing("h1"))

	assert.NotPanics(t, func() {
		m.alerts.Clear("h1")
		assert.Nil(t, m.alerts.Evaluate("h1", cpuMetrics(99), testThresholds()))
		assert.Nil(t, m.alerts.alertEffectsCmd([]alertEvent{{Host: "h1"}}))
		_ = m.cardStyle("h1", 40, true)
	})
}

func TestAlertTracker_Flashing(t *testing.T) {
	thresholds := testThresholds()
	now := time.Now()

	t.Run("flash on renders the alert border", func(t *testing.T) {
		tr := newTestTracker(config.AlertsConfig{Enabled: true, Flash: true}, &now)
		require.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
		assert.True(t, tr.Flashing("h1"))
	})

	t.Run("flash off keeps the alert but not the border", func(t *testing.T) {
		tr := newTestTracker(config.AlertsConfig{Enabled: true, Flash: false}, &now)
		require.Len(t, tr.Evaluate("h1", cpuMetrics(95), thresholds), 1)
		assert.True(t, tr.Firing("h1"), "alert still counts for the header badge")
		assert.False(t, tr.Flashing("h1"))
	})
}

func TestAlertTracker_CooldownParsing(t *testing.T) {
	tests := []struct {
		name     string
		cooldown string
		expect   time.Duration
	}{
		{"empty falls back to default", "", defaultAlertCooldown},
		{"invalid falls back to default", "not-a-duration", defaultAlertCooldown},
		{"negative falls back to default", "-5s", defaultAlertCooldown},
		{"explicit zero is honored", "0s", 0},
		{"configured value is used", "5m", 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newAlertTracker(config.AlertsConfig{Enabled: true, Cooldown: tt.cooldown})
			assert.Equal(t, tt.expect, tr.cooldown)
		})
	}
}

func TestAlertTracker_EffectsCmd(t *testing.T) {
	events := []alertEvent{{Host: "h1", Metric: AlertMetricCPU, Value: 95}}

	t.Run("no command without events", func(t *testing.T) {
		tr := newAlertTracker(config.AlertsConfig{Enabled: true, Bell: true})
		assert.Nil(t, tr.alertEffectsCmd(nil))
	})

	t.Run("no command when disabled", func(t *testing.T) {
		tr := newAlertTracker(config.AlertsConfig{Enabled: false, Bell: true})
		assert.Nil(t, tr.alertEffectsCmd(events))
	})

	t.Run("no command when bell and hook are both off", func(t *testing.T) {
		tr := newAlertTracker(config.AlertsConfig{Enabled: true, Bell: false})
		assert.Nil(t, tr.alertEffectsCmd(events))
	})

	t.Run("bell produces a command", func(t *testing.T) {
		tr := newAlertTracker(config.AlertsConfig{Enabled: true, Bell: true})
		assert.NotNil(t, tr.alertEffectsCmd(events))
	})

	t.Run("hook alone produces a command", func(t *testing.T) {
		tr := newAlertTracker(config.AlertsConfig{Enabled: true, Bell: false, OnAlert: "true"})
		assert.NotNil(t, tr.alertEffectsCmd(events))
	})
}

func TestOnAlertCmd_RunsHookWithEnv(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "alert.txt")

	cmd := onAlertCmd(
		"printf '%s %s %s' \"$RR_HOST\" \"$RR_METRIC\" \"$RR_VALUE\" > "+out,
		alertEvent{Host: "server1", Metric: AlertMetricCPU, Value: 93.42},
	)
	require.NotNil(t, cmd)

	// tea would run this on its own goroutine; running it inline is equivalent
	// and keeps the test deterministic.
	assert.Nil(t, cmd())

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "server1 cpu 93.4", string(data))
}

func TestOnAlertCmd_SwallowsFailure(t *testing.T) {
	// A hook that doesn't exist must not panic or surface an error message:
	// a broken hook can't be allowed to take down the dashboard.
	cmd := onAlertCmd("this-command-does-not-exist-rr-test", alertEvent{Host: "h1", Metric: AlertMetricRAM})
	require.NotNil(t, cmd)
	assert.NotPanics(t, func() {
		assert.Nil(t, cmd())
	})
}
