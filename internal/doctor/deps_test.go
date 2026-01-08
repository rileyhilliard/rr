package doctor

import (
	"os/exec"
	"testing"
)

func TestRsyncLocalCheck(t *testing.T) {
	check := &RsyncLocalCheck{}

	t.Run("name and category", func(t *testing.T) {
		if check.Name() != "rsync_local" {
			t.Errorf("expected name 'rsync_local', got %s", check.Name())
		}
		if check.Category() != "DEPENDENCIES" {
			t.Errorf("expected category 'DEPENDENCIES', got %s", check.Category())
		}
	})

	t.Run("run", func(t *testing.T) {
		result := check.Run()

		// Check depends on whether rsync is installed
		_, err := exec.LookPath("rsync")
		if err != nil {
			if result.Status != StatusFail {
				t.Errorf("expected StatusFail when rsync not installed, got %v", result.Status)
			}
		} else {
			if result.Status != StatusPass {
				t.Errorf("expected StatusPass when rsync installed, got %v: %s", result.Status, result.Message)
			}
		}
	})

	t.Run("fix returns nil", func(t *testing.T) {
		if err := check.Fix(); err != nil {
			t.Errorf("expected Fix() to return nil, got %v", err)
		}
	})
}

func TestRsyncRemoteCheck(t *testing.T) {
	t.Run("name and category", func(t *testing.T) {
		check := &RsyncRemoteCheck{HostName: "test-host"}

		if check.Name() != "rsync_remote_test-host" {
			t.Errorf("expected name 'rsync_remote_test-host', got %s", check.Name())
		}
		if check.Category() != "DEPENDENCIES" {
			t.Errorf("expected category 'DEPENDENCIES', got %s", check.Category())
		}
	})

	t.Run("no connection", func(t *testing.T) {
		check := &RsyncRemoteCheck{
			HostName: "test-host",
			Conn:     nil,
		}

		result := check.Run()

		if result.Status != StatusFail {
			t.Errorf("expected StatusFail with no connection, got %v", result.Status)
		}
	})
}

func TestParseRsyncVersion(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "standard format",
			output:   "rsync  version 3.2.7  protocol version 31",
			expected: "3.2.7",
		},
		{
			name:     "older format",
			output:   "rsync version 2.6.9 protocol version 29",
			expected: "2.6.9",
		},
		{
			name:     "multi-line output",
			output:   "rsync  version 3.1.3  protocol version 31\nCopyright (C) 1996-2018 by Andrew Tridgell",
			expected: "3.1.3",
		},
		{
			name:     "no version found",
			output:   "some other output",
			expected: "unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRsyncVersion(tc.output)
			if got != tc.expected {
				t.Errorf("parseRsyncVersion() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestNewDepsChecks(t *testing.T) {
	checks := NewDepsChecks()

	if len(checks) != 1 {
		t.Errorf("expected 1 deps check (local rsync), got %d", len(checks))
	}

	if checks[0].Category() != "DEPENDENCIES" {
		t.Errorf("expected DEPENDENCIES category, got %s", checks[0].Category())
	}
}

func TestNewRemoteDepsChecks(t *testing.T) {
	checks := NewRemoteDepsChecks("test-host", nil)

	if len(checks) != 1 {
		t.Errorf("expected 1 remote deps check, got %d", len(checks))
	}

	if checks[0].Category() != "DEPENDENCIES" {
		t.Errorf("expected DEPENDENCIES category, got %s", checks[0].Category())
	}
}

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		msg      string
		expected string
	}{
		{"rsync 3.2.7 (local)", "3.2.7"},
		{"rsync 3.1 (remote)", "3.1"},
		{"rsync found (version unknown)", "unknown"},
	}

	for _, tc := range tests {
		got := extractVersion(tc.msg)
		if got != tc.expected {
			t.Errorf("extractVersion(%q) = %q, want %q", tc.msg, got, tc.expected)
		}
	}
}
