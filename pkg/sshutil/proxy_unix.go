//go:build !windows

package sshutil

import (
	"os/exec"
	"syscall"
	"time"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
