package lock

import "errors"

// ErrLocked is returned by TryAcquire when the lock is held by another process.
// This is a sentinel error that can be checked with errors.Is().
var ErrLocked = errors.New("lock is held by another process")
