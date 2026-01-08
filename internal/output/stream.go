package output

import (
	"bufio"
	"io"
	"sync"
)

// StreamHandler multiplexes stdout and stderr from a remote command,
// with line buffering and ANSI passthrough.
type StreamHandler struct {
	stdout io.Writer
	stderr io.Writer
	mu     sync.Mutex

	// Formatter processes each line before output.
	// If nil, lines pass through unchanged.
	formatter Formatter

	// Stats tracking
	stdoutLines int
	stderrLines int
}

// NewStreamHandler creates a handler that writes to the given stdout/stderr.
func NewStreamHandler(stdout, stderr io.Writer) *StreamHandler {
	return &StreamHandler{
		stdout: stdout,
		stderr: stderr,
	}
}

// SetFormatter sets the line formatter.
func (h *StreamHandler) SetFormatter(f Formatter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.formatter = f
}

// Stdout returns a writer that processes lines for stdout.
func (h *StreamHandler) Stdout() io.Writer {
	return &streamWriter{
		handler:  h,
		isStderr: false,
	}
}

// Stderr returns a writer that processes lines for stderr.
func (h *StreamHandler) Stderr() io.Writer {
	return &streamWriter{
		handler:  h,
		isStderr: true,
	}
}

// StdoutLines returns the number of stdout lines processed.
func (h *StreamHandler) StdoutLines() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stdoutLines
}

// StderrLines returns the number of stderr lines processed.
func (h *StreamHandler) StderrLines() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stderrLines
}

// WriteStdout writes a line to stdout after processing.
func (h *StreamHandler) WriteStdout(line string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stdoutLines++

	processedLine := line
	if h.formatter != nil {
		processedLine = h.formatter.ProcessLine(line)
	}

	_, err := h.stdout.Write([]byte(processedLine + "\n"))
	return err
}

// WriteStderr writes a line to stderr after processing.
func (h *StreamHandler) WriteStderr(line string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stderrLines++

	processedLine := line
	if h.formatter != nil {
		processedLine = h.formatter.ProcessLine(line)
	}

	_, err := h.stderr.Write([]byte(processedLine + "\n"))
	return err
}

// streamWriter wraps the handler to implement io.Writer.
type streamWriter struct {
	handler  *StreamHandler
	isStderr bool
	buf      []byte
}

// Write implements io.Writer with line buffering.
// Incomplete lines are buffered until a newline arrives.
func (w *streamWriter) Write(p []byte) (n int, err error) {
	n = len(p)

	// Append to buffer
	w.buf = append(w.buf, p...)

	// Process complete lines
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}

		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]

		if w.isStderr {
			if err := w.handler.WriteStderr(line); err != nil {
				return n, err
			}
		} else {
			if err := w.handler.WriteStdout(line); err != nil {
				return n, err
			}
		}
	}

	return n, nil
}

// Flush writes any remaining buffered content.
func (w *streamWriter) Flush() error {
	if len(w.buf) > 0 {
		line := string(w.buf)
		w.buf = nil

		if w.isStderr {
			return w.handler.WriteStderr(line)
		}
		return w.handler.WriteStdout(line)
	}
	return nil
}

// LineWriter wraps an io.Writer to write complete lines.
type LineWriter struct {
	w   io.Writer
	buf []byte
}

// NewLineWriter creates a line-buffered writer.
func NewLineWriter(w io.Writer) *LineWriter {
	return &LineWriter{w: w}
}

// Write buffers data and writes complete lines.
func (lw *LineWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	lw.buf = append(lw.buf, p...)

	for {
		idx := -1
		for i, b := range lw.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}

		if _, err := lw.w.Write(lw.buf[:idx+1]); err != nil {
			return n, err
		}
		lw.buf = lw.buf[idx+1:]
	}

	return n, nil
}

// Flush writes any remaining buffered content.
func (lw *LineWriter) Flush() error {
	if len(lw.buf) > 0 {
		_, err := lw.w.Write(lw.buf)
		lw.buf = nil
		return err
	}
	return nil
}

// CopyLines copies from r to h line by line, processing each line.
func CopyLines(r io.Reader, h *StreamHandler, isStderr bool) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if isStderr {
			if err := h.WriteStderr(line); err != nil {
				return err
			}
		} else {
			if err := h.WriteStdout(line); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
