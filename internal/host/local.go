package host

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// LocalAlias is the alias a local host's connection reports, in place of the
// SSH alias a remote host connects through.
const LocalAlias = "local"

// localInterruptGrace is how long a cancelled command gets to exit after
// SIGINT before it's killed, matching the SSH client.
var localInterruptGrace = 3 * time.Second

// LocalClient runs commands on this machine through the same interface the
// SSH client implements, so a host with local: true goes through the remote
// code paths (lock, requirement checks, command building, parallel workers)
// unchanged. Commands run under /bin/sh -c; the command rr builds for a host
// picks the user's shell inside that (see exec.BuildRemoteCommand), so the
// outer shell only has to parse it, which a non-POSIX $SHELL like fish can't.
type LocalClient struct{}

var _ sshutil.SSHClient = (*LocalClient)(nil)

// NewLocalClient returns a client that runs commands locally.
func NewLocalClient() *LocalClient {
	return &LocalClient{}
}

// localWaitDelay bounds how long Wait keeps reading a command's output after
// the command's process has exited or been killed, in case something it
// started still holds the pipes open.
const localWaitDelay = time.Second

// LocalCommand returns the command that runs cmd under shell -c on this
// machine the way sshd runs a remote command. It starts in the home
// directory, so setup commands that run before the cd and a relative lock
// dir mean the same thing on every host. It runs in its own process group:
// when ctx is done the whole group is killed, not only the shell, and Wait
// stops reading output localWaitDelay later even if something the command
// started still holds the pipes. The group is a new session with no
// controlling terminal, as under sshd without a pty, so a command that reads
// /dev/tty fails at once instead of stopping on SIGTTIN.
func LocalCommand(ctx context.Context, shell, cmd string) *exec.Cmd {
	command := exec.CommandContext(ctx, shell, "-c", cmd)
	if home, err := os.UserHomeDir(); err == nil {
		command.Dir = home
	}
	startOwnProcessGroup(command)
	command.Cancel = func() error {
		killProcessGroup(command)
		return nil
	}
	command.WaitDelay = localWaitDelay
	return command
}

// localShellCommand builds the command for cmd under /bin/sh.
// Cancellation is handled by ExecStreamContext, which interrupts before it
// kills.
func localShellCommand(cmd string) *exec.Cmd {
	return LocalCommand(context.Background(), "/bin/sh", cmd)
}

// liveCommands are the local commands started by a LocalClient that haven't
// finished yet, so a force-quit can kill them instead of leaving them
// running after rr exits.
var liveCommands = struct {
	sync.Mutex
	cmds map[*exec.Cmd]struct{}
}{cmds: make(map[*exec.Cmd]struct{})}

// startTracked starts command and records it in liveCommands. The caller
// removes it with untrack once it has waited for it.
func startTracked(command *exec.Cmd) error {
	liveCommands.Lock()
	defer liveCommands.Unlock()
	if err := command.Start(); err != nil {
		return err
	}
	liveCommands.cmds[command] = struct{}{}
	return nil
}

func untrack(command *exec.Cmd) {
	liveCommands.Lock()
	delete(liveCommands.cmds, command)
	liveCommands.Unlock()
}

// KillLocalCommands kills the process group of every command a LocalClient
// is still running. rr calls it before a force-quit (a second Ctrl+C), which
// exits without waiting for a cancelled command to stop.
func KillLocalCommands() {
	liveCommands.Lock()
	defer liveCommands.Unlock()
	for command := range liveCommands.cmds {
		killProcessGroup(command)
	}
}

