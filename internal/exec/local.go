package exec

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// localInterruptGrace is how long a cancelled ExecuteLocalContext command
// gets to exit after SIGINT before it's killed, the same grace a local
// host's commands get. A var so tests can shorten it.
var localInterruptGrace = host.LocalInterruptGrace

// ExecuteLocal runs a command locally, streaming output to the provided writers.
// Returns the exit code and any execution error.
// This provides the same interface as SSH execution for consistent handling.
func ExecuteLocal(cmd string, workDir string, stdout, stderr io.Writer) (exitCode int, err error) {
	return ExecuteLocalContext(context.Background(), cmd, workDir, stdout, stderr)
}

// ExecuteLocalContext is ExecuteLocal that stops the command when ctx is
// done (see RunLocalCommand).
func ExecuteLocalContext(ctx context.Context, cmd string, workDir string, stdout, stderr io.Writer) (exitCode int, err error) {
	// Use shell to interpret the command (handles pipes, redirects, etc.)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	command := exec.Command(shell, "-c", cmd)

	// Set working directory if specified
	if workDir != "" {
		command.Dir = workDir
	}

	// Connect stdout/stderr
	command.Stdout = stdout
	command.Stderr = stderr

	return RunLocalCommand(ctx, command)
}

// RunLocalCommand runs command, which must not be started yet, and returns
// its exit code; a non-zero exit is not an error. If ctx is done first, the
// command gets SIGINT, then SIGKILL if it's still running
// localInterruptGrace later, and the result is its exit code (130 when
// killed) with ctx.Err().
//
// The command keeps rr's process group and session, so it shares the
// terminal: a Ctrl+C there reaches it directly, as it does any foreground
// job. The cancel covers what the terminal doesn't, such as SIGTERM sent to
// rr alone. Only the process itself is signalled; a child a shell started
// stops when the shell passes the signal on or exits. If something it
// started still holds the output open after the kill, this returns without
// waiting for it.
func RunLocalCommand(ctx context.Context, command *exec.Cmd) (exitCode int, err error) {
	if err := command.Start(); err != nil {
		return -1, errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't run the command locally",
			"Make sure the command exists and is executable.")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		_ = command.Process.Signal(os.Interrupt) // fails only if it already exited
		select {
		case runErr = <-done:
			return host.LocalExitCode(runErr), ctx.Err()
		case <-time.After(localInterruptGrace):
			_ = command.Process.Kill()
			return 130, ctx.Err() // as a local host reports a killed command
		}
	}
	if runErr != nil {
		// Check if it's an exit error (command ran but returned non-zero)
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		// Actual execution failure
		return -1, errors.WrapWithCode(runErr, errors.ErrExec,
			"Couldn't run the command locally",
			"Make sure the command exists and is executable.")
	}

	return 0, nil
}

// ExecuteLocalWithInput runs a command locally with stdin support.
// Returns the exit code and any execution error.
func ExecuteLocalWithInput(cmd string, workDir string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	command := exec.Command(shell, "-c", cmd)

	if workDir != "" {
		command.Dir = workDir
	}

	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr

	runErr := command.Run()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, errors.WrapWithCode(runErr, errors.ErrExec,
			"Couldn't run the command locally",
			"Make sure the command exists and is executable.")
	}

	return 0, nil
}

// ExecuteLocalCapture runs a command locally and captures all output.
// Returns stdout, stderr, exit code, and any execution error.
func ExecuteLocalCapture(cmd string, workDir string) (stdout, stderr []byte, exitCode int, err error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	command := exec.Command(shell, "-c", cmd)

	if workDir != "" {
		command.Dir = workDir
	}

	// Use CombinedOutput alternative with separate buffers
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, -1, errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't create stdout pipe",
			"This shouldn't happen - please report this bug!")
	}

	stderrPipe, err := command.StderrPipe()
	if err != nil {
		return nil, nil, -1, errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't create stderr pipe",
			"This shouldn't happen - please report this bug!")
	}

	if err := command.Start(); err != nil {
		return nil, nil, -1, errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't start the command",
			"Make sure the command exists and is executable.")
	}

	// Read all output
	stdout, _ = io.ReadAll(stdoutPipe)
	stderr, _ = io.ReadAll(stderrPipe)

	runErr := command.Wait()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return stdout, stderr, exitErr.ExitCode(), nil
		}
		return stdout, stderr, -1, errors.WrapWithCode(runErr, errors.ErrExec,
			"Failed to execute local command",
			"Check that the command exists and is executable")
	}

	return stdout, stderr, 0, nil
}
