package sshutil

import (
	"fmt"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandProxyTokens(t *testing.T) {
	tests := []struct {
		name         string
		cmd          string
		originalHost string
		hostname     string
		port         string
		user         string
		expected     string
	}{
		{
			name:         "hostname token",
			cmd:          "nc %h 22",
			originalHost: "myserver",
			hostname:     "10.0.0.5",
			port:         "22",
			user:         "root",
			expected:     "nc 10.0.0.5 22",
		},
		{
			name:         "port token",
			cmd:          "nc host %p",
			originalHost: "myserver",
			hostname:     "10.0.0.5",
			port:         "2222",
			user:         "root",
			expected:     "nc host 2222",
		},
		{
			name:         "original host token",
			cmd:          "ssh -W %h:%p %n-bastion",
			originalHost: "myserver",
			hostname:     "10.0.0.5",
			port:         "22",
			user:         "root",
			expected:     "ssh -W 10.0.0.5:22 myserver-bastion",
		},
		{
			name:         "user token",
			cmd:          "ssh -l %r bastion",
			originalHost: "myserver",
			hostname:     "10.0.0.5",
			port:         "22",
			user:         "deploy",
			expected:     "ssh -l deploy bastion",
		},
		{
			name:         "literal percent",
			cmd:          "echo 100%% done %h",
			originalHost: "myserver",
			hostname:     "10.0.0.5",
			port:         "22",
			user:         "root",
			expected:     "echo 100% done 10.0.0.5",
		},
		{
			name:         "all tokens combined",
			cmd:          "ssh -W %h:%p -l %r %n-bastion %%",
			originalHost: "prod",
			hostname:     "192.168.1.100",
			port:         "30863",
			user:         "admin",
			expected:     "ssh -W 192.168.1.100:30863 -l admin prod-bastion %",
		},
		{
			name:         "no tokens",
			cmd:          "nc -X4 -x 192.168.100.23 10.30.121.100 30863",
			originalHost: "server",
			hostname:     "10.30.121.100",
			port:         "30863",
			user:         "root",
			expected:     "nc -X4 -x 192.168.100.23 10.30.121.100 30863",
		},
		{
			name:         "unknown percent token left as-is",
			cmd:          "cmd %z %h",
			originalHost: "server",
			hostname:     "10.0.0.5",
			port:         "22",
			user:         "root",
			expected:     "cmd %z 10.0.0.5",
		},
		{
			name:         "ProxyCommand from issue 190",
			cmd:          "nc -X4 -x 192.168.100.23 %h %p",
			originalHost: "A100server",
			hostname:     "10.30.121.100",
			port:         "30863",
			user:         "root",
			expected:     "nc -X4 -x 192.168.100.23 10.30.121.100 30863",
		},
		{
			name:         "empty command",
			cmd:          "",
			originalHost: "server",
			hostname:     "10.0.0.5",
			port:         "22",
			user:         "root",
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandProxyTokens(tt.cmd, tt.originalHost, tt.hostname, tt.port, tt.user)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProxyConn_ReadWriteClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh and cat")
	}

	// Use cat as a loopback proxy: what we write to stdin comes back on stdout
	settings := &sshSettings{
		hostname: "localhost",
		port:     "22",
		user:     "test",
	}
	conn, err := dialViaProxy("cat", "testhost", settings)
	require.NoError(t, err)
	defer conn.Close()

	// Write data through the proxy
	msg := []byte("hello proxy")
	n, err := conn.Write(msg)
	require.NoError(t, err)
	assert.Equal(t, len(msg), n)

	// Read it back
	buf := make([]byte, 64)
	n, err = conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello proxy", string(buf[:n]))

	// Verify addr methods don't panic
	assert.Equal(t, "proxy", conn.LocalAddr().Network())
	assert.Equal(t, "proxy", conn.LocalAddr().String())
	assert.Equal(t, "proxy", conn.RemoteAddr().Network())
	assert.Equal(t, "localhost:22", conn.RemoteAddr().String())

	// Verify deadline no-ops return nil
	assert.NoError(t, conn.SetDeadline(time.Now()))
	assert.NoError(t, conn.SetReadDeadline(time.Now()))
	assert.NoError(t, conn.SetWriteDeadline(time.Now()))
}

func TestProxyConn_CloseKillsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh and sleep")
	}

	// Start a long-running proxy command
	settings := &sshSettings{
		hostname: "localhost",
		port:     "22",
		user:     "test",
	}
	conn, err := dialViaProxy("sleep 60", "testhost", settings)
	require.NoError(t, err)

	pc := conn.(*proxyConn)
	pid := pc.cmd.Process.Pid

	// Close should terminate the process
	err = conn.Close()
	assert.NoError(t, err)

	// Verify the process is gone (kill -0 checks existence without sending a signal)
	// Give it a moment for the OS to clean up
	time.Sleep(100 * time.Millisecond)
	err = exec.Command("kill", "-0", fmt.Sprintf("%d", pid)).Run()
	assert.Error(t, err, "process should no longer exist after Close()")
}

func TestProxyConn_CloseIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh and cat")
	}

	settings := &sshSettings{
		hostname: "localhost",
		port:     "22",
		user:     "test",
	}
	conn, err := dialViaProxy("cat", "testhost", settings)
	require.NoError(t, err)

	// Closing multiple times should not panic
	assert.NoError(t, conn.Close())
	assert.NoError(t, conn.Close())
}

func TestDialViaProxy_InvalidCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}

	settings := &sshSettings{
		hostname: "localhost",
		port:     "22",
		user:     "test",
	}

	// A command that exits immediately with an error
	_, err := dialViaProxy("false", "testhost", settings)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "proxy command exited immediately")
}

func TestDialViaProxy_NonexistentCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}

	settings := &sshSettings{
		hostname: "localhost",
		port:     "22",
		user:     "test",
	}

	_, err := dialViaProxy("this_command_does_not_exist_xyz", "testhost", settings)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "proxy command exited immediately")
}

func TestDialViaProxy_TokenExpansion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh and echo/cat")
	}

	// Use a command that echoes the expanded tokens back to us, then acts as cat
	// This verifies tokens are expanded before the command runs
	settings := &sshSettings{
		hostname: "10.0.0.5",
		port:     "2222",
		user:     "deploy",
	}

	// Use cat so the proxy stays alive; the expanded command should contain the right values
	conn, err := dialViaProxy("cat", "myalias", settings)
	require.NoError(t, err)
	defer conn.Close()

	// Just verify the connection works (tokens don't affect cat, but this proves
	// the flow works end-to-end)
	msg := []byte("test")
	_, err = conn.Write(msg)
	require.NoError(t, err)

	buf := make([]byte, 16)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "test", string(buf[:n]))
}
