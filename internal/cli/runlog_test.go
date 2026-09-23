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

// TestAttachRunOutcomePipedExitCode pins the caveat that explains why the exit
// code can't be trusted on a zero-test run: a pipe means the shell reported the
// last stage's status, not the runner's. The flag is advisory only - rr does not
// rewrite the command, since `cmd | grep -q` tolerates upstream failure on
// purpose.
func TestAttachRunOutcomePipedExitCode(t *testing.T) {
	const noTestsLog = "collected 0 items\n\n===== no tests ran in 0.05s =====\n"

	tests := []struct {
		name      string
		command   string
		log       string
		wantPiped bool
	}{
		{
			name:      "piped zero-test run",
			command:   "uv run pytest -k classify | tail -8",
			log:       noTestsLog,
			wantPiped: true,
		},
		{
			name:      "unpiped zero-test run",
			command:   "uv run pytest -k classify",
			log:       noTestsLog,
			wantPiped: false,
		},
		{
			name:      "pipe but tests actually ran",
			command:   "uv run pytest tests/ | tail -8",
			log:       "collected 2 items\n\n===== 2 passed in 0.10s =====\n",
			wantPiped: false,
		},
		{
			name:      "logical or is not a pipe",
			command:   "uv run pytest -k classify || true",
			log:       noTestsLog,
			wantPiped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "output.log")
			require.NoError(t, os.WriteFile(logPath, []byte(tt.log), 0o600))

			wf := &WorkflowContext{}
			attachRunOutcome(wf, tt.command, logPath, 0)

			piped, present := wf.ResultDetails["piped_exit_code"].(bool)
			assert.Equal(t, tt.wantPiped, present && piped)
		})
	}
}

// TestAttachRunOutcomeFailures checks the JSON renderer of the shared
// outcome: failures only for failed runs, file:line joined, and messages
// truncated here (the parser keeps them whole).
func TestAttachRunOutcomeFailures(t *testing.T) {
	long := strings.Repeat("x", maxFailureMessageLen+200)
	log := "collected 1 item\n\n" +
		"tests/test_math.py::test_div FAILED [100%]\n\n" +
		"=================================== FAILURES ===================================\n" +
		"_________________________________ test_div _________________________________\n\n" +
		"E       ZeroDivisionError: " + long + "\n\n" +
		"tests/test_math.py:12: ZeroDivisionError\n" +
		"========================= 1 failed in 0.03s ==========================\n"
	logPath := filepath.Join(t.TempDir(), "output.log")
	require.NoError(t, os.WriteFile(logPath, []byte(log), 0o600))

	t.Run("failed run lists failures", func(t *testing.T) {
		wf := &WorkflowContext{}
		outcome := attachRunOutcome(wf, "pytest tests/", logPath, 1)

		require.Len(t, outcome.Failures, 1)
		assert.Contains(t, outcome.Failures[0].Message, long, "the parsed outcome keeps the full message")

		failures, ok := wf.ResultDetails["failures"].([]map[string]string)
		require.True(t, ok)
		require.Len(t, failures, 1)
		assert.Equal(t, "test_div", failures[0]["name"])
		assert.Equal(t, "tests/test_math.py:12", failures[0]["file"])
		assert.LessOrEqual(t, len(failures[0]["message"]), maxFailureMessageLen+3)
		assert.True(t, strings.HasSuffix(failures[0]["message"], "..."))
	})

	t.Run("passing run lists none", func(t *testing.T) {
		wf := &WorkflowContext{}
		attachRunOutcome(wf, "pytest tests/", logPath, 0)
		assert.NotContains(t, wf.ResultDetails, "failures")
	})
}
