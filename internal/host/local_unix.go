//go:build !windows

package host

import (
	stderrors "errors"
	"os/exec"
	"syscall"
)

// startOwnProcessGroup puts cmd in a new process group, so a signal sent to
// the group reaches the shell's children and grandchildren too.
func startOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalProcessGroup sends sig to cmd's whole process group.
func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, sig)
	}
}

func interruptProcessGroup(cmd *exec.Cmd) { signalProcessGroup(cmd, syscall.SIGINT) }

func killProcessGroup(cmd *exec.Cmd) { signalProcessGroup(cmd, syscall.SIGKILL) }

// LocalExitCode returns the exit code a shell would report for err from
// exec.Cmd.Wait: the status for a normal exit, 128+signal for a command
// killed by a signal, and -1 when the command didn't run.
func LocalExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !stderrors.As(err, &exitErr) {
		return -1
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return exitErr.ExitCode()
}
