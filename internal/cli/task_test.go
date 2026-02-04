package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: formatHosts functionality moved to internal/util.JoinOrNone
// Tests for that are in internal/util/strings_test.go

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

	assert.Equal(t, "test [args...]", cmd.Use)
	assert.Equal(t, "Run tests", cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCreateTaskCommand_EmptyDescription(t *testing.T) {
	task := config.TaskConfig{
		Run: "make build",
	}

	cmd := createTaskCommand("build", task)

	assert.Equal(t, "build [args...]", cmd.Use)
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

// captureStdout captures stdout during function execution and returns the output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)

	return buf.String()
}

func TestRenderTaskHeader(t *testing.T) {
	tests := []struct {
		name        string
		taskName    string
		task        *config.TaskConfig
		wantStrings []string
	}{
		{
			name:     "simple run command with description",
			taskName: "test",
			task: &config.TaskConfig{
				Run:         "make test",
				Description: "Run unit tests",
			},
			wantStrings: []string{"Task:", "test", "Run unit tests", "$", "make test"},
		},
		{
			name:     "run command without description",
			taskName: "build",
			task: &config.TaskConfig{
				Run: "make build",
			},
			wantStrings: []string{"Task:", "build", "$", "make build"},
		},
		{
			name:     "multi-step task",
			taskName: "deploy",
			task: &config.TaskConfig{
				Description: "Deploy to production",
				Steps: []config.TaskStep{
					{Name: "build", Run: "make build"},
					{Name: "push", Run: "docker push"},
					{Name: "apply", Run: "kubectl apply"},
				},
			},
			wantStrings: []string{"Task:", "deploy", "Deploy to production", "(3 steps)"},
		},
		{
			name:     "multi-step task without description",
			taskName: "pipeline",
			task: &config.TaskConfig{
				Steps: []config.TaskStep{
					{Name: "step1", Run: "echo 1"},
					{Name: "step2", Run: "echo 2"},
				},
			},
			wantStrings: []string{"Task:", "pipeline", "(2 steps)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			pd := ui.NewPhaseDisplay(&buf)

			output := captureStdout(t, func() {
				renderTaskHeader(pd, tt.taskName, tt.task)
			})

			// Combine both outputs (stdout and buffer from PhaseDisplay)
			combined := output + buf.String()

			for _, want := range tt.wantStrings {
				assert.Contains(t, combined, want, "output should contain %q", want)
			}
		})
	}
}

func TestRenderTaskHeader_EmptyTask(t *testing.T) {
	var buf bytes.Buffer
	pd := ui.NewPhaseDisplay(&buf)

	// Task with neither Run nor Steps should produce no output
	task := &config.TaskConfig{}

	output := captureStdout(t, func() {
		renderTaskHeader(pd, "empty", task)
	})

	combined := output + buf.String()

	// Should not contain "Task:" since there's nothing to render
	assert.NotContains(t, combined, "Task:")
}

func TestRenderTaskSummary(t *testing.T) {
	tests := []struct {
		name        string
		result      *exec.TaskResult
		taskName    string
		totalTime   time.Duration
		execTime    time.Duration
		host        string
		wantStrings []string
		wantSymbol  string
	}{
		{
			name: "successful task",
			result: &exec.TaskResult{
				ExitCode:   0,
				FailedStep: -1,
			},
			taskName:    "test",
			totalTime:   5 * time.Second,
			execTime:    3 * time.Second,
			host:        "my-server",
			wantStrings: []string{"test", "completed", "my-server", "5.0s total", "3.0s exec"},
			wantSymbol:  ui.SymbolSuccess,
		},
		{
			name: "failed simple task",
			result: &exec.TaskResult{
				ExitCode:   1,
				FailedStep: -1,
			},
			taskName:    "build",
			totalTime:   2 * time.Second,
			execTime:    1 * time.Second,
			host:        "build-server",
			wantStrings: []string{"build", "failed", "build-server", "exit code 1", "2.0s"},
			wantSymbol:  ui.SymbolFail,
		},
		{
			name: "failed multi-step task",
			result: &exec.TaskResult{
				ExitCode:   2,
				FailedStep: 1,
				StepResults: []exec.StepResult{
					{Name: "init", ExitCode: 0},
					{Name: "compile", ExitCode: 2},
				},
			},
			taskName:    "deploy",
			totalTime:   10 * time.Second,
			execTime:    8 * time.Second,
			host:        "prod-server",
			wantStrings: []string{"deploy", "failed", "step 'compile'", "prod-server", "exit code 2"},
			wantSymbol:  ui.SymbolFail,
		},
		{
			name: "success with fractional seconds",
			result: &exec.TaskResult{
				ExitCode:   0,
				FailedStep: -1,
			},
			taskName:    "quick",
			totalTime:   1500 * time.Millisecond,
			execTime:    750 * time.Millisecond,
			host:        "fast-host",
			wantStrings: []string{"quick", "completed", "fast-host", "1.5s total", "0.8s exec"},
			wantSymbol:  ui.SymbolSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			pd := ui.NewPhaseDisplay(&buf)

			output := captureStdout(t, func() {
				renderTaskSummary(pd, tt.result, tt.taskName, tt.totalTime, tt.execTime, tt.host)
			})

			for _, want := range tt.wantStrings {
				assert.Contains(t, output, want, "output should contain %q", want)
			}

			assert.Contains(t, output, tt.wantSymbol, "output should contain symbol %q", tt.wantSymbol)
		})
	}
}

