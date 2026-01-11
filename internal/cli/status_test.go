package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatLatency(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "sub-millisecond returns <1ms",
			duration: 500 * time.Microsecond,
			want:     "<1ms",
		},
		{
			name:     "zero duration returns <1ms",
			duration: 0,
			want:     "<1ms",
		},
		{
			name:     "exactly 1 millisecond",
			duration: 1 * time.Millisecond,
			want:     "1ms",
		},
		{
			name:     "multiple milliseconds",
			duration: 42 * time.Millisecond,
			want:     "42ms",
		},
		{
			name:     "one second",
			duration: 1 * time.Second,
			want:     "1000ms",
		},
		{
			name:     "mixed duration rounds to milliseconds",
			duration: 1500 * time.Microsecond, // 1.5ms -> 1ms
			want:     "1ms",
		},
		{
			name:     "large duration",
			duration: 5 * time.Second,
			want:     "5000ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLatency(tt.duration)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindSelectedHost(t *testing.T) {
	tests := []struct {
		name        string
		defaultHost string
		results     map[string]probeResult
		expected    *Selected
	}{
		{
			name:        "returns nil when no hosts are healthy",
			defaultHost: "auto",
			results: map[string]probeResult{
				"dev": {
					HostName: "dev",
					Aliases: []host.ProbeResult{
						{SSHAlias: "dev-lan", Success: false},
						{SSHAlias: "dev-vpn", Success: false},
					},
				},
			},
			expected: nil,
		},
		{
			name:        "auto mode selects first healthy host",
			defaultHost: "auto",
			results: map[string]probeResult{
				"dev": {
					HostName: "dev",
					Aliases: []host.ProbeResult{
						{SSHAlias: "dev-lan", Success: false},
						{SSHAlias: "dev-vpn", Success: true, Latency: 50 * time.Millisecond},
					},
				},
			},
			expected: &Selected{Host: "dev", Alias: "dev-vpn"},
		},
		{
			name:        "empty default behaves like auto",
			defaultHost: "",
			results: map[string]probeResult{
				"prod": {
					HostName: "prod",
					Aliases: []host.ProbeResult{
						{SSHAlias: "prod-direct", Success: true, Latency: 10 * time.Millisecond},
					},
				},
			},
			expected: &Selected{Host: "prod", Alias: "prod-direct"},
		},
		{
			name:        "explicit default selects specified host",
			defaultHost: "staging",
			results: map[string]probeResult{
				"dev": {
					HostName: "dev",
					Aliases: []host.ProbeResult{
						{SSHAlias: "dev-lan", Success: true, Latency: 5 * time.Millisecond},
					},
				},
				"staging": {
					HostName: "staging",
					Aliases: []host.ProbeResult{
						{SSHAlias: "staging-vpn", Success: true, Latency: 100 * time.Millisecond},
					},
				},
			},
			expected: &Selected{Host: "staging", Alias: "staging-vpn"},
		},
		{
			name:        "explicit default with unhealthy host returns nil",
			defaultHost: "prod",
			results: map[string]probeResult{
				"dev": {
					HostName: "dev",
					Aliases: []host.ProbeResult{
						{SSHAlias: "dev-lan", Success: true},
					},
				},
				"prod": {
					HostName: "prod",
					Aliases: []host.ProbeResult{
						{SSHAlias: "prod-ssh", Success: false},
					},
				},
			},
			expected: nil,
		},
		{
			name:        "explicit default for non-existent host returns nil",
			defaultHost: "nonexistent",
			results: map[string]probeResult{
				"dev": {
					HostName: "dev",
					Aliases: []host.ProbeResult{
						{SSHAlias: "dev-lan", Success: true},
					},
				},
			},
			expected: nil,
		},
		{
			name:        "selects first successful alias for preferred host",
			defaultHost: "gpu-box",
			results: map[string]probeResult{
				"gpu-box": {
					HostName: "gpu-box",
					Aliases: []host.ProbeResult{
						{SSHAlias: "gpu-lan", Success: false},
						{SSHAlias: "gpu-vpn", Success: true, Latency: 50 * time.Millisecond},
						{SSHAlias: "gpu-public", Success: true, Latency: 200 * time.Millisecond},
					},
				},
			},
			expected: &Selected{Host: "gpu-box", Alias: "gpu-vpn"},
		},
		{
			name:        "empty results returns nil",
			defaultHost: "auto",
			results:     map[string]probeResult{},
			expected:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSelectedHost(tt.defaultHost, tt.results)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestOutputStatusJSON(t *testing.T) {
	tests := []struct {
		name        string
		defaultHost string
		results     map[string]probeResult
		selected    *Selected
		validate    func(t *testing.T, output StatusOutput)
	}{
		{
			name:        "includes all hosts with their aliases",
			defaultHost: "dev",
			results: map[string]probeResult{
				"dev": {
					HostName: "dev",
					Aliases: []host.ProbeResult{
						{SSHAlias: "dev-lan", Success: true, Latency: 10 * time.Millisecond},
						{SSHAlias: "dev-vpn", Success: false, Error: fmt.Errorf("connection refused")},
					},
				},
			},
			selected: &Selected{Host: "dev", Alias: "dev-lan"},
			validate: func(t *testing.T, output StatusOutput) {
				assert.Equal(t, "dev", output.Default)
				require.Len(t, output.Hosts, 1)
				assert.Equal(t, "dev", output.Hosts[0].Name)
				assert.True(t, output.Hosts[0].Healthy)
				require.Len(t, output.Hosts[0].Aliases, 2)

				// Find the aliases (order may vary)
				var lanAlias, vpnAlias *AliasStatus
				for i := range output.Hosts[0].Aliases {
					if output.Hosts[0].Aliases[i].Alias == "dev-lan" {
						lanAlias = &output.Hosts[0].Aliases[i]
					}
					if output.Hosts[0].Aliases[i].Alias == "dev-vpn" {
						vpnAlias = &output.Hosts[0].Aliases[i]
					}
				}

				require.NotNil(t, lanAlias)
				assert.Equal(t, "connected", lanAlias.Status)
				assert.Equal(t, "10ms", lanAlias.Latency)
				assert.Empty(t, lanAlias.Error)

				require.NotNil(t, vpnAlias)
				assert.Equal(t, "failed", vpnAlias.Status)
				assert.Equal(t, "connection refused", vpnAlias.Error)

				require.NotNil(t, output.Selected)
				assert.Equal(t, "dev", output.Selected.Host)
				assert.Equal(t, "dev-lan", output.Selected.Alias)
			},
		},
		{
			name:        "healthy is false when no aliases succeed",
			defaultHost: "auto",
			results: map[string]probeResult{
				"unreachable": {
					HostName: "unreachable",
					Aliases: []host.ProbeResult{
						{SSHAlias: "host1", Success: false},
						{SSHAlias: "host2", Success: false},
					},
				},
			},
			selected: nil,
			validate: func(t *testing.T, output StatusOutput) {
				require.Len(t, output.Hosts, 1)
				assert.False(t, output.Hosts[0].Healthy)
				assert.Nil(t, output.Selected)
			},
		},
		{
			name:        "empty results produces empty hosts array",
			defaultHost: "",
			results:     map[string]probeResult{},
			selected:    nil,
			validate: func(t *testing.T, output StatusOutput) {
				assert.Empty(t, output.Hosts)
				assert.Nil(t, output.Selected)
			},
		},
		{
			name:        "nil error does not appear in output",
			defaultHost: "test",
			results: map[string]probeResult{
				"test": {
					HostName: "test",
					Aliases: []host.ProbeResult{
						{SSHAlias: "test-ssh", Success: false, Error: nil},
					},
				},
			},
			selected: nil,
			validate: func(t *testing.T, output StatusOutput) {
				require.Len(t, output.Hosts, 1)
				require.Len(t, output.Hosts[0].Aliases, 1)
				assert.Empty(t, output.Hosts[0].Aliases[0].Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			// Run the function
			outputErr := outputStatusJSON(tt.defaultHost, tt.results, tt.selected)
			require.NoError(t, outputErr)

			// Restore stdout and read captured output
			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, err = io.Copy(&buf, r)
			require.NoError(t, err)

			// Parse the JSON output
			var output StatusOutput
			err = json.Unmarshal(buf.Bytes(), &output)
			require.NoError(t, err, "output should be valid JSON: %s", buf.String())

			// Run validation
			tt.validate(t, output)
		})
	}
}

func TestOutputStatusText(t *testing.T) {
	tests := []struct {
		name           string
		defaultHost    string
		results        map[string]probeResult
		selected       *Selected
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:        "shows default host",
			defaultHost: "dev-machine",
			results: map[string]probeResult{
				"dev-machine": {
					HostName: "dev-machine",
					Aliases: []host.ProbeResult{
						{SSHAlias: "dev-lan", Success: true, Latency: 5 * time.Millisecond},
					},
				},
			},
			selected:     &Selected{Host: "dev-machine", Alias: "dev-lan"},
			wantContains: []string{"Default: dev-machine", "Selected: dev-machine"},
		},
		{
			name:        "shows auto for empty or auto default",
			defaultHost: "auto",
			results: map[string]probeResult{
				"server": {
					HostName: "server",
					Aliases: []host.ProbeResult{
						{SSHAlias: "server-ssh", Success: true, Latency: 10 * time.Millisecond},
					},
				},
			},
			selected:     &Selected{Host: "server", Alias: "server-ssh"},
			wantContains: []string{"auto", "Selected: server"},
		},
		{
			name:        "shows none when no hosts reachable",
			defaultHost: "auto",
			results: map[string]probeResult{
				"broken": {
					HostName: "broken",
					Aliases: []host.ProbeResult{
						{SSHAlias: "broken-ssh", Success: false},
					},
				},
			},
			selected:     nil,
			wantContains: []string{"none"},
		},
		{
			name:        "shows via alias for selected host",
			defaultHost: "gpu",
			results: map[string]probeResult{
				"gpu": {
					HostName: "gpu",
					Aliases: []host.ProbeResult{
						{SSHAlias: "gpu-tailscale", Success: true, Latency: 50 * time.Millisecond},
					},
				},
			},
			selected:     &Selected{Host: "gpu", Alias: "gpu-tailscale"},
			wantContains: []string{"gpu-tailscale"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stdout = w

			// Run the function
			outputErr := outputStatusText(tt.defaultHost, tt.results, tt.selected)
			require.NoError(t, outputErr)

			// Restore stdout and read captured output
			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, err = io.Copy(&buf, r)
			require.NoError(t, err)

			output := buf.String()

			for _, want := range tt.wantContains {
				assert.Contains(t, output, want, "output should contain %q", want)
			}

			for _, notWant := range tt.wantNotContain {
				assert.NotContains(t, output, notWant, "output should not contain %q", notWant)
			}
		})
	}
}

func TestStatusOutput_JSONStructure(t *testing.T) {
	// Test that the struct marshals correctly
	output := StatusOutput{
		Hosts: []HostStatus{
			{
				Name:    "test-host",
				Healthy: true,
				Aliases: []AliasStatus{
					{Alias: "test-ssh", Status: "connected", Latency: "5ms"},
				},
			},
		},
		Default:  "test-host",
		Selected: &Selected{Host: "test-host", Alias: "test-ssh"},
	}

	data, err := json.Marshal(output)
	require.NoError(t, err)

	// Unmarshal back to verify round-trip
	var parsed StatusOutput
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, output.Default, parsed.Default)
	assert.Equal(t, output.Selected.Host, parsed.Selected.Host)
	assert.Equal(t, output.Selected.Alias, parsed.Selected.Alias)
	require.Len(t, parsed.Hosts, 1)
	assert.Equal(t, "test-host", parsed.Hosts[0].Name)
	assert.True(t, parsed.Hosts[0].Healthy)
}

func TestStatusOutput_SelectedOmittedWhenNil(t *testing.T) {
	output := StatusOutput{
		Hosts:    []HostStatus{},
		Default:  "auto",
		Selected: nil,
	}

	data, err := json.Marshal(output)
	require.NoError(t, err)

	// Check that "selected" is omitted (not null)
	assert.NotContains(t, string(data), `"selected":null`)
	assert.NotContains(t, string(data), `"selected"`)
}

func TestProbeResult_Structure(t *testing.T) {
	// Verify the probeResult type works as expected
	result := probeResult{
		HostName: "test-host",
		Aliases: []host.ProbeResult{
			{SSHAlias: "alias1", Success: true, Latency: 10 * time.Millisecond},
			{SSHAlias: "alias2", Success: false, Error: fmt.Errorf("timeout")},
		},
	}

	assert.Equal(t, "test-host", result.HostName)
	assert.Len(t, result.Aliases, 2)
	assert.True(t, result.Aliases[0].Success)
	assert.False(t, result.Aliases[1].Success)
}
