package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The setup-file hint blames a setup command only when that setup ran. A
// bare local run (--local with no local host) of rr run runs just the
// command, without defaults.setup, so a missing file there is the command's
// own; a task runs defaults.setup even bare, so the hint applies.
func TestSetupFileHint_OnlyForSetupThatRan(t *testing.T) {
	const settings = "hosts: [box]\ndefaults:\n  setup:\n    - . ./scripts/env.sh"

	t.Run("rr run, bare local", func(t *testing.T) {
		withStructuredOutput(t)
		projectDir, _ := writeRoutingConfigs(t, false, settings)
		t.Chdir(projectDir)

		code, events, err := runQuietly(t, RunOptions{Command: "cat ./scripts/env.sh", Local: true})
		require.NoError(t, err, events)
		require.NotEqual(t, 0, code, events)

		result := commandResult(t, events)
		assert.NotContains(t, result.Details, "hint", "defaults.setup never ran: %s", events)
	})

	t.Run("task, bare local", func(t *testing.T) {
		withStructuredOutput(t)
		projectDir, _ := writeRoutingConfigs(t, false, settings)
		t.Chdir(projectDir)

		var code int
		var err error
		var events string
		captureStdout(t, func() {
			events = captureStderr(t, func() {
				code, err = RunTask(TaskOptions{TaskName: "show", Local: true})
			})
		})
		require.NoError(t, err, events)
		require.NotEqual(t, 0, code, events)
		_, statErr := os.Stat(filepath.Join(projectDir, "show.out"))
		require.True(t, os.IsNotExist(statErr), "the failed setup stopped the task")

		result := commandResult(t, events)
		assert.Contains(t, result.Details["hint"], "the setup command '. ./scripts/env.sh' needs it", events)
	})
}

// commandResult returns the result event from events, skipping the
// command's own stderr lines between the JSON events.
func commandResult(t *testing.T, events string) PhaseEvent {
	t.Helper()
	for _, line := range strings.Split(events, "\n") {
		var ev PhaseEvent
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Type == "result" {
			return ev
		}
	}
	require.Fail(t, "no result event", events)
	return PhaseEvent{}
}
