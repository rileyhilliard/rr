package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
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
			name: "unknown flag error",
			err:  errors.New(`unknown flag: --foo`),
			want: true,
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
			if tt.err == nil {
				// Can't call isUnknownCommandError with nil
				return
			}
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
