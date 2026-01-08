package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConnectionDisplay(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)
	assert.NotNil(t, cd)
}

func TestConnectionStatusString(t *testing.T) {
	tests := []struct {
		status ConnectionStatus
		want   string
	}{
		{StatusTrying, "trying"},
		{StatusSuccess, "connected"},
		{StatusTimeout, "timeout"},
		{StatusRefused, "refused"},
		{StatusUnreachable, "unreachable"},
		{StatusAuthFailed, "auth failed"},
		{StatusFailed, "failed"},
		{ConnectionStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.String())
		})
	}
}

func TestConnectionDisplayAddAttemptTimeout(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	// Don't start spinner for this test to avoid async output
	cd.AddAttempt("mini-local", StatusTimeout, 2*time.Second, "")

	output := buf.String()
	assert.Contains(t, output, SymbolPending)
	assert.Contains(t, output, "mini-local")
	assert.Contains(t, output, "timeout")
}

func TestConnectionDisplayAddAttemptSuccess(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	cd.AddAttempt("mini", StatusSuccess, 300*time.Millisecond, "")

	output := buf.String()
	assert.Contains(t, output, SymbolComplete)
	assert.Contains(t, output, "mini")
	assert.Contains(t, output, "0.3s")
}

func TestConnectionDisplayAddAttemptRefused(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	cd.AddAttempt("gpu-box", StatusRefused, 0, "")

	output := buf.String()
	assert.Contains(t, output, SymbolPending)
	assert.Contains(t, output, "gpu-box")
	assert.Contains(t, output, "refused")
}

func TestConnectionDisplayAddAttemptUnreachable(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	cd.AddAttempt("remote-server", StatusUnreachable, 0, "")

	output := buf.String()
	assert.Contains(t, output, SymbolPending)
	assert.Contains(t, output, "remote-server")
	assert.Contains(t, output, "unreachable")
}

func TestConnectionDisplayAddAttemptAuthFailed(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	cd.AddAttempt("secure-host", StatusAuthFailed, 0, "")

	output := buf.String()
	assert.Contains(t, output, SymbolPending)
	assert.Contains(t, output, "secure-host")
	assert.Contains(t, output, "auth failed")
}

func TestConnectionDisplayAddAttemptFailed(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	cd.AddAttempt("broken-host", StatusFailed, 0, "custom error")

	output := buf.String()
	assert.Contains(t, output, SymbolPending)
	assert.Contains(t, output, "broken-host")
	assert.Contains(t, output, "custom error")
}

func TestConnectionDisplayQuietMode(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)
	cd.SetQuiet(true)

	// Add attempts in quiet mode - they should NOT appear in output
	cd.AddAttempt("mini-local", StatusTimeout, 2*time.Second, "")
	cd.AddAttempt("mini", StatusSuccess, 300*time.Millisecond, "")

	// In quiet mode, nothing should be written during AddAttempt
	assert.Empty(t, buf.String())

	// But attempts should still be tracked
	attempts := cd.Attempts()
	require.Len(t, attempts, 2)
}

func TestConnectionDisplayAttempts(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	cd.AddAttempt("host1", StatusTimeout, 2*time.Second, "")
	cd.AddAttempt("host2", StatusRefused, 0, "")
	cd.AddAttempt("host3", StatusSuccess, 500*time.Millisecond, "")

	attempts := cd.Attempts()
	require.Len(t, attempts, 3)

	assert.Equal(t, "host1", attempts[0].Alias)
	assert.Equal(t, StatusTimeout, attempts[0].Status)

	assert.Equal(t, "host2", attempts[1].Alias)
	assert.Equal(t, StatusRefused, attempts[1].Status)

	assert.Equal(t, "host3", attempts[2].Alias)
	assert.Equal(t, StatusSuccess, attempts[2].Status)
}

func TestConnectionDisplayHasFailedAttempts(t *testing.T) {
	tests := []struct {
		name     string
		attempts []struct {
			alias  string
			status ConnectionStatus
		}
		want bool
	}{
		{
			name: "no failures",
			attempts: []struct {
				alias  string
				status ConnectionStatus
			}{
				{"host1", StatusSuccess},
			},
			want: false,
		},
		{
			name: "has timeout",
			attempts: []struct {
				alias  string
				status ConnectionStatus
			}{
				{"host1", StatusTimeout},
				{"host2", StatusSuccess},
			},
			want: true,
		},
		{
			name: "has refused",
			attempts: []struct {
				alias  string
				status ConnectionStatus
			}{
				{"host1", StatusRefused},
				{"host2", StatusSuccess},
			},
			want: true,
		},
		{
			name: "empty",
			attempts: []struct {
				alias  string
				status ConnectionStatus
			}{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cd := NewConnectionDisplay(&buf)
			cd.SetQuiet(true) // Suppress output

			for _, a := range tt.attempts {
				cd.AddAttempt(a.alias, a.status, 0, "")
			}

			assert.Equal(t, tt.want, cd.HasFailedAttempts())
		})
	}
}

