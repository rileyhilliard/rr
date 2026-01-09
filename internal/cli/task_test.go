package cli

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatHosts(t *testing.T) {
	tests := []struct {
		name  string
		hosts []string
		want  string
	}{
		{
			name:  "empty slice returns none",
			hosts: []string{},
			want:  "(none)",
		},
		{
			name:  "nil slice returns none",
			hosts: nil,
			want:  "(none)",
		},
		{
			name:  "single host",
			hosts: []string{"dev-server"},
			want:  "dev-server",
		},
		{
			name:  "multiple hosts comma separated",
			hosts: []string{"dev", "staging", "prod"},
			want:  "dev, staging, prod",
		},
		{
			name:  "two hosts",
			hosts: []string{"local", "remote"},
			want:  "local, remote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatHosts(tt.hosts)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildTaskLongDescription_WithRun(t *testing.T) {
	task := config.TaskConfig{
		Run:         "make test",
		Description: "Run all tests",
	}

	desc := buildTaskLongDescription("test", task)

	assert.Contains(t, desc, "Run the 'test' task")
	assert.Contains(t, desc, "Run all tests")
	assert.Contains(t, desc, "Command: make test")
}

func TestBuildTaskLongDescription_WithSteps(t *testing.T) {
	task := config.TaskConfig{
		Description: "Deploy the application",
		Steps: []config.TaskStep{
			{Name: "build", Run: "make build"},
			{Name: "deploy", Run: "kubectl apply -f deploy.yaml"},
			{Run: "echo done"}, // unnamed step
		},
	}

	desc := buildTaskLongDescription("deploy", task)

	assert.Contains(t, desc, "Run the 'deploy' task")
	assert.Contains(t, desc, "Deploy the application")
	assert.Contains(t, desc, "Steps:")
	assert.Contains(t, desc, "1. build: make build")
	assert.Contains(t, desc, "2. deploy: kubectl apply -f deploy.yaml")
	assert.Contains(t, desc, "3. step 3: echo done")
}

func TestBuildTaskLongDescription_WithHostRestriction(t *testing.T) {
	task := config.TaskConfig{
		Run:   "make deploy",
		Hosts: []string{"prod-server", "staging-server"},
	}

	desc := buildTaskLongDescription("deploy", task)

	assert.Contains(t, desc, "Restricted to hosts: prod-server, staging-server")
}

func TestBuildTaskLongDescription_NoDescription(t *testing.T) {
	task := config.TaskConfig{
		Run: "echo hello",
	}

	desc := buildTaskLongDescription("greet", task)

	assert.Contains(t, desc, "Run the 'greet' task")
	assert.Contains(t, desc, "Command: echo hello")
	// Should not have double newlines from missing description
	assert.NotContains(t, desc, "\n\n\n")
}

func TestCreateTaskCommand_BasicCommand(t *testing.T) {
	task := config.TaskConfig{
		Run:         "make test",
		Description: "Run tests",
	}

	cmd := createTaskCommand("test", task)

	assert.Equal(t, "test", cmd.Use)
	assert.Equal(t, "Run tests", cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCreateTaskCommand_EmptyDescription(t *testing.T) {
	task := config.TaskConfig{
		Run: "make build",
	}

	cmd := createTaskCommand("build", task)

	assert.Equal(t, "build", cmd.Use)
	assert.Equal(t, "Run the 'build' task", cmd.Short)
}

func TestCreateTaskCommand_HasExpectedFlags(t *testing.T) {
	task := config.TaskConfig{
		Run: "echo test",
	}

	cmd := createTaskCommand("mytask", task)

	hostFlag := cmd.Flags().Lookup("host")
	require.NotNil(t, hostFlag, "should have --host flag")
	assert.Equal(t, "string", hostFlag.Value.Type())
	assert.Empty(t, hostFlag.DefValue)

	tagFlag := cmd.Flags().Lookup("tag")
	require.NotNil(t, tagFlag, "should have --tag flag")
	assert.Equal(t, "string", tagFlag.Value.Type())

	probeFlag := cmd.Flags().Lookup("probe-timeout")
	require.NotNil(t, probeFlag, "should have --probe-timeout flag")
	assert.Equal(t, "string", probeFlag.Value.Type())
}

func TestRegisterTaskCommands_NilConfig(t *testing.T) {
	// Should not panic
	RegisterTaskCommands(nil)
}

func TestRegisterTaskCommands_NilTasks(t *testing.T) {
	cfg := &config.Config{
		Tasks: nil,
	}

	// Should not panic
	RegisterTaskCommands(cfg)
}

func TestTaskOptions_Defaults(t *testing.T) {
	opts := TaskOptions{}

	assert.Empty(t, opts.TaskName)
	assert.Empty(t, opts.Host)
	assert.Empty(t, opts.Tag)
	assert.Zero(t, opts.ProbeTimeout)
	assert.False(t, opts.SkipSync)
	assert.False(t, opts.SkipLock)
	assert.False(t, opts.DryRun)
	assert.Empty(t, opts.WorkingDir)
	assert.False(t, opts.Quiet)
}

func TestTaskOptions_WithValues(t *testing.T) {
	opts := TaskOptions{
		TaskName:   "deploy",
		Host:       "prod",
		Tag:        "critical",
		SkipSync:   true,
		SkipLock:   true,
		DryRun:     true,
		WorkingDir: "/app",
		Quiet:      true,
	}

	assert.Equal(t, "deploy", opts.TaskName)
	assert.Equal(t, "prod", opts.Host)
	assert.Equal(t, "critical", opts.Tag)
	assert.True(t, opts.SkipSync)
	assert.True(t, opts.SkipLock)
	assert.True(t, opts.DryRun)
	assert.Equal(t, "/app", opts.WorkingDir)
	assert.True(t, opts.Quiet)
}
