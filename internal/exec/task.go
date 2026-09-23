package exec

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
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
// Extra args replace {args} placeholders in single-command tasks, or are
// appended (shell-quoted) when the command is simple. Compound commands
// (pipes, &&, redirections) without a placeholder reject extra args, since a
// blind append would bind them to the wrong command.
func ExecuteTask(ctx context.Context, conn *host.Connection, task *config.TaskConfig, args []string, env map[string]string, workDir string, stdout, stderr io.Writer, opts *TaskExecOptions) (*TaskResult, error) {
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
		cmd, err := ApplyTaskArgs(task.Run, args)
		if err != nil {
			return nil, err
		}
		exitCode, err := executeCommand(ctx, conn, cmd, env, workDir, opts.SetupCommands, stdout, stderr)
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

	return executeSteps(ctx, conn, task.Steps, env, workDir, opts, stdout, stderr)
}

// ApplyTaskArgs merges extra CLI args into a task's run command.
// Placeholders ({args} / {args:-default}) are substituted with shell-quoted
// args wherever they appear. Without a placeholder, args are appended
// (shell-quoted) only when the command is simple; compound commands error
// because appended args would bind to the last command in the pipeline.
func ApplyTaskArgs(run string, args []string) (string, error) {
	cmd, hadPlaceholder := config.ExpandArgs(run, args)
	if hadPlaceholder || len(args) == 0 {
		return cmd, nil
	}
	if util.IsCompoundCommand(run) {
		return "", errors.New(errors.ErrConfig,
			"This task is a compound command (pipes, &&, or redirections), so extra arguments would land on the last command in the pipeline instead of the one you mean",
			"Add an {args} placeholder where the arguments belong, e.g.: pytest {args:-.} -n 4 | grep ...")
	}
	return cmd + " " + util.ShellQuoteJoin(args), nil
}

// executeSteps runs multiple steps in sequence.
func executeSteps(ctx context.Context, conn *host.Connection, steps []config.TaskStep, env map[string]string, workDir string, opts *TaskExecOptions, stdout, stderr io.Writer) (*TaskResult, error) {
	result := &TaskResult{
		StepResults: make([]StepResult, 0, len(steps)),
		FailedStep:  -1,
	}

	totalSteps := len(steps)

	for i, step := range steps {
		// Check for cancellation between steps
		select {
		case <-ctx.Done():
			return result, errors.WrapWithCode(ctx.Err(), errors.ErrExec,
				"task execution canceled",
				"The task was interrupted (e.g. Ctrl+C).")
		default:
		}

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
		exitCode, err := executeCommand(ctx, conn, step.Run, env, workDir, opts.SetupCommands, stdout, stderr)
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

// executeCommand runs a single command on the connection. Remote commands cd
// into workDir first; local ones run in the current directory.
func executeCommand(ctx context.Context, conn *host.Connection, cmd string, env map[string]string, workDir string, setupCommands []string, stdout, stderr io.Writer) (int, error) {
	if conn.IsLocal {
		return ExecuteLocal(BuildCommand(cmd, env, "", setupCommands), "", stdout, stderr)
	}

	fullCmd := BuildCommand(cmd, env, config.ExpandRemote(workDir), setupCommands)
	return conn.Client.ExecStreamContext(ctx, fullCmd, stdout, stderr)
}

// BuildCommand builds the shell command a task runs, local or remote, single
// or parallel: cd into workDir (skipped when empty), each setup command, then
// env exported ahead of cmd, the parts joined with && so a failure stops the
// chain.
//
// Env keys are exported in sorted order. Values are double-quoted with
// util.ShellDoubleQuote, so the shell expands $VAR references in them (e.g.
// PATH: "$HOME/.local/bin:$PATH") while quotes and backticks stay literal.
func BuildCommand(cmd string, env map[string]string, workDir string, setupCommands []string) string {
	var parts []string
	if workDir != "" {
		parts = append(parts, "cd "+util.ShellQuotePreserveTilde(workDir))
	}
	parts = append(parts, setupCommands...)

	var exports strings.Builder
	for _, k := range slices.Sorted(maps.Keys(env)) {
		fmt.Fprintf(&exports, "export %s=%s; ", k, util.ShellDoubleQuote(env[k]))
	}
	parts = append(parts, exports.String()+cmd)

	return strings.Join(parts, " && ")
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

	return ExecuteTask(context.Background(), conn, task, nil, env, workDir, os.Stdout, os.Stderr, nil)
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
