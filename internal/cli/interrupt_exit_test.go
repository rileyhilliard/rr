package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/lock"
)

// A command that failed because the user stopped rr exits 130, like Ctrl+C
// during the command, and says INTERRUPTED; other errors exit 1. Neither
// mode adds troubleshooting advice to an interrupt.
func TestReportCommandError_ExitCodes(t *testing.T) {
	interrupted := lock.InterruptedError("dev", context.Canceled)
	held := errors.New(errors.ErrLock, "Lock timeout after 5m0s - someone else is using this remote", "Wait for it to finish.")

	tests := []struct {
		name     string
		err      error
		pretty   bool
		wantExit int
		wantCode string // structured mode only
	}{
		{name: "interrupted lock wait", err: interrupted, wantExit: 130, wantCode: ErrCodeInterrupted},
		{name: "interrupted lock wait, wrapped", err: fmt.Errorf("lock phase: %w", interrupted), wantExit: 130, wantCode: ErrCodeInterrupted},
		{name: "interrupted lock wait, pretty", err: interrupted, pretty: true, wantExit: 130},
		{name: "held lock", err: held, wantExit: 1, wantCode: ErrCodeLockHeld},
		{name: "held lock, pretty", err: held, pretty: true, wantExit: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev := prettyMode
			prettyMode = tt.pretty
			t.Cleanup(func() { prettyMode = prev })

			var code int
			stdout, stderr := runCaptured(t, func() { code = reportCommandError(tt.err) })

			assert.Equal(t, tt.wantExit, code)
			assert.Empty(t, stdout)
			assert.NotContains(t, stderr, "Troubleshooting")
			if tt.pretty {
				assert.False(t, json.Valid([]byte(stderr)), "pretty mode prints text: %s", stderr)
				var rrErr *errors.Error
				require.ErrorAs(t, tt.err, &rrErr)
				assert.Contains(t, stderr, rrErr.Message)
				return
			}
			var env JSONEnvelope
			require.NoError(t, json.Unmarshal([]byte(stderr), &env), "stderr: %s", stderr)
			assert.False(t, env.Success)
			require.NotNil(t, env.Error)
			assert.Equal(t, tt.wantCode, env.Error.Code)
		})
	}
}

// Ctrl+C while rr run waits for a held lock stops the wait; the run fails
// with an INTERRUPTED error that exits 130, and the command never runs.
func TestRun_InterruptedLockWaitExits130(t *testing.T) {
	projectDir, lockCfg := writeMachineLockConfigs(t, "[dev]")
	t.Chdir(projectDir)
	project := `version: 1
hosts: [dev]
lock:
  timeout: 1m
  dir: ` + lockCfg.Dir + `
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	holdMachineLock(t, lockCfg)

	self, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	// The workflow catches SIGINT from before the lock phase, so the signal
	// cancels the run instead of killing the test binary.
	interrupted := onPhaseEvent(t, "lock", "waiting", func() { _ = self.Signal(os.Interrupt) })

	marker := filepath.Join(projectDir, "ran")
	var runErr error
	captureStdout(t, func() {
		_, runErr = Run(RunOptions{Command: "touch " + marker})
	})

	select {
	case <-interrupted:
	default:
		t.Fatal("the lock wait never started")
	}
	require.Error(t, runErr)
	assert.True(t, errors.IsCode(runErr, errors.ErrInterrupted), "got %v", runErr)
	assert.Equal(t, ErrCodeInterrupted, ErrorToJSON(runErr).Code)
	assert.Equal(t, 130, errorExitCode(runErr))
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "the command never ran")
}
