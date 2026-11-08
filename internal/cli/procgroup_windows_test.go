//go:build windows

package cli

// processGroup has no Windows equivalent. The tests that call it skip on
// Windows before they get here (the local client needs a POSIX shell); this
// only lets the package's tests compile there.
func processGroup() int { return -1 }
