//go:build !windows

package tty

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// newPipe returns a pipe whose read end is driven through a stdinReader.
// poll supports pipes, so this exercises the same path a terminal does.
func newPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})
	return r, w
}

func TestStdinReader_ReadsInput(t *testing.T) {
	r, w := newPipe(t)
	reader := newStdinReader(int(r.Fd()))

	_, err := w.WriteString("hello")
	require.NoError(t, err)

	buf := make([]byte, 16)
	n, err := reader.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:n]))
}

// After the command ends, what the user types belongs to whatever reads
// stdin next (their shell, a later rr prompt), not to a reader left over
// from the command.
func TestStdinReader_StopLeavesLaterInputUnread(t *testing.T) {
	r, w := newPipe(t)
	reader := newStdinReader(int(r.Fd()))

	// A Read blocked waiting for input, as the SSH session's copy is while
	// the command runs.
	readErr := make(chan error, 1)
	go func() {
		_, err := reader.Read(make([]byte, 16))
		readErr <- err
	}()

	stopped := make(chan struct{})
	go func() {
		reader.stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stop didn't return")
	}
	select {
	case err := <-readErr:
		assert.ErrorIs(t, err, io.EOF, "the blocked Read ends")
	case <-time.After(5 * time.Second):
		t.Fatal("the blocked Read didn't end after stop")
	}

	_, err := w.WriteString("typeahead")
	require.NoError(t, err)

	n, err := reader.Read(make([]byte, 16))
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, io.EOF, "a stopped reader reads nothing")

	buf := make([]byte, 16)
	n, err = r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "typeahead", string(buf[:n]), "the input is still there for the next reader")
}

func TestStdinReader_EOFWhenWriterCloses(t *testing.T) {
	r, w := newPipe(t)
	reader := newStdinReader(int(r.Fd()))
	require.NoError(t, w.Close())

	_, err := reader.Read(make([]byte, 16))
	assert.ErrorIs(t, err, io.EOF)
}

// A read that ends because the terminal is gone ends the command's input,
// not the command: io.EOF, which x/crypto doesn't turn into a failure of a
// command that exited 0.
func TestStdinReader_EOFWhenStdinGoes(t *testing.T) {
	t.Run("data, then the writer closes", func(t *testing.T) {
		r, w := newPipe(t)
		reader := newStdinReader(int(r.Fd()))
		_, err := w.WriteString("bye")
		require.NoError(t, err)
		require.NoError(t, w.Close())

		buf := make([]byte, 16)
		n, err := reader.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, "bye", string(buf[:n]), "input before the hangup is still read")
		_, err = reader.Read(buf)
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("an fd that isn't open (POLLNVAL)", func(t *testing.T) {
		reader := newStdinReader(1 << 20)
		_, err := reader.Read(make([]byte, 16))
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("the terminal hangs up", func(t *testing.T) {
		ptm, pts := openPTY(t)
		reader := newStdinReader(int(pts.Fd()))
		require.NoError(t, ptm.Close())

		_, err := reader.Read(make([]byte, 16))
		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestSession_StartAndClose(t *testing.T) {
	ptm, pts := openPTY(t)
	fd := int(pts.Fd())
	require.NoError(t, unix.IoctlSetWinsize(int(ptm.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 100}))

	before, err := term.GetState(fd)
	require.NoError(t, err)

	s, err := start(fd, fd)
	require.NoError(t, err)
	assert.Equal(t, 100, s.Size().Width)
	assert.Equal(t, 30, s.Size().Height)

	raw, err := term.GetState(fd)
	require.NoError(t, err)
	assert.NotEqual(t, before, raw, "the terminal is raw while the session is open")

	_, err = ptm.WriteString("y")
	require.NoError(t, err)
	buf := make([]byte, 16)
	n, err := s.Stdin().Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "y", string(buf[:n]), "a raw terminal hands over keystrokes without waiting for Enter")

	require.NoError(t, s.Close())
	require.NoError(t, s.Close(), "Close is safe to call twice")

	after, err := term.GetState(fd)
	require.NoError(t, err)
	assert.Equal(t, before, after, "Close puts the terminal back as it was")

	requireClosed(t, s.Resize())
	_, err = s.Stdin().Read(buf)
	assert.ErrorIs(t, err, io.EOF)
}

// rr's force-exit path can't run a deferred Close, so RestoreAll puts a
// live session's terminal back.
func TestRestoreAll_RestoresLiveSession(t *testing.T) {
	_, pts := openPTY(t)
	fd := int(pts.Fd())

	before, err := term.GetState(fd)
	require.NoError(t, err)
	s, err := start(fd, fd)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	RestoreAll()

	after, err := term.GetState(fd)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	rawMu.Lock()
	assert.Empty(t, rawTerminals, "a restored session isn't restored again")
	rawMu.Unlock()
}

// requireClosed waits for ch to close, skipping any sizes still queued (a
// real resize of the test's own terminal can send one).
func requireClosed(t *testing.T, ch <-chan sshutil.WindowSize) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("Resize didn't close")
		}
	}
}
