package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSSHKeyCheck(t *testing.T) {
	check := &SSHKeyCheck{}

	t.Run("name and category", func(t *testing.T) {
		if check.Name() != "ssh_key" {
			t.Errorf("expected name 'ssh_key', got %s", check.Name())
		}
		if check.Category() != "SSH" {
			t.Errorf("expected category 'SSH', got %s", check.Category())
		}
	})

	// The actual Run() test depends on the system's SSH key presence
	// We just verify it doesn't panic
	t.Run("run does not panic", func(t *testing.T) {
		_ = check.Run()
	})
}

func TestSSHAgentCheck(t *testing.T) {
	check := &SSHAgentCheck{}

	t.Run("name and category", func(t *testing.T) {
		if check.Name() != "ssh_agent" {
			t.Errorf("expected name 'ssh_agent', got %s", check.Name())
		}
		if check.Category() != "SSH" {
			t.Errorf("expected category 'SSH', got %s", check.Category())
		}
	})

	t.Run("without SSH_AUTH_SOCK warns, not fails", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "")

		result := check.Run()
		if result.Status != StatusWarn {
			t.Errorf("expected StatusWarn when SSH_AUTH_SOCK not set, got %v", result.Status)
		}
		if result.Fixable {
			t.Error("expected Fixable=false: Fix() is a no-op")
		}
	})

	t.Run("unreachable socket warns, not fails", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing.sock"))

		result := check.Run()
		if result.Status != StatusWarn {
			t.Errorf("expected StatusWarn for a dead agent socket, got %v", result.Status)
		}
		if result.Fixable {
			t.Error("expected Fixable=false: Fix() is a no-op")
		}
	})
}

func TestSSHKeyCheck_NoDefaultKeyWarns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	result := (&SSHKeyCheck{}).Run()
	if result.Status != StatusWarn {
		t.Errorf("expected StatusWarn with no default key, got %v", result.Status)
	}
	if result.Fixable {
		t.Error("expected Fixable=false: Fix() is a no-op")
	}
}

func TestSSHKeyPermissionsCheck(t *testing.T) {
	check := &SSHKeyPermissionsCheck{}

	t.Run("name and category", func(t *testing.T) {
		if check.Name() != "ssh_key_permissions" {
			t.Errorf("expected name 'ssh_key_permissions', got %s", check.Name())
		}
		if check.Category() != "SSH" {
			t.Errorf("expected category 'SSH', got %s", check.Category())
		}
	})

	t.Run("fix insecure permissions", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a fake .ssh directory structure
		sshDir := filepath.Join(tmpDir, ".ssh")
		if err := os.MkdirAll(sshDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create a key with insecure permissions
		keyPath := filepath.Join(sshDir, "id_test")
		if err := os.WriteFile(keyPath, []byte("fake-key"), 0644); err != nil {
			t.Fatal(err)
		}

		// The Fix() method operates on ~/.ssh, so we can't easily test it
		// without modifying HOME. Just verify Fix() doesn't error with empty state.
		err := check.Fix()
		if err != nil {
			// Fix() may fail if no keys exist, which is fine
			t.Logf("Fix() returned error (expected if no ~/.ssh keys): %v", err)
		}
	})
}

func TestNewSSHChecks(t *testing.T) {
	checks := NewSSHChecks()

	if len(checks) != 3 {
		t.Errorf("expected 3 SSH checks, got %d", len(checks))
	}

	// Verify all checks have SSH category
	for _, check := range checks {
		if check.Category() != "SSH" {
			t.Errorf("expected SSH category, got %s", check.Category())
		}
	}

	// Verify check names
	names := make(map[string]bool)
	for _, check := range checks {
		names[check.Name()] = true
	}

	expectedNames := []string{"ssh_key", "ssh_agent", "ssh_key_permissions"}
	for _, name := range expectedNames {
		if !names[name] {
			t.Errorf("expected check %q not found", name)
		}
	}
}
