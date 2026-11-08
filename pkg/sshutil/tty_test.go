package sshutil

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

const ttyTestTimeout = 5 * time.Second

// ptyRequest is the payload of a "pty-req" channel request (RFC 4254 6.2).
type ptyRequest struct {
	Term    string
	Columns uint32
	Rows    uint32
	Width   uint32
	Height  uint32
	Modes   string
}

// windowChange is the payload of a "window-change" request (RFC 4254 6.7).
type windowChange struct {
	Columns uint32
	Rows    uint32
	Width   uint32
	Height  uint32
}

// ttyServer is an in-process SSH server for one session at a time. It
// records the PTY and window-change requests it gets, and runs each exec'd
// command through run, which returns the exit status.
type ttyServer struct {
	run func(s *ttyServer, cmd string, ch ssh.Channel) uint32

	mu  sync.Mutex
	pty *ptyRequest

	resized chan windowChange // every window-change, in order
	closed  chan struct{}     // closed once the client closes the session
}

func (s *ttyServer) ptyReq() *ptyRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pty
}

// startTTYServer runs a ttyServer and returns a Client connected to it.
func startTTYServer(t *testing.T, run func(s *ttyServer, cmd string, ch ssh.Channel) uint32) (*Client, *ttyServer) {
	t.Helper()
	srv := &ttyServer{
		run:     run,
		resized: make(chan windowChange, 8),
		closed:  make(chan struct{}),
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_, chans, reqs, err := ssh.NewServerConn(c, config)
		if err != nil {
			c.Close()
			return
		}
		go ssh.DiscardRequests(reqs)
		for newCh := range chans {
			if newCh.ChannelType() != "session" {
				_ = newCh.Reject(ssh.UnknownChannelType, "session only")
				continue
			}
			ch, chReqs, err := newCh.Accept()
			if err != nil {
				return
			}
			go srv.serveSession(ch, chReqs)
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, ln.Addr().String(), &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	require.NoError(t, err)
	client := &Client{Client: ssh.NewClient(clientConn, chans, reqs), Host: "test", Address: ln.Addr().String()}
	t.Cleanup(func() { client.Close() })
	return client, srv
}

func (s *ttyServer) serveSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer close(s.closed)
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var p ptyRequest
			if err := ssh.Unmarshal(req.Payload, &p); err != nil {
				_ = req.Reply(false, nil)
				continue
			}
			s.mu.Lock()
			s.pty = &p
			s.mu.Unlock()
			_ = req.Reply(true, nil)
		case "window-change":
			var w windowChange
			if err := ssh.Unmarshal(req.Payload, &w); err == nil {
				s.resized <- w
			}
		case "exec":
			var e struct{ Command string }
			if err := ssh.Unmarshal(req.Payload, &e); err != nil {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			go func() {
				status := s.run(s, e.Command, ch)
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				_ = ch.Close()
			}()
		default:
			_ = req.Reply(false, nil)
		}
	}
	// reqs closes when the channel does: the client closed the session.
}

