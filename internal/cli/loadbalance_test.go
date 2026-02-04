package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBuildConnectionError(t *testing.T) {
	tests := []struct {
		name           string
		attempts       []hostAttempt
		wantContains   []string
		wantNoContains []string
	}{
		{
			name:         "no attempts returns no hosts configured",
			attempts:     []hostAttempt{},
			wantContains: []string{"No hosts configured"},
		},
		{
			name: "single failed host",
			attempts: []hostAttempt{
				{hostName: "server1", connErr: errors.New("connection refused")},
			},
			wantContains: []string{"Couldn't connect to host 'server1'"},
		},
		{
			name: "multiple failed hosts",
			attempts: []hostAttempt{
				{hostName: "server1", connErr: errors.New("connection refused")},
				{hostName: "server2", connErr: errors.New("timeout")},
			},
			wantContains: []string{"Couldn't connect to any host", "server1", "server2"},
		},
		{
			name: "mixed hosts - only failed ones in message",
			attempts: []hostAttempt{
				{hostName: "server1", connErr: errors.New("connection refused")},
				{hostName: "server2", connErr: nil}, // This one succeeded (but was locked)
				{hostName: "server3", connErr: errors.New("timeout")},
			},
			wantContains:   []string{"server1", "server3"},
			wantNoContains: []string{"server2"}, // server2 shouldn't appear since it didn't fail to connect
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := buildConnectionError(tt.attempts)
			assert.Error(t, err)

			errStr := err.Error()
			for _, want := range tt.wantContains {
				assert.Contains(t, errStr, want)
			}
			for _, notWant := range tt.wantNoContains {
				assert.NotContains(t, errStr, notWant)
			}
		})
	}
}

func TestBuildAllHostsLockedError(t *testing.T) {
	tests := []struct {
		name         string
		lockedHosts  []hostAttempt
		timeout      time.Duration
		wantContains []string
	}{
		{
			name: "single locked host with holder",
			lockedHosts: []hostAttempt{
				{hostName: "server1", lockHolder: "alice@laptop"},
			},
			timeout:      30 * time.Second,
			wantContains: []string{"server1", "alice@laptop", "30s", "locked"},
		},
		{
			name: "multiple locked hosts",
			lockedHosts: []hostAttempt{
				{hostName: "server1", lockHolder: "alice@laptop"},
				{hostName: "server2", lockHolder: "bob@desktop"},
			},
			timeout:      1 * time.Minute,
			wantContains: []string{"server1", "server2", "alice@laptop", "bob@desktop", "1m0s"},
		},
		{
			name: "locked host with unknown holder",
			lockedHosts: []hostAttempt{
				{hostName: "server1", lockHolder: ""},
			},
			timeout:      10 * time.Second,
			wantContains: []string{"server1", "unknown", "10s"},
		},
		{
			name:         "empty locked hosts",
			lockedHosts:  []hostAttempt{},
			timeout:      5 * time.Second,
			wantContains: []string{"timed out", "5s"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := buildAllHostsLockedError(tt.lockedHosts, tt.timeout)
			assert.Error(t, err)

			errStr := err.Error()
			for _, want := range tt.wantContains {
				assert.Contains(t, errStr, want)
			}
		})
	}
}

func TestHostAttempt_Structure(t *testing.T) {
	// Verify hostAttempt struct can hold all required fields
	attempt := hostAttempt{
		hostName:   "test-host",
		conn:       nil, // Would be *host.Connection in real use
		connErr:    errors.New("test error"),
		lockHolder: "test-user",
	}

	assert.Equal(t, "test-host", attempt.hostName)
	assert.Nil(t, attempt.conn)
	assert.Error(t, attempt.connErr)
	assert.Equal(t, "test-user", attempt.lockHolder)
}

func TestFindAvailableHostResult_Structure(t *testing.T) {
	// Verify findAvailableHostResult struct can hold all required fields
	result := findAvailableHostResult{
		conn:    nil, // Would be *host.Connection in real use
		lock:    nil, // Would be *lock.Lock in real use
		isLocal: true,
		hostsState: []hostAttempt{
			{hostName: "host1"},
			{hostName: "host2"},
		},
	}

	assert.Nil(t, result.conn)
	assert.Nil(t, result.lock)
	assert.True(t, result.isLocal)
	assert.Len(t, result.hostsState, 2)
}
