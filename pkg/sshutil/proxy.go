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

// proxyJumpValue returns a ProxyJump value's comma-separated jump hosts, each
// [user@]host[:port] or an ssh:// URI, or "" for none. "none" means no jump.
func proxyJumpValue(proxyJump string) string {
	proxyJump = strings.TrimSpace(proxyJump)
	if strings.EqualFold(proxyJump, "none") {
		return ""
	}
	return proxyJump
}

// proxyJumpArgs returns the arguments for the system ssh that OpenSSH runs
// for a ProxyJump value: connect to the last jump host (through any earlier
// ones, with -J) and forward stdio to hostname:port with -W. rr runs ssh
// without a shell, and "--" keeps the last jump host from being read as an
// option; ssh handles the earlier hops itself, as `ssh target` would.
// BatchMode keeps it from prompting, since rr can't answer. LogLevel=ERROR
// keeps banners and notices out of stderr, which rr reports as the cause.
func proxyJumpArgs(jumpHosts, hostname, port string) []string {
	hops := strings.Split(jumpHosts, ",")
	for i := range hops {
		hops[i] = strings.TrimSpace(hops[i])
	}
	args := []string{"-o", "BatchMode=yes", "-o", "LogLevel=ERROR"}
	if len(hops) > 1 {
		args = append(args, "-J", strings.Join(hops[:len(hops)-1], ","))
	}
	args = append(args, "-W", "["+hostname+"]:"+port)
	dest, hopPort := splitJumpPort(hops[len(hops)-1])
	if hopPort != "" {
		args = append(args, "-p", hopPort)
	}
	return append(args, "--", dest)
}

// splitJumpPort splits a jump host ([user@]host[:port], with the host in
// brackets for IPv6) into the destination ssh takes and its port, which ssh
// takes as -p. An ssh:// URI is returned whole; ssh parses it itself.
func splitJumpPort(hop string) (dest, port string) {
	if strings.HasPrefix(hop, "ssh://") {
		return hop, ""
	}
	user, host := "", hop
	if at := strings.LastIndex(hop, "@"); at != -1 {
		user, host = hop[:at+1], hop[at+1:]
	}
	if strings.HasPrefix(host, "[") {
		// [v6] or [v6]:port
		end := strings.Index(host, "]")
		if end == -1 {
			return hop, ""
		}
		if rest := host[end+1:]; strings.HasPrefix(rest, ":") && isPort(rest[1:]) {
			port = rest[1:]
		} else if rest != "" {
			return hop, ""
		}
		return user + host[1:end], port
	}
	if i := strings.LastIndex(host, ":"); i != -1 && strings.Count(host, ":") == 1 && isPort(host[i+1:]) {
		return user + host[:i], host[i+1:]
	}
	return hop, ""
}

func isPort(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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
	stderr *lockedBuffer
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

// proxyCmd returns the process that carries the connection to the target:
// the system ssh for a ProxyJump, run directly, or the ProxyCommand, run with
// sh as OpenSSH runs it.
func proxyCmd(originalHost string, settings *sshSettings) *exec.Cmd {
	if settings.jumpHosts != "" {
		return exec.Command("ssh", proxyJumpArgs(settings.jumpHosts, settings.hostname, settings.port)...)
	}
	expanded := expandProxyTokens(settings.proxyCommand, originalHost, settings.hostname, settings.port, settings.user)
	return exec.Command("sh", "-c", expanded)
}

// startProxy starts cmd and uses its stdin and stdout as the connection.
func startProxy(cmd *exec.Cmd, settings *sshSettings) (net.Conn, error) {
	setProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("proxy stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("proxy stdout pipe: %w", err)
	}

	stderrBuf := &lockedBuffer{}
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, &proxyStartError{name: cmd.Path, err: err}
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		stdin.Close()
		stdout.Close()
		return nil, &proxyExitError{stderr: proxyStderr(stderrBuf.String()), err: err}
	case <-time.After(100 * time.Millisecond):
	}

	conn := &proxyConn{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderrBuf,
		addr:   net.JoinHostPort(settings.hostname, settings.port),
		waitCh: waitCh,
	}

	return conn, nil
}

// proxyStartError is a proxy process that couldn't be started at all, such
// as ssh missing from PATH. Nothing was dialed, so it isn't a proxy failure.
type proxyStartError struct {
	name string
	err  error
}

func (e *proxyStartError) Error() string {
	return fmt.Sprintf("couldn't start %s: %v", e.name, e.err)
}

func (e *proxyStartError) Unwrap() error { return e.err }

// proxyExitError is a proxy command that exited before rr could use it.
type proxyExitError struct {
	stderr string // what it printed, trimmed
	err    error  // its exit status, if it failed
}

func (e *proxyExitError) Error() string {
	switch {
	case e.stderr != "":
		return "proxy command exited immediately: " + e.stderr
	case e.err != nil:
		return "proxy command exited immediately: " + e.err.Error()
	default:
		return "proxy command exited immediately with no output"
	}
}

func (e *proxyExitError) Unwrap() error { return e.err }

// proxyStderr trims what a proxy command printed, dropping the "Killed by
// signal" line ssh prints when rr stops it, which says nothing about why
// the connection failed.
func proxyStderr(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Killed by signal") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// lockedBuffer collects a process's stderr; exec writes it from another
// goroutine while rr may read it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ProxyError is a connection that failed in its ProxyJump or ProxyCommand
// rather than in the SSH handshake with the target.
type ProxyError struct {
	Host      string // the alias rr dialed
	Directive string // "ProxyJump" or "ProxyCommand"
	JumpHost  string // the ProxyJump value; empty for ProxyCommand
	Stderr    string // what the proxy printed, which says why
	TimedOut  bool   // nothing answered before the dial timeout
	Err       error
}

func (e *ProxyError) Error() string {
	switch {
	case e.TimedOut && e.Stderr != "":
		return "no SSH response through the proxy before the timeout; it printed: " + e.Stderr
	case e.TimedOut:
		return "no SSH response through the proxy before the timeout"
	case e.Stderr != "":
		return e.Stderr
	case e.Err != nil:
		return e.Err.Error()
	default:
		return "the proxy closed the connection"
	}
}

func (e *ProxyError) Unwrap() error { return e.Err }
