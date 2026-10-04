package host

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
// unchanged. Commands run under $SHELL -c (/bin/sh when unset), the way sshd
// runs a remote command under the user's login shell.
type LocalClient struct{}

var _ sshutil.SSHClient = (*LocalClient)(nil)

// NewLocalClient returns a client that runs commands locally.
func NewLocalClient() *LocalClient {
	return &LocalClient{}
}

func localShellCommand(cmd string) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return exec.Command(shell, "-c", cmd)
}

// Exec runs cmd and returns its output and exit code. A non-zero exit is not
// an error; -1 with an error means it couldn't run.
func (c *LocalClient) Exec(cmd string) (stdout, stderr []byte, exitCode int, err error) {
	var outBuf, errBuf bytes.Buffer
	command := localShellCommand(cmd)
	command.Stdout = &outBuf
	command.Stderr = &errBuf

	if runErr := command.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if stderrors.As(runErr, &exitErr) {
			return outBuf.Bytes(), errBuf.Bytes(), exitErr.ExitCode(), nil
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
// command gets SIGINT, then is killed if it hasn't exited after a grace
// period; the result is then ctx.Err().
func (c *LocalClient) ExecStreamContext(ctx context.Context, cmd string, stdout, stderr io.Writer) (int, error) {
	command := localShellCommand(cmd)
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Start(); err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrExec,
			fmt.Sprintf("Couldn't start: %s", cmd),
			"Make sure the command exists on this machine.")
	}

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	select {
	case <-ctx.Done():
		_ = command.Process.Signal(os.Interrupt)
		select {
		case waitErr := <-done:
			return localExitCode(waitErr), ctx.Err()
		case <-time.After(localInterruptGrace):
			_ = command.Process.Kill()
			<-done
			return 130, ctx.Err()
		}
	case waitErr := <-done:
		if waitErr == nil {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if stderrors.As(waitErr, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return -1, errors.WrapWithCode(waitErr, errors.ErrExec,
			"Couldn't write the command's output",
			"Check that wherever the output goes (a file or a pipe) is writable and has space.")
	}
}

func localExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if stderrors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
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
func NewLocalHostConnection(name string, h config.Host) *Connection {
	return &Connection{
		Name:   name,
		Alias:  LocalAlias,
		Client: NewLocalClient(),
		Host:   h,
	}
}
