//go:build windows

package sshutil

import (
	"os/exec"
	"time"
)

func setProcessGroup(cmd *exec.Cmd) {}

func (c *proxyConn) Close() error {
	c.closeOnce.Do(func() {
		c.stdin.Close()

		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}

		select {
		case err := <-c.waitCh:
			c.closeErr = err
		case <-time.After(2 * time.Second):
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
				c.closeErr = <-c.waitCh
			}
		}

		c.stdout.Close()
	})
	return c.closeErr
}
