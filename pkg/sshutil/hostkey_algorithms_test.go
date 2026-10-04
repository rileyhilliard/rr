package sshutil

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	stderrors "errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Regression: `ssh` records only the host key it negotiated (ed25519), while
// rr offered Go's default order, which puts ecdsa first. A server with both
// sent its ecdsa key, and rr failed with "host key mismatch" for a host the
// user had just accepted. rr must offer the key types known_hosts has.
func TestDial_HostKnownByOneKeyTypeIsAccepted(t *testing.T) {
	withSSHConfig(t, "")
	withTestKey(t)
	port, keys := startMultiKeySSHServer(t)
	home := os.Getenv("HOME")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ssh", "config"),
		[]byte(fmt.Sprintf("Host target\n  HostName 127.0.0.1\n  Port %d\n", port)), 0o600))
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, keys["ed25519"])
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line+"\n"), 0o600))

	_, err := Dial("target", 5*time.Second)

	// The server accepts no credentials, so getting to auth means the host
	// key was verified.
	require.Error(t, err)
	var mismatch *HostKeyMismatchError
	assert.False(t, stderrors.As(err, &mismatch), "host key verified: %v", err)
	assert.Contains(t, err.Error(), "unable to authenticate")
}

func TestKnownHostKeyAlgorithms(t *testing.T) {
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	edKey, err := ssh.NewPublicKey(edPriv.Public())
	require.NoError(t, err)
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rsaKey, err := ssh.NewPublicKey(&rsaPriv.PublicKey)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	content := knownhosts.Line([]string{"10.0.0.5"}, edKey) + "\n" +
		knownhosts.Line([]string{"10.0.0.5"}, rsaKey) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	assert.Equal(t,
		[]string{ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
		knownHostKeyAlgorithms(path, "10.0.0.5:22"))
	assert.Nil(t, knownHostKeyAlgorithms(path, "10.0.0.6:22"), "unknown host keeps the default order")
	assert.Nil(t, knownHostKeyAlgorithms(filepath.Join(dir, "missing"), "10.0.0.5:22"))
}

// startMultiKeySSHServer runs an SSH server on localhost with an ecdsa and
// an ed25519 host key that accepts no credentials, and returns its port and
// public keys by type.
func startMultiKeySSHServer(t *testing.T) (int, map[string]ssh.PublicKey) {
	t.Helper()
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, fmt.Errorf("denied")
		},
	}
	keys := map[string]ssh.PublicKey{}
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	for name, priv := range map[string]interface{}{"ecdsa": ecPriv, "ed25519": edPriv} {
		signer, err := ssh.NewSignerFromKey(priv)
		require.NoError(t, err)
		config.AddHostKey(signer)
		keys[name] = signer.PublicKey()
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _, _, _ = ssh.NewServerConn(c, config)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, keys
}
