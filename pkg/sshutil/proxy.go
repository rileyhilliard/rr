// NOTE: This file uses Unix-specific syscalls (Setpgid, Kill with negative PID).
// If Windows support is ever needed, this file needs a //go:build !windows constraint.

package sshutil

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// expandProxyTokens expands SSH-style tokens in a ProxyCommand string.
// Supported tokens: %h (hostname), %p (port), %n (original host alias),
// %r (remote user), %% (literal %).
func expandProxyTokens(cmd, originalHost, hostname, port, user string) string {
	// Replace %% with a sentinel first so it doesn't get caught by other replacements
	const sentinel = "\x00PERCENT\x00"
	result := strings.ReplaceAll(cmd, "%%", sentinel)

	result = strings.ReplaceAll(result, "%h", hostname)
	result = strings.ReplaceAll(result, "%p", port)
	result = strings.ReplaceAll(result, "%n", originalHost)
	result = strings.ReplaceAll(result, "%r", user)

	// Restore literal percent signs
	result = strings.ReplaceAll(result, sentinel, "%")
	return result
}

// proxyAddr implements net.Addr for proxy connections.
type proxyAddr struct {
	addr string
}

func (a proxyAddr) Network() string { return "proxy" }
func (a proxyAddr) String() string  { return a.addr }

// proxyConn wraps a subprocess's stdin/stdout as a net.Conn.
// This is used to tunnel SSH connections through a ProxyCommand.
type proxyConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	addr   string     // remote address for RemoteAddr
	waitCh chan error // receives cmd.Wait() result from background goroutine

	closeOnce sync.Once
	closeErr  error
}

func (c *proxyConn) Read(b []byte) (int, error) {
	return c.stdout.Read(b)
}

func (c *proxyConn) Write(b []byte) (int, error) {
	return c.stdin.Write(b)
}

// Close terminates the proxy command process and cleans up resources.
// It sends SIGTERM to the process group first, then SIGKILL after a grace period.
func (c *proxyConn) Close() error {
	c.closeOnce.Do(func() {
		// Close stdin to signal EOF to the proxy process
		c.stdin.Close()

		// Try graceful shutdown: SIGTERM to process group
		if c.cmd.Process != nil {
			// Negative PID sends signal to the entire process group
			_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
		}

		// Wait for process to exit with a grace period
		select {
		case err := <-c.waitCh:
			c.closeErr = err
		case <-time.After(2 * time.Second):
			// Force kill the process group
			if c.cmd.Process != nil {
				_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
				c.closeErr = <-c.waitCh
			}
		}

		c.stdout.Close()
	})
	return c.closeErr
}

func (c *proxyConn) LocalAddr() net.Addr  { return proxyAddr{addr: "proxy"} }
func (c *proxyConn) RemoteAddr() net.Addr { return proxyAddr{addr: c.addr} }

// Deadline methods are no-ops. Verified in golang.org/x/crypto@v0.48.0 that
// ssh.NewClientConn does NOT call SetDeadline on the net.Conn. The SSH library's
// own test mocks (server_test.go, recording_test.go) use no-op deadlines.
// Handshake timeout for proxy connections is enforced externally via time.AfterFunc
// in Dial(), which closes the conn if the handshake stalls.
func (c *proxyConn) SetDeadline(_ time.Time) error      { return nil }
func (c *proxyConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *proxyConn) SetWriteDeadline(_ time.Time) error { return nil }

// dialViaProxy establishes a connection through a ProxyCommand.
// The proxy command is executed as a shell command, with its stdin/stdout
// used as the transport for the SSH connection.
func dialViaProxy(proxyCommand, originalHost string, settings *sshSettings) (net.Conn, error) {
	expanded := expandProxyTokens(proxyCommand, originalHost, settings.hostname, settings.port, settings.user)

	cmd := exec.Command("sh", "-c", expanded)

	// Use process groups so we can kill the entire tree on cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("proxy stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("proxy stdout pipe: %w", err)
	}

	// Capture stderr for error reporting
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("proxy command failed to start: %w", err)
	}

	// Start a goroutine to wait on the process. The result is used by both
	// the early-exit check and proxyConn.Close().
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	// Best-effort check for commands that fail immediately (bad command, connection
	// refused, etc.). 100ms is enough for obvious failures while avoiding false
	// positives on legitimate proxy commands that take a moment to connect.
	select {
	case err := <-waitCh:
		stderr := strings.TrimSpace(stderrBuf.String())
		if stderr != "" {
			return nil, fmt.Errorf("proxy command exited immediately: %s", stderr)
		}
		if err != nil {
			return nil, fmt.Errorf("proxy command exited immediately: %w", err)
		}
		return nil, fmt.Errorf("proxy command exited immediately with no output")
	case <-time.After(100 * time.Millisecond):
		// Process is still running, acting as a proxy.
	}

	conn := &proxyConn{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		addr:   net.JoinHostPort(settings.hostname, settings.port),
		waitCh: waitCh,
	}

	return conn, nil
}