// withTimeout fails the test if fn doesn't return in time.
func withTimeout(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(ttyTestTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestExecTTYContext_RequestsPTYWithSizeAndTerm(t *testing.T) {
	tests := []struct {
		name       string
		opts       TTYOptions
		wantTerm   string
		wantWidth  uint32
		wantHeight uint32
	}{
		{name: "given size and term", opts: TTYOptions{Term: "screen", Width: 132, Height: 50}, wantTerm: "screen", wantWidth: 132, wantHeight: 50},
		{name: "defaults", opts: TTYOptions{}, wantTerm: DefaultTerm, wantWidth: 80, wantHeight: 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, srv := startTTYServer(t, func(*ttyServer, string, ssh.Channel) uint32 { return 0 })

			var code int
			var err error
			withTimeout(t, "the command", func() {
				code, err = client.ExecTTYContext(context.Background(), "true", tt.opts)
			})
			require.NoError(t, err)
			assert.Equal(t, 0, code)

			p := srv.ptyReq()
			require.NotNil(t, p, "a PTY was requested")
			assert.Equal(t, tt.wantTerm, p.Term)
			assert.Equal(t, tt.wantWidth, p.Columns)
			assert.Equal(t, tt.wantHeight, p.Rows)
			// The modes end with TTY_OP_END (0); ECHO (53) is set to 1.
			assert.Contains(t, p.Modes, string([]byte{ssh.ECHO, 0, 0, 0, 1}), "ECHO is on")
		})
	}
}

// The case #266 is about: sudo writes "[sudo] password for riley: " with no
// newline and waits. The prompt must reach the output while the command is
// still waiting, and what's typed must reach the command.
func TestExecTTYContext_PromptWithoutNewlineAndStdin(t *testing.T) {
	client, _ := startTTYServer(t, func(_ *ttyServer, _ string, ch ssh.Channel) uint32 {
		_, _ = io.WriteString(ch, "Password: ")
		line, err := bufio.NewReader(ch).ReadString('\n')
		if err != nil {
			return 1
		}
		_, _ = fmt.Fprintf(ch, "got %s\r\n", strings.TrimSpace(line))
		return 0
	})

	stdinR, stdinW := io.Pipe()
	defer stdinW.Close()
	outR, outW := io.Pipe()

	type result struct {
		code int
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		code, err := client.ExecTTYContext(context.Background(), "sudo true", TTYOptions{Stdin: stdinR, Output: outW})
		outW.Close()
		resCh <- result{code, err}
	}()

	prompt := make([]byte, len("Password: "))
	withTimeout(t, "the prompt", func() {
		_, err := io.ReadFull(outR, prompt)
		require.NoError(t, err)
	})
	assert.Equal(t, "Password: ", string(prompt))

	select {
	case <-resCh:
		t.Fatal("the command finished before it got its input")
	default:
	}

	_, err := io.WriteString(stdinW, "hunter2\n")
	require.NoError(t, err)

	var rest []byte
	withTimeout(t, "the rest of the output", func() {
		rest, err = io.ReadAll(outR)
		require.NoError(t, err)
	})
	assert.Equal(t, "got hunter2\r\n", string(rest))

	res := <-resCh
	require.NoError(t, res.err)
	assert.Equal(t, 0, res.code)
}

func TestExecTTYContext_ForwardsResize(t *testing.T) {
	client, srv := startTTYServer(t, func(s *ttyServer, _ string, _ ssh.Channel) uint32 {
		// Exit once the resize arrived, so the request can't race the
		// session's end.
		w := <-s.resized
		s.resized <- w
		return 0
	})

	resize := make(chan WindowSize, 1)
	resize <- WindowSize{Width: 120, Height: 40}
	var err error
	withTimeout(t, "the command", func() {
		_, err = client.ExecTTYContext(context.Background(), "vim", TTYOptions{Width: 80, Height: 24, Resize: resize})
	})
	require.NoError(t, err)

	select {
	case w := <-srv.resized:
		assert.Equal(t, uint32(120), w.Columns)
		assert.Equal(t, uint32(40), w.Rows)
	default:
		t.Fatal("no window-change reached the server")
	}
}

func TestExecTTYContext_CancelClosesSession(t *testing.T) {
	client, srv := startTTYServer(t, func(s *ttyServer, _ string, _ ssh.Channel) uint32 {
		<-s.closed // a command that never exits on its own
		return 0
	})

	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		code int
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		code, err := client.ExecTTYContext(ctx, "sleep infinity", TTYOptions{})
		resCh <- result{code, err}
	}()

	// Cancel only once the command is running, so this tests closing a
	// live session rather than one that never started.
	require.Eventually(t, func() bool { return srv.ptyReq() != nil }, ttyTestTimeout, 10*time.Millisecond)
	cancel()

	var res result
	withTimeout(t, "ExecTTYContext to return", func() { res = <-resCh })
	assert.Equal(t, 130, res.code)
	assert.ErrorIs(t, res.err, context.Canceled)
	withTimeout(t, "the server to see the session close", func() { <-srv.closed })
}

func TestExecTTYContext_ExitCodes(t *testing.T) {
	tests := []struct {
		name   string
		status uint32
	}{
		{name: "success", status: 0},
		{name: "failure", status: 3},
		{name: "interrupted", status: 130},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := startTTYServer(t, func(*ttyServer, string, ssh.Channel) uint32 { return tt.status })

			var code int
			var err error
			withTimeout(t, "the command", func() {
				code, err = client.ExecTTYContext(context.Background(), "cmd", TTYOptions{Output: io.Discard})
			})
			require.NoError(t, err, "a command that ran and exited is not an error")
			assert.Equal(t, int(tt.status), code)
		})
	}
}
