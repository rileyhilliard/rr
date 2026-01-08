package doctor

import (
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
)

func TestRemoteDirCheck(t *testing.T) {
	t.Run("name and category", func(t *testing.T) {
		check := &RemoteDirCheck{HostName: "test-host", Dir: "~/projects"}

		if check.Name() != "remote_dir_test-host" {
			t.Errorf("expected name 'remote_dir_test-host', got %s", check.Name())
		}
		if check.Category() != "REMOTE" {
			t.Errorf("expected category 'REMOTE', got %s", check.Category())
		}
	})

	t.Run("no connection", func(t *testing.T) {
		check := &RemoteDirCheck{
			HostName: "test-host",
			Dir:      "~/projects",
			Conn:     nil,
		}

		result := check.Run()

		if result.Status != StatusFail {
			t.Errorf("expected StatusFail with no connection, got %v", result.Status)
		}
	})

	t.Run("fix with no connection", func(t *testing.T) {
		check := &RemoteDirCheck{
			HostName: "test-host",
			Dir:      "~/projects",
			Conn:     nil,
		}

		err := check.Fix()
		if err == nil {
			t.Error("expected Fix() to return error with no connection")
		}
	})
}

func TestRemoteWritePermCheck(t *testing.T) {
	t.Run("name and category", func(t *testing.T) {
		check := &RemoteWritePermCheck{HostName: "test-host", Dir: "~/projects"}

		if check.Name() != "remote_write_test-host" {
			t.Errorf("expected name 'remote_write_test-host', got %s", check.Name())
		}
		if check.Category() != "REMOTE" {
			t.Errorf("expected category 'REMOTE', got %s", check.Category())
		}
	})

	t.Run("no connection", func(t *testing.T) {
		check := &RemoteWritePermCheck{
			HostName: "test-host",
			Dir:      "~/projects",
			Conn:     nil,
		}

		result := check.Run()

		if result.Status != StatusFail {
			t.Errorf("expected StatusFail with no connection, got %v", result.Status)
		}
	})
}

func TestRemoteStaleLockCheck(t *testing.T) {
	t.Run("name and category", func(t *testing.T) {
		check := &RemoteStaleLockCheck{HostName: "test-host"}

		if check.Name() != "remote_locks_test-host" {
			t.Errorf("expected name 'remote_locks_test-host', got %s", check.Name())
		}
		if check.Category() != "REMOTE" {
			t.Errorf("expected category 'REMOTE', got %s", check.Category())
		}
	})

	t.Run("locking disabled", func(t *testing.T) {
		check := &RemoteStaleLockCheck{
			HostName: "test-host",
			Conn:     nil, // No connection, but locking disabled
			LockConfig: config.LockConfig{
				Enabled: false,
			},
		}

		result := check.Run()

		if result.Status != StatusPass {
			t.Errorf("expected StatusPass when locking disabled, got %v", result.Status)
		}
	})

	t.Run("no connection", func(t *testing.T) {
		check := &RemoteStaleLockCheck{
			HostName: "test-host",
			Conn:     nil,
			LockConfig: config.LockConfig{
				Enabled: true,
			},
		}

		result := check.Run()

		// Should pass because we can't check without a connection
		if result.Status != StatusPass {
			t.Errorf("expected StatusPass with no connection, got %v", result.Status)
		}
	})
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d        time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{65 * time.Minute, "1h5m"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			got := formatDuration(tc.d)
			if got != tc.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.expected)
			}
		})
	}
}

func TestNewRemoteChecks(t *testing.T) {
	hostCfg := config.Host{
		SSH: []string{"test-host"},
		Dir: "~/projects",
	}
	lockCfg := config.LockConfig{
		Enabled: true,
		Dir:     "/tmp/rr-locks",
	}

	checks := NewRemoteChecks("test-host", hostCfg, nil, lockCfg)

	if len(checks) != 3 {
		t.Errorf("expected 3 remote checks, got %d", len(checks))
	}

	// Verify all checks have REMOTE category
	for _, check := range checks {
		if check.Category() != "REMOTE" {
			t.Errorf("expected REMOTE category, got %s", check.Category())
		}
	}

	// Verify check names
	names := make(map[string]bool)
	for _, check := range checks {
		names[check.Name()] = true
	}

	expectedNames := []string{
		"remote_dir_test-host",
		"remote_write_test-host",
		"remote_locks_test-host",
	}
	for _, name := range expectedNames {
		if !names[name] {
			t.Errorf("expected check %q not found", name)
		}
	}
}
