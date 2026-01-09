package setup

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
)

// CopyKey copies an SSH public key to a remote host using ssh-copy-id.
// This enables passwordless authentication to the host.
func CopyKey(host string, keyPath string) error {
	if keyPath == "" {
		// Find the best available key
		key := GetPreferredKey()
		if key == nil {
			return errors.New(errors.ErrSSH,
				"No SSH keys on this machine",
				"Generate one first: rr setup or ssh-keygen -t ed25519")
		}
		keyPath = key.Path
	}

	// Ensure we have the public key path
	pubKeyPath := keyPath
	if !strings.HasSuffix(pubKeyPath, ".pub") {
		pubKeyPath = keyPath + ".pub"
	}

	// Check ssh-copy-id exists
	sshCopyIDPath, err := exec.LookPath("ssh-copy-id")
	if err != nil {
		return errors.New(errors.ErrSSH,
			"Can't find ssh-copy-id",
			"Install OpenSSH, or copy the key manually.")
	}

	// Run ssh-copy-id
	cmd := exec.Command(sshCopyIDPath, "-i", pubKeyPath, host)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(output))

		// Check for common error patterns
		if strings.Contains(outputStr, "Permission denied") {
			return errors.New(errors.ErrSSH,
				fmt.Sprintf("Permission denied on %s", host),
				"Double-check the password or credentials and try again.")
		}
		if strings.Contains(outputStr, "Connection refused") {
			return errors.New(errors.ErrSSH,
				fmt.Sprintf("Connection refused to %s", host),
				"Make sure SSH is running on the remote machine.")
		}
		if strings.Contains(outputStr, "Could not resolve hostname") {
			return errors.New(errors.ErrSSH,
				fmt.Sprintf("Can't resolve hostname %s", host),
				"Check the hostname and your network connection.")
		}

		return errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Couldn't copy SSH key to %s: %s", host, outputStr),
			"Try manually: ssh-copy-id -i "+pubKeyPath+" "+host)
	}

	return nil
}

// CopyKeyManual provides instructions for manual key copying when ssh-copy-id isn't available.
func CopyKeyManual(host string, pubKeyPath string) string {
	pubKey, err := ReadPublicKey(pubKeyPath)
	if err != nil {
		return fmt.Sprintf(`To copy your SSH key manually:

1. Display your public key:
   cat %s

2. Copy the output and add it to the remote host:
   ssh %s "mkdir -p ~/.ssh && chmod 700 ~/.ssh && cat >> ~/.ssh/authorized_keys" << 'EOF'
   <paste your public key here>
   EOF

3. Set correct permissions:
   ssh %s "chmod 600 ~/.ssh/authorized_keys"
`, pubKeyPath, host, host)
	}

	return fmt.Sprintf(`To copy your SSH key manually, run:

ssh %s "mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '%s' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
`, host, pubKey)
}

// TestPasswordlessAuth tests if passwordless authentication works for a host.
// Returns true if we can connect without password prompts.
func TestPasswordlessAuth(host string) (bool, error) {
	// Use SSH with batch mode to disable password prompts
	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=accept-new",
		host,
		"echo ok",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)

		// Check if it's an auth failure vs other issues
		if strings.Contains(outputStr, "Permission denied") {
			return false, nil // Auth failed, but connection worked
		}

		// Other error (network, etc)
		return false, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("SSH connection to %s failed", host),
			"Make sure the host is reachable: ping "+host)
	}

	return strings.TrimSpace(string(output)) == "ok", nil
}
