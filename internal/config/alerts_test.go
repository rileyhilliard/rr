package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig_Alerts(t *testing.T) {
	alerts := DefaultConfig().Monitor.Alerts

	assert.False(t, alerts.Enabled, "alerting is opt-in")
	assert.True(t, alerts.Bell, "bell defaults on so enabling alerts is useful out of the box")
	assert.True(t, alerts.Flash, "flash defaults on")
	assert.Equal(t, "60s", alerts.Cooldown)
	assert.Empty(t, alerts.OnAlert)

	cooldown, err := time.ParseDuration(alerts.Cooldown)
	require.NoError(t, err, "the default cooldown must parse")
	assert.Equal(t, time.Minute, cooldown)
}

func TestValidateAlerts(t *testing.T) {
	tests := []struct {
		name    string
		alerts  AlertsConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:   "empty cooldown is allowed",
			alerts: AlertsConfig{Enabled: true},
		},
		{
			name:   "valid cooldown",
			alerts: AlertsConfig{Enabled: true, Cooldown: "90s"},
		},
		{
			name:   "zero cooldown is allowed",
			alerts: AlertsConfig{Enabled: true, Cooldown: "0s"},
		},
		{
			name:   "free-form on_alert is accepted",
			alerts: AlertsConfig{Enabled: true, Cooldown: "60s", OnAlert: "notify-send \"$RR_HOST\" | tee /tmp/x"},
		},
		{
			name:    "malformed cooldown",
			alerts:  AlertsConfig{Enabled: true, Cooldown: "60 seconds"},
			wantErr: true,
			errMsg:  "doesn't look like a valid duration",
		},
		{
			name:    "bare number cooldown",
			alerts:  AlertsConfig{Enabled: true, Cooldown: "60"},
			wantErr: true,
			errMsg:  "doesn't look like a valid duration",
		},
		{
			name:    "negative cooldown",
			alerts:  AlertsConfig{Enabled: true, Cooldown: "-30s"},
			wantErr: true,
			errMsg:  "can't be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAlerts(tt.alerts)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// TestValidateMonitorConfig_RejectsBadAlerts proves alert validation is
// actually reached from the monitor config path, not just defined.
func TestValidateMonitorConfig_RejectsBadAlerts(t *testing.T) {
	cfg := MonitorConfig{
		Interval: "2s",
		Alerts:   AlertsConfig{Enabled: true, Cooldown: "banana"},
	}

	err := validateMonitorConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "monitor.alerts.cooldown")
}

// TestLoad_AlertsFromYAML checks the whole config path end to end: the block
// parses, explicit values win, and the true-by-default flags survive a load
// that doesn't mention them.
func TestLoad_AlertsFromYAML(t *testing.T) {
	t.Run("explicit values are loaded", func(t *testing.T) {
		cfg := loadAlertConfig(t, `
version: 1
monitor:
  alerts:
    enabled: true
    bell: false
    flash: false
    cooldown: "5m"
    on_alert: "echo $RR_HOST >> /tmp/alerts.log"
`)

		assert.True(t, cfg.Monitor.Alerts.Enabled)
		assert.False(t, cfg.Monitor.Alerts.Bell, "an explicit false must not be overwritten by the default")
		assert.False(t, cfg.Monitor.Alerts.Flash)
		assert.Equal(t, "5m", cfg.Monitor.Alerts.Cooldown)
		assert.Equal(t, "echo $RR_HOST >> /tmp/alerts.log", cfg.Monitor.Alerts.OnAlert)
	})

	t.Run("enabling alerts keeps the bell and flash defaults", func(t *testing.T) {
		cfg := loadAlertConfig(t, `
version: 1
monitor:
  alerts:
    enabled: true
`)

		assert.True(t, cfg.Monitor.Alerts.Enabled)
		assert.True(t, cfg.Monitor.Alerts.Bell)
		assert.True(t, cfg.Monitor.Alerts.Flash)
		assert.Equal(t, "60s", cfg.Monitor.Alerts.Cooldown)
	})

	t.Run("config without an alerts block stays disabled", func(t *testing.T) {
		cfg := loadAlertConfig(t, `
version: 1
monitor:
  interval: "2s"
`)

		assert.False(t, cfg.Monitor.Alerts.Enabled)
	})

	// Load only parses; Validate is the gate every command runs through
	// (cli/root.go, ValidateResolved). A bad cooldown must be rejected there.
	t.Run("a bad cooldown is rejected by Validate", func(t *testing.T) {
		cfg := loadAlertConfig(t, `
version: 1
monitor:
  alerts:
    enabled: true
    cooldown: "not-a-duration"
`)

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "monitor.alerts.cooldown")
	})

	t.Run("a valid alerts block passes Validate", func(t *testing.T) {
		cfg := loadAlertConfig(t, `
version: 1
monitor:
  alerts:
    enabled: true
    cooldown: "2m"
    on_alert: "say alert"
`)

		assert.NoError(t, Validate(cfg))
	})
}

func loadAlertConfig(t *testing.T, yaml string) *Config {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, ".rr.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	return cfg
}