func TestRenderTaskSummary_MultiStepFirstStepFails(t *testing.T) {
	var buf bytes.Buffer
	pd := ui.NewPhaseDisplay(&buf)

	result := &exec.TaskResult{
		ExitCode:   1,
		FailedStep: 0,
		StepResults: []exec.StepResult{
			{Name: "setup", ExitCode: 1},
			{Name: "build", ExitCode: 0},
		},
	}

	output := captureStdout(t, func() {
		renderTaskSummary(pd, result, "pipeline", 5*time.Second, 2*time.Second, "test-host")
	})

	assert.Contains(t, output, "pipeline")
	assert.Contains(t, output, "failed")
	assert.Contains(t, output, "step 'setup'")
	assert.Contains(t, output, ui.SymbolFail)
}

func TestBuildParallelTaskLongDescription(t *testing.T) {
	tests := []struct {
		name        string
		taskName    string
		task        config.TaskConfig
		wantStrings []string
	}{
		{
			name:     "basic parallel task",
			taskName: "test-all",
			task: config.TaskConfig{
				Description: "Run all tests in parallel",
				Parallel:    []string{"test-unit", "test-integration", "test-e2e"},
			},
			wantStrings: []string{
				"test-all",
				"Run all tests in parallel",
				"3 tasks concurrently",
				"1. test-unit",
				"2. test-integration",
				"3. test-e2e",
				"--stream",
				"--verbose",
				"--quiet",
				"--fail-fast",
				"--dry-run",
			},
		},
		{
			name:     "parallel task with fail_fast",
			taskName: "ci",
			task: config.TaskConfig{
				Parallel: []string{"lint", "test"},
				FailFast: true,
			},
			wantStrings: []string{
				"ci",
				"Stops on first failure",
			},
		},
		{
			name:     "parallel task with max_parallel",
			taskName: "deploy",
			task: config.TaskConfig{
				Parallel:    []string{"deploy-a", "deploy-b"},
				MaxParallel: 2,
			},
			wantStrings: []string{
				"deploy",
				"Max concurrent tasks: 2",
			},
		},
		{
			name:     "parallel task without description",
			taskName: "build-all",
			task: config.TaskConfig{
				Parallel: []string{"build-frontend", "build-backend"},
			},
			wantStrings: []string{
				"build-all",
				"2 tasks concurrently",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := buildParallelTaskLongDescription(tt.taskName, tt.task)

			for _, want := range tt.wantStrings {
				assert.Contains(t, desc, want, "description should contain %q", want)
			}
		})
	}
}

