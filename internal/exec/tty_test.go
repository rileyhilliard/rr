package exec

import (
	"context"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/tty"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ttyClient is a mock client that can also run TTY commands, and records
// whether it was asked to.
type ttyClient struct {
	*sshtesting.MockClient
	called bool
}

func (c *ttyClient) ExecTTYContext(context.Context, string, sshutil.TTYOptions) (int, error) {
	c.called = true
	return 0, nil
}

func TestExecuteTask_TTYRemote(t *testing.T) {
	task := &config.TaskConfig{Run: "sudo true"}

	t.Run("a client without TTY support says so", func(t *testing.T) {
		conn := &host.Connection{Name: "box", Client: sshtesting.NewMockClient("box")}
		_, err := ExecuteTask(context.Background(), conn, task, nil, nil, "~/rr", nil, nil, &TaskExecOptions{TTY: true})
		require.Error(t, err)
		assert.True(t, errors.IsCode(err, errors.ErrExec))
		assert.Contains(t, err.Error(), "can't run commands with a terminal")
	})

	t.Run("the terminal is needed before the command starts", func(t *testing.T) {
		if tty.IsTerminal() {
			t.Skip("stdin and stdout are a terminal")
		}
		client := &ttyClient{MockClient: sshtesting.NewMockClient("box")}
		conn := &host.Connection{Name: "box", Client: client}
		_, err := ExecuteTask(context.Background(), conn, task, nil, nil, "~/rr", nil, nil, &TaskExecOptions{TTY: true})
		require.Error(t, err, "no terminal to lend under go test")
		assert.False(t, client.called, "the command doesn't start without the terminal")
	})
}

// A Ctrl+C between the steps of a steps: task stops the run: the next step
// doesn't start, and the terminal is never touched.
func TestExecuteTTY_CancelledBeforeStart(t *testing.T) {
	client := &ttyClient{MockClient: sshtesting.NewMockClient("box")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exitCode, err := ExecuteTTY(ctx, client, "sudo true", nil)
	assert.Equal(t, 130, exitCode)
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, client.called)
}