// Exec runs cmd and returns its output and exit code. A non-zero exit is not
// an error; -1 with an error means it couldn't run.
func (c *LocalClient) Exec(cmd string) (stdout, stderr []byte, exitCode int, err error) {
	var outBuf, errBuf bytes.Buffer
	command := localShellCommand(cmd)
	command.Stdout = &outBuf
	command.Stderr = &errBuf

	runErr := startTracked(command)
	if runErr == nil {
		runErr = command.Wait()
		untrack(command)
	}
	if runErr != nil && !stderrors.Is(runErr, exec.ErrWaitDelay) {
		var exitErr *exec.ExitError
		if stderrors.As(runErr, &exitErr) {
			return outBuf.Bytes(), errBuf.Bytes(), LocalExitCode(runErr), nil
		}
		return nil, nil, -1, errors.WrapWithCode(runErr, errors.ErrExec,
			fmt.Sprintf("Couldn't run: %s", cmd),
			"Make sure the command exists on this machine.")
	}
	return outBuf.Bytes(), errBuf.Bytes(), 0, nil
}

// ExecStream runs cmd, streaming its output.
func (c *LocalClient) ExecStream(cmd string, stdout, stderr io.Writer) (int, error) {
	return c.ExecStreamContext(context.Background(), cmd, stdout, stderr)
}

// ExecStreamContext runs cmd, streaming its output. When ctx is cancelled the
// command's process group gets SIGINT, then is killed if it hasn't exited
// after a grace period; the result is then ctx.Err(). Whatever is left in
// the group once the command exits is killed too: a background job ignores
// SIGINT (`sleep 30 & wait`), and would otherwise outlive the cancel.
func (c *LocalClient) ExecStreamContext(ctx context.Context, cmd string, stdout, stderr io.Writer) (int, error) {
	command := localShellCommand(cmd)
	command.Stdout = stdout
	command.Stderr = stderr

	if err := startTracked(command); err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrExec,
			fmt.Sprintf("Couldn't start: %s", cmd),
			"Make sure the command exists on this machine.")
	}
	defer untrack(command)

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	select {
	case <-ctx.Done():
		interruptProcessGroup(command)
		select {
		case waitErr := <-done:
			killProcessGroup(command)
			return LocalExitCode(waitErr), ctx.Err()
		case <-time.After(localInterruptGrace):
			killProcessGroup(command)
			<-done
			return 130, ctx.Err()
		}
	case waitErr := <-done:
		// ErrWaitDelay means the command exited 0 but left something
		// running that still held its output open.
		if waitErr == nil || stderrors.Is(waitErr, exec.ErrWaitDelay) {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if stderrors.As(waitErr, &exitErr) {
			return LocalExitCode(waitErr), nil
		}
		return -1, errors.WrapWithCode(waitErr, errors.ErrExec,
			"Couldn't write the command's output",
			"Check that wherever the output goes (a file or a pipe) is writable and has space.")
	}
}

// Close does nothing: there's no connection to close.
func (c *LocalClient) Close() error { return nil }

// GetHost returns LocalAlias.
func (c *LocalClient) GetHost() string { return LocalAlias }

// GetAddress returns LocalAlias.
func (c *LocalClient) GetAddress() string { return LocalAlias }

// NewSession returns a no-op session; there's no connection to check.
func (c *LocalClient) NewSession() (sshutil.Session, error) { return localSession{}, nil }

// SendRequest always succeeds, so a local connection is always alive.
func (c *LocalClient) SendRequest(string, bool, []byte) (bool, []byte, error) {
	return true, nil, nil
}

type localSession struct{}

func (localSession) Close() error { return nil }

// NewLocalHostConnection returns the connection for a host with local: true.
// A host without a dir gets the one config.ResolveHosts gives it (the
// project root, or the current directory), so a command built for it never
// runs in the home directory it starts in.
func NewLocalHostConnection(name string, h config.Host) *Connection {
	if h.Dir == "" {
		h.Dir = config.DefaultLocalHostDir()
	}
	return &Connection{
		Name:   name,
		Alias:  LocalAlias,
		Client: NewLocalClient(),
		Host:   h,
	}
}

// LocalMachineConnection returns the connection for the host in hosts that
// has local: true, or nil when there's none. Runs that execute on this
// machine without going through that host (--local, local mode, a local
// fallback) take its lock through this connection, so they can't run beside
// a job on it.
func LocalMachineConnection(hosts map[string]config.Host) *Connection {
	for name := range hosts {
		if hosts[name].Local {
			return NewLocalHostConnection(name, hosts[name])
		}
	}
	return nil
}
