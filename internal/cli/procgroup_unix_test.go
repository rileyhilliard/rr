//go:build !windows

package cli

import "syscall"

// processGroup is rr's own process group, for telling a command that shares
// it from one started in its own session.
func processGroup() int { return syscall.Getpgrp() }
