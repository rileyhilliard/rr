package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/monitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorConfigParsing(t *testing.T) {
	t.Run("default monitor config", func(t *testing.T) {
		cfg := config.DefaultConfig()

		assert.Equal(t, "1s", cfg.Monitor.Interval)
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
  - test-host
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
  - test-host
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
	// Exercise the monitor coloring code with configured thresholds and assert
	// severity-class transitions (healthy -> warning -> critical) rather than
	// exact colors, so palette changes don't churn this test.
	t.Run("class transitions follow configured boundaries", func(t *testing.T) {
		thresholds := config.ThresholdValues{Warning: 60, Critical: 85}

		healthy := monitor.MetricColorWithThresholds(0, thresholds.Warning, thresholds.Critical)

		// Just below warning is still the healthy class
		assert.Equal(t, healthy, monitor.MetricColorWithThresholds(59.9, thresholds.Warning, thresholds.Critical))

		// At warning the class changes
		warning := monitor.MetricColorWithThresholds(60, thresholds.Warning, thresholds.Critical)
		assert.NotEqual(t, healthy, warning)

		// Just below critical is still the warning class
		assert.Equal(t, warning, monitor.MetricColorWithThresholds(84.9, thresholds.Warning, thresholds.Critical))

		// At critical the class changes again
		critical := monitor.MetricColorWithThresholds(85, thresholds.Warning, thresholds.Critical)
		assert.NotEqual(t, warning, critical)
		assert.NotEqual(t, healthy, critical)
	})

	t.Run("same value classifies differently under different thresholds", func(t *testing.T) {
		// 75% is warning-class with defaults (70/90) but healthy-class with
		// relaxed thresholds (80/95)
		defaultClass := monitor.MetricColorWithThresholds(75, 70, 90)
		relaxedClass := monitor.MetricColorWithThresholds(75, 80, 95)
		assert.NotEqual(t, defaultClass, relaxedClass)

		// The relaxed classification matches the healthy anchor
		healthy := monitor.MetricColorWithThresholds(0, 80, 95)
		assert.Equal(t, healthy, relaxedClass)
	})
}
