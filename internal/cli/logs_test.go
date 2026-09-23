package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunLogsList_Wording checks `rr logs list` describes the logs as coming
// from any run, since single runs and tasks write there, not only parallel
// tasks.
func TestRunLogsList_Wording(t *testing.T) {
	writeGlobalConfig(t, "version: 1\n")

	empty := captureStdout(t, func() { require.NoError(t, runLogsList(nil, nil)) })
	assert.Contains(t, empty, "Run a command or task to generate logs.")
	assert.NotContains(t, empty, "parallel")

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr", "logs", "run-20260101-120000"), 0o755))

	listed := captureStdout(t, func() { require.NoError(t, runLogsList(nil, nil)) })
	assert.Contains(t, listed, "Recent Run Logs")
	assert.Contains(t, listed, "run-20260101-120000")
	assert.NotContains(t, listed, "Parallel")
}
