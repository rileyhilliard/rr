// Package setup handles SSH key management for Road Runner.
//
// The package provides functionality for discovering existing SSH keys,
// generating new key pairs, and deploying public keys to remote hosts.
// It wraps the standard ssh-keygen and ssh-copy-id tools with error
// handling and user-friendly messages.
//
// # Key Discovery
//
// FindLocalKeys() searches standard locations for existing SSH keys:
//
//	~/.ssh/id_ed25519
//	~/.ssh/id_rsa
//	~/.ssh/id_ecdsa
//
// GetPreferredKey() returns the best available key, preferring ed25519
// over ECDSA over RSA (following modern security recommendations).
//
// # Key Generation
//
// GenerateKey() creates a new SSH key pair using ssh-keygen:
//
//	err := setup.GenerateKey("~/.ssh/id_ed25519", "ed25519")
//
// Supported key types:
//
//	ed25519 - Recommended. Fast, secure, small keys.
//	ecdsa   - Good alternative. Uses elliptic curves.
//	rsa     - Legacy compatibility. Uses 4096-bit keys.
//
// Keys are generated with an empty passphrase by default. Users can
// add a passphrase manually if desired using ssh-keygen -p.
//
// # Key Deployment
//
// CopyKey() deploys a public key to a remote host using ssh-copy-id:
//
//	err := setup.CopyKey("user@hostname", "~/.ssh/id_ed25519")
//
// This is equivalent to the ssh-copy-id command and enables passwordless
// authentication to the remote host.
//
// If ssh-copy-id is unavailable, CopyKeyManual() returns instructions
// for manual key deployment.
//
// # Connection Testing
//
// TestPasswordlessAuth() verifies that passwordless authentication works:
//
//	ok, err := setup.TestPasswordlessAuth("user@hostname")
//
// It uses SSH batch mode to prevent password prompts, returning false
// if authentication fails (but connection succeeded) or an error for
// network/connectivity issues.
//
// # Security Notes
//
// This package handles cryptographic key material. Key files are created
// with restrictive permissions (0600 for private keys, 0700 for .ssh dir).
// The package never logs or displays private key contents.
//
// Empty passphrases are used for convenience in automation scenarios.
// For higher security environments, users should add passphrases manually
// and use ssh-agent for key management.
package setup
