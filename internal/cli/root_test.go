package cli

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	rrerrors "github.com/rileyhilliard/rr/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsUnknownCommandError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unknown command error",
			err:  errors.New(`unknown command "foo" for "rr"`),
			want: true,
		},
		{
			name: "unknown flag error is not unknown command",
			err:  errors.New(`unknown flag: --foo`),
			want: false,
		},
		{
			name: "other error",
			err:  errors.New("connection failed"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUnknownCommandError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractUnknownCommand(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "standard cobra format",
			err:  errors.New(`unknown command "foo" for "rr"`),
			want: "foo",
		},
		{
			name: "task name",
			err:  errors.New(`unknown command "test" for "rr"`),
			want: "test",
		},
		{
			name: "command with hyphen",
			err:  errors.New(`unknown command "my-task" for "rr"`),
			want: "my-task",
		},
		{
			name: "no quotes returns empty",
			err:  errors.New("unknown command foo"),
			want: "",
		},
		{
			name: "single quote returns empty",
			err:  errors.New(`unknown command "foo`),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUnknownCommand(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConfigDiscoveryState(t *testing.T) {
	// Test that configDiscoveryState struct works as expected
	state := &configDiscoveryState{
		ProjectPath:    "/path/to/.rr.yaml",
		ProjectErr:     nil,
		LoadErr:        errors.New("invalid YAML"),
		ValidateErr:    nil,
		TasksAvailable: []string{"test", "build"},
	}

	assert.Equal(t, "/path/to/.rr.yaml", state.ProjectPath)
	assert.Nil(t, state.ProjectErr)
	assert.NotNil(t, state.LoadErr)
	assert.Contains(t, state.LoadErr.Error(), "invalid YAML")
	assert.Equal(t, []string{"test", "build"}, state.TasksAvailable)
}

// TestHandleUnknownCommand_Envelope checks an unknown command (typically a
// task with no .rr.yaml to define it) reports through the active output
// mode: the error envelope on stderr by default, plain text with --pretty.
func TestHandleUnknownCommand_Envelope(t *testing.T) {
	cobraErr := errors.New(`unknown command "sometask" for "rr"`)
	noConfig := &configDiscoveryState{ProjectErr: rrerrors.New(rrerrors.ErrConfigNotFound,
		"No .rr.yaml found in this directory or parent directories", "Run 'rr init' to create one.")}
	badConfig := &configDiscoveryState{ValidateErr: rrerrors.New(rrerrors.ErrConfig,
		"Task name 'run' is reserved", "Rename the task.")}

	tests := []struct {
		name     string
		state    *configDiscoveryState
		args     []string
		wantCode string // empty: expect pretty plain text
		wantText string
	}{
		{name: "no .rr.yaml", state: noConfig, args: []string{"sometask"}, wantCode: ErrCodeConfigNotFound},
		{name: "invalid config", state: badConfig, args: []string{"sometask"}, wantCode: ErrCodeConfigInvalid},
		{name: "no config problem", state: &configDiscoveryState{}, args: []string{"sometask"}, wantCode: ErrCodeCommandFailed},
		{name: "--pretty", state: noConfig, args: []string{"--pretty", "sometask"}, wantText: "No .rr.yaml found"},
		{name: "-p after the command", state: noConfig, args: []string{"sometask", "-p"}, wantText: "No .rr.yaml found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withDiscoveryState(t, tt.state)
			prevArgs := os.Args
			os.Args = append([]string{"rr"}, tt.args...)
			t.Cleanup(func() { os.Args = prevArgs })

			var code int
			stdout, stderr := runCaptured(t, func() { code = handleUnknownCommand(cobraErr) })

			assert.Equal(t, 1, code)
			assert.Empty(t, stdout)
			if tt.wantCode == "" {
				assert.Contains(t, stderr, tt.wantText)
				assert.False(t, json.Valid([]byte(stderr)), "pretty mode prints text, not JSON")
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

// TestVerboseFlag_WarnsWithoutBreakingJSON checks the deprecated --verbose
// still parses, and warns through the structured channel instead of cobra's
// plain-text deprecation line, which corrupted JSON on stderr.
func TestVerboseFlag_WarnsWithoutBreakingJSON(t *testing.T) {
	withStructuredOutput(t)
	withGlobalHosts(t)
	prevEmitted, prevVerbose := configWarningsEmitted, verbose
	cmd := createTaskCommand("hi", config.TaskConfig{Run: "echo hi"})
	rootCmd.AddCommand(cmd)
	t.Cleanup(func() {
		rootCmd.RemoveCommand(cmd)
		configWarningsEmitted, verbose = prevEmitted, prevVerbose
		if f := rootCmd.PersistentFlags().Lookup("verbose"); f != nil {
			f.Changed = false
		}
	})
	configWarningsEmitted = false

	_, stderr := runCaptured(t, func() {
		require.NoError(t, cmd.ParseFlags([]string{"--verbose"}))
		rootCmd.PersistentPreRun(cmd, nil)
	})

	assert.NotContains(t, stderr, "has been deprecated")
	events := parseEvents(t, stderr)
	var found bool
	for _, ev := range events {
		if ev.Status == "warn" && ev.Details["flag"] == "--verbose" {
			found = true
			assert.Contains(t, ev.Details["message"], "deprecated")
		}
	}
	assert.True(t, found, "expected a --verbose warn event, got: %s", stderr)
}
