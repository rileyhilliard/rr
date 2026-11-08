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
		{"true, any case", "Host target\n  IdentitiesOnly TRUE\n", true},
		{"Yes, any case", "Host target\n  IdentitiesOnly Yes\n", true},
		{"Lima config from #265", strings.Replace(limaConfig, "lima-devbox", "target", 1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSSHConfig(t, tt.config)
			assert.Equal(t, tt.want, resolveSSHSettings("target").identitiesOnly)
		})
	}
}

func TestResolveSSHSettings_IdentityFiles(t *testing.T) {
	withSSHConfig(t, "Host target\n  IdentityFile ~/.ssh/one\n  IdentityFile /keys/two\n\n"+
		"Host *\n  IdentityFile ~/.ssh/one\n  IdentityFile ~/.ssh/three\n")
	home := os.Getenv("HOME")

	got := resolveSSHSettings("target").identityFiles

	assert.Equal(t, []string{
		filepath.Join(home, ".ssh", "one"), "/keys/two", filepath.Join(home, ".ssh", "three"),
	}, got, "every IdentityFile, in order, without repeats")
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

	// An encrypted key with no .pub: its public key is still readable, and
	// the agent copy is the one an IdentitiesOnly host needs.
	_, encPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	encBlock, err := ssh.MarshalPrivateKeyWithPassphrase(encPriv, "", []byte("secret"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "encrypted"), pem.EncodeToMemory(encBlock), 0o600))
	encrypted, err := ssh.NewSignerFromKey(encPriv)
	require.NoError(t, err)

	agentKeys := func() ([]ssh.Signer, error) { return []ssh.Signer{other, fromPub, fromPriv, encrypted}, nil }
	allowed := identityPublicKeys([]string{
		filepath.Join(dir, "with_pub"), filepath.Join(dir, "no_pub"), filepath.Join(dir, "missing"),
		filepath.Join(dir, "encrypted"),
	})

	kept, err := onlySigners(agentKeys, allowed)()

	require.NoError(t, err)
	require.Len(t, kept, 3)
	assert.Equal(t, fromPub.PublicKey().Marshal(), kept[0].PublicKey().Marshal())
	assert.Equal(t, fromPriv.PublicKey().Marshal(), kept[1].PublicKey().Marshal())
	assert.Equal(t, encrypted.PublicKey().Marshal(), kept[2].PublicKey().Marshal())
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
		name        string
		fromAgent   func() ([]ssh.Signer, error)
		files       []ssh.Signer
		want        [][]byte
		wantWarning string
	}{
		{name: "agent keys first, then key files",
			fromAgent: func() ([]ssh.Signer, error) { return []ssh.Signer{agentKey}, nil },
			files:     []ssh.Signer{fileKey},
			want:      keys([]ssh.Signer{agentKey, fileKey})},
		{name: "no agent", files: []ssh.Signer{fileKey}, want: keys([]ssh.Signer{fileKey})},
		{name: "an agent that errors still leaves the key files, with a warning",
			fromAgent:   func() ([]ssh.Signer, error) { return nil, stderrors.New("agent gone") },
			files:       []ssh.Signer{fileKey},
			want:        keys([]ssh.Signer{fileKey}),
			wantWarning: "/tmp/agent.sock (agent gone)"},
		{name: "a key file the agent holds is offered once",
			fromAgent: func() ([]ssh.Signer, error) { return []ssh.Signer{agentKey}, nil },
			files:     []ssh.Signer{agentKey, fileKey},
			want:      keys([]ssh.Signer{agentKey, fileKey})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := captureWarnings(t)
			got, err := combinedSigners("/tmp/agent.sock", tt.fromAgent, tt.files)()
			require.NoError(t, err)
			assert.Equal(t, tt.want, keys(got))
			if tt.wantWarning == "" {
				assert.Empty(t, warnings())
			} else {
				require.Len(t, warnings(), 1)
				assert.Contains(t, warnings()[0], tt.wantWarning)
			}
		})
	}
}

func TestOnlySigners_PassesAgentErrorsThrough(t *testing.T) {
	agentErr := stderrors.New("agent: connection reset")
	_, err := onlySigners(func() ([]ssh.Signer, error) { return nil, agentErr }, nil)()
	assert.ErrorIs(t, err, agentErr)
}
