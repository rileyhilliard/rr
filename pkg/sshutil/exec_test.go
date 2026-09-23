package sshutil

import (
	stderrors "errors"
	"testing"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"
)

// TestWaitResult - a session that ends without an exit status (a dropped
// connection) used to come back as exit -1 with no error, so callers couldn't
// say what happened.
func TestWaitResult(t *testing.T) {
	t.Run("clean exit", func(t *testing.T) {
		code, err := waitResult(nil)
		assert.Equal(t, 0, code)
		assert.NoError(t, err)
	})

	t.Run("dropped connection", func(t *testing.T) {
		code, err := waitResult(&ssh.ExitMissingError{})
		assert.Equal(t, -1, code)
		assert.True(t, errors.IsCode(err, errors.ErrSSH))
		assert.True(t, IsConnectionLost(err), "the wrapped error still identifies a lost connection")
	})

	t.Run("connection found dead between commands", func(t *testing.T) {
		err := errors.WrapWithCode(ErrConnectionLost, errors.ErrSSH, "Lost the connection", "")
		assert.True(t, IsConnectionLost(err))
	})

	t.Run("output write failure", func(t *testing.T) {
		code, err := waitResult(stderrors.New("write /tmp/out: no space left on device"))
		assert.Equal(t, -1, code)
		assert.True(t, errors.IsCode(err, errors.ErrExec))
		assert.False(t, IsConnectionLost(err))
	})
}
