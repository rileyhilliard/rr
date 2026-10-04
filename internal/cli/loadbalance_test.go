package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAllLockedAction(t *testing.T) {
	sameMachine := []lockHolderDetail{{Host: "m4-mini", Pid: 46822, Command: "rr test-backend", SameMachine: true}}
	remote := []lockHolderDetail{{Host: "m4-mini", Pid: 123, Command: "rr test", SameMachine: false}}
	mixed := []lockHolderDetail{
		{Host: "m4-mini", Pid: 123, SameMachine: false},
		{Host: "m1-linux", Pid: 456, SameMachine: true},
	}

	tests := []struct {
		name     string
		mode     config.LocalFallbackMode
		holders  []lockHolderDetail
		expected allLockedAction
	}{
		{"never + remote holder", config.LocalFallbackNever, remote, actionWaitThenError},
		{"never + same machine", config.LocalFallbackNever, sameMachine, actionWaitThenError},
		{"on-unreachable + remote holder", config.LocalFallbackOnUnreachable, remote, actionWaitThenError},
		{"on-unreachable + same machine", config.LocalFallbackOnUnreachable, sameMachine, actionWaitThenError},
		{"always + remote holder", config.LocalFallbackAlways, remote, actionFallbackImmediately},
		{"always + same machine", config.LocalFallbackAlways, sameMachine, actionWaitThenFallback},
		{"always + mixed holders", config.LocalFallbackAlways, mixed, actionWaitThenFallback},
		{"always + no holder info", config.LocalFallbackAlways, nil, actionFallbackImmediately},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, resolveAllLockedAction(tt.mode, tt.holders))
		})
	}
}

func TestHolderDetails(t *testing.T) {
	started := time.Now().Add(-5 * time.Minute)
	attempts := []hostAttempt{
		{
			hostName: "m4-mini",
			lockInfo: &lock.LockInfo{
				User:     "riley",
				Hostname: "some-other-box",
				Started:  started,
				PID:      46822,
				Command:  "rr test-backend",
			},
		},
		{hostName: "m1-linux"}, // no lock info readable
	}

	details := holderDetails(attempts)
	assert.Len(t, details, 2)

	assert.Equal(t, "m4-mini", details[0].Host)
	assert.Equal(t, "riley", details[0].User)
	assert.Equal(t, 46822, details[0].Pid)
	assert.Equal(t, "rr test-backend", details[0].Command)
	assert.InDelta(t, 300, details[0].AgeS, 5)
	assert.False(t, details[0].SameMachine)

	assert.Equal(t, "m1-linux", details[1].Host)
	assert.Zero(t, details[1].Pid)

	// In JSON, a known holder on another machine says so; an unreadable
	// holder leaves same_machine out rather than claiming false.
	raw, err := json.Marshal(details)
	require.NoError(t, err)
	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, false, decoded[0]["same_machine"])
	assert.NotContains(t, decoded[1], "same_machine")
	assert.Equal(t, "m1-linux", decoded[1]["host"])
}

func TestWaitMessage(t *testing.T) {
	sameMachine := []lockHolderDetail{{Host: "m4-mini", Pid: 46822, Command: "rr test-backend", SameMachine: true}}
	remote := []lockHolderDetail{{Host: "m4-mini", Pid: 123, SameMachine: false}}

	msg := waitMessage(sameMachine, time.Minute)
	assert.Contains(t, msg, "your own runs")
	assert.Contains(t, msg, "pid 46822: rr test-backend")
	assert.Contains(t, msg, "1m0s")

	msg = waitMessage(remote, time.Minute)
	assert.Contains(t, msg, "All hosts locked")
	assert.NotContains(t, msg, "your own runs")
}

func TestDescribeHolders(t *testing.T) {
	holders := []lockHolderDetail{
		{Host: "m4-mini", Pid: 46822, Command: "rr test-backend", SameMachine: true},
		{Host: "m1-linux", Pid: 99, SameMachine: false},
	}

	desc := describeHolders(holders)
	assert.Equal(t, "m4-mini: 'rr test-backend' (pid 46822, this machine); m1-linux (pid 99)", desc)
}

// A load-balanced fallback runs on the global config's local host when there
// is one, and bare otherwise; the warn event names where it went.
func TestFallBackLocally(t *testing.T) {
	withStructuredOutput(t)
	projectRoot := t.TempDir()
	remote := config.Host{SSH: []string{"m4-mini"}, SetupCommands: []string{"echo remote-only"}}

	tests := []struct {
		name      string
		hosts     map[string]config.Host
		wantName  string
		wantBare  bool
		wantSetup []string
	}{
		{
			name:     "no local host",
			hosts:    map[string]config.Host{"m4-mini": remote},
			wantName: "local",
			wantBare: true,
		},
		{
			name: "local host",
			hosts: map[string]config.Host{
				"m4-mini": remote,
				"dev":     {Local: true, SetupCommands: []string{"export FROM_SETUP=yes"}},
			},
			wantName:  "dev",
			wantSetup: []string{"export FROM_SETUP=yes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &WorkflowContext{Resolved: &config.ResolvedConfig{
				Global:      &config.GlobalConfig{Hosts: tt.hosts},
				ProjectRoot: projectRoot,
			}}
			attempts := []hostAttempt{{hostName: "m4-mini"}}

			var result *findAvailableHostResult
			events := captureStderr(t, func() {
				result = fallBackLocally(ctx, fallbackDetail{Reason: host.LocalReasonAllHostsLocked}, attempts)
			})

			conn := result.conn
			assert.Equal(t, tt.wantName, conn.Name)
			assert.Equal(t, tt.wantBare, conn.IsLocal)
			assert.Equal(t, host.LocalReasonAllHostsLocked, conn.LocalReason)
			assert.Equal(t, tt.wantSetup, conn.Host.SetupCommands, "never the remote's setup")
			assert.True(t, conn.InPlace())
			if !tt.wantBare {
				assert.Equal(t, projectRoot, conn.Host.Dir)
			}
			assert.Nil(t, result.lock, "the caller locks a fallback")
			assert.Equal(t, attempts, result.hostsState)

			warn := eventsWith(parseEvents(t, events), "connect", "warn")
			require.Len(t, warn, 1)
			assert.Equal(t, tt.wantName, warn[0].Host)
			assert.Equal(t, host.LocalReasonAllHostsLocked, warn[0].Details["reason"])
		})
	}
}

func TestBuildAllHostsLockedError_NoForceUnlockMention(t *testing.T) {
	lockedHosts := []hostAttempt{
		{hostName: "m4-mini", lockHolder: "'rr test-backend' held by riley@mac (pid 46822, this machine)"},
	}

	err := buildAllHostsLockedError(lockedHosts, time.Minute)
	assert.Contains(t, err.Error(), "rr unlock --all")
	assert.Contains(t, err.Error(), "rr test-backend")
	assert.NotContains(t, err.Error(), "--force-unlock")
}
