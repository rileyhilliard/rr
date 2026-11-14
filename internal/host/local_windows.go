//go:build windows

package host

import (
	stderrors "errors"
	"os/exec"
)

func startOwnProcessGroup(*exec.Cmd) {}

func interruptProcessGroup(cmd *exec.Cmd) { killProcessGroup(cmd) }

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// LocalExitCode returns the exit code for err from exec.Cmd.Wait, or -1 when
// the command didn't run.
func LocalExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !stderrors.As(err, &exitErr) {
		return -1
	}
	return exitErr.ExitCode()
}
