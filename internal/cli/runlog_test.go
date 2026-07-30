package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTailLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.log")

	t.Run("missing file returns nil", func(t *testing.T) {
		assert.Nil(t, tailLines(filepath.Join(dir, "nope.log"), 5))
	})

	t.Run("fewer lines than requested returns all", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o644))
		assert.Equal(t, []string{"a", "b"}, tailLines(path, 10))
	})

	t.Run("returns last n lines", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte("1\n2\n3\n4\n5\n"), 0o644))
		assert.Equal(t, []string{"4", "5"}, tailLines(path, 2))
	})

	t.Run("empty file returns nil", func(t *testing.T) {
		require.NoError(t, os.WriteFile(path, []byte(""), 0o644))
		assert.Nil(t, tailLines(path, 3))
	})

	t.Run("large file reads only the end", func(t *testing.T) {
		big := strings.Repeat("filler line\n", 200_000) // > maxTailReadBytes
		require.NoError(t, os.WriteFile(path, []byte(big+"last\n"), 0o644))
		lines := tailLines(path, 1)
		assert.Equal(t, []string{"last"}, lines)
	})
}

// TestAttachRunOutcomeNoTests pins that a run collecting zero tests is flagged
// in the result envelope. Without the no_tests detail such a run is reported
// exactly like a clean suite - the original false-green bug.
func TestAttachRunOutcomeNoTests(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		logContents string
		wantNoTests bool
	}{
		{
			name:        "pytest collected nothing",
			command:     "uv run pytest tests/scripts/test_cluster.py -k classify",
			logContents: "collected 0 items\n\n===== no tests ran in 0.05s =====\n",
			wantNoTests: true,
		},
		{
			name:        "pytest ran tests",
			command:     "uv run pytest tests/",
			logContents: "collected 2 items\n\n===== 2 passed in 0.10s =====\n",
			wantNoTests: false,
		},
		{
			name:        "collect-only is an intentional zero-test run",
			command:     "uv run pytest --collect-only tests/",
			logContents: "collected 12 items\n\n12 tests collected in 0.01s\n",
			wantNoTests: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "output.log")
			require.NoError(t, os.WriteFile(logPath, []byte(tt.logContents), 0o600))

			wf := &WorkflowContext{}
			attachRunOutcome(wf, tt.command, logPath, 0)

			noTests, present := wf.ResultDetails["no_tests"].(bool)
			if tt.wantNoTests {
				assert.True(t, present && noTests, "expected no_tests detail")
			} else {
				assert.False(t, present && noTests, "no_tests must not be set")
			}
		})
	}
}
