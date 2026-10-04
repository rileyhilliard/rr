package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/rileyhilliard/rr/internal/host"
)

// withJumpHostConfig sets up a HOME whose ~/.ssh/config reaches each target
// through jump host bastion, a key so the dial gets to the proxy, and a
// stand-in for the system ssh that fails the way ssh does when bastion
// doesn't resolve. The system ssh is the one dependency replaced.
func withJumpHostConfig(t *testing.T, targets ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ssh"), 0o700))
	var config strings.Builder
	for _, target := range targets {
		config.WriteString("Host " + target + "\n  HostName 10.0.0.5\n  ProxyJump bastion\n\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(config.String()), 0o600))

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), pem.EncodeToMemory(block), 0o600))

	bin := t.TempDir()
	fake := "#!/bin/sh\necho 'ssh: Could not resolve hostname bastion: nodename nor servname provided' >&2\nexit 255\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ssh"), []byte(fake), 0o755)) // #nosec G306 -- stand-in must be executable
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func dialHost(t *testing.T, name string, aliases ...string) error {
	t.Helper()
	_, err := host.DialAliases(name, aliases, host.DialOptions{Timeout: 2 * time.Second, Grace: 10 * time.Millisecond})
	require.Error(t, err)
	return err
}

// A host that fails in its jump host: the error, from the real dial through
// the connect error, names the jump host and carries what ssh printed.
func TestConnectionError_JumpHostFailureEndToEnd(t *testing.T) {
	withJumpHostConfig(t, "m1-tailscale")

	err := buildConnectionError([]hostAttempt{
		{hostName: "m1-mini", connErr: dialHost(t, "m1-mini", "m1-tailscale")},
	})

	result := ErrorToJSON(err)
	assert.Equal(t, ErrCodeSSHConnectionFail, result.Code)
	assert.Contains(t, result.Message, "m1-mini")
	assert.Contains(t, err.Error(), "hostname not found via jump host 'bastion' (ssh: Could not resolve hostname bastion")
}

// Regression: with several project hosts, the "couldn't connect to any host"
// error dropped every host's failure. Each host's must be kept as the cause.
func TestConnectionError_AllHostsUnreachableKeepsEachFailure(t *testing.T) {
	withJumpHostConfig(t, "a-jump", "b-jump")

	err := buildConnectionError([]hostAttempt{
		{hostName: "a", connErr: dialHost(t, "a", "a-jump")},
		{hostName: "b", connErr: dialHost(t, "b", "b-jump")},
	})

	result := ErrorToJSON(err)
	assert.Equal(t, ErrCodeSSHConnectionFail, result.Code)
	assert.Equal(t, "Couldn't connect to any host (tried: a, b)", result.Message)
	for _, alias := range []string{"a-jump", "b-jump"} {
		assert.Contains(t, err.Error(), "probe "+alias+" failed: hostname not found via jump host 'bastion'")
	}
}

// rr doctor names the jump host on the alias's line.
func TestDoctorProbeOutput_JumpHostFailure(t *testing.T) {
	withJumpHostConfig(t, "m1-tailscale")
	_, err := host.Probe("m1-tailscale", 2*time.Second)
	require.Error(t, err)

	assert.Equal(t, "Hostname not found via jump host 'bastion'", formatProbeError(err))
	suggestion := getSSHErrorSuggestion(err, "m1-tailscale")
	assert.Equal(t, "The failure was in its jump host 'bastion'. Check that first, then try: ssh -v m1-tailscale", suggestion)
}
