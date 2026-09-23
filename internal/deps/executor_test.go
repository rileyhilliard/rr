package deps

import (
	"bytes"
	"context"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	rrerrors "github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// A dependency cut off by a dropped connection comes back with its stage
// recorded and its exit code kept, so the caller can still report the run.
func TestExecutor_ConnectionLostKeepsResult(t *testing.T) {
	tasks := map[string]config.TaskConfig{
		"a": {Run: "echo a"},
		"b": {Run: "echo b", Depends: []config.DependencyItem{{Task: "a"}}},
	}
	plan, err := NewResolver(tasks).Resolve("b", ResolveOptions{})
	require.NoError(t, err)

	client := sshtesting.NewMockClient("test-host")
	client.SetCommandResponse("echo a", sshtesting.CommandResponse{
		Error: rrerrors.WrapWithCode(&ssh.ExitMissingError{}, rrerrors.ErrSSH, "Lost the connection", ""),
	})
	resolved := &config.ResolvedConfig{
		Global:  &config.GlobalConfig{},
		Project: &config.Config{Tasks: tasks},
	}
	var out bytes.Buffer
	executor := NewExecutor(resolved, &host.Connection{Name: "test-host", Client: client},
		ExecutorOptions{Stdout: &out, Stderr: &out})

	result, err := executor.Execute(context.Background(), plan)

	require.Error(t, err)
	assert.True(t, sshutil.IsConnectionLost(err))
	require.NotNil(t, result)
	assert.Equal(t, 0, result.FailedStage)
	require.Len(t, result.StageResults, 1)
	assert.Equal(t, -1, result.ExitCode())
}

// FailedStage stays the first failed stage when a later one loses the
// connection.
func TestExecutor_ConnectionLostAfterEarlierFailure(t *testing.T) {
	tasks := map[string]config.TaskConfig{
		"a": {Run: "exit 3"},
		"b": {Run: "echo b", Depends: []config.DependencyItem{{Task: "a"}}},
		"c": {Run: "echo c", Depends: []config.DependencyItem{{Task: "b"}}},
	}
	plan, err := NewResolver(tasks).Resolve("c", ResolveOptions{})
	require.NoError(t, err)

	client := sshtesting.NewMockClient("test-host")
	client.SetCommandResponse("exit 3", sshtesting.CommandResponse{ExitCode: 3})
	client.SetCommandResponse("echo b", sshtesting.CommandResponse{
		Error: rrerrors.WrapWithCode(&ssh.ExitMissingError{}, rrerrors.ErrSSH, "Lost the connection", ""),
	})
	resolved := &config.ResolvedConfig{
		Global:  &config.GlobalConfig{},
		Project: &config.Config{Tasks: tasks},
	}
	var out bytes.Buffer
	executor := NewExecutor(resolved, &host.Connection{Name: "test-host", Client: client},
		ExecutorOptions{Stdout: &out, Stderr: &out})

	result, err := executor.Execute(context.Background(), plan)

	require.Error(t, err)
	assert.Equal(t, 0, result.FailedStage)
	require.Len(t, result.StageResults, 2)
	assert.Equal(t, 3, result.ExitCode())
}
