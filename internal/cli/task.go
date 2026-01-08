package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/spf13/cobra"
)

// TaskOptions holds options for task execution.
type TaskOptions struct {
	TaskName     string
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
	startTime := time.Now()
	phaseDisplay := ui.NewPhaseDisplay(os.Stdout)

	// Load config
	cfgPath, err := config.Find(Config())
	if err != nil {
		return 1, err
	}
	if cfgPath == "" {
		return 1, errors.New(errors.ErrConfig,
			"No config file found",
			"Run 'rr init' to create a .rr.yaml config file")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return 1, err
	}

	if err := config.Validate(cfg); err != nil {
		return 1, err
	}

	// Get the task
	task, mergedEnv, err := config.GetTaskWithMergedEnv(cfg, opts.TaskName, opts.Host)
	if err != nil {
		return 1, err
	}

	// Determine working directory
	workDir := opts.WorkingDir
	if workDir == "" {
		workDir, err = os.Getwd()
		if err != nil {
			return 1, errors.WrapWithCode(err, errors.ErrExec,
				"Failed to get working directory",
				"Check directory permissions")
		}
	}

	// Generate project hash for locking
	projectHash := hashProject(workDir)

	// Create host selector
	selector := host.NewSelector(cfg.Hosts)
	defer selector.Close()

	// Enable local fallback if configured
	selector.SetLocalFallback(cfg.LocalFallback)

	// Set probe timeout (CLI flag overrides config)
	probeTimeout := cfg.ProbeTimeout
	if opts.ProbeTimeout > 0 {
		probeTimeout = opts.ProbeTimeout
	}
	if probeTimeout > 0 {
		selector.SetTimeout(probeTimeout)
	}

	// Phase 1: Connect
	connDisplay := ui.NewConnectionDisplay(os.Stdout)
	connDisplay.SetQuiet(opts.Quiet)
	connDisplay.Start()

	preferredHost := opts.Host
	if preferredHost == "" {
		preferredHost = cfg.Default
	}

	// Check if task is allowed on this host
	if preferredHost != "" && !config.IsTaskHostAllowed(task, preferredHost) {
		return 1, errors.New(errors.ErrConfig,
			fmt.Sprintf("Task '%s' is not allowed on host '%s'", opts.TaskName, preferredHost),
			fmt.Sprintf("This task is restricted to hosts: %s", formatHosts(task.Hosts)))
	}

	// Track connection status
	var usedLocalFallback bool

	// Set up event handler for connection progress
	selector.SetEventHandler(func(event host.ConnectionEvent) {
		switch event.Type {
		case host.EventFailed:
			status := mapProbeErrorToStatus(event.Error)
			connDisplay.AddAttempt(event.Alias, status, event.Latency, event.Message)
		case host.EventConnected:
			connDisplay.AddAttempt(event.Alias, ui.StatusSuccess, event.Latency, "")
		case host.EventLocalFallback:
			usedLocalFallback = true
		}
	})

	// Connect
	var conn *host.Connection
	if opts.Tag != "" {
		conn, err = selector.SelectByTag(opts.Tag)
	} else {
		conn, err = selector.Select(preferredHost)
	}
	if err != nil {
		connDisplay.Fail(err.Error())
		return 1, err
	}

	// Verify task is allowed on the connected host
	if !config.IsTaskHostAllowed(task, conn.Name) {
		return 1, errors.New(errors.ErrConfig,
			fmt.Sprintf("Task '%s' is not allowed on host '%s'", opts.TaskName, conn.Name),
			fmt.Sprintf("This task is restricted to hosts: %s", formatHosts(task.Hosts)))
	}

	// Show connection result
	if usedLocalFallback {
		connDisplay.SuccessLocal()
	} else {
		connDisplay.Success(conn.Name, conn.Alias)
	}

	// Phase 2: Sync (unless skipped or local)
	var syncDuration time.Duration
	if conn.IsLocal {
		phaseDisplay.RenderSkipped("Sync", "local")
	} else if !opts.SkipSync {
		syncStart := time.Now()
		syncSpinner := ui.NewSpinner("Syncing files")
		syncSpinner.Start()

		err = sync.Sync(conn, workDir, cfg.Sync, nil)
		if err != nil {
			syncSpinner.Fail()
			return 1, err
		}

		syncDuration = time.Since(syncStart)
		syncSpinner.Success()
		phaseDisplay.RenderSuccess("Files synced", syncDuration)
	} else {
		phaseDisplay.RenderSkipped("Sync", "skipped")
	}

	// Phase 3: Acquire lock (unless disabled or local)
	var lck *lock.Lock
	if cfg.Lock.Enabled && !opts.SkipLock && !conn.IsLocal {
		lockStart := time.Now()
		lockSpinner := ui.NewSpinner("Acquiring lock")
		lockSpinner.Start()

		lck, err = lock.Acquire(conn, cfg.Lock, projectHash)
		if err != nil {
			lockSpinner.Fail()
			return 1, err
		}
		defer lck.Release() //nolint:errcheck // Lock release errors are non-fatal in cleanup

		lockSpinner.Success()
		phaseDisplay.RenderSuccess("Lock acquired", time.Since(lockStart))
	}

	// Phase 4: Execute task
	phaseDisplay.Divider()

	// Show task info
	renderTaskHeader(phaseDisplay, opts.TaskName, task)
	fmt.Println()

	// Set up output streaming
	streamHandler := output.NewStreamHandler(os.Stdout, os.Stderr)
	streamHandler.SetFormatter(output.NewGenericFormatter())

	execStart := time.Now()

	// Get remote directory for task execution
	remoteDir := ""
	if !conn.IsLocal {
		remoteDir = config.Expand(conn.Host.Dir)
	}

	// Execute the task
	result, err := exec.ExecuteTask(conn, task, mergedEnv, remoteDir, streamHandler.Stdout(), streamHandler.Stderr())
	execDuration := time.Since(execStart)

	if err != nil {
		return 1, err
	}

	// Release lock early if task completed
	if lck != nil {
		lck.Release() //nolint:errcheck // Lock release errors are non-fatal
	}

	// Show summary
	phaseDisplay.ThinDivider()
	renderTaskSummary(phaseDisplay, result, opts.TaskName, time.Since(startTime), execDuration, conn.Alias)

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
func renderTaskSummary(pd *ui.PhaseDisplay, result *exec.TaskResult, taskName string, totalTime, execTime time.Duration, host string) {
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

// formatHosts returns a comma-separated list of hosts.
func formatHosts(hosts []string) string {
	if len(hosts) == 0 {
		return "(none)"
	}
	result := hosts[0]
	for i := 1; i < len(hosts); i++ {
		result += ", " + hosts[i]
	}
	return result
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
		Use:   name,
		Short: task.Description,
		Long:  buildTaskLongDescription(name, task),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskCommand(name, hostFlag, tagFlag, probeTimeoutFlag)
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
	} else if len(task.Steps) > 0 {
		desc += "Steps:\n"
		for i, step := range task.Steps {
			stepName := step.Name
			if stepName == "" {
				stepName = fmt.Sprintf("step %d", i+1)
			}
			desc += fmt.Sprintf("  %d. %s: %s\n", i+1, stepName, step.Run)
		}
	}

	if len(task.Hosts) > 0 {
		desc += fmt.Sprintf("\nRestricted to hosts: %s\n", formatHosts(task.Hosts))
	}

	return desc
}

// runTaskCommand is the implementation for task commands.
func runTaskCommand(taskName, hostFlag, tagFlag, probeTimeoutFlag string) error {
	// Parse probe timeout if provided
	var probeTimeout time.Duration
	if probeTimeoutFlag != "" {
		var err error
		probeTimeout, err = time.ParseDuration(probeTimeoutFlag)
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				fmt.Sprintf("Invalid probe timeout: %s", probeTimeoutFlag),
				"Use a valid duration like 5s, 2m, or 500ms")
		}
	}

	exitCode, err := RunTask(TaskOptions{
		TaskName:     taskName,
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
