//go:build !windows

package tty

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
