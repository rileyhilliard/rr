//go:build windows

package cli

// ignoreSigpipe is a no-op on Windows, which has no SIGPIPE.
func ignoreSigpipe() {}
