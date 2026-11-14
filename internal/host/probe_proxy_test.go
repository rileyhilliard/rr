package host

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rrerrors "github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// Regression: a jump host that couldn't be resolved showed as "hostname not
// found" next to the alias, as if the alias were the problem, and a timeout
// through a jump host showed as "unknown error". The reason must name the
// proxy it happened in.
func TestCategorizeProbeError_ProxyFailures(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantReason  ProbeFailReason
		wantSummary string
	}{
		{
			name:        "jump host not resolvable",
			err:         &sshutil.ProxyError{Directive: "ProxyJump", JumpHost: "bastion", Stderr: "ssh: Could not resolve hostname bastion: nodename nor servname provided"},
			wantReason:  ProbeFailDNS,
			wantSummary: "hostname not found via jump host 'bastion'",
		},
		{
			name:        "nothing answers through the jump host",
			err:         &sshutil.ProxyError{Directive: "ProxyJump", JumpHost: "bastion", TimedOut: true},
			wantReason:  ProbeFailTimeout,
			wantSummary: "connection timed out via jump host 'bastion'",
		},
		{
			name:        "jump host can't route to the target",
			err:         &sshutil.ProxyError{Directive: "ProxyJump", JumpHost: "bastion", Stderr: "channel 0: open failed: connect failed: No route to host"},
			wantReason:  ProbeFailUnreachable,
			wantSummary: "host unreachable via jump host 'bastion'",
		},
		{
			name:        "ProxyCommand fails for an unrecognized reason",
			err:         &sshutil.ProxyError{Directive: "ProxyCommand", Stderr: "proxy: bad gateway"},
			wantReason:  ProbeFailUnknown,
			wantSummary: "failed via ProxyCommand",
		},
		{
			name:        "target's host key not in known_hosts",
			err:         fmt.Errorf("ssh: handshake failed: knownhosts: key is unknown"),
			wantReason:  ProbeFailHostKey,
			wantSummary: "host key verification failed",
		},
		{
			name:        "direct connection names no proxy",
			err:         fmt.Errorf("dial tcp 10.0.0.5:22: i/o timeout"),
			wantReason:  ProbeFailTimeout,
			wantSummary: "connection timed out",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rrerrors.WrapWithCode(tt.err, rrerrors.ErrSSH, "Couldn't reach 'target'", "Check it")

			got := categorizeProbeError("target", err)

			assert.Equal(t, tt.wantReason, got.Reason)
			assert.Equal(t, tt.wantSummary, got.Summary())
		})
	}
}

// jumpFailure is the error sshutil.Dial returns when bastion can't be
// resolved, categorized as a probe does.
func jumpFailure(alias string) error {
	return categorizeProbeError(alias, rrerrors.WrapWithCode(
		&sshutil.ProxyError{Directive: "ProxyJump", JumpHost: "bastion", Stderr: "ssh: Could not resolve hostname bastion"},
		rrerrors.ErrSSH,
		"Couldn't reach '"+alias+"' through jump host 'bastion'",
		"Check the jump host on its own (ssh bastion)."))
}

// Regression: a host whose alias failed in its jump host showed "hostname
// not found" next to the alias, as if the alias were the problem, and nested
// the dial's whole formatted error in parentheses. The progress line must
// name the jump host, and the error must carry ssh's message on one line.
func TestDialAliases_ProxyFailureNamesTheJumpHost(t *testing.T) {
	tests := []struct {
		name     string
		failures map[string]error
		aliases  []string
	}{
		{
			name:     "only alias fails in its jump host",
			aliases:  []string{"m1-tailscale"},
			failures: map[string]error{"m1-tailscale": jumpFailure("m1-tailscale")},
		},
		{
			name:    "a direct alias and a jump alias fail",
			aliases: []string{"m1-local", "m1-tailscale"},
			failures: map[string]error{
				"m1-local":     categorizeProbeError("m1-local", fmt.Errorf("dial tcp 192.168.1.5:22: i/o timeout")),
				"m1-tailscale": jumpFailure("m1-tailscale"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dial := func(alias string, _ time.Duration) (*sshutil.Client, time.Duration, error) {
				return nil, 0, tt.failures[alias]
			}
			var events []ConnectionEvent

			_, err := DialAliases("m1-mini", tt.aliases, DialOptions{
				Dial: dial, Grace: 10 * time.Millisecond,
				OnEvent: func(e ConnectionEvent) { events = append(events, e) },
			})

			require.Error(t, err)
			assert.True(t, rrerrors.IsCode(err, rrerrors.ErrSSH))
			assert.Contains(t, err.Error(), "hostname not found via jump host 'bastion' (ssh: Could not resolve hostname bastion)")
			assert.NotContains(t, err.Error(), "(✗", "the dial's formatted error isn't nested")
			failed := eventsOfType(events, EventFailed)
			require.Len(t, failed, len(tt.aliases))
			for _, e := range failed {
				if e.Alias == "m1-tailscale" {
					assert.Equal(t, "hostname not found via jump host 'bastion'", e.Message, "the progress line names the jump host")
				}
			}
		})
	}
}
