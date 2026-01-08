package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetRootCmd creates a fresh root command for testing.
// This prevents test pollution from registered task commands.
func resetRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rr",
		Short: "Road Runner - Sync and execute on remote machines",
	}
	return cmd
}

func TestCompletionBashGeneration(t *testing.T) {
	cmd := resetRootCmd()

	var buf bytes.Buffer
	err := cmd.GenBashCompletion(&buf)

	require.NoError(t, err)
	output := buf.String()

	// Verify basic bash completion structure
	assert.Contains(t, output, "# bash completion for rr")
	assert.Contains(t, output, "__rr_debug")
	assert.Contains(t, output, "complete -o default -F __start_rr rr")
}

func TestCompletionZshGeneration(t *testing.T) {
	cmd := resetRootCmd()

	var buf bytes.Buffer
	err := cmd.GenZshCompletion(&buf)

	require.NoError(t, err)
	output := buf.String()

	// Verify basic zsh completion structure
	assert.Contains(t, output, "#compdef rr")
	assert.Contains(t, output, "_rr()")
}

func TestCompletionFishGeneration(t *testing.T) {
	cmd := resetRootCmd()

	var buf bytes.Buffer
	err := cmd.GenFishCompletion(&buf, true)

	require.NoError(t, err)
	output := buf.String()

	// Verify basic fish completion structure
	assert.Contains(t, output, "fish completion for rr")
	assert.Contains(t, output, "complete -c rr")
}

func TestCompletionPowershellGeneration(t *testing.T) {
	cmd := resetRootCmd()

	var buf bytes.Buffer
	err := cmd.GenPowerShellCompletion(&buf)

	require.NoError(t, err)
	output := buf.String()

	// Verify basic powershell completion structure (case insensitive check)
	assert.Contains(t, strings.ToLower(output), "powershell completion")
	assert.Contains(t, output, "Register-ArgumentCompleter")
}

func TestCompletionIncludesBuiltinCommands(t *testing.T) {
	// Test using the real rootCmd which has all commands registered
	// Cobra uses dynamic completion - it calls the binary with __completeNoDesc
	// to get completions at runtime, so we verify the completion script contains
	// the necessary infrastructure to call back into the binary

	var buf bytes.Buffer
	err := rootCmd.GenBashCompletion(&buf)

	require.NoError(t, err)
	output := buf.String()

	// Verify the completion script has the dynamic completion infrastructure
	assert.Contains(t, output, "__completeNoDesc", "should use dynamic completion")
	assert.Contains(t, output, "__start_rr", "should have start function")
	assert.Contains(t, output, "_rr_root_command", "should have root command function")

	// Verify commands with flags generate their own functions
	// These are statically generated because the commands have local flags
	assert.Contains(t, output, "_rr_run()")
	assert.Contains(t, output, "_rr_exec()")
	assert.Contains(t, output, "_rr_sync()")
	assert.Contains(t, output, "_rr_completion()")
}

func TestCompletionIncludesTaskNames(t *testing.T) {
	// Modern Cobra uses dynamic completion - it calls the binary with __completeNoDesc
	// at runtime to get completions. This test verifies that task commands are properly
	// registered as subcommands, which makes them available for dynamic completion.

	cmd := resetRootCmd()

	// Simulate registering task commands with flags (like real task commands)
	cfg := &config.Config{
		Tasks: map[string]config.TaskConfig{
			"mytask": {
				Run:         "make test",
				Description: "Run tests",
			},
			"mybuild": {
				Run:         "make build",
				Description: "Build project",
			},
		},
	}

	// Register tasks as subcommands with flags (mimics createTaskCommand behavior)
	for name, task := range cfg.Tasks {
		taskCmd := &cobra.Command{
			Use:   name,
			Short: task.Description,
		}
		// Add flags like the real task commands do
		taskCmd.Flags().String("host", "", "target host")
		taskCmd.Flags().String("tag", "", "select by tag")
		taskCmd.Flags().String("probe-timeout", "", "probe timeout")
		cmd.AddCommand(taskCmd)
	}

	// Verify the commands are registered
	assert.Len(t, cmd.Commands(), 2, "should have 2 task commands registered")

	// Find the registered commands
	var foundMytask, foundMybuild bool
	for _, subCmd := range cmd.Commands() {
		if subCmd.Use == "mytask" {
			foundMytask = true
			assert.Equal(t, "Run tests", subCmd.Short)
			// Verify flags are registered
			assert.NotNil(t, subCmd.Flags().Lookup("host"))
			assert.NotNil(t, subCmd.Flags().Lookup("tag"))
		}
		if subCmd.Use == "mybuild" {
			foundMybuild = true
			assert.Equal(t, "Build project", subCmd.Short)
		}
	}
	assert.True(t, foundMytask, "mytask should be registered")
	assert.True(t, foundMybuild, "mybuild should be registered")

	// Verify completion script generates successfully
	var buf bytes.Buffer
	err := cmd.GenBashCompletion(&buf)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())

	// Verify it contains the dynamic completion infrastructure
	output := buf.String()
	assert.Contains(t, output, "__completeNoDesc", "should use dynamic completion")
}

