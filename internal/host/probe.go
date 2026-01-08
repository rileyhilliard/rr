package host

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// ProbeError represents a failed probe with categorized failure reason.
type ProbeError struct {
	SSHAlias string
	Reason   ProbeFailReason
	Cause    error
}

// ProbeFailReason categorizes why a probe failed.
type ProbeFailReason int

const (
	ProbeFailUnknown ProbeFailReason = iota
	ProbeFailTimeout
	ProbeFailRefused
	ProbeFailUnreachable
	ProbeFailAuth
	ProbeFailHostKey
)

// String returns a human-readable description of the failure reason.
func (r ProbeFailReason) String() string {
	switch r {
	case ProbeFailTimeout:
		return "connection timed out"
	case ProbeFailRefused:
		return "connection refused"
	case ProbeFailUnreachable:
		return "host unreachable"
	case ProbeFailAuth:
		return "authentication failed"
	case ProbeFailHostKey:
		return "host key verification failed"
	default:
		return "unknown error"
	}
}

func (e *ProbeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("probe %s failed: %s (%v)", e.SSHAlias, e.Reason, e.Cause)
	}
	return fmt.Sprintf("probe %s failed: %s", e.SSHAlias, e.Reason)
}

func (e *ProbeError) Unwrap() error {
	return e.Cause
}

// Probe tests connectivity to an SSH host and returns the connection latency.
// It performs:
//  1. A quick TCP connection test to verify the port is open
//  2. A full SSH handshake to verify authentication works
//
// Returns the total latency (TCP + SSH handshake time) on success.
// Returns a ProbeError with categorized failure reason on error.
func Probe(sshAlias string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()

	// Connect and perform SSH handshake
	client, err := sshutil.Dial(sshAlias, timeout)
	if err != nil {
		return 0, categorizeProbeError(sshAlias, err)
	}
	defer client.Close()

	latency := time.Since(start)
	return latency, nil
}

// ProbeTCP performs only a TCP connection test without SSH handshake.
// Useful for quick reachability checks before attempting full SSH connection.
func ProbeTCP(address string, timeout time.Duration) (time.Duration, error) {
	start := time.Now()

	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return 0, categorizeProbeError(address, err)
	}
	defer conn.Close()

	return time.Since(start), nil
}

// ProbeResult contains the result of probing a single SSH alias.
type ProbeResult struct {
	SSHAlias string
	Latency  time.Duration
	Error    error
	Success  bool
}

// ProbeAll tests multiple SSH aliases and returns results for each.
// Probes are performed sequentially (not in parallel) to avoid overwhelming
// the network or triggering rate limits.
func ProbeAll(sshAliases []string, timeout time.Duration) []ProbeResult {
	results := make([]ProbeResult, len(sshAliases))

	for i, alias := range sshAliases {
		latency, err := Probe(alias, timeout)
		results[i] = ProbeResult{
			SSHAlias: alias,
			Latency:  latency,
			Error:    err,
			Success:  err == nil,
		}
	}

	return results
}

// FirstReachable probes SSH aliases in order and returns the first successful one.
// This is useful for fallback chains where you want the first working host.
func FirstReachable(sshAliases []string, timeout time.Duration) (*ProbeResult, error) {
	if len(sshAliases) == 0 {
		return nil, errors.New(errors.ErrSSH,
			"No SSH hosts configured",
			"Add at least one SSH alias to the host configuration")
	}

	var lastErr error
	for _, alias := range sshAliases {
		latency, err := Probe(alias, timeout)
		if err == nil {
			return &ProbeResult{
				SSHAlias: alias,
				Latency:  latency,
				Success:  true,
			}, nil
		}
		lastErr = err
	}

	// All probes failed
	return nil, errors.WrapWithCode(lastErr, errors.ErrSSH,
		fmt.Sprintf("All SSH hosts unreachable (tried %d)", len(sshAliases)),
		"Check your network connection and SSH configuration")
}

// categorizeProbeError converts a generic error into a ProbeError with
// a categorized failure reason.
func categorizeProbeError(sshAlias string, err error) *ProbeError {
	probeErr := &ProbeError{
		SSHAlias: sshAlias,
		Reason:   ProbeFailUnknown,
		Cause:    err,
	}

	if err == nil {
		return nil
	}

	errStr := strings.ToLower(err.Error())

	// Check for timeout
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "i/o timeout") {
		probeErr.Reason = ProbeFailTimeout
		return probeErr
	}

	// Check for connection refused
	if strings.Contains(errStr, "connection refused") {
		probeErr.Reason = ProbeFailRefused
		return probeErr
	}

	// Check for unreachable host
	if strings.Contains(errStr, "no route to host") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "host is down") {
		probeErr.Reason = ProbeFailUnreachable
		return probeErr
	}

	// Check for authentication failure
	if strings.Contains(errStr, "unable to authenticate") ||
		strings.Contains(errStr, "no supported methods") ||
		strings.Contains(errStr, "permission denied") ||
		strings.Contains(errStr, "authentication failed") {
		probeErr.Reason = ProbeFailAuth
		return probeErr
	}

	// Check for host key issues
	if strings.Contains(errStr, "host key") {
		probeErr.Reason = ProbeFailHostKey
		return probeErr
	}

	return probeErr
}
