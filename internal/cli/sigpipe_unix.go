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
func ignoreSigpipe() {
	signal.Ignore(syscall.SIGPIPE)
}
