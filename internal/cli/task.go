package cli

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/internal/util"
	"github.com/spf13/cobra"
)

// TaskOptions holds options for task execution.
type TaskOptions struct {
	TaskName     string
	Args         []string      // Extra arguments to append to task command
	Host         string        // Preferred host name
	Tag          string        // Filter hosts by tag
	ProbeTimeout time.Duration // Override SSH probe timeout
	SkipSync     bool          // If true, skip sync phase
	SkipLock     bool          // If true, skip locking
	DryRun       bool          // If true, show what would be done without doing it
	WorkingDir   string        // Override local working directory
	Quiet        bool          // If true, minimize output
}

// RunTask executes a named task from the configuration.
// This handles the full workflow: connect, sync, lock, execute.
func RunTask(opts TaskOptions) (int, error) {
	// Setup common workflow phases (config, connect, sync, lock)
	wf, err := SetupWorkflow(WorkflowOptions{
		Host:         opts.Host,
		Tag:          opts.Tag,
		ProbeTimeout: opts.ProbeTimeout,
		SkipSync:     opts.SkipSync,
		SkipLock:     opts.SkipLock,
		WorkingDir:   opts.WorkingDir,
		Quiet:        opts.Quiet,
	})
	if err != nil {
		return 1, err
	}
	defer wf.Close()

	// Get the task from loaded config
	// Get host config from global hosts if connected to a remote host
	var hostCfg *config.Host
	if !wf.Conn.IsLocal {
		if h, ok := wf.Resolved.Global.Hosts[wf.Conn.Name]; ok {
			hostCfg = &h
		}
	}
	task, mergedEnv, err := config.GetTaskWithMergedEnv(wf.Resolved.Project, opts.TaskName, hostCfg)
	if err != nil {
		return 1, err
	}

	// Verify task is allowed on the connected host
	if !config.IsTaskHostAllowed(task, wf.Conn.Name) {
		return 1, errors.New(errors.ErrConfig,
			fmt.Sprintf("Task '%s' can't run on host '%s'", opts.TaskName, wf.Conn.Name),
			fmt.Sprintf("This task is restricted to: %s", util.JoinOrNone(task.Hosts)))
	}

	// Validate args are only used with single-command tasks
	if len(opts.Args) > 0 && len(task.Steps) > 0 {
		return 1, errors.New(errors.ErrConfig,
			"Can't pass arguments to multi-step tasks",
			"Arguments are only supported for tasks with a single 'run' command.")
	}

	// Phase 4: Execute task
	wf.PhaseDisplay.Divider()

	// Show task info
	renderTaskHeader(wf.PhaseDisplay, opts.TaskName, task)
	fmt.Println()

	// Set up output streaming
	streamHandler := output.NewStreamHandler(os.Stdout, os.Stderr)
	streamHandler.SetFormatter(output.NewGenericFormatter())

	execStart := time.Now()

	// Get remote directory for task execution
	remoteDir := ""
	if !wf.Conn.IsLocal {
		remoteDir = config.ExpandRemote(wf.Conn.Host.Dir)
	}

	// Get merged setup commands (host + project defaults)
	setupCommands := config.GetMergedSetupCommands(wf.Resolved.Project, hostCfg)

	// Create exec options with setup commands and step handler for multi-step tasks
	execOpts := &exec.TaskExecOptions{
		SetupCommands: setupCommands,
	}

	// Add step handler for multi-step tasks to show progress
	if len(task.Steps) > 0 {
		execOpts.StepHandler = &taskStepHandler{
			phaseDisplay: wf.PhaseDisplay,
			quiet:        opts.Quiet,
		}
	}

	// Execute the task
	result, err := exec.ExecuteTask(wf.Conn, task, opts.Args, mergedEnv, remoteDir, streamHandler.Stdout(), streamHandler.Stderr(), execOpts)
	execDuration := time.Since(execStart)

	if err != nil {
		return 1, err
	}

	// Release lock early if task completed (wf.Close() will also release, but early release is cleaner)
	if wf.Lock != nil {
		wf.Lock.Release() //nolint:errcheck // Lock release errors are non-fatal
	}

	// Show summary
	wf.PhaseDisplay.ThinDivider()
	renderTaskSummary(wf.PhaseDisplay, result, opts.TaskName, time.Since(wf.StartTime), execDuration, wf.Conn.Alias)

	return result.ExitCode, nil
}

