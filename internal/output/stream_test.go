package output

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStreamHandler(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)
	assert.NotNil(t, h)
}

func TestStreamHandlerWriteStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	err := h.WriteStdout("test line")
	require.NoError(t, err)

	assert.Equal(t, "test line\n", stdout.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, 1, h.StdoutLines())
	assert.Equal(t, 0, h.StderrLines())
}

func TestStreamHandlerWriteStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	err := h.WriteStderr("error line")
	require.NoError(t, err)

	assert.Empty(t, stdout.String())
	assert.Equal(t, "error line\n", stderr.String())
	assert.Equal(t, 0, h.StdoutLines())
	assert.Equal(t, 1, h.StderrLines())
}

func TestStreamHandlerWithFormatter(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)
	h.SetFormatter(NewGenericFormatter())

	// Regular line should pass through
	err := h.WriteStdout("normal line")
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "normal line")

	stdout.Reset()

	// Error line should be formatted (contains ANSI codes)
	err = h.WriteStdout("ERROR: something failed")
	require.NoError(t, err)
	// Output will have ANSI color codes
	assert.Contains(t, stdout.String(), "ERROR: something failed")
}

func TestStreamWriterLineBuffering(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	w := h.Stdout()

	// Write partial line
	_, err := w.Write([]byte("hello "))
	require.NoError(t, err)
	assert.Empty(t, stdout.String()) // Not flushed yet

	// Complete the line
	_, err = w.Write([]byte("world\n"))
	require.NoError(t, err)
	assert.Equal(t, "hello world\n", stdout.String())
}

func TestStreamWriterMultipleLines(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	w := h.Stdout()

	_, err := w.Write([]byte("line1\nline2\nline3\n"))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
	assert.Equal(t, 3, h.StdoutLines())
}

func TestStreamWriterFlush(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	sw := h.Stdout().(*streamWriter)

	// Write without newline
	_, err := sw.Write([]byte("incomplete"))
	require.NoError(t, err)
	assert.Empty(t, stdout.String())

	// Flush should write the incomplete line
	err = sw.Flush()
	require.NoError(t, err)
	assert.Equal(t, "incomplete\n", stdout.String())
}

func TestStreamWriterFlushEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	sw := h.Stdout().(*streamWriter)

	// Flush with nothing buffered should be a no-op
	err := sw.Flush()
	require.NoError(t, err)
	assert.Empty(t, stdout.String())
}

func TestStreamHandlerConcurrent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			_ = h.WriteStdout("stdout")
		}(i)
		go func(n int) {
			defer wg.Done()
			_ = h.WriteStderr("stderr")
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, h.StdoutLines())
	assert.Equal(t, 100, h.StderrLines())
}

func TestLineWriter(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLineWriter(&buf)

	// Write data without newline
	_, err := lw.Write([]byte("partial"))
	require.NoError(t, err)
	assert.Empty(t, buf.String())

	// Complete with newline
	_, err = lw.Write([]byte(" line\n"))
	require.NoError(t, err)
	assert.Equal(t, "partial line\n", buf.String())
}

func TestLineWriterFlush(t *testing.T) {
	var buf bytes.Buffer
	lw := NewLineWriter(&buf)

	_, err := lw.Write([]byte("no newline"))
	require.NoError(t, err)
	assert.Empty(t, buf.String())

	err = lw.Flush()
	require.NoError(t, err)
	assert.Equal(t, "no newline", buf.String())
}

func TestCopyLines(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	input := strings.NewReader("line1\nline2\nline3\n")

	err := CopyLines(input, h, false)
	require.NoError(t, err)

	assert.Equal(t, "line1\nline2\nline3\n", stdout.String())
	assert.Equal(t, 3, h.StdoutLines())
}

func TestCopyLinesStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	input := strings.NewReader("error1\nerror2\n")

	err := CopyLines(input, h, true)
	require.NoError(t, err)

	assert.Empty(t, stdout.String())
	assert.Equal(t, "error1\nerror2\n", stderr.String())
	assert.Equal(t, 2, h.StderrLines())
}

func TestStreamHandlerStdoutStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	stdoutW := h.Stdout()
	stderrW := h.Stderr()

	// Both should implement io.Writer (compile-time check)
	_ = io.Writer(stdoutW)
	_ = io.Writer(stderrW)

	_, err := stdoutW.Write([]byte("out\n"))
	require.NoError(t, err)

	_, err = stderrW.Write([]byte("err\n"))
	require.NoError(t, err)

	assert.Equal(t, "out\n", stdout.String())
	assert.Equal(t, "err\n", stderr.String())
}

func TestANSIPassthrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := NewStreamHandler(&stdout, &stderr)

	// ANSI codes should pass through unchanged
	ansiLine := "\033[31mRed text\033[0m"
	err := h.WriteStdout(ansiLine)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "\033[31m")
	assert.Contains(t, stdout.String(), "Red text")
	assert.Contains(t, stdout.String(), "\033[0m")
}
