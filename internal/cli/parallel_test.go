package cli

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestFilterHostsByTag(t *testing.T) {
	tests := []struct {
		name      string
		hosts     map[string]config.Host
		hostOrder []string
		tag       string
		wantHosts []string
		wantOrder []string
	}{
		{
			name: "filters to matching tag",
			hosts: map[string]config.Host{
				"gpu-box":    {Tags: []string{"gpu", "linux"}},
				"cpu-server": {Tags: []string{"linux"}},
				"mac-mini":   {Tags: []string{"macos"}},
			},
			hostOrder: []string{"gpu-box", "cpu-server", "mac-mini"},
			tag:       "linux",
			wantHosts: []string{"gpu-box", "cpu-server"},
			wantOrder: []string{"gpu-box", "cpu-server"},
		},
		{
			name: "preserves order",
			hosts: map[string]config.Host{
				"host-a": {Tags: []string{"fast"}},
				"host-b": {Tags: []string{"fast"}},
				"host-c": {Tags: []string{"fast"}},
			},
			hostOrder: []string{"host-c", "host-a", "host-b"},
			tag:       "fast",
			wantHosts: []string{"host-c", "host-a", "host-b"},
			wantOrder: []string{"host-c", "host-a", "host-b"},
		},
		{
			name: "no matches returns empty",
			hosts: map[string]config.Host{
				"host-a": {Tags: []string{"linux"}},
				"host-b": {Tags: []string{"macos"}},
			},
			hostOrder: []string{"host-a", "host-b"},
			tag:       "windows",
			wantHosts: []string{},
			wantOrder: []string{},
		},
		{
			name:      "empty hosts returns empty",
			hosts:     map[string]config.Host{},
			hostOrder: []string{},
			tag:       "any",
			wantHosts: []string{},
			wantOrder: []string{},
		},
		{
			name: "host in order but not in map is skipped",
			hosts: map[string]config.Host{
				"existing": {Tags: []string{"target"}},
			},
			hostOrder: []string{"missing", "existing"},
			tag:       "target",
			wantHosts: []string{"existing"},
			wantOrder: []string{"existing"},
		},
		{
			name: "host with no tags is not matched",
			hosts: map[string]config.Host{
				"tagged":   {Tags: []string{"target"}},
				"untagged": {Tags: nil},
			},
			hostOrder: []string{"tagged", "untagged"},
			tag:       "target",
			wantHosts: []string{"tagged"},
			wantOrder: []string{"tagged"},
		},
		{
			name: "single host with matching tag",
			hosts: map[string]config.Host{
				"only-one": {Tags: []string{"special"}},
			},
			hostOrder: []string{"only-one"},
			tag:       "special",
			wantHosts: []string{"only-one"},
			wantOrder: []string{"only-one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHosts, gotOrder := filterHostsByTag(tt.hosts, tt.hostOrder, tt.tag)

			// Check host count
			assert.Len(t, gotHosts, len(tt.wantHosts))
			assert.Equal(t, tt.wantOrder, gotOrder)

			// Verify all expected hosts are present
			for _, name := range tt.wantHosts {
				_, ok := gotHosts[name]
				assert.True(t, ok, "expected host %s to be in filtered result", name)
			}
		})
	}
}

func TestFilterHostsByTag_PreservesHostData(t *testing.T) {
	// Verify that the filtered hosts retain their original data
	hosts := map[string]config.Host{
		"test-host": {
			SSH:  []string{"test.local", "test.vpn"},
			Dir:  "/home/user/project",
			Tags: []string{"target", "other"},
			Env: map[string]string{
				"KEY": "value",
			},
		},
	}
	hostOrder := []string{"test-host"}

	filtered, order := filterHostsByTag(hosts, hostOrder, "target")

	assert.Len(t, filtered, 1)
	assert.Equal(t, []string{"test-host"}, order)

	host := filtered["test-host"]
	assert.Equal(t, []string{"test.local", "test.vpn"}, host.SSH)
	assert.Equal(t, "/home/user/project", host.Dir)
	assert.Equal(t, []string{"target", "other"}, host.Tags)
	assert.Equal(t, "value", host.Env["KEY"])
}
