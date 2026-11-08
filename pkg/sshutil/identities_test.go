package sshutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestResolveSSHSettings_IdentitiesOnly(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{"unset", "Host target\n  HostName 10.0.0.5\n", false},
		{"yes", "Host target\n  IdentitiesOnly yes\n", true},
		{"no", "Host target\n  IdentitiesOnly no\n", false},
		{"Lima config from #265", strings.Replace(limaConfig, "lima-devbox", "target", 1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSSHConfig(t, tt.config)
			assert.Equal(t, tt.want, resolveSSHSettings("target").identitiesOnly)
		})
	}
}

// writeIdentity writes an unencrypted ed25519 private key to path, with its
// .pub next to it when withPub is set, and returns its signer.
func writeIdentity(t *testing.T, path string, withPub bool) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(block), 0o600))
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	if withPub {
		require.NoError(t, os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o600))
	}
	return signer
}

func TestOnlySigners_KeepsTheHostsIdentities(t *testing.T) {
	dir := t.TempDir()
	fromPub := writeIdentity(t, filepath.Join(dir, "with_pub"), true)
	fromPriv := writeIdentity(t, filepath.Join(dir, "no_pub"), false)
	other := writeIdentity(t, filepath.Join(dir, "other"), true)

	agentKeys := func() ([]ssh.Signer, error) { return []ssh.Signer{other, fromPub, fromPriv}, nil }
	allowed := identityPublicKeys([]string{
		filepath.Join(dir, "with_pub"), filepath.Join(dir, "no_pub"), filepath.Join(dir, "missing"),
	})

	kept, err := onlySigners(agentKeys, allowed)()

	require.NoError(t, err)
	require.Len(t, kept, 2)
	assert.Equal(t, fromPub.PublicKey().Marshal(), kept[0].PublicKey().Marshal())
	assert.Equal(t, fromPriv.PublicKey().Marshal(), kept[1].PublicKey().Marshal())
}

func TestCombinedSigners(t *testing.T) {
	dir := t.TempDir()
	agentKey := writeIdentity(t, filepath.Join(dir, "agent"), false)
	fileKey := writeIdentity(t, filepath.Join(dir, "file"), false)
	keys := func(signers []ssh.Signer) [][]byte {
		var out [][]byte
		for _, s := range signers {
			out = append(out, s.PublicKey().Marshal())
		}
		return out
	}

	tests := []struct {
		name      string
		fromAgent func() ([]ssh.Signer, error)
		want      [][]byte
	}{
		{"agent keys first, then key files",
			func() ([]ssh.Signer, error) { return []ssh.Signer{agentKey}, nil },
			keys([]ssh.Signer{agentKey, fileKey})},
		{"no agent", nil, keys([]ssh.Signer{fileKey})},
		{"an agent that errors still leaves the key files",
			func() ([]ssh.Signer, error) { return nil, stderrors.New("agent gone") },
			keys([]ssh.Signer{fileKey})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := combinedSigners(tt.fromAgent, []ssh.Signer{fileKey})()
			require.NoError(t, err)
			assert.Equal(t, tt.want, keys(got))
		})
	}
}

func TestOnlySigners_PassesAgentErrorsThrough(t *testing.T) {
	agentErr := stderrors.New("agent: connection reset")
	_, err := onlySigners(func() ([]ssh.Signer, error) { return nil, agentErr }, nil)()
	assert.ErrorIs(t, err, agentErr)
}