func TestConnectionDisplaySuccessfulAlias(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)
	cd.SetQuiet(true)

	cd.AddAttempt("host1", StatusTimeout, 2*time.Second, "")
	cd.AddAttempt("host2", StatusSuccess, 500*time.Millisecond, "")

	assert.Equal(t, "host2", cd.SuccessfulAlias())
}

func TestConnectionDisplaySuccessfulAliasNotFound(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)
	cd.SetQuiet(true)

	cd.AddAttempt("host1", StatusTimeout, 2*time.Second, "")
	cd.AddAttempt("host2", StatusRefused, 0, "")

	assert.Equal(t, "", cd.SuccessfulAlias())
}

func TestRenderAttemptLine(t *testing.T) {
	tests := []struct {
		name    string
		alias   string
		status  ConnectionStatus
		latency time.Duration
		errMsg  string
		want    []string // substrings that should be present
		notWant []string // substrings that should NOT be present
	}{
		{
			name:    "success with latency",
			alias:   "mini",
			status:  StatusSuccess,
			latency: 300 * time.Millisecond,
			want:    []string{SymbolComplete, "mini", "0.3s"},
			notWant: []string{"timeout", "refused", "failed"},
		},
		{
			name:    "timeout with duration",
			alias:   "mini-local",
			status:  StatusTimeout,
			latency: 2 * time.Second,
			want:    []string{SymbolPending, "mini-local", "timeout", "2.0s"},
		},
		{
			name:   "refused",
			alias:  "gpu-box",
			status: StatusRefused,
			want:   []string{SymbolPending, "gpu-box", "refused"},
		},
		{
			name:   "unreachable",
			alias:  "remote-server",
			status: StatusUnreachable,
			want:   []string{SymbolPending, "remote-server", "unreachable"},
		},
		{
			name:   "auth failed",
			alias:  "secure-host",
			status: StatusAuthFailed,
			want:   []string{SymbolPending, "secure-host", "auth failed"},
		},
		{
			name:   "failed with custom error",
			alias:  "broken-host",
			status: StatusFailed,
			errMsg: "connection reset",
			want:   []string{SymbolPending, "broken-host", "connection reset"},
		},
		{
			name:   "failed without error",
			alias:  "broken-host",
			status: StatusFailed,
			want:   []string{SymbolPending, "broken-host", "failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RenderAttemptLine(tt.alias, tt.status, tt.latency, tt.errMsg)

			for _, s := range tt.want {
				assert.Contains(t, result, s, "expected to contain: %s", s)
			}

			for _, s := range tt.notWant {
				assert.NotContains(t, result, s, "expected NOT to contain: %s", s)
			}

			// Should be indented (starts with 2 spaces)
			assert.True(t, strings.HasPrefix(result, "  "), "expected to be indented")
		})
	}
}

// Visual output tests - these verify the expected output format

func TestConnectionDisplayOutputFormat_SingleSuccess(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	cd.AddAttempt("mini", StatusSuccess, 300*time.Millisecond, "")

	output := buf.String()

	// Expected format:   ● mini                                                  0.3s
	assert.Contains(t, output, SymbolComplete)
	assert.Contains(t, output, "mini")
	assert.Contains(t, output, "0.3s")
	assert.True(t, strings.HasPrefix(output, "  "), "should be indented")
}

func TestConnectionDisplayOutputFormat_FallbackChain(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	// Simulate a fallback chain where first host times out
	cd.AddAttempt("mini-local", StatusTimeout, 2*time.Second, "")
	cd.AddAttempt("mini-tailscale", StatusSuccess, 300*time.Millisecond, "")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	require.Len(t, lines, 2, "expected 2 lines of output")

	// First line: timeout
	assert.Contains(t, lines[0], SymbolPending)
	assert.Contains(t, lines[0], "mini-local")
	assert.Contains(t, lines[0], "timeout")

	// Second line: success
	assert.Contains(t, lines[1], SymbolComplete)
	assert.Contains(t, lines[1], "mini-tailscale")
	assert.Contains(t, lines[1], "0.3s")
}

func TestConnectionDisplayOutputFormat_AllFailed(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)

	cd.AddAttempt("host1", StatusTimeout, 5*time.Second, "")
	cd.AddAttempt("host2", StatusRefused, 0, "")
	cd.AddAttempt("host3", StatusUnreachable, 0, "")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	require.Len(t, lines, 3, "expected 3 lines of output")

	// All should have pending symbol (failure indicator for sub-items)
	for _, line := range lines {
		assert.Contains(t, line, SymbolPending)
		// Lines are indented but may have ANSI escape codes before the spaces
		// So just check that the symbol and content are present
		assert.Contains(t, line, "host")
	}
}

// Test that attempts are properly copied to prevent mutation
func TestConnectionDisplayAttemptsCopy(t *testing.T) {
	var buf bytes.Buffer
	cd := NewConnectionDisplay(&buf)
	cd.SetQuiet(true)

	cd.AddAttempt("host1", StatusSuccess, 100*time.Millisecond, "")

	attempts1 := cd.Attempts()
	attempts2 := cd.Attempts()

	// Modify one copy
	attempts1[0].Alias = "modified"

	// Other copy should be unchanged
	assert.NotEqual(t, attempts1[0].Alias, attempts2[0].Alias)
}
