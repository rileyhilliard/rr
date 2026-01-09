package exec

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// TaskResult contains the result of a task execution.
type TaskResult struct {
	ExitCode    int          // Final exit code (0 if all steps passed)
	StepResults []StepResult // Results for each step (nil for single-command tasks)
	FailedStep  int          // Index of first failed step (-1 if none)
}

// StepResult contains the result of a single step execution.
type StepResult struct {
	Name     string // Step name (or "run" for single-command tasks)
	ExitCode int    // Exit code from the step
	OnFail   string // The on_fail behavior for this step
}

// ExecuteTask runs a task on the given connection.
// Handles both single-command tasks (Run field) and multi-step tasks (Steps field).
func ExecuteTask(conn *host.Connection, task *config.TaskConfig, env map[string]string, workDir string, stdout, stderr io.Writer) (*TaskResult, error) {
	if task == nil {
		return nil, errors.New(errors.ErrExec,
			"Task is nil",
			"This is an internal error - task should be validated before execution")
	}

	// Single-command task
	if task.Run != "" {
		exitCode, err := executeCommand(conn, task.Run, env, workDir, stdout, stderr)
		if err != nil {
			return nil, err
		}
		return &TaskResult{
			ExitCode:   exitCode,
			FailedStep: -1,
		}, nil
	}

	// Multi-step task
	if len(task.Steps) == 0 {
		return nil, errors.New(errors.ErrExec,
			"Task has no run command or steps",
			"Add either 'run' or 'steps' to your task configuration")
	}

	return executeSteps(conn, task.Steps, env, workDir, stdout, stderr)
}

// executeSteps runs multiple steps in sequence.
func executeSteps(conn *host.Connection, steps []config.TaskStep, env map[string]string, workDir string, stdout, stderr io.Writer) (*TaskResult, error) {
	result := &TaskResult{
		StepResults: make([]StepResult, 0, len(steps)),
		FailedStep:  -1,
	}

	for i, step := range steps {
		stepResult := StepResult{
			Name:   step.Name,
			OnFail: config.GetStepOnFail(step),
		}

		if step.Name == "" {
			stepResult.Name = fmt.Sprintf("step %d", i+1)
		}

		exitCode, err := executeCommand(conn, step.Run, env, workDir, stdout, stderr)
		if err != nil {
			return nil, err
		}

		stepResult.ExitCode = exitCode
		result.StepResults = append(result.StepResults, stepResult)

		if exitCode != 0 {
			if result.FailedStep == -1 {
				result.FailedStep = i
			}
			result.ExitCode = exitCode

			// Check on_fail behavior
			if stepResult.OnFail == config.OnFailStop {
				// Stop execution
				return result, nil
			}
			// Continue to next step
		}
	}

	return result, nil
}

// executeCommand runs a single command on the connection.
func executeCommand(conn *host.Connection, cmd string, env map[string]string, workDir string, stdout, stderr io.Writer) (int, error) {
	// Build the full command with environment variables and working directory
	fullCmd := buildCommand(cmd, env, workDir, conn.IsLocal)

	if conn.IsLocal {
		return ExecuteLocal(fullCmd, "", stdout, stderr)
	}

	// Remote execution
	return conn.Client.ExecStream(fullCmd, stdout, stderr)
}

// buildCommand constructs the full command string with env vars and cd.
func buildCommand(cmd string, env map[string]string, workDir string, isLocal bool) string {
	// For local execution, we handle workDir via the exec.Command.Dir field
	// But for remote, we need to cd to the directory
	if isLocal {
		// For local, env is handled by the shell's environment
		// We'll prepend env exports to the command
		return buildEnvPrefix(env) + cmd
	}

	// Remote execution: cd to workDir and set env
	if workDir != "" {
		workDir = config.Expand(workDir)
		return fmt.Sprintf("cd %q && %s%s", workDir, buildEnvPrefix(env), cmd)
	}

	return buildEnvPrefix(env) + cmd
}

// buildEnvPrefix creates the environment variable prefix for a command.
func buildEnvPrefix(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}

	prefix := ""
	for k, v := range env {
		// Use shell-safe quoting
		prefix += fmt.Sprintf("export %s=%q; ", k, v)
	}
	return prefix
}

// ExecuteLocalTask is a convenience function for local task execution.
// Uses the current working directory and standard output.
func ExecuteLocalTask(task *config.TaskConfig, env map[string]string) (*TaskResult, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrExec,
			"Failed to get working directory",
			"Check directory permissions")
	}

	// Create a local connection
	conn := &host.Connection{
		Name:    "local",
		Alias:   "local",
		IsLocal: true,
	}

	return ExecuteTask(conn, task, env, workDir, os.Stdout, os.Stderr)
}

// BuildRemoteCommand constructs a remote command with shell config, setup commands, and working directory.
// This is the recommended way to build commands for remote execution with full configuration support.
func BuildRemoteCommand(cmd string, host *config.Host) string {
	var parts []string

	// Add setup commands if configured
	if len(host.SetupCommands) > 0 {
		parts = append(parts, host.SetupCommands...)
	}

	// Add cd to working directory
	if host.Dir != "" {
		dir := config.Expand(host.Dir)
		parts = append(parts, fmt.Sprintf("cd %q", dir))
	}

	// Add the actual command
	parts = append(parts, cmd)

	// Join all parts with &&
	fullCmd := strings.Join(parts, " && ")

	// Wrap in shell if configured
	if host.Shell != "" {
		// Shell format is "bash -l -c" - we append the quoted command
		return fmt.Sprintf("%s %q", host.Shell, fullCmd)
	}

	return fullCmd
}
