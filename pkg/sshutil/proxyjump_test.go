package sshutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	stderrors "errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/rileyhilliard/rr/internal/errors"
)

// withSSHConfig points HOME at a temp dir holding config as ~/.ssh/config,
// and puts a stand-in ssh first on PATH that prints its arguments, one per
// line, then relays stdin to stdout like ssh -W does. The system ssh is the
// one external dependency these tests replace.
func withSSHConfig(t *testing.T, config string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ssh"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(config), 0o600))
	withFakeSSH(t, "for a in \"$@\"; do echo \"$a\"; done\necho END\ncat")
}

// withFakeSSH puts a stand-in ssh with the given sh body first on PATH.
func withFakeSSH(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\n"+body+"\n"), 0o755)) // #nosec G306 -- stand-in must be executable
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// proxyArgs starts host's proxy the way Dial does and returns the lines it
// printed before END: the arguments the stand-in ssh was started with.
func proxyArgs(t *testing.T, host string) []string {
	t.Helper()
	settings := resolveSSHSettings(host)
	conn, err := startProxy(proxyCmd(host, settings), settings)
	require.NoError(t, err)
	defer conn.Close()

	var out strings.Builder
	buf := make([]byte, 512)
	for !strings.Contains(out.String(), "END\n") {
		n, err := conn.Read(buf)
		require.NoError(t, err)
		out.Write(buf[:n])
	}
	return strings.Split(strings.TrimSuffix(out.String(), "\nEND\n"), "\n")
}

// Regression: rr warned "ProxyJump ... not yet supported" (on every monitor
// refresh) and dialed the target directly, which fails when it's only
// reachable through the jump host. ProxyJump must connect through the jump
// host, as OpenSSH does, without prompting.
func TestResolveSSHSettings_ProxyJumpConnectsThroughJumpHost(t *testing.T) {
	opts := []string{"-o", "BatchMode=yes", "-o", "LogLevel=ERROR"}
	tests := []struct {
		name      string
		proxyJump string
		want      []string
	}{
		{"alias", "bastion", []string{"-W", "[10.0.0.5]:22", "bastion"}},
		{"user and port", "admin@bastion.example.com:2222", []string{"-W", "[10.0.0.5]:22", "-p", "2222", "admin@bastion.example.com"}},
		{"bracketed IPv6 with port", "admin@[fd00::1]:2222", []string{"-W", "[10.0.0.5]:22", "-p", "2222", "admin@fd00::1"}},
		{"bracketed IPv6 without port", "[fd00::1]", []string{"-W", "[10.0.0.5]:22", "fd00::1"}},
		{"ssh URI", "ssh://admin@bastion:2222", []string{"-W", "[10.0.0.5]:22", "ssh://admin@bastion:2222"}},
		{"chain", "first, second", []string{"-J", "first", "-W", "[10.0.0.5]:22", "second"}},
		{"longer chain", "a,b,c", []string{"-J", "a,b", "-W", "[10.0.0.5]:22", "c"}},
		{"non-numeric port stays in the host", "bastion:$(id)", []string{"-W", "[10.0.0.5]:22", "bastion:$(id)"}},
		{"option-like jump host", "-oProxyCommand=evil", []string{"-W", "[10.0.0.5]:22", "-oProxyCommand=evil"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSSHConfig(t, "Host target\n  HostName 10.0.0.5\n  ProxyJump "+tt.proxyJump+"\n")

			// The last jump host goes after "--", so it's never read as an option.
			want := append(append([]string{}, opts...), tt.want[:len(tt.want)-1]...)
			want = append(want, "--", tt.want[len(tt.want)-1])
			assert.Equal(t, want, proxyArgs(t, "target"))
		})
	}
}

// The jump ssh runs without a shell, so a ProxyJump value is never
// interpreted: it reaches ssh as one argument and runs nothing.
func TestResolveSSHSettings_ProxyJumpRunsNoShell(t *testing.T) {
	withSSHConfig(t, "")
	marker := filepath.Join(t.TempDir(), "ran")
	value := fmt.Sprintf("bastion';touch %s;'", marker)
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ssh", "config"),
		[]byte("Host target\n  HostName 10.0.0.5\n  ProxyJump "+value+"\n"), 0o600))

	args := proxyArgs(t, "target")

	assert.Equal(t, value, args[len(args)-1])
	assert.NoFileExists(t, marker)
}

