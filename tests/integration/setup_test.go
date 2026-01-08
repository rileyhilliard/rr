package integration

import (
	"os"
	"path/filepath"
	"testing"
)

// SkipIfNoSSH skips the current test if SSH tests are disabled.
// Set RR_TEST_SKIP_SSH=1 to skip SSH-dependent tests.
func SkipIfNoSSH(t *testing.T) {
	t.Helper()
	if os.Getenv("RR_TEST_SKIP_SSH") == "1" {
		t.Skip("Skipping SSH test: RR_TEST_SKIP_SSH=1")
	}
}

// GetTestSSHHost returns the SSH host configured for testing.
// Defaults to "localhost" if RR_TEST_SSH_HOST is not set.
func GetTestSSHHost() string {
	host := os.Getenv("RR_TEST_SSH_HOST")
	if host == "" {
		return "localhost"
	}
	return host
}

// GetTestSSHUser returns the SSH user configured for testing.
// Defaults to the current user if RR_TEST_SSH_USER is not set.
func GetTestSSHUser() string {
	user := os.Getenv("RR_TEST_SSH_USER")
	if user == "" {
		return os.Getenv("USER")
	}
	return user
}

// GetTestSSHKey returns the path to the SSH key for testing.
// Defaults to ~/.ssh/id_rsa if RR_TEST_SSH_KEY is not set.
func GetTestSSHKey() string {
	key := os.Getenv("RR_TEST_SSH_KEY")
	if key == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".ssh", "id_rsa")
	}
	return key
}

// TempSyncDir creates a temporary directory for sync tests.
// The directory is automatically cleaned up when the test completes.
func TempSyncDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

// TempSyncDirWithFiles creates a temporary directory with test files.
// Returns the directory path. Cleanup is automatic.
func TempSyncDirWithFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := TempSyncDir(t, "rr-sync-test-")

	for name, content := range files {
		path := filepath.Join(dir, name)
		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", name, err)
		}
	}

	return dir
}

// TestSetupHelpers verifies the test helpers work correctly.
// This is a placeholder test that uses the helpers defined above.
func TestSetupHelpers(t *testing.T) {
	t.Run("GetTestSSHHost returns default", func(t *testing.T) {
		// Temporarily unset the env var
		orig := os.Getenv("RR_TEST_SSH_HOST")
		_ = os.Unsetenv("RR_TEST_SSH_HOST")
		defer func() { _ = os.Setenv("RR_TEST_SSH_HOST", orig) }()

		host := GetTestSSHHost()
		if host != "localhost" {
			t.Errorf("Expected localhost, got %s", host)
		}
	})

	t.Run("GetTestSSHHost returns env value", func(t *testing.T) {
		_ = os.Setenv("RR_TEST_SSH_HOST", "testhost:2222")
		defer func() { _ = os.Unsetenv("RR_TEST_SSH_HOST") }()

		host := GetTestSSHHost()
		if host != "testhost:2222" {
			t.Errorf("Expected testhost:2222, got %s", host)
		}
	})

	t.Run("TempSyncDir creates and cleans up directory", func(t *testing.T) {
		dir := TempSyncDir(t, "test-")
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("Temp directory was not created: %s", dir)
		}
		// Cleanup happens automatically via t.Cleanup
	})

	t.Run("TempSyncDirWithFiles creates files", func(t *testing.T) {
		files := map[string]string{
			"hello.txt":        "Hello, world!",
			"subdir/nested.go": "package main",
		}
		dir := TempSyncDirWithFiles(t, files)

		for name, expectedContent := range files {
			path := filepath.Join(dir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("Failed to read %s: %v", name, err)
				continue
			}
			if string(content) != expectedContent {
				t.Errorf("File %s: expected %q, got %q", name, expectedContent, string(content))
			}
		}
	})
}

// TestSkipSSH demonstrates the SkipIfNoSSH helper.
// This test will be skipped when RR_TEST_SKIP_SSH=1.
func TestSkipSSH(t *testing.T) {
	SkipIfNoSSH(t)

	// This code only runs if SSH tests are enabled
	t.Log("SSH tests are enabled, host:", GetTestSSHHost())
}
