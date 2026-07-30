package cli

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterHosts(t *testing.T) {
	allHosts := map[string]config.Host{
		"mini":        {SSH: []string{"user@mini"}},
		"workstation": {SSH: []string{"user@workstation"}},
		"gpu-box":     {SSH: []string{"user@gpu-box"}},
	}

	tests := []struct {
		name     string
		filter   string
		expected []string
	}{
		{
			name:     "empty filter returns all hosts",
			filter:   "",
			expected: []string{"mini", "workstation", "gpu-box"},
		},
		{
			name:     "single host",
			filter:   "mini",
			expected: []string{"mini"},
		},
		{
			name:     "multiple hosts",
			filter:   "mini,gpu-box",
			expected: []string{"mini", "gpu-box"},
		},
		{
			name:     "whitespace around names is trimmed",
			filter:   " mini , gpu-box ",
			expected: []string{"mini", "gpu-box"},
		},
		{
			name:     "unknown host matches nothing",
			filter:   "nope",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterHosts(allHosts, tt.filter)
			assert.Len(t, result, len(tt.expected))
			for _, name := range tt.expected {
				assert.Contains(t, result, name)
			}
		})
	}
}

func TestFilterHostOrder(t *testing.T) {
	hosts := map[string]config.Host{
		"mini":    {SSH: []string{"user@mini"}},
		"gpu-box": {SSH: []string{"user@gpu-box"}},
	}

	tests := []struct {
		name     string
		order    []string
		expected []string
	}{
		{
			name:     "keeps only hosts in the map, preserving order",
			order:    []string{"workstation", "gpu-box", "mini"},
			expected: []string{"gpu-box", "mini"},
		},
		{
			name:     "empty order returns nil",
			order:    nil,
			expected: nil,
		},
		{
			name:     "no matches returns nil",
			order:    []string{"a", "b"},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, filterHostOrder(tt.order, hosts))
		})
	}
}

func TestExcludeHosts(t *testing.T) {
	allHosts := map[string]config.Host{
		"mini":        {SSH: []string{"user@mini"}},
		"workstation": {SSH: []string{"user@workstation"}},
		"slow-host":   {SSH: []string{"user@slow-host"}},
	}

	tests := []struct {
		name        string
		exclude     []string
		hostsFilter string
		expected    []string
	}{
		{
			name:        "empty exclude keeps all hosts",
			exclude:     nil,
			hostsFilter: "",
			expected:    []string{"mini", "workstation", "slow-host"},
		},
		{
			name:        "excluded host is removed",
			exclude:     []string{"slow-host"},
			hostsFilter: "",
			expected:    []string{"mini", "workstation"},
		},
		{
			name:        "multiple excludes",
			exclude:     []string{"slow-host", "workstation"},
			hostsFilter: "",
			expected:    []string{"mini"},
		},
		{
			name:        "explicit --hosts request wins over exclude",
			exclude:     []string{"slow-host"},
			hostsFilter: "slow-host",
			expected:    []string{"mini", "workstation", "slow-host"},
		},
		{
			name:        "exclude still applies to hosts not explicitly requested",
			exclude:     []string{"slow-host", "workstation"},
			hostsFilter: "slow-host,mini",
			expected:    []string{"mini", "slow-host"},
		},
		{
			name:        "whitespace in exclude entries is trimmed",
			exclude:     []string{" slow-host "},
			hostsFilter: "",
			expected:    []string{"mini", "workstation"},
		},
		{
			name:        "excluding unknown host is a no-op",
			exclude:     []string{"nope"},
			hostsFilter: "",
			expected:    []string{"mini", "workstation", "slow-host"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := excludeHosts(allHosts, tt.exclude, tt.hostsFilter)
			assert.Len(t, result, len(tt.expected))
			for _, name := range tt.expected {
				assert.Contains(t, result, name)
			}
		})
	}
}

func TestResolveMonitorInterval(t *testing.T) {
	projectWith := func(interval string) *config.Config {
		cfg := config.DefaultConfig()
		cfg.Monitor.Interval = interval
		return cfg
	}

	tests := []struct {
		name         string
		flagInterval time.Duration
		flagSet      bool
		project      *config.Config
		expected     time.Duration
		wantErr      bool
	}{
		{
			name:         "explicit flag wins over config",
			flagInterval: 5 * time.Second,
			flagSet:      true,
			project:      projectWith("3s"),
			expected:     5 * time.Second,
		},
		{
			name:     "config interval used when flag not set",
			project:  projectWith("3s"),
			expected: 3 * time.Second,
		},
		{
			name:     "defaults to 1s with nil project",
			project:  nil,
			expected: time.Second,
		},
		{
			name:     "defaults to 1s with empty config interval",
			project:  projectWith(""),
			expected: time.Second,
		},
		{
			name:    "invalid config interval errors",
			project: projectWith("banana"),
			wantErr: true,
		},
		{
			name:    "config interval below 500ms errors",
			project: projectWith("100ms"),
			wantErr: true,
		},
		{
			name:     "config interval at 500ms is allowed",
			project:  projectWith("500ms"),
			expected: 500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolveMonitorInterval(tt.flagInterval, tt.flagSet, tt.project)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