func TestCreateParallelTaskCommand(t *testing.T) {
	task := config.TaskConfig{
		Parallel:    []string{"task-a", "task-b"},
		Description: "Run tasks in parallel",
	}

	cmd := createParallelTaskCommand("parallel-test", task)

	assert.Equal(t, "parallel-test", cmd.Use)
	assert.Equal(t, "Run tasks in parallel", cmd.Short)
	assert.NotNil(t, cmd.RunE)

	// Check parallel-specific flags exist
	flags := []string{"host", "tag", "local", "stream", "verbose", "quiet", "fail-fast", "max-parallel", "no-logs", "dry-run"}
	for _, flagName := range flags {
		flag := cmd.Flags().Lookup(flagName)
		require.NotNil(t, flag, "should have --%s flag", flagName)
	}
}

func TestCreateParallelTaskCommand_EmptyDescription(t *testing.T) {
	task := config.TaskConfig{
		Parallel: []string{"task-a"},
	}

	cmd := createParallelTaskCommand("mytask", task)

	assert.Equal(t, "Run 'mytask' parallel tasks", cmd.Short)
}

func TestBuildTaskLongDescription_WithDependencies(t *testing.T) {
	task := config.TaskConfig{
		Run:         "make deploy",
		Description: "Deploy the app",
		Depends: []config.DependencyItem{
			{Task: "build"},
			{Task: "test"},
		},
	}

	desc := buildTaskLongDescription("deploy", task)

	assert.Contains(t, desc, "Dependencies:")
	assert.Contains(t, desc, "1. build")
	assert.Contains(t, desc, "2. test")
	assert.Contains(t, desc, "--skip-deps")
	assert.Contains(t, desc, "--from")
}

func TestBuildTaskLongDescription_WithParallelDependencies(t *testing.T) {
	task := config.TaskConfig{
		Run: "make ci",
		Depends: []config.DependencyItem{
			{Parallel: []string{"lint", "typecheck"}},
			{Task: "test"},
		},
	}

	desc := buildTaskLongDescription("ci", task)

	assert.Contains(t, desc, "Dependencies:")
	assert.Contains(t, desc, "[lint, typecheck] (parallel)")
	assert.Contains(t, desc, "2. test")
}

func TestBuildTaskLongDescription_OrchestratorTask(t *testing.T) {
	// A task with dependencies but no run command
	task := config.TaskConfig{
		Description: "Full CI pipeline",
		Depends: []config.DependencyItem{
			{Task: "lint"},
			{Task: "test"},
		},
	}

	desc := buildTaskLongDescription("ci", task)

	assert.Contains(t, desc, "Dependencies:")
	assert.Contains(t, desc, "orchestrates its dependencies")
	assert.NotContains(t, desc, "Command:")
}

func TestCreateTaskCommand_WithDependencies(t *testing.T) {
	task := config.TaskConfig{
		Run: "make deploy",
		Depends: []config.DependencyItem{
			{Task: "build"},
		},
	}

	cmd := createTaskCommand("deploy", task)

	// Should have dependency flags
	skipDepsFlag := cmd.Flags().Lookup("skip-deps")
	require.NotNil(t, skipDepsFlag, "should have --skip-deps flag")

	fromFlag := cmd.Flags().Lookup("from")
	require.NotNil(t, fromFlag, "should have --from flag")
}

func TestCreateTaskCommand_WithoutDependencies(t *testing.T) {
	task := config.TaskConfig{
		Run: "make test",
	}

	cmd := createTaskCommand("test", task)

	// Should NOT have dependency flags
	skipDepsFlag := cmd.Flags().Lookup("skip-deps")
	assert.Nil(t, skipDepsFlag, "should not have --skip-deps flag")

	fromFlag := cmd.Flags().Lookup("from")
	assert.Nil(t, fromFlag, "should not have --from flag")
}

func TestCreateTaskCommand_HasRepeatFlag(t *testing.T) {
	task := config.TaskConfig{
		Run: "make test",
	}

	cmd := createTaskCommand("test", task)

	repeatFlag := cmd.Flags().Lookup("repeat")
	require.NotNil(t, repeatFlag, "should have --repeat flag")
	assert.Equal(t, "int", repeatFlag.Value.Type())
}
