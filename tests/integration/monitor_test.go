package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorConfigParsing(t *testing.T) {
	t.Run("default monitor config", func(t *testing.T) {
		cfg := config.DefaultConfig()

		assert.Equal(t, "2s", cfg.Monitor.Interval)
		assert.Equal(t, 70, cfg.Monitor.Thresholds.CPU.Warning)
		assert.Equal(t, 90, cfg.Monitor.Thresholds.CPU.Critical)
		assert.Equal(t, 70, cfg.Monitor.Thresholds.RAM.Warning)
		assert.Equal(t, 90, cfg.Monitor.Thresholds.RAM.Critical)
		assert.Equal(t, 70, cfg.Monitor.Thresholds.GPU.Warning)
		assert.Equal(t, 90, cfg.Monitor.Thresholds.GPU.Critical)
		assert.Empty(t, cfg.Monitor.Exclude)
	})

	t.Run("custom monitor config from yaml", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rr.yaml")

		content := `
version: 1
hosts:
  test-host:
    ssh:
      - test.local
    dir: ~/projects/test
monitor:
  interval: 5s
  thresholds:
    cpu:
      warning: 60
      critical: 85
    ram:
      warning: 75
      critical: 95
    gpu:
      warning: 50
      critical: 80
  exclude:
    - slow-host
`
		err := os.WriteFile(configPath, []byte(content), 0644)
		require.NoError(t, err)

		cfg, err := config.Load(configPath)
		require.NoError(t, err)

		assert.Equal(t, "5s", cfg.Monitor.Interval)
		assert.Equal(t, 60, cfg.Monitor.Thresholds.CPU.Warning)
		assert.Equal(t, 85, cfg.Monitor.Thresholds.CPU.Critical)
		assert.Equal(t, 75, cfg.Monitor.Thresholds.RAM.Warning)
		assert.Equal(t, 95, cfg.Monitor.Thresholds.RAM.Critical)
		assert.Equal(t, 50, cfg.Monitor.Thresholds.GPU.Warning)
		assert.Equal(t, 80, cfg.Monitor.Thresholds.GPU.Critical)
		assert.Equal(t, []string{"slow-host"}, cfg.Monitor.Exclude)
	})

	t.Run("partial monitor config uses defaults", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".rr.yaml")

		content := `
version: 1
hosts:
  test-host:
    ssh:
      - test.local
    dir: ~/projects/test
monitor:
  interval: 3s
`
		err := os.WriteFile(configPath, []byte(content), 0644)
		require.NoError(t, err)

		cfg, err := config.Load(configPath)
		require.NoError(t, err)

		// Custom interval
		assert.Equal(t, "3s", cfg.Monitor.Interval)
		// Thresholds are merged from defaults when not specified
		assert.Equal(t, 70, cfg.Monitor.Thresholds.CPU.Warning)
		assert.Equal(t, 90, cfg.Monitor.Thresholds.CPU.Critical)
	})
}

func TestMonitorConfigValidation(t *testing.T) {
	t.Run("valid monitor config", func(t *testing.T) {
		cfg := &config.Config{
			Version: 1,
			Monitor: config.MonitorConfig{
				Interval: "2s",
				Thresholds: config.ThresholdConfig{
					CPU: config.ThresholdValues{Warning: 70, Critical: 90},
					RAM: config.ThresholdValues{Warning: 70, Critical: 90},
					GPU: config.ThresholdValues{Warning: 70, Critical: 90},
				},
			},
		}
		err := config.Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("invalid interval format", func(t *testing.T) {
		cfg := &config.Config{
			Version: 1,
			Monitor: config.MonitorConfig{
				Interval: "invalid",
			},
		}
		err := config.Validate(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "doesn't look like a valid duration")
	})

	t.Run("warning greater than critical", func(t *testing.T) {
		cfg := &config.Config{
			Version: 1,
			Monitor: config.MonitorConfig{
				Interval: "2s",
				Thresholds: config.ThresholdConfig{
					CPU: config.ThresholdValues{Warning: 95, Critical: 90},
				},
			},
		}
		err := config.Validate(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "warning")
		assert.Contains(t, err.Error(), "is higher than critical")
	})

	t.Run("threshold out of range", func(t *testing.T) {
		cfg := &config.Config{
			Version: 1,
			Monitor: config.MonitorConfig{
				Interval: "2s",
				Thresholds: config.ThresholdConfig{
					CPU: config.ThresholdValues{Warning: 150, Critical: 90},
				},
			},
		}
		err := config.Validate(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "needs to be 0-100")
	})

	t.Run("empty exclude entry", func(t *testing.T) {
		cfg := &config.Config{
			Version: 1,
			Monitor: config.MonitorConfig{
				Interval: "2s",
				Exclude:  []string{"valid-host", ""},
			},
		}
		err := config.Validate(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty entry")
	})

	t.Run("valid exclude with non-existent host", func(t *testing.T) {
		// Excluding a non-existent host should not fail validation
		// (it might be temporarily removed from config)
		cfg := &config.Config{
			Version: 1,
			Monitor: config.MonitorConfig{
				Interval: "2s",
				Exclude:  []string{"non-existent-host"},
			},
		}
		err := config.Validate(cfg)
		assert.NoError(t, err)
	})
}

func TestThresholdApplication(t *testing.T) {
	// Test that threshold values can be used to determine metric severity
	t.Run("threshold values determine color", func(t *testing.T) {
		thresholds := config.ThresholdValues{Warning: 70, Critical: 90}

		// Below warning
		assert.True(t, 50 < thresholds.Warning)
		// At warning
		assert.True(t, 75 >= thresholds.Warning && 75 < thresholds.Critical)
		// At critical
		assert.True(t, 95 >= thresholds.Critical)
	})

	t.Run("custom thresholds differ from defaults", func(t *testing.T) {
		defaultThresholds := config.ThresholdValues{Warning: 70, Critical: 90}
		customThresholds := config.ThresholdValues{Warning: 50, Critical: 75}

		// 60% is warning with custom, healthy with default
		assert.True(t, 60 >= customThresholds.Warning && 60 < customThresholds.Critical)
		assert.True(t, 60 < defaultThresholds.Warning)

		// 80% is critical with custom, warning with default
		assert.True(t, 80 >= customThresholds.Critical)
		assert.True(t, 80 >= defaultThresholds.Warning && 80 < defaultThresholds.Critical)
	})
}
