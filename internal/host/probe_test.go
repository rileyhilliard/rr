package host

import (
	"errors"
	"testing"
	"time"
)

func TestCategorizeProbeError_Timeout(t *testing.T) {
	testCases := []string{
		"i/o timeout",
		"connection timeout",
		"dial tcp: timeout",
	}

	for _, errMsg := range testCases {
		err := categorizeProbeError("test-host", errors.New(errMsg))
		if err == nil {
			t.Errorf("categorizeProbeError(%q) returned nil", errMsg)
			continue
		}

		if err.Reason != ProbeFailTimeout {
			t.Errorf("categorizeProbeError(%q).Reason = %v, want ProbeFailTimeout", errMsg, err.Reason)
		}
	}
}

func TestCategorizeProbeError_Refused(t *testing.T) {
	err := categorizeProbeError("test-host", errors.New("connection refused"))
	if err == nil {
		t.Fatal("categorizeProbeError returned nil")
	}

	if err.Reason != ProbeFailRefused {
		t.Errorf("Reason = %v, want ProbeFailRefused", err.Reason)
	}
}

func TestCategorizeProbeError_Unreachable(t *testing.T) {
	testCases := []string{
		"no route to host",
		"network is unreachable",
		"host is down",
	}

	for _, errMsg := range testCases {
		err := categorizeProbeError("test-host", errors.New(errMsg))
		if err == nil {
			t.Errorf("categorizeProbeError(%q) returned nil", errMsg)
			continue
		}

		if err.Reason != ProbeFailUnreachable {
			t.Errorf("categorizeProbeError(%q).Reason = %v, want ProbeFailUnreachable", errMsg, err.Reason)
		}
	}
}

func TestCategorizeProbeError_Auth(t *testing.T) {
	testCases := []string{
		"unable to authenticate",
		"no supported methods remain",
		"permission denied (publickey)",
		"authentication failed",
	}

	for _, errMsg := range testCases {
		err := categorizeProbeError("test-host", errors.New(errMsg))
		if err == nil {
			t.Errorf("categorizeProbeError(%q) returned nil", errMsg)
			continue
		}

		if err.Reason != ProbeFailAuth {
			t.Errorf("categorizeProbeError(%q).Reason = %v, want ProbeFailAuth", errMsg, err.Reason)
		}
	}
}

func TestCategorizeProbeError_HostKey(t *testing.T) {
	err := categorizeProbeError("test-host", errors.New("host key verification failed"))
	if err == nil {
		t.Fatal("categorizeProbeError returned nil")
	}

	if err.Reason != ProbeFailHostKey {
		t.Errorf("Reason = %v, want ProbeFailHostKey", err.Reason)
	}
}

func TestCategorizeProbeError_Unknown(t *testing.T) {
	err := categorizeProbeError("test-host", errors.New("some random error"))
	if err == nil {
		t.Fatal("categorizeProbeError returned nil")
	}

	if err.Reason != ProbeFailUnknown {
		t.Errorf("Reason = %v, want ProbeFailUnknown", err.Reason)
	}
}

func TestCategorizeProbeError_Nil(t *testing.T) {
	err := categorizeProbeError("test-host", nil)
	if err != nil {
		t.Errorf("categorizeProbeError(nil) = %v, want nil", err)
	}
}

func TestProbeError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	probeErr := &ProbeError{
		SSHAlias: "test",
		Reason:   ProbeFailTimeout,
		Cause:    cause,
	}

	unwrapped := probeErr.Unwrap()
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}
}

func TestProbeAll_EmptyList(t *testing.T) {
	results := ProbeAll([]string{}, 1*time.Second)
	if len(results) != 0 {
		t.Errorf("ProbeAll([]) returned %d results, want 0", len(results))
	}
}

func TestProbeResult_Fields(t *testing.T) {
	result := ProbeResult{
		SSHAlias: "test-host",
		Latency:  100 * time.Millisecond,
		Error:    nil,
		Success:  true,
	}

	if result.SSHAlias != "test-host" {
		t.Errorf("SSHAlias = %q, want 'test-host'", result.SSHAlias)
	}

	if result.Latency != 100*time.Millisecond {
		t.Errorf("Latency = %v, want 100ms", result.Latency)
	}

	if !result.Success {
		t.Error("Success should be true")
	}
}