func TestResolveSSHSettings_ProxyJumpWarnsNoMore(t *testing.T) {
	withSSHConfig(t, "Host target\n  HostName 10.0.0.5\n  ProxyJump bastion\n")
	var warnings []string
	WarningHandler = func(m string) { warnings = append(warnings, m) }
	t.Cleanup(func() { WarningHandler = nil })

	args := proxyArgs(t, "target")

	assert.Empty(t, warnings)
	assert.Equal(t, "bastion", args[len(args)-1])
}

// "none" turns a proxy off, and a ProxyCommand on the host beats ProxyJump.
func TestDial_ProxyNoneAndProxyCommandPrecedence(t *testing.T) {
	withSSHConfig(t, `Host nojump
  HostName 127.0.0.1
  Port 1
  ProxyJump none

Host nocommand
  HostName 127.0.0.1
  Port 1
  ProxyCommand none

Host both
  HostName 10.0.0.7
  ProxyCommand echo via-proxycommand; echo END; cat
  ProxyJump bastion
`)
	withTestKey(t)

	for _, host := range []string{"nojump", "nocommand"} {
		_, err := Dial(host, time.Second)
		require.Error(t, err)
		var pe *ProxyError
		assert.False(t, stderrors.As(err, &pe), "%s dials directly, got %v", host, err)
		assert.Contains(t, err.Error(), "Can't reach '"+host+"' at 127.0.0.1:1")
	}

	assert.Equal(t, []string{"via-proxycommand"}, proxyArgs(t, "both"))
}

// Regression: a jump host that failed slower than 100ms lost its error and
// was reported as "SSH handshake ... didn't go through. Try: ssh <host>";
// one that hung was blamed on the target not running SSH. Each failure must
// name the jump host (or ProxyCommand) and carry what ssh printed.
func TestDial_ProxyFailureNamesTheProxyAndWhy(t *testing.T) {
	tests := []struct {
		name         string
		config       string
		fakeSSH      string // body of the stand-in ssh
		wantMessage  string
		wantCause    string
		wantSuggests []string
	}{
		{
			name:         "jump host fails at once",
			config:       "Host target\n  HostName 10.0.0.5\n  ProxyJump bastion\n",
			fakeSSH:      "echo 'ssh: Could not resolve hostname bastion' >&2; exit 255",
			wantMessage:  "Couldn't reach 'target' through jump host 'bastion'",
			wantCause:    "Could not resolve hostname bastion",
			wantSuggests: []string{"(ssh bastion)", "10.0.0.5:22"},
		},
		{
			name:         "jump host fails after the dial has started",
			config:       "Host target\n  HostName 10.0.0.5\n  ProxyJump bastion\n",
			fakeSSH:      "sleep 0.3; echo 'channel 0: open failed: connect failed: No route to host' >&2; exit 255",
			wantMessage:  "Couldn't reach 'target' through jump host 'bastion'",
			wantCause:    "No route to host",
			wantSuggests: []string{"(ssh bastion)", "10.0.0.5:22"},
		},
		{
			name:         "nothing answers through the jump host",
			config:       "Host target\n  HostName 10.0.0.5\n  ProxyJump admin@bastion:2222\n",
			fakeSSH:      "exec sleep 30",
			wantMessage:  "Timed out reaching 'target' through jump host 'admin@bastion:2222'",
			wantCause:    "no SSH response",
			wantSuggests: []string{"within 1s", "(ssh -p 2222 admin@bastion)", "10.0.0.5:22"},
		},
		{
			name:         "jump host prints a banner, then nothing answers",
			config:       "Host target\n  HostName 10.0.0.5\n  ProxyJump bastion\n",
			fakeSSH:      "echo 'Authorized users only' >&2; exec sleep 30",
			wantMessage:  "Timed out reaching 'target' through jump host 'bastion'",
			wantCause:    "no SSH response through the proxy before the timeout; it printed: Authorized users only",
			wantSuggests: []string{"within 1s", "(ssh bastion)"},
		},
		{
			name:         "ProxyCommand fails",
			config:       "Host target\n  HostName 10.0.0.5\n  ProxyCommand ssh -W %h:%p bastion\n",
			fakeSSH:      "sleep 0.3; echo 'bastion: Connection refused' >&2; exit 255",
			wantMessage:  "Couldn't reach 'target' through its ProxyCommand",
			wantCause:    "Connection refused",
			wantSuggests: []string{"ProxyCommand for 'target'", "ssh target"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSSHConfig(t, tt.config)
			withTestKey(t)
			withFakeSSH(t, tt.fakeSSH)

			_, err := Dial("target", time.Second)

			require.Error(t, err)
			assert.True(t, errors.IsCode(err, errors.ErrSSH), "got %v", err)
			var pe *ProxyError
			require.ErrorAs(t, err, &pe, "callers can tell a proxy failure from a handshake failure")
			msg := err.Error()
			assert.Contains(t, msg, tt.wantMessage)
			assert.Contains(t, msg, tt.wantCause)
			for _, s := range tt.wantSuggests {
				assert.Contains(t, msg, s)
			}
			assert.NotContains(t, msg, "<host>")
			assert.NotContains(t, msg, "Killed by signal")
		})
	}
}

