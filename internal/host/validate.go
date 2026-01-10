package host

import "github.com/rileyhilliard/rr/internal/errors"

// ValidateConnection checks that a connection is usable for remote operations.
// Returns nil if the connection is valid, or an error describing the issue.
//
// A connection is valid if:
//   - conn is not nil
//   - conn.Client is not nil (unless IsLocal is true)
func ValidateConnection(conn *Connection) error {
	if conn == nil {
		return errors.New(errors.ErrSSH,
			"No connection provided",
			"Connect to a remote host first using host selection.")
	}

	// Local connections don't need an SSH client
	if conn.IsLocal {
		return nil
	}

	if conn.Client == nil {
		return errors.New(errors.ErrSSH,
			"Connection has no SSH client",
			"The connection may have been closed. Try reconnecting.")
	}

	return nil
}

// ValidateConnectionForSync validates a connection for sync operations.
// Returns a sync-specific error message.
func ValidateConnectionForSync(conn *Connection) error {
	if conn == nil || conn.Client == nil {
		return errors.New(errors.ErrSync,
			"No active SSH connection",
			"Connect to the remote host first.")
	}
	return nil
}

// ValidateConnectionForLock validates a connection for lock operations.
// Returns a lock-specific error message.
func ValidateConnectionForLock(conn *Connection) error {
	if conn == nil || conn.Client == nil {
		return errors.New(errors.ErrLock,
			"Can't grab the lock without a connection",
			"Connect to the remote host first.")
	}
	return nil
}

// HasClient returns true if the connection has an active SSH client.
// This is a simple check useful for boolean contexts (like isAlive checks).
func HasClient(conn *Connection) bool {
	return conn != nil && conn.Client != nil
}
