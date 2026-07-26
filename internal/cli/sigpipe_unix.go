//go:build !windows

package cli

import (
	"os/signal"
	"syscall"
)

// ignoreSigpipe stops SIGPIPE from killing the process with exit 141 when
// a consumer closes its end of a pipe (e.g. `rr run ... | head`). With the
// signal ignored, writes surface EPIPE errors instead, which the stream
// handler tolerates while the run log keeps capturing output.
//
// The SIG_IGN disposition is inherited across exec, so locally spawned
// commands (--local, local fallback) also see EPIPE errors instead of dying
// on SIGPIPE. Well-behaved tools treat the two identically; the run's exit
// code comes from the command either way.
func ignoreSigpipe() {
	signal.Ignore(syscall.SIGPIPE)
}
