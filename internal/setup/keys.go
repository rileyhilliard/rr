package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
)

// KeyInfo contains information about an SSH key.
type KeyInfo struct {
	Path       string // Full path to private key
	Type       string // Key type (ed25519, rsa, ecdsa)
	PublicPath string // Path to public key
	HasPublic  bool   // Whether public key file exists
}

// DefaultKeyPaths returns the standard locations for SSH keys.
func DefaultKeyPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	return []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
	}
}

// FindLocalKeys searches for existing SSH keys and returns info about each.
func FindLocalKeys() []KeyInfo {
	var keys []KeyInfo

	for _, path := range DefaultKeyPaths() {
		if _, err := os.Stat(path); err == nil {
			keyType := inferKeyType(path)
			pubPath := path + ".pub"
			_, pubErr := os.Stat(pubPath)

			keys = append(keys, KeyInfo{
				Path:       path,
				Type:       keyType,
				PublicPath: pubPath,
				HasPublic:  pubErr == nil,
			})
		}
	}

	return keys
}

// HasAnyKey returns true if at least one SSH key exists.
func HasAnyKey() bool {
	return len(FindLocalKeys()) > 0
}

// GetPreferredKey returns the best available key (prefers ed25519).
func GetPreferredKey() *KeyInfo {
	keys := FindLocalKeys()
	if len(keys) == 0 {
		return nil
	}

	// Prefer ed25519 > ecdsa > rsa
	for _, key := range keys {
		if key.Type == "ed25519" && key.HasPublic {
			return &key
		}
	}
	for _, key := range keys {
		if key.Type == "ecdsa" && key.HasPublic {
			return &key
		}
	}
	for _, key := range keys {
		if key.HasPublic {
			return &key
		}
	}

	// Return first key if none have public keys
	return &keys[0]
}

// GenerateKey creates a new SSH key pair using ssh-keygen.
// Returns the path to the created private key.
func GenerateKey(path string, keyType string) error {
	if keyType == "" {
		keyType = "ed25519"
	}

	// Validate key type
	validTypes := map[string]bool{
		"ed25519": true,
		"rsa":     true,
		"ecdsa":   true,
	}
	if !validTypes[keyType] {
		return errors.New(errors.ErrSSH,
			fmt.Sprintf("Invalid key type: %s", keyType),
			"Supported types: ed25519 (recommended), rsa, ecdsa")
	}

	// Expand path
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrSSH,
				"Failed to determine home directory",
				"Set HOME environment variable")
		}
		path = filepath.Join(home, path[1:])
	}

	// Ensure .ssh directory exists
	sshDir := filepath.Dir(path)
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Failed to create SSH directory: %s", sshDir),
			"Check permissions on home directory")
	}

	// Check if key already exists
	if _, err := os.Stat(path); err == nil {
		return errors.New(errors.ErrSSH,
			fmt.Sprintf("Key already exists at %s", path),
			"Choose a different path or delete the existing key")
	}

	// Generate key using ssh-keygen
	args := []string{
		"-t", keyType,
		"-f", path,
		"-N", "", // Empty passphrase (user can add one if they want)
		"-C", fmt.Sprintf("rr-generated-%s", keyType),
	}

	// For RSA, specify key size
	if keyType == "rsa" {
		args = append(args, "-b", "4096")
	}

	cmd := exec.Command("ssh-keygen", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Failed to generate SSH key: %s", strings.TrimSpace(string(output))),
			"Ensure ssh-keygen is installed and accessible")
	}

	// Verify the key was created
	if _, err := os.Stat(path); err != nil {
		return errors.New(errors.ErrSSH,
			"Key generation completed but key file not found",
			"Check disk space and permissions")
	}

	return nil
}

// DefaultKeyPath returns the default path for new SSH keys.
func DefaultKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.ssh/id_ed25519"
	}
	return filepath.Join(home, ".ssh", "id_ed25519")
}

// inferKeyType determines key type from filename.
func inferKeyType(path string) string {
	base := filepath.Base(path)
	switch {
	case strings.Contains(base, "ed25519"):
		return "ed25519"
	case strings.Contains(base, "ecdsa"):
		return "ecdsa"
	case strings.Contains(base, "rsa"):
		return "rsa"
	default:
		return "unknown"
	}
}

// ReadPublicKey reads the contents of a public key file.
func ReadPublicKey(pubPath string) (string, error) {
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Failed to read public key: %s", pubPath),
			"Check that the file exists and is readable")
	}
	return strings.TrimSpace(string(data)), nil
}