func TestCompletionBashSyntaxValid(t *testing.T) {
	cmd := resetRootCmd()

	// Add some commands
	cmd.AddCommand(&cobra.Command{Use: "run", Short: "Run command"})
	cmd.AddCommand(&cobra.Command{Use: "test", Short: "Run tests"})

	var buf bytes.Buffer
	err := cmd.GenBashCompletion(&buf)

	require.NoError(t, err)
	output := buf.String()

	// Basic syntax checks - ensure no obvious errors
	// Check balanced braces
	openBraces := strings.Count(output, "{")
	closeBraces := strings.Count(output, "}")
	assert.Equal(t, openBraces, closeBraces, "braces should be balanced")

	// Should have the main function defined
	assert.Contains(t, output, "__start_rr()")

	// Verify it contains the expected completion setup
	assert.Contains(t, output, "complete -o default -F __start_rr rr")
}

func TestCompletionCommandValidArgs(t *testing.T) {
	// Verify the completion command has correct valid args
	assert.Contains(t, completionCmd.ValidArgs, "bash")
	assert.Contains(t, completionCmd.ValidArgs, "zsh")
	assert.Contains(t, completionCmd.ValidArgs, "fish")
	assert.Contains(t, completionCmd.ValidArgs, "powershell")
	assert.Len(t, completionCmd.ValidArgs, 4)
}

func TestRegisterTaskCommandsAddsToRoot(t *testing.T) {
	// Save original state
	originalTasksRegistered := tasksRegistered

	// Reset for test
	tasksRegistered = false
	defer func() { tasksRegistered = originalTasksRegistered }()

	cfg := &config.Config{
		Tasks: map[string]config.TaskConfig{
			"mytask": {
				Run:         "echo hello",
				Description: "My custom task",
			},
		},
	}

	// Count commands before registration
	commandsBefore := len(rootCmd.Commands())

	// Register tasks
	RegisterTaskCommands(cfg)

	// Should have one more command
	commandsAfter := len(rootCmd.Commands())
	assert.Equal(t, commandsBefore+1, commandsAfter, "should have added one task command")

	// Find the added command
	var foundTask *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "mytask" {
			foundTask = cmd
			break
		}
	}

	require.NotNil(t, foundTask, "mytask command should be registered")
	assert.Equal(t, "My custom task", foundTask.Short)
}

func TestRegisterTaskCommandsSkipsReservedNames(t *testing.T) {
	// Save original state
	originalTasksRegistered := tasksRegistered

	// Reset for test
	tasksRegistered = false
	defer func() { tasksRegistered = originalTasksRegistered }()

	cfg := &config.Config{
		Tasks: map[string]config.TaskConfig{
			"run": { // Reserved name - should be skipped
				Run:         "echo hello",
				Description: "Should be skipped",
			},
			"validtask": {
				Run:         "echo valid",
				Description: "Valid task",
			},
		},
	}

	// Count commands before
	commandsBefore := len(rootCmd.Commands())

	RegisterTaskCommands(cfg)

	// Should only add the valid task (run is reserved)
	commandsAfter := len(rootCmd.Commands())
	assert.Equal(t, commandsBefore+1, commandsAfter, "should only add non-reserved tasks")
}

func TestRegisterTaskCommandsNilConfig(t *testing.T) {
	// Save original state
	originalTasksRegistered := tasksRegistered

	// Reset for test
	tasksRegistered = false
	defer func() { tasksRegistered = originalTasksRegistered }()

	commandsBefore := len(rootCmd.Commands())

	// Should not panic with nil config
	RegisterTaskCommands(nil)

	commandsAfter := len(rootCmd.Commands())
	assert.Equal(t, commandsBefore, commandsAfter, "nil config should not add commands")
}

func TestRegisterTaskCommandsEmptyTasks(t *testing.T) {
	// Save original state
	originalTasksRegistered := tasksRegistered

	// Reset for test
	tasksRegistered = false
	defer func() { tasksRegistered = originalTasksRegistered }()

	cfg := &config.Config{
		Tasks: nil,
	}

	commandsBefore := len(rootCmd.Commands())

	RegisterTaskCommands(cfg)

	commandsAfter := len(rootCmd.Commands())
	assert.Equal(t, commandsBefore, commandsAfter, "nil tasks should not add commands")
}

func TestTaskCommandHasExpectedFlags(t *testing.T) {
	task := config.TaskConfig{
		Run:         "make test",
		Description: "Run tests",
	}

	cmd := createTaskCommand("test", task)

	// Should have common flags
	hostFlag := cmd.Flags().Lookup("host")
	assert.NotNil(t, hostFlag, "should have --host flag")

	tagFlag := cmd.Flags().Lookup("tag")
	assert.NotNil(t, tagFlag, "should have --tag flag")

	probeFlag := cmd.Flags().Lookup("probe-timeout")
	assert.NotNil(t, probeFlag, "should have --probe-timeout flag")
}

func TestTaskCommandLongDescription(t *testing.T) {
	task := config.TaskConfig{
		Run:         "make test",
		Description: "Run the test suite",
	}

	cmd := createTaskCommand("test", task)

	// Long description should contain task details
	assert.Contains(t, cmd.Long, "test")
	assert.Contains(t, cmd.Long, "Run the test suite")
	assert.Contains(t, cmd.Long, "make test")
}

func TestTaskCommandMultiStep(t *testing.T) {
	task := config.TaskConfig{
		Description: "Build and deploy",
		Steps: []config.TaskStep{
			{Name: "build", Run: "make build"},
			{Name: "test", Run: "make test"},
			{Name: "deploy", Run: "make deploy"},
		},
	}

	cmd := createTaskCommand("deploy", task)

	// Long description should list steps
	assert.Contains(t, cmd.Long, "Steps:")
	assert.Contains(t, cmd.Long, "build")
	assert.Contains(t, cmd.Long, "make build")
	assert.Contains(t, cmd.Long, "make test")
	assert.Contains(t, cmd.Long, "make deploy")
}
