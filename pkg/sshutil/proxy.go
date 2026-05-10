package sshutil

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// expandProxyTokens expands SSH-style tokens in a ProxyCommand string.
// Supported tokens: %h (hostname), %p (port), %n (original host alias),
// %r (remote user), %% (literal %).
func expandProxyTokens(cmd, originalHost, hostname, port, user string) string {
	const sentinel = "\x00PERCENT\x00"
	result := strings.ReplaceAll(cmd, "%%", sentinel)

	result = strings.ReplaceAll(result, "%h", hostname)
	result = strings.ReplaceAll(result, "%p", port)
	result = strings.ReplaceAll(result, "%n", originalHost)
	result = strings.ReplaceAll(result, "%r", user)

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
type proxyConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	addr   string
	waitCh chan error

	closeOnce sync.Once
	closeErr  error
}

func (c *proxyConn) Read(b []byte) (int, error) {
	return c.stdout.Read(b)
}

func (c *proxyConn) Write(b []byte) (int, error) {
	return c.stdin.Write(b)
}

func (c *proxyConn) LocalAddr() net.Addr  { return proxyAddr{addr: "proxy"} }
func (c *proxyConn) RemoteAddr() net.Addr { return proxyAddr{addr: c.addr} }

func (c *proxyConn) SetDeadline(_ time.Time) error      { return nil }
func (c *proxyConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *proxyConn) SetWriteDeadline(_ time.Time) error { return nil }

// dialViaProxy establishes a connection through a ProxyCommand.
func dialViaProxy(proxyCommand, originalHost string, settings *sshSettings) (net.Conn, error) {
	expanded := expandProxyTokens(proxyCommand, originalHost, settings.hostname, settings.port, settings.user)

	cmd := exec.Command("sh", "-c", expanded)

	setProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("proxy stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("proxy stdout pipe: %w", err)
	}

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("proxy command failed to start: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		stdin.Close()
		stdout.Close()
		stderr := strings.TrimSpace(stderrBuf.String())
		if stderr != "" {
			return nil, fmt.Errorf("proxy command exited immediately: %s", stderr)
		}
		if err != nil {
			return nil, fmt.Errorf("proxy command exited immediately: %w", err)
		}
		return nil, fmt.Errorf("proxy command exited immediately with no output")
	case <-time.After(100 * time.Millisecond):
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
