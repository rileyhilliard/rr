//go:build !windows

package tty

import (
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Supported reports whether --tty works on this platform.
const Supported = true

// pollInterval is how long the stdin reader waits for input before checking
// whether it's been stopped. It bounds how long Close waits for the reader.
const pollInterval = 50 * time.Millisecond

// rawTerminals holds the saved state of every terminal a live Session put in
// raw mode, so RestoreAll can put them back on a forced exit.
var (
	rawMu        sync.Mutex
	rawTerminals = map[*Session]struct{}{}
)

// Session is the local terminal lent to one command. Start it right before
// the command runs and Close it right after.
type Session struct {
	fd       int
	oldState *term.State
	size     sshutil.WindowSize

	stdin  *stdinReader
	resize chan sshutil.WindowSize
	winch  chan os.Signal
	stop   chan struct{}
	wg     sync.WaitGroup

	closeOnce sync.Once
	closeErr  error
}

// Start puts stdin in raw mode, reads the window size, and starts watching
// for resizes. Keystrokes, Ctrl+C included, then go to the command as bytes
// instead of being handled by the local terminal, as with ssh -t.
func Start() (*Session, error) {
	return start(int(os.Stdin.Fd()), int(os.Stdout.Fd()))
}

// start is Start on the given terminal fds, so tests can hand it a PTY in
// place of the real stdin and stdout.
func start(fd, outFd int) (*Session, error) {
	width, height, err := term.GetSize(outFd)
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't read the terminal size",
			"Run from an interactive terminal, or run without --tty.")
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't put the terminal in raw mode",
			"Run from an interactive terminal, or run without --tty.")
	}

	s := &Session{
		fd:       fd,
		oldState: oldState,
		size:     sshutil.WindowSize{Width: width, Height: height},
		stdin:    newStdinReader(fd),
		resize:   make(chan sshutil.WindowSize, 1),
		winch:    make(chan os.Signal, 1),
		stop:     make(chan struct{}),
	}

	rawMu.Lock()
	rawTerminals[s] = struct{}{}
	rawMu.Unlock()

	signal.Notify(s.winch, unix.SIGWINCH)
	s.wg.Add(1)
	go s.watchResize(outFd)

	return s, nil
}

// Stdin returns what the user types. It returns io.EOF once the session is
// closed, and reads nothing after that.
func (s *Session) Stdin() io.Reader { return s.stdin }

// Size returns the window size when the session started.
func (s *Session) Size() sshutil.WindowSize { return s.size }

// Resize delivers the new window size after each resize. Only the latest
// size is kept if the receiver falls behind. It closes when the session does.
func (s *Session) Resize() <-chan sshutil.WindowSize { return s.resize }

// watchResize sends the window size after each SIGWINCH until stop closes.
func (s *Session) watchResize(outFd int) {
	defer s.wg.Done()
	defer close(s.resize)
	for {
		select {
		case <-s.stop:
			return
		case <-s.winch:
			width, height, err := term.GetSize(outFd)
			if err != nil {
				// The terminal went away mid-command; there's no size to
				// forward, and the command ends with the session anyway.
				continue
			}
			size := sshutil.WindowSize{Width: width, Height: height}
			// Drop a size nobody picked up yet: only the latest matters.
			select {
			case <-s.resize:
			default:
			}
			s.resize <- size
		}
	}
}

// Close stops the stdin reader and the resize watcher, waits for both, and
// restores the terminal. Safe to call more than once.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.stdin.stop()
		signal.Stop(s.winch)
		close(s.stop)
		s.wg.Wait()

		rawMu.Lock()
		delete(rawTerminals, s)
		rawMu.Unlock()

		if err := term.Restore(s.fd, s.oldState); err != nil {
			s.closeErr = errors.WrapWithCode(fmt.Errorf("%w: %w", ErrRestore, err), errors.ErrExec,
				"Couldn't restore the terminal after the command",
				"Run 'stty sane' (or 'reset') to fix the terminal.")
		}
	})
	return s.closeErr
}

// RestoreAll restores every terminal a live Session put in raw mode. rr's
// force-exit path calls it, since a deferred Close never runs past os.Exit.
func RestoreAll() {
	rawMu.Lock()
	defer rawMu.Unlock()
	for s := range rawTerminals {
		// The process is exiting; there's nothing left to try if this fails.
		_ = term.Restore(s.fd, s.oldState)
		delete(rawTerminals, s)
	}
}

// stdinReader reads fd only when poll says input is ready, and checks
// between polls whether it's been stopped. A plain read of stdin would stay
// blocked after the command ends and take the user's next keystroke (typed
// for their shell, or for a later rr prompt) for a command that's gone.
type stdinReader struct {
	fd int32 // poll(2) takes an int fd; descriptors are small
	// mu is held while a Read polls or reads, so stop waits for one in
	// progress and no read starts after stop returns.
	mu      sync.Mutex
	stopped bool // guarded by mu
}

func newStdinReader(fd int) *stdinReader {
	return &stdinReader{fd: int32(fd)} //nolint:gosec // a file descriptor fits in an int32, which is what poll(2) takes
}

func (r *stdinReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		n, done, err := r.readOnce(p)
		if done {
			return n, err
		}
	}
}

// readOnce waits up to pollInterval for input and reads it if there is any.
// done is false when nothing was read and Read should poll again.
func (r *stdinReader) readOnce(p []byte) (n int, done bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return 0, true, io.EOF
	}

	fds := []unix.PollFd{{Fd: r.fd, Events: unix.POLLIN}}
	ready, err := unix.Poll(fds, int(pollInterval.Milliseconds()))
	if stderrors.Is(err, unix.EINTR) {
		// A signal (SIGWINCH on a resize) interrupted the poll.
		return 0, false, nil
	}
	if err != nil {
		return 0, true, err
	}
	if ready == 0 || fds[0].Revents == 0 {
		return 0, false, nil
	}
	// Stdin ending (the terminal hung up, the fd was closed) ends the
	// command's input, not the command, so it reads as io.EOF. An error here
	// would fail a command that exits 0: x/crypto's Session.Wait returns the
	// stdin copy's error when the exit status is clean.
	if fds[0].Revents&unix.POLLIN == 0 {
		// POLLHUP, POLLERR or POLLNVAL with nothing left to read.
		return 0, true, io.EOF
	}

	n, err = unix.Read(int(r.fd), p)
	if stderrors.Is(err, unix.EINTR) || stderrors.Is(err, unix.EAGAIN) {
		return 0, false, nil
	}
	if err != nil {
		// EIO from a terminal that hung up, for one.
		return 0, true, io.EOF
	}
	if n == 0 {
		return 0, true, io.EOF
	}
	return n, true, nil
}

// stop makes Read return io.EOF. Taking mu waits for a Read in progress
// (at most pollInterval), so once stop returns nothing more is read from fd.
func (r *stdinReader) stop() {
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
}