// renderTaskHeader displays the task being executed.
func renderTaskHeader(pd *ui.PhaseDisplay, taskName string, task *config.TaskConfig) {
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	boldStyle := lipgloss.NewStyle().Bold(true)

	taskInfo := boldStyle.Render(taskName)
	if task.Description != "" {
		taskInfo += " " + mutedStyle.Render("- "+task.Description)
	}

	if task.Run != "" {
		fmt.Printf("Task: %s\n", taskInfo)
		pd.CommandPrompt(task.Run)
	} else if len(task.Steps) > 0 {
		fmt.Printf("Task: %s %s\n", taskInfo, mutedStyle.Render(fmt.Sprintf("(%d steps)", len(task.Steps))))
	}
}

// renderTaskSummary displays the task execution result.
func renderTaskSummary(_ *ui.PhaseDisplay, result *exec.TaskResult, taskName string, totalTime, execTime time.Duration, host string) {
	var symbol string
	var symbolColor lipgloss.Color

	if result.ExitCode == 0 {
		symbol = ui.SymbolSuccess
		symbolColor = ui.ColorSuccess
	} else {
		symbol = ui.SymbolFail
		symbolColor = ui.ColorError
	}

	symbolStyle := lipgloss.NewStyle().Foreground(symbolColor)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	if result.ExitCode == 0 {
		fmt.Printf("%s Task '%s' completed on %s %s\n",
			symbolStyle.Render(symbol),
			taskName,
			host,
			mutedStyle.Render(fmt.Sprintf("(%.1fs total, %.1fs exec)",
				totalTime.Seconds(), execTime.Seconds())),
		)
	} else {
		// Show which step failed if it's a multi-step task
		failInfo := ""
		if result.FailedStep >= 0 && len(result.StepResults) > 0 {
			failedStepResult := result.StepResults[result.FailedStep]
			failInfo = fmt.Sprintf(" at step '%s'", failedStepResult.Name)
		}
		fmt.Printf("%s Task '%s' failed%s on %s with exit code %d %s\n",
			symbolStyle.Render(symbol),
			taskName,
			failInfo,
			host,
			result.ExitCode,
			mutedStyle.Render(fmt.Sprintf("(%.1fs)", totalTime.Seconds())),
		)
	}
}

// ListTasks displays all available tasks from the configuration.
func ListTasks() error {
	// Find and load config
	cfgPath, err := config.Find("")
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't find a config file",
			"Run 'rr init' to create one.")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	if len(cfg.Tasks) == 0 {
		fmt.Println("No tasks defined.")
		fmt.Println()
		fmt.Println("Add tasks to your .rr.yaml:")
		fmt.Println("  tasks:")
		fmt.Println("    test:")
		fmt.Println("      run: pytest")
		fmt.Println("      description: Run the test suite")
		return nil
	}

	// Sort task names for consistent output
	var names []string
	for name := range cfg.Tasks {
		names = append(names, name)
	}
	sort.Strings(names)

	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	boldStyle := lipgloss.NewStyle().Bold(true)

	fmt.Printf("Available tasks (%d):\n\n", len(names))

	for _, name := range names {
		task := cfg.Tasks[name]

		// Task name (bold)
		fmt.Printf("  %s", boldStyle.Render(name))

		// Description if present
		if task.Description != "" {
			fmt.Printf("  %s", mutedStyle.Render(task.Description))
		}
		fmt.Println()

		// Command or steps
		if task.Run != "" {
			fmt.Printf("    %s\n", mutedStyle.Render(task.Run))
		} else if len(task.Steps) > 0 {
			fmt.Printf("    %s\n", mutedStyle.Render(fmt.Sprintf("(%d steps)", len(task.Steps))))
		}

		// Host restrictions if any
		if len(task.Hosts) > 0 {
			fmt.Printf("    %s\n", mutedStyle.Render("hosts: "+util.JoinOrNone(task.Hosts)))
		}
	}

	fmt.Println()
	fmt.Println("Run a task:")
	fmt.Printf("  rr %s\n", names[0])

	return nil
}

// RegisterTaskCommands dynamically registers task commands from config.
// This should be called after config is loaded.
func RegisterTaskCommands(cfg *config.Config) {
	if cfg == nil || cfg.Tasks == nil {
		return
	}

	for name, task := range cfg.Tasks {
		// Skip reserved names (validation should have caught these already)
		if config.IsReservedTaskName(name) {
			continue
		}

		// Create a command for this task
		taskCmd := createTaskCommand(name, task)
		rootCmd.AddCommand(taskCmd)
	}
}

