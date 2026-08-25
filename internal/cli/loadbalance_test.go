package cli

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/stretchr/testify/assert"
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

func TestLocalFallbackResult(t *testing.T) {
	attempts := []hostAttempt{{hostName: "m4-mini"}}
	result := localFallbackResult(attempts)

	assert.True(t, result.isLocal)
	assert.True(t, result.fellBack)
	assert.True(t, result.conn.IsLocal)
	assert.Equal(t, "local", result.conn.Name)
	assert.Nil(t, result.lock)
	assert.Equal(t, attempts, result.hostsState)
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
