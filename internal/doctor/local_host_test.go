package doctor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
)

func TestHostConnectivityCheck_LocalHost(t *testing.T) {
	check := &HostConnectivityCheck{
		HostName:   "dev",
		HostConfig: config.Host{Local: true},
		Probe: func([]string, time.Duration) []host.ProbeResult {
			t.Fatal("a local host is never probed over SSH")
			return nil
		},
	}
	result := check.Run()
	assert.Equal(t, StatusPass, result.Status)
	assert.Contains(t, result.Message, "dev")
	assert.Contains(t, result.Message, "this machine")
	require.Len(t, check.Results, 1)
	assert.True(t, check.Results[0].Success)
}

func TestNewRemoteDepsChecks_LocalHostSkipsRsync(t *testing.T) {
	conn := &host.Connection{Name: "dev", Host: config.Host{Local: true}}
	assert.Empty(t, NewRemoteDepsChecks("dev", conn), "nothing is synced to a local host")
}

func TestWorktreeMappingCheck_LocalHost(t *testing.T) {
	check := &WorktreeMappingCheck{Hosts: map[string]config.Host{
		"dev":  {Local: true, Dir: "/work/proj"},
		"mini": {SSH: []string{"mini"}, Dir: "/srv/rr/proj"},
	}}
	result := check.Run()
	assert.Regexp(t, `syncs to mini:/srv/rr/proj and runs in place on dev$`, result.Message)

	check.Hosts = map[string]config.Host{"dev": {Local: true}}
	result = check.Run()
	assert.Regexp(t, `runs in place on dev$`, result.Message)
	assert.NotContains(t, result.Message, "syncs to")
}
