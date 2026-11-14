//go:build !windows

package sshutil

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup starts the proxy in its own session: its own process group,
// so Close can signal everything it started, and no controlling terminal, so
// a prompt from any hop (a password, a new host key) fails at once with ssh's
// reason instead of stopping the process on the terminal read.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func (c *proxyConn) Close() error {
	c.closeOnce.Do(func() {
		c.stdin.Close()

		if c.cmd.Process != nil {
			_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
		}

		select {
		case err := <-c.waitCh:
			c.closeErr = err
		case <-time.After(2 * time.Second):
			if c.cmd.Process != nil {
				_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
				c.closeErr = <-c.waitCh
			}
		}

		c.stdout.Close()
	})
	return c.closeErr
}
