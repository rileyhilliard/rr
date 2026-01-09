package sshutil

import "io"

// SSHClient defines the interface for SSH command execution.
// Both the real Client and mock implementations satisfy this interface.
//
// This interface enables testing of SSH-dependent code without requiring
// actual SSH connections. The mock implementation provides a virtual
// filesystem that responds realistically to common shell commands.
type SSHClient interface {
	// Exec runs a command and returns stdout, stderr, and exit code.
	// Exit code is -1 if the command couldn't be executed at all.
	// A non-zero exit code with nil error means the command ran but failed.
	Exec(cmd string) (stdout, stderr []byte, exitCode int, err error)

	// ExecStream runs a command and streams output to the provided writers.
	// Returns the exit code and any error.
	ExecStream(cmd string, stdout, stderr io.Writer) (exitCode int, err error)

	// Close closes the SSH connection.
	Close() error

	// GetHost returns the original host/alias used to connect.
	GetHost() string

	// GetAddress returns the resolved host:port address.
	GetAddress() string

	// NewSession creates a new SSH session for command execution or liveness checks.
	// The returned session should be closed after use.
	NewSession() (Session, error)
}

// Session represents an SSH session that can be closed.
// This is a minimal interface for the ssh.Session type.
type Session interface {
	io.Closer
}
