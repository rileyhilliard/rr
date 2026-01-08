package doctor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SSHKeyCheck verifies an SSH key exists.
type SSHKeyCheck struct{}

func (c *SSHKeyCheck) Name() string     { return "ssh_key" }
func (c *SSHKeyCheck) Category() string { return "SSH" }

func (c *SSHKeyCheck) Run() CheckResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    "Cannot determine home directory",
			Suggestion: "Check HOME environment variable",
		}
	}

	// Check common key locations in order of preference
	keyPaths := []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
	}

	for _, keyPath := range keyPaths {
		pubKeyPath := keyPath + ".pub"
		if _, err := os.Stat(pubKeyPath); err == nil {
			keyName := filepath.Base(pubKeyPath)
			return CheckResult{
				Name:    c.Name(),
				Status:  StatusPass,
				Message: fmt.Sprintf("SSH key found: ~/.ssh/%s", keyName),
			}
		}
	}

	return CheckResult{
		Name:       c.Name(),
		Status:     StatusFail,
		Message:    "No SSH key found",
		Suggestion: "Generate a key with: ssh-keygen -t ed25519",
		Fixable:    true,
	}
}

func (c *SSHKeyCheck) Fix() error {
	// Could generate a key, but that's probably too invasive for auto-fix
	return nil
}

// SSHAgentCheck verifies the SSH agent is running.
type SSHAgentCheck struct{}

func (c *SSHAgentCheck) Name() string     { return "ssh_agent" }
func (c *SSHAgentCheck) Category() string { return "SSH" }

func (c *SSHAgentCheck) Run() CheckResult {
	// Check if SSH_AUTH_SOCK is set
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    "SSH agent not running",
			Suggestion: "Fix: eval $(ssh-agent) && ssh-add",
			Fixable:    true,
		}
	}

	// Verify we can connect to the socket
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    "SSH agent socket not accessible",
			Suggestion: "Fix: eval $(ssh-agent) && ssh-add",
			Fixable:    true,
		}
	}
	conn.Close() //nolint:errcheck // Best-effort close, error not actionable

	// Check how many keys are loaded
	cmd := exec.Command("ssh-add", "-l")
	output, err := cmd.Output()
	if err != nil {
		// Exit code 1 means no keys loaded
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return CheckResult{
				Name:       c.Name(),
				Status:     StatusWarn,
				Message:    "SSH agent running but no keys loaded",
				Suggestion: "Add a key with: ssh-add",
				Fixable:    true,
			}
		}
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    "Cannot query SSH agent",
			Suggestion: "Check SSH agent: ssh-add -l",
		}
	}

	// Count keys
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	keyCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			keyCount++
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: fmt.Sprintf("SSH agent running with %d key%s loaded", keyCount, pluralize(keyCount)),
	}
}

func (c *SSHAgentCheck) Fix() error {
	// Running ssh-add would require interaction, so not auto-fixable
	return nil
}

// SSHKeyPermissionsCheck verifies SSH key file permissions.
type SSHKeyPermissionsCheck struct{}

func (c *SSHKeyPermissionsCheck) Name() string     { return "ssh_key_permissions" }
func (c *SSHKeyPermissionsCheck) Category() string { return "SSH" }

func (c *SSHKeyPermissionsCheck) Run() CheckResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return CheckResult{
			Name:   c.Name(),
			Status: StatusPass, // Skip if we can't check
		}
	}

	// Check common private key locations
	keyPaths := []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
	}

	var badPerms []string
	var foundKey bool

	for _, keyPath := range keyPaths {
		info, err := os.Stat(keyPath)
		if err != nil {
			continue // Key doesn't exist
		}
		foundKey = true

		// Check permissions (should be 0600 or 0400)
		perm := info.Mode().Perm()
		if perm&0077 != 0 {
			badPerms = append(badPerms, filepath.Base(keyPath))
		}
	}

	if !foundKey {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass, // SSH key check will catch this
			Message: "No private keys to check",
		}
	}

	if len(badPerms) > 0 {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusWarn,
			Message:    fmt.Sprintf("Insecure permissions on: %v", badPerms),
			Suggestion: "Fix: chmod 600 ~/.ssh/<keyfile>",
			Fixable:    true,
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: "SSH key permissions OK",
	}
}

func (c *SSHKeyPermissionsCheck) Fix() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	keyPaths := []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
	}

	for _, keyPath := range keyPaths {
		info, err := os.Stat(keyPath)
		if err != nil {
			continue
		}

		perm := info.Mode().Perm()
		if perm&0077 != 0 {
			if err := os.Chmod(keyPath, 0600); err != nil {
				return fmt.Errorf("failed to fix permissions on %s: %w", keyPath, err)
			}
		}
	}

	return nil
}

// NewSSHChecks creates all SSH-related checks.
func NewSSHChecks() []Check {
	return []Check{
		&SSHKeyCheck{},
		&SSHAgentCheck{},
		&SSHKeyPermissionsCheck{},
	}
}