// createTaskCommand creates a cobra command for a task.
func createTaskCommand(name string, task config.TaskConfig) *cobra.Command {
	var hostFlag string
	var tagFlag string
	var probeTimeoutFlag string

	cmd := &cobra.Command{
		Use:   name + " [args...]",
		Short: task.Description,
		Long:  buildTaskLongDescription(name, task),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskCommand(name, args, hostFlag, tagFlag, probeTimeoutFlag)
		},
	}

	// Set description if empty
	if cmd.Short == "" {
		cmd.Short = fmt.Sprintf("Run the '%s' task", name)
	}

	// Add common flags
	cmd.Flags().StringVar(&hostFlag, "host", "", "target host name")
	cmd.Flags().StringVar(&tagFlag, "tag", "", "select host by tag")
	cmd.Flags().StringVar(&probeTimeoutFlag, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")

	return cmd
}

// buildTaskLongDescription creates a detailed description for a task command.
func buildTaskLongDescription(name string, task config.TaskConfig) string {
	desc := fmt.Sprintf("Run the '%s' task defined in .rr.yaml.\n\n", name)

	if task.Description != "" {
		desc += task.Description + "\n\n"
	}

	if task.Run != "" {
		desc += fmt.Sprintf("Command: %s\n", task.Run)
		desc += "\nExtra arguments are appended to the command.\n"
		desc += fmt.Sprintf("Example: rr %s -v  =>  %s -v\n", name, task.Run)
	} else if len(task.Steps) > 0 {
		desc += "Steps:\n"
		for i, step := range task.Steps {
			stepName := step.Name
			if stepName == "" {
				stepName = fmt.Sprintf("step %d", i+1)
			}
			desc += fmt.Sprintf("  %d. %s: %s\n", i+1, stepName, step.Run)
		}
		desc += "\nNote: Extra arguments are not supported for multi-step tasks.\n"
	}

	if len(task.Hosts) > 0 {
		desc += fmt.Sprintf("\nRestricted to hosts: %s\n", util.JoinOrNone(task.Hosts))
	}

	return desc
}

// runTaskCommand is the implementation for task commands.
func runTaskCommand(taskName string, args []string, hostFlag, tagFlag, probeTimeoutFlag string) error {
	probeTimeout, err := ParseProbeTimeout(probeTimeoutFlag)
	if err != nil {
		return err
	}

	exitCode, err := RunTask(TaskOptions{
		TaskName:     taskName,
		Args:         args,
		Host:         hostFlag,
		Tag:          tagFlag,
		ProbeTimeout: probeTimeout,
		Quiet:        Quiet(),
	})

	if err != nil {
		return err
	}

	if exitCode != 0 {
		return errors.NewExitError(exitCode)
	}

	return nil
}

// taskStepHandler implements exec.StepHandler to show step progress during multi-step tasks.
type taskStepHandler struct {
	phaseDisplay *ui.PhaseDisplay
	quiet        bool
}

// OnStepStart is called before a step begins execution.
func (h *taskStepHandler) OnStepStart(stepNum, totalSteps int, step config.TaskStep) {
	if h.quiet {
		return
	}

	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	boldStyle := lipgloss.NewStyle().Bold(true)

	stepName := step.Name
	if stepName == "" {
		stepName = fmt.Sprintf("step %d", stepNum)
	}

	// Show step header: ━━━ Step 1/3: Sync dependencies ━━━
	header := fmt.Sprintf("Step %d/%d: %s", stepNum, totalSteps, stepName)
	headerLine := fmt.Sprintf("━━━ %s ━━━", header)
	fmt.Printf("\n%s\n", boldStyle.Render(headerLine))

	// Show the command being run
	fmt.Printf("%s %s\n\n", mutedStyle.Render("$"), step.Run)
}

// OnStepComplete is called after a step finishes execution.
func (h *taskStepHandler) OnStepComplete(stepNum, totalSteps int, step config.TaskStep, duration time.Duration, exitCode int) {
	if h.quiet {
		return
	}

	var symbol string
	var symbolColor lipgloss.Color

	if exitCode == 0 {
		symbol = ui.SymbolSuccess
		symbolColor = ui.ColorSuccess
	} else {
		symbol = ui.SymbolFail
		symbolColor = ui.ColorError
	}

	symbolStyle := lipgloss.NewStyle().Foreground(symbolColor)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	stepName := step.Name
	if stepName == "" {
		stepName = fmt.Sprintf("step %d", stepNum)
	}

	// Show step completion: ● Step 1/3 complete (2.3s)
	fmt.Printf("\n%s Step %d/%d: %s %s\n",
		symbolStyle.Render(symbol),
		stepNum,
		totalSteps,
		stepName,
		mutedStyle.Render(fmt.Sprintf("(%.1fs)", duration.Seconds())),
	)
}
