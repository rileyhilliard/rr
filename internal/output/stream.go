package output

import (
	"bufio"
	"io"
	"sync"
)

// LineBuffer provides line-buffering for streaming data.
// It accumulates bytes until a newline is encountered, then returns complete lines.
type LineBuffer struct {
	buf []byte
}

// ProcessBytes adds data to the buffer and returns any complete lines.
// Incomplete lines remain buffered until more data arrives.
func (lb *LineBuffer) ProcessBytes(p []byte) []string {
	lb.buf = append(lb.buf, p...)
	var lines []string

	for {
		idx := -1
		for i, b := range lb.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}

		lines = append(lines, string(lb.buf[:idx]))
		lb.buf = lb.buf[idx+1:]
	}

	return lines
}

// Flush returns any remaining buffered content as a final line.
// Returns empty string if buffer is empty.
func (lb *LineBuffer) Flush() string {
	if len(lb.buf) == 0 {
		return ""
	}
	line := string(lb.buf)
	lb.buf = nil
	return line
}

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

	// Captured stderr content for post-execution analysis.
	// Limited to prevent memory issues with large output.
	stderrCapture []byte
	maxCapture    int
}

// NewStreamHandler creates a handler that writes to the given stdout/stderr.
func NewStreamHandler(stdout, stderr io.Writer) *StreamHandler {
	return &StreamHandler{
		stdout:     stdout,
		stderr:     stderr,
		maxCapture: 4096, // Capture first 4KB of stderr for analysis
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

// GetFormatter returns the current formatter, or nil if none is set.
func (h *StreamHandler) GetFormatter() Formatter {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.formatter
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

	// Capture stderr content for post-execution analysis (limited)
	if len(h.stderrCapture) < h.maxCapture {
		remaining := h.maxCapture - len(h.stderrCapture)
		lineBytes := []byte(line + "\n")
		if len(lineBytes) > remaining {
			lineBytes = lineBytes[:remaining]
		}
		h.stderrCapture = append(h.stderrCapture, lineBytes...)
	}

	processedLine := line
	if h.formatter != nil {
		processedLine = h.formatter.ProcessLine(line)
	}

	_, err := h.stderr.Write([]byte(processedLine + "\n"))
	return err
}

// GetStderrCapture returns the captured stderr content for analysis.
func (h *StreamHandler) GetStderrCapture() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return string(h.stderrCapture)
}

// streamWriter wraps the handler to implement io.Writer.
type streamWriter struct {
	handler    *StreamHandler
	isStderr   bool
	lineBuffer LineBuffer
}

// Write implements io.Writer with line buffering.
// Incomplete lines are buffered until a newline arrives.
func (w *streamWriter) Write(p []byte) (n int, err error) {
	n = len(p)

	lines := w.lineBuffer.ProcessBytes(p)
	for _, line := range lines {
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
	if line := w.lineBuffer.Flush(); line != "" {
		if w.isStderr {
			return w.handler.WriteStderr(line)
		}
		return w.handler.WriteStdout(line)
	}
	return nil
}

// LineWriter wraps an io.Writer to write complete lines.
type LineWriter struct {
	w          io.Writer
	lineBuffer LineBuffer
}

// NewLineWriter creates a line-buffered writer.
func NewLineWriter(w io.Writer) *LineWriter {
	return &LineWriter{w: w}
}

// Write buffers data and writes complete lines.
func (lw *LineWriter) Write(p []byte) (n int, err error) {
	n = len(p)

	lines := lw.lineBuffer.ProcessBytes(p)
	for _, line := range lines {
		// Write line with newline since ProcessBytes strips it
		if _, err := lw.w.Write([]byte(line + "\n")); err != nil {
			return n, err
		}
	}

	return n, nil
}

// Flush writes any remaining buffered content.
func (lw *LineWriter) Flush() error {
	if line := lw.lineBuffer.Flush(); line != "" {
		_, err := lw.w.Write([]byte(line))
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
