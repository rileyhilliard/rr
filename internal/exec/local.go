package exec

import (
	"io"
	"os"
	"os/exec"

	"github.com/rileyhilliard/rr/internal/errors"
)

// ExecuteLocal runs a command locally, streaming output to the provided writers.
// Returns the exit code and any execution error.
// This provides the same interface as SSH execution for consistent handling.
func ExecuteLocal(cmd string, workDir string, stdout, stderr io.Writer) (exitCode int, err error) {
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

	// Run the command
	runErr := command.Run()
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
