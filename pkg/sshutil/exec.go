package sshutil

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/rileyhilliard/rr/internal/errors"
	"golang.org/x/crypto/ssh"
)

// Exec runs a command on the remote host and returns the output.
// Returns stdout, stderr, exit code, and any error.
// Exit code is -1 if the command couldn't be executed at all.
func (c *Client) Exec(cmd string) (stdout, stderr []byte, exitCode int, err error) {
	session, err := c.newSSHSession()
	if err != nil {
		return nil, nil, -1, errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't create an SSH session",
			"The connection might have dropped. Try reconnecting.")
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	exitCode = 0
	err = session.Run(cmd)
	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
			err = nil // Command ran, just had non-zero exit
		} else {
			return nil, nil, -1, errors.WrapWithCode(err, errors.ErrExec,
				fmt.Sprintf("Couldn't run: %s", cmd),
				"Make sure the command exists on the remote.")
		}
	}

	return stdoutBuf.Bytes(), stderrBuf.Bytes(), exitCode, nil
}

// ExecStream runs a command and streams output to the provided writers.
// Returns the exit code and any error.
// Exit code is -1 if the command couldn't be executed at all.
func (c *Client) ExecStream(cmd string, stdout, stderr io.Writer) (exitCode int, err error) {
	return c.ExecStreamContext(context.Background(), cmd, stdout, stderr)
}

// ExecStreamContext runs a command with context cancellation support.
// When the context is cancelled, SIGINT is sent to the remote process.
// Returns the exit code and any error.
// Exit code is -1 if the command couldn't be executed at all.
func (c *Client) ExecStreamContext(ctx context.Context, cmd string, stdout, stderr io.Writer) (exitCode int, err error) {
	session, err := c.newSSHSession()
	if err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't create an SSH session",
			"The connection might have dropped. Try reconnecting.")
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr

	// Start the command (non-blocking)
	if err := session.Start(cmd); err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrExec,
			fmt.Sprintf("Couldn't start: %s", cmd),
			"Make sure the command exists on the remote.")
	}

	// Wait for either command completion or context cancellation
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context cancelled - send SIGINT to the remote process
		_ = session.Signal(ssh.SIGINT)
		// Give it a moment to terminate gracefully, then force close
		select {
		case runErr := <-done:
			return exitCodeFromError(runErr), ctx.Err()
		default:
			// Force close the session if it doesn't respond to SIGINT
			session.Close()
			return 130, ctx.Err()
		}
	case runErr := <-done:
		return exitCodeFromError(runErr), nil
	}
}

// exitCodeFromError extracts the exit code from an error.
func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus()
	}
	return -1
}

// ExecPTY runs a command with a pseudo-terminal allocated.
// This is useful for commands that expect an interactive terminal.
// Returns the exit code and any error.
func (c *Client) ExecPTY(cmd string, stdout, stderr io.Writer) (exitCode int, err error) {
	session, err := c.newSSHSession()
	if err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't create an SSH session",
			"The connection might have dropped. Try reconnecting.")
	}
	defer session.Close()

	// Request pseudo-terminal
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // Disable echoing
		ssh.TTY_OP_ISPEED: 14400, // Input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // Output speed = 14.4kbaud
	}

	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't allocate a PTY",
			"The remote might not support pseudo-terminals.")
	}

	session.Stdout = stdout
	session.Stderr = stderr

	exitCode = 0
	runErr := session.Run(cmd)
	if runErr != nil {
		if exitErr, ok := runErr.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			return -1, errors.WrapWithCode(runErr, errors.ErrExec,
				fmt.Sprintf("Couldn't run: %s", cmd),
				"Make sure the command exists on the remote.")
		}
	}

	return exitCode, nil
}

// ExecInteractive runs a command with full stdin/stdout/stderr handling.
// This allows for interactive commands where input is needed.
func (c *Client) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error) {
	session, err := c.newSSHSession()
	if err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't create an SSH session",
			"The connection might have dropped. Try reconnecting.")
	}
	defer session.Close()

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr

	exitCode = 0
	runErr := session.Run(cmd)
	if runErr != nil {
		if exitErr, ok := runErr.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			return -1, errors.WrapWithCode(runErr, errors.ErrExec,
				fmt.Sprintf("Couldn't run: %s", cmd),
				"Make sure the command exists on the remote.")
		}
	}

	return exitCode, nil
}

// Shell starts an interactive shell session.
// The caller is responsible for handling stdin/stdout/stderr.
func (c *Client) Shell(stdin io.Reader, stdout, stderr io.Writer) error {
	session, err := c.newSSHSession()
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrSSH,
			"Failed to create SSH session",
			"Connection may have been closed. Try reconnecting.")
	}
	defer session.Close()

	// Request pseudo-terminal for shell
	modes := ssh.TerminalModes{
		ssh.ECHO:          1, // Enable echoing
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", 80, 24, modes); err != nil {
		return errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't allocate a PTY for the shell",
			"The remote might not support pseudo-terminals.")
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr

	if err := session.Shell(); err != nil {
		return errors.WrapWithCode(err, errors.ErrSSH,
			"Couldn't start the shell",
			"Make sure your user has shell access on the remote.")
	}

	return session.Wait()
}
