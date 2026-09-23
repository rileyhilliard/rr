package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReservedTaskNames_CoverBuiltinCommands keeps config.ReservedTaskNames
// in step with the command tree: a task named after a built-in command or
// alias would collide with it. Commands generated from tasks are skipped
// (other tests leave some registered on rootCmd).
func TestReservedTaskNames_CoverBuiltinCommands(t *testing.T) {
	rootCmd.InitDefaultHelpCmd()

	for _, cmd := range rootCmd.Commands() {
		if _, isTask := cmd.Annotations[taskCommandAnnotation]; isTask {
			continue
		}
		// cobra's hidden __complete commands can't clash with task names users write.
		if strings.HasPrefix(cmd.Name(), "__") {
			continue
		}
		for _, name := range append([]string{cmd.Name()}, cmd.Aliases...) {
			assert.True(t, config.IsReservedTaskName(name),
				"built-in command %q is missing from config.ReservedTaskNames", name)
		}
	}
}

func TestRegisterTaskCommands_Annotated(t *testing.T) {
	original := tasksRegistered
	tasksRegistered = false
	defer func() { tasksRegistered = original }()

	RegisterTaskCommands(&config.Config{Tasks: map[string]config.TaskConfig{
		"annotated-task": {Run: "echo hi"},
	}})

	cmd, _, err := rootCmd.Find([]string{"annotated-task"})
	require.NoError(t, err)
	assert.Equal(t, "annotated-task", cmd.Annotations[taskCommandAnnotation])
}

// TestInitTemplate_LoadsWithoutWarnings makes sure 'rr init' never writes a
// config that warns on first load (e.g. the removed output: section).
func TestInitTemplate_LoadsWithoutWarnings(t *testing.T) {
	content := generateProjectConfigContent(&projectConfigValues{hostRefs: []string{"dev"}})
	assert.NotContains(t, content, "output:")

	path := filepath.Join(t.TempDir(), ".rr.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Warnings)
}

func TestDedupeConfigWarnings(t *testing.T) {
	a := config.Warning{File: "/p/.rr.yaml", Key: "taks", Message: "m", Suggestion: "s"}
	b := config.Warning{File: "/p/.rr.yaml", Key: "output", Message: "m2", Suggestion: "s2"}
	assert.Equal(t, []config.Warning{a, b}, dedupeConfigWarnings([]config.Warning{a, b, a, b, a}))
}

func TestEmitConfigWarnings(t *testing.T) {
	warnings := []config.Warning{
		{File: "/p/.rr.yaml", Key: "taks", Message: "Unknown config key 'taks' is ignored", Suggestion: "Check 'taks'"},
		{File: "/p/.rr.yaml", Key: "output", Message: "The 'output' section has no effect", Suggestion: "Remove it"},
	}

	tests := []struct {
		name   string
		pretty bool
		check  func(t *testing.T, out string)
	}{
		{
			name: "structured mode emits one config warn event per warning",
			check: func(t *testing.T, out string) {
				lines := strings.Split(strings.TrimSpace(out), "\n")
				require.Len(t, lines, 2)
				for i, line := range lines {
					var ev PhaseEvent
					require.NoError(t, json.Unmarshal([]byte(line), &ev))
					assert.Equal(t, "phase", ev.Type)
					assert.Equal(t, "config", ev.Phase)
					assert.Equal(t, "warn", ev.Status)
					assert.Equal(t, warnings[i].Key, ev.Details["key"])
					assert.Equal(t, warnings[i].File, ev.Details["file"])
					assert.Equal(t, warnings[i].Message, ev.Details["message"])
					assert.Equal(t, warnings[i].Suggestion, ev.Details["suggestion"])
				}
			},
		},
		{
			name:   "pretty mode prints styled warnings",
			pretty: true,
			check: func(t *testing.T, out string) {
				assert.Contains(t, out, "Unknown config key 'taks' is ignored")
				assert.Contains(t, out, "The 'output' section has no effect")
				assert.NotContains(t, out, `"phase"`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPretty, oldEmitted := prettyMode, configWarningsEmitted
			defer func() { prettyMode, configWarningsEmitted = oldPretty, oldEmitted }()
			prettyMode = tt.pretty
			configWarningsEmitted = false

			out := captureStderr(t, func() { emitConfigWarnings(warnings) })
			tt.check(t, out)

			again := captureStderr(t, func() { emitConfigWarnings(warnings) })
			assert.Empty(t, again, "warnings are emitted once per invocation")
		})
	}
}

func TestCollectConfigWarnings_ProjectAndGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	globalPath := filepath.Join(home, ".rr", "config.yaml")
	require.NoError(t, os.WriteFile(globalPath,
		[]byte("version: 1\nhosts:\n  dev:\n    ssh: [dev]\n    dir: /tmp/x\n    sshh: [typo]\n"), 0o644))

	projectWarning := config.Warning{File: "/p/.rr.yaml", Key: "taks", Message: "m", Suggestion: "s"}
	oldState := discoveryState
	defer func() { discoveryState = oldState }()
	discoveryState = &configDiscoveryState{Warnings: []config.Warning{projectWarning, projectWarning}}

	got := collectConfigWarnings()
	require.Len(t, got, 2)
	assert.Equal(t, projectWarning, got[0])
	assert.Equal(t, "hosts.dev.sshh", got[1].Key)
	assert.Equal(t, globalPath, got[1].File)
}