// When the jump host works and the target itself refuses the handshake, the
// error is the target's (unknown host key, auth), not the jump host's.
func TestDial_TargetHandshakeFailureThroughJumpHostIsNotBlamedOnIt(t *testing.T) {
	if _, err := exec.LookPath("nc"); err != nil {
		t.Skip("requires nc")
	}
	tests := []struct {
		name        string
		knownHost   bool
		closeAtOnce bool // the target drops the connection without a word
		wantMessage string
		wantSuggest string
	}{
		{"unknown host key", false, false, "SSH handshake with 'target' didn't go through", "ssh -o StrictHostKeyChecking=accept-new target exit"},
		{"auth rejected", true, false, "SSH handshake with 'target' didn't go through", "Auth failed"},
		{"target closes the connection", true, true, "'target' closed the connection before the SSH handshake", "isn't refusing this connection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSSHConfig(t, "Host target\n  HostName 10.0.0.5\n  ProxyJump bastion\n")
			withTestKey(t)
			port, hostKey := startRejectingSSHServer(t, tt.closeAtOnce)
			if tt.knownHost {
				line := knownhosts.Line([]string{"10.0.0.5"}, hostKey)
				require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts"), []byte(line+"\n"), 0o600))
			}
			// The jump host works: it relays to the in-process server.
			withFakeSSH(t, fmt.Sprintf("exec nc 127.0.0.1 %d", port))

			_, err := Dial("target", 5*time.Second)

			require.Error(t, err)
			var pe *ProxyError
			assert.False(t, stderrors.As(err, &pe), "the jump host isn't at fault: %v", err)
			msg := err.Error()
			assert.Contains(t, msg, tt.wantMessage)
			assert.Contains(t, msg, tt.wantSuggest)
			assert.NotContains(t, msg, "through jump host")
		})
	}
}

// Regression: a missing system ssh was reported as the jump host failing.
func TestDial_ProxyJumpWithoutSSHSaysSo(t *testing.T) {
	withSSHConfig(t, "Host target\n  HostName 10.0.0.5\n  ProxyJump bastion\n")
	withTestKey(t)
	t.Setenv("PATH", t.TempDir())

	_, err := Dial("target", time.Second)

	require.Error(t, err)
	var pe *ProxyError
	assert.False(t, stderrors.As(err, &pe), "nothing was dialed: %v", err)
	assert.Contains(t, err.Error(), "Couldn't run ssh to reach 'target' through jump host 'bastion'")
	assert.Contains(t, err.Error(), "ssh is on PATH")
}

// startRejectingSSHServer runs an SSH server on localhost that accepts no
// credentials, or with closeAtOnce drops each connection as soon as it's
// accepted, and returns its port and host key.
func startRejectingSSHServer(t *testing.T, closeAtOnce bool) (int, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, fmt.Errorf("denied")
		},
	}
	config.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if closeAtOnce {
				c.Close()
				continue
			}
			go func() {
				defer c.Close()
				_, _, _, _ = ssh.NewServerConn(c, config)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, signer.PublicKey()
}

// withTestKey writes a key under HOME and hides the agent, so Dial gets past
// auth setup to the proxy whatever the environment's ssh-agent holds.
func withTestKey(t *testing.T) {
	t.Helper()
	t.Setenv("SSH_AUTH_SOCK", "")
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519"), pem.EncodeToMemory(block), 0o600))
}
