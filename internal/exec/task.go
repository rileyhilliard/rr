package exec

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/util"
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

// TaskExecOptions contains options for task execution.
type TaskExecOptions struct {
	// SetupCommands are prepended to each command (from host + project defaults).
	SetupCommands []string

	// StepHandler is called before and after each step in multi-step tasks.
	// If nil, steps run silently without progress output.
	StepHandler StepHandler
}

// StepHandler receives callbacks during multi-step task execution.
type StepHandler interface {
	// OnStepStart is called before a step begins.
	// stepNum is 1-indexed, totalSteps is the total number of steps.
	OnStepStart(stepNum, totalSteps int, step config.TaskStep)

	// OnStepComplete is called after a step finishes.
	// duration is how long the step took, exitCode is the result.
	OnStepComplete(stepNum, totalSteps int, step config.TaskStep, duration time.Duration, exitCode int)
}

// ExecuteTask runs a task on the given connection.
// Handles both single-command tasks (Run field) and multi-step tasks (Steps field).
// Extra args are appended to single-command tasks (not supported for multi-step).
func ExecuteTask(conn *host.Connection, task *config.TaskConfig, args []string, env map[string]string, workDir string, stdout, stderr io.Writer, opts *TaskExecOptions) (*TaskResult, error) {
	if task == nil {
		return nil, errors.New(errors.ErrExec,
			"No task provided",
			"This shouldn't happen - please report this bug!")
	}

	// Normalize options
	if opts == nil {
		opts = &TaskExecOptions{}
	}

	// Single-command task
	if task.Run != "" {
		cmd := task.Run
		// Append extra args if provided
		if len(args) > 0 {
			cmd = cmd + " " + strings.Join(args, " ")
		}
		exitCode, err := executeCommand(conn, cmd, env, workDir, opts.SetupCommands, stdout, stderr)
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
			"This task doesn't have anything to run",
			"Add a 'run' command or 'steps' to your task config.")
	}

	return executeSteps(conn, task.Steps, env, workDir, opts, stdout, stderr)
}

// executeSteps runs multiple steps in sequence.
func executeSteps(conn *host.Connection, steps []config.TaskStep, env map[string]string, workDir string, opts *TaskExecOptions, stdout, stderr io.Writer) (*TaskResult, error) {
	result := &TaskResult{
		StepResults: make([]StepResult, 0, len(steps)),
		FailedStep:  -1,
	}

	totalSteps := len(steps)

	for i, step := range steps {
		stepNum := i + 1
		stepResult := StepResult{
			Name:   step.Name,
			OnFail: config.GetStepOnFail(step),
		}

		if step.Name == "" {
			stepResult.Name = fmt.Sprintf("step %d", stepNum)
		}

		// Notify handler that step is starting
		if opts.StepHandler != nil {
			opts.StepHandler.OnStepStart(stepNum, totalSteps, step)
		}

		stepStart := time.Now()
		exitCode, err := executeCommand(conn, step.Run, env, workDir, opts.SetupCommands, stdout, stderr)
		stepDuration := time.Since(stepStart)

		if err != nil {
			return nil, err
		}

		stepResult.ExitCode = exitCode
		result.StepResults = append(result.StepResults, stepResult)

		// Notify handler that step completed
		if opts.StepHandler != nil {
			opts.StepHandler.OnStepComplete(stepNum, totalSteps, step, stepDuration, exitCode)
		}

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
func executeCommand(conn *host.Connection, cmd string, env map[string]string, workDir string, setupCommands []string, stdout, stderr io.Writer) (int, error) {
	// Build the full command with environment variables, working directory, and setup commands
	fullCmd := buildCommand(cmd, env, workDir, setupCommands, conn.IsLocal)

	if conn.IsLocal {
		return ExecuteLocal(fullCmd, "", stdout, stderr)
	}

	// Remote execution
	return conn.Client.ExecStream(fullCmd, stdout, stderr)
}

// buildCommand constructs the full command string with setup commands, env vars, and cd.
func buildCommand(cmd string, env map[string]string, workDir string, setupCommands []string, isLocal bool) string {
	var parts []string

	// For local execution, we handle workDir via the exec.Command.Dir field
	// But for remote, we need to cd to the directory
	if !isLocal && workDir != "" {
		workDir = config.ExpandRemote(workDir)
		parts = append(parts, fmt.Sprintf("cd %s", util.ShellQuotePreserveTilde(workDir)))
	}

	// Add setup commands (these run shell commands like "source ~/.local/bin/env")
	parts = append(parts, setupCommands...)

	// Add env prefix and actual command
	cmdWithEnv := buildEnvPrefix(env) + cmd
	parts = append(parts, cmdWithEnv)

	// Join with && so each part must succeed
	return strings.Join(parts, " && ")
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
			"Couldn't figure out the current directory",
			"Check that the directory still exists and you have access.")
	}

	// Create a local connection
	conn := &host.Connection{
		Name:    "local",
		Alias:   "local",
		IsLocal: true,
	}

	return ExecuteTask(conn, task, nil, env, workDir, os.Stdout, os.Stderr, nil)
}

// DefaultShell is used when no shell is configured.
// Falls back to /bin/bash if $SHELL is unset.
const DefaultShell = "${SHELL:-/bin/bash}"

// rcSourceCommand sources the appropriate shell rc file if it exists.
// SSH non-interactive sessions don't source .bashrc/.zshrc, so we explicitly source them.
// This ensures PATH modifications from tools like nvm, bun, pyenv, etc. are available.
// The trailing semicolon ensures this is always a successful command that can be followed by &&.
const rcSourceCommand = `[ -f ~/.bashrc ] && . ~/.bashrc || true; [ -f ~/.zshrc ] && . ~/.zshrc || true;`

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
		dir := config.ExpandRemote(host.Dir)
		parts = append(parts, fmt.Sprintf("cd %s", util.ShellQuotePreserveTilde(dir)))
	}

	// Add the actual command
	parts = append(parts, cmd)

	// Join all parts with &&
	cmdChain := strings.Join(parts, " && ")

	// Prepend rc sourcing to get PATH setup from tools like nvm, bun, pyenv, etc.
	// SSH non-interactive sessions skip .bashrc/.zshrc, so we do it explicitly.
	// The rc source command ends with semicolons and || true, so it's safe to concatenate.
	fullCmd := rcSourceCommand + " " + cmdChain

	// Wrap in shell (use default login shell if not configured)
	shell := host.Shell
	if shell == "" {
		shell = DefaultShell
	}

	// Escape special characters so they're evaluated inside the shell -c, not by the outer shell.
	// Without this, $PATH in setup_commands would be expanded before rc files are sourced,
	// resulting in the minimal PATH instead of the user's configured PATH.
	escapedCmd := fullCmd
	escapedCmd = strings.ReplaceAll(escapedCmd, "\\", "\\\\") // Escape backslashes first
	escapedCmd = strings.ReplaceAll(escapedCmd, "\"", "\\\"") // Escape double quotes
	escapedCmd = strings.ReplaceAll(escapedCmd, "$", "\\$")   // Escape $ to prevent variable expansion
	escapedCmd = strings.ReplaceAll(escapedCmd, "`", "\\`")   // Escape backticks to prevent command substitution

	// Shell format is "bash -c" or custom - we append the quoted command.
	// Using manual "%s" instead of %q because we've already escaped the string properly.
	return fmt.Sprintf("%s -c \"%s\"", shell, escapedCmd) //nolint:gocritic // Manual escaping required
}
