package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/rileyhilliard/rr/internal/parallel/logs"
)

// ParallelTaskOptions configures parallel task execution.
type ParallelTaskOptions struct {
	TaskName    string        // Name of the parallel task
	Host        string        // If set, only use this host
	Tag         string        // Filter hosts by tag
	Stream      bool          // Force stream output mode
	Verbose     bool          // Force verbose output mode
	Quiet       bool          // Force quiet output mode
	FailFast    bool          // Stop on first failure (overrides task config)
	MaxParallel int           // Limit concurrency (overrides task config)
	NoLogs      bool          // Don't save output to log files
	DryRun      bool          // Show plan only, don't execute
	Local       bool          // Force local execution
	Timeout     time.Duration // Per-task timeout
}

// RunParallelTask executes a parallel task group.
// Returns the aggregate exit code and any error.
func RunParallelTask(opts ParallelTaskOptions) (int, error) {
	// Load and validate config
	resolved, err := config.LoadResolved(Config())
	if err != nil {
		return 1, err
	}

	if err := config.ValidateResolved(resolved); err != nil {
		return 1, err
	}

	// Get the parallel task
	task, err := config.GetTask(resolved.Project, opts.TaskName)
	if err != nil {
		return 1, err
	}

	if !config.IsParallelTask(task) {
		return 1, errors.New(errors.ErrConfig,
			fmt.Sprintf("Task '%s' is not a parallel task", opts.TaskName),
			"Parallel tasks must have a 'parallel' field with subtask names.")
	}

	// Flatten nested parallel references into a list of executable tasks
	flattenedNames, err := config.FlattenParallelTasks(opts.TaskName, resolved.Project.Tasks)
	if err != nil {
		return 1, errors.WrapWithCode(err, errors.ErrConfig,
			"Failed to flatten parallel tasks",
			"Check for circular references or missing tasks.")
	}

	// Build TaskInfo for each flattened subtask
	tasks := make([]parallel.TaskInfo, 0, len(flattenedNames))
	for i, subtaskName := range flattenedNames {
		subtask, err := config.GetTask(resolved.Project, subtaskName)
		if err != nil {
			return 1, err
		}

		// Build the command
		cmd := subtask.Run
		if cmd == "" && len(subtask.Steps) > 0 {
			// For multi-step tasks, build a command that runs all steps
			cmd = buildStepsCommand(subtask.Steps)
		}

		tasks = append(tasks, parallel.TaskInfo{
			Name:    subtaskName,
			Index:   i, // Unique index for duplicate name handling
			Command: cmd,
			Env:     subtask.Env,
		})
	}

	// Resolve hosts - hostOrder preserves priority from config
	hostOrder, hosts, err := config.ResolveHosts(resolved, opts.Host)
	if err != nil {
		return 1, err
	}

	// Handle --local flag
	if opts.Local {
		hosts = make(map[string]config.Host)
		hostOrder = nil
	}

	// Filter by tag if specified
	if opts.Tag != "" {
		hosts, hostOrder = filterHostsByTag(hosts, hostOrder, opts.Tag)
		if len(hosts) == 0 {
			return 1, errors.New(errors.ErrConfig,
				fmt.Sprintf("No hosts found with tag '%s'", opts.Tag),
				"Check your host tags in ~/.rr/config.yaml.")
		}
	}

	// If dry run, just show the plan
	if opts.DryRun {
		renderDryRunPlan(opts.TaskName, task.Parallel, flattenedNames, tasks, hosts, task.Setup, resolved.Project.Tasks)
		return 0, nil
	}

	// Determine output mode
	outputMode := determineOutputMode(opts, task)

	// Build parallel config
	parallelCfg := parallel.Config{
		MaxParallel: task.MaxParallel,
		FailFast:    task.FailFast,
		OutputMode:  outputMode,
		SaveLogs:    !opts.NoLogs,
		Setup:       task.Setup,
	}

	// Apply CLI overrides
	if opts.FailFast {
		parallelCfg.FailFast = true
	}
	if opts.MaxParallel > 0 {
		parallelCfg.MaxParallel = opts.MaxParallel
	}
	if opts.Timeout > 0 {
		parallelCfg.Timeout = opts.Timeout
	}

	// Parse task timeout if specified
	if task.Timeout != "" && opts.Timeout == 0 {
		d, err := time.ParseDuration(task.Timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid timeout '%s', ignoring: %v\n", task.Timeout, err)
		} else {
			parallelCfg.Timeout = d
		}
	}

	// Set up log writer if enabled
	var logWriter *logs.LogWriter
	if parallelCfg.SaveLogs {
		logDir := resolved.Global.Logs.Dir
		if logDir == "" {
			logDir = "~/.rr/logs"
		}
		logWriter, err = logs.NewLogWriter(logDir, opts.TaskName)
		if err != nil {
			// Log creation failure is non-fatal, just disable logging
			logWriter = nil
		} else {
			parallelCfg.LogDir = logWriter.Dir()
		}
	}

	// Run cleanup on old logs before execution
	if parallelCfg.SaveLogs {
		// Cleanup is best-effort, don't fail if it doesn't work
		_ = logs.Cleanup(resolved.Global.Logs)
	}

	// Create orchestrator with host priority order preserved
	orchestrator := parallel.NewOrchestrator(tasks, hosts, hostOrder, resolved, parallelCfg)

	// Create context with signal handling for graceful cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT/SIGTERM to cancel running tasks
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Execute
	result, err := orchestrator.Run(ctx)
	if err != nil {
		return 1, err
	}

	// Write task outputs to logs
	if logWriter != nil {
		writeTaskLogs(logWriter, result, opts.TaskName)
	}

	// Render summary
	logDir := ""
	if logWriter != nil {
		logDir = logWriter.Dir()
	}
	parallel.RenderSummary(result, logDir)

	// Return aggregate exit code
	if result.Failed > 0 {
		return 1, nil
	}
	return 0, nil
}

// writeTaskLogs writes task outputs to log files, warning on errors.
// Errors are non-fatal since tasks already completed successfully.
func writeTaskLogs(logWriter *logs.LogWriter, result *parallel.Result, taskName string) {
	var logErrors []error
	for i := range result.TaskResults {
		tr := &result.TaskResults[i]
		if err := logWriter.WriteTask(tr.TaskName, tr.TaskIndex, tr.Output); err != nil {
			logErrors = append(logErrors, err)
		}
	}
	if err := logWriter.WriteSummary(result, taskName); err != nil {
		logErrors = append(logErrors, err)
	}
	if err := logWriter.Close(); err != nil {
		logErrors = append(logErrors, err)
	}
	// Warn if log writing failed
	if len(logErrors) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: couldn't save some log files (%d errors)\n", len(logErrors))
	}
}

// determineOutputMode determines the output mode based on options and task config.
func determineOutputMode(opts ParallelTaskOptions, task *config.TaskConfig) parallel.OutputMode {
	// CLI flags take precedence
	if opts.Stream {
		return parallel.OutputStream
	}
	if opts.Verbose {
		return parallel.OutputVerbose
	}
	if opts.Quiet {
		return parallel.OutputQuiet
	}

	// Task-level output config
	if task.Output != "" {
		switch task.Output {
		case "stream":
			return parallel.OutputStream
		case "verbose":
			return parallel.OutputVerbose
		case "quiet":
			return parallel.OutputQuiet
		case "progress":
			return parallel.OutputProgress
		}
	}

	// Default to progress
	return parallel.OutputProgress
}

// buildStepsCommand builds a command that runs all steps in sequence.
func buildStepsCommand(steps []config.TaskStep) string {
	if len(steps) == 0 {
		return ""
	}
	if len(steps) == 1 {
		return steps[0].Run
	}

	// Wrap each step in a subshell to isolate failures and prevent
	// shell metacharacters from breaking the command chain
	parts := make([]string, len(steps))
	for i, step := range steps {
		parts[i] = fmt.Sprintf("(%s)", step.Run)
	}
	return strings.Join(parts, " && ")
}

// filterHostsByTag filters hosts to only those with the specified tag.
// Preserves the priority order from hostOrder.
func filterHostsByTag(hosts map[string]config.Host, hostOrder []string, tag string) (map[string]config.Host, []string) {
	filtered := make(map[string]config.Host)
	filteredOrder := make([]string, 0, len(hostOrder))

	for _, name := range hostOrder {
		host, ok := hosts[name]
		if !ok {
			continue
		}
		for _, t := range host.Tags {
			if t == tag {
				filtered[name] = host
				filteredOrder = append(filteredOrder, name)
				break
			}
		}
	}
	return filtered, filteredOrder
}

// renderDryRunPlan shows what would be executed without actually running.
func renderDryRunPlan(taskName string, originalRefs []string, flattenedNames []string, tasks []parallel.TaskInfo, hosts map[string]config.Host, setup string, allTasks map[string]config.TaskConfig) {
	fmt.Printf("Dry run for parallel task: %s\n\n", taskName)

	if setup != "" {
		fmt.Println("Setup (runs once per host):")
		fmt.Printf("  $ %s\n\n", setup)
	}

	// Check if any expansion happened
	wasExpanded := len(originalRefs) != len(flattenedNames)
	if wasExpanded {
		fmt.Println("Task expansion:")
		for _, ref := range originalRefs {
			if refTask, ok := allTasks[ref]; ok && len(refTask.Parallel) > 0 {
				// This was a nested parallel task - show its direct subtasks
				fmt.Printf("  %s -> [%s]\n", ref, strings.Join(refTask.Parallel, ", "))
			} else {
				fmt.Printf("  %s\n", ref)
			}
		}
		fmt.Println()
	}

	fmt.Printf("Tasks to execute (%d total):\n", len(tasks))
	for i, task := range tasks {
		fmt.Printf("  %d. %s\n", i+1, task.Name)
		if task.Command != "" {
			fmt.Printf("     $ %s\n", task.Command)
		}
	}

	fmt.Println()
	fmt.Println("Available hosts:")
	if len(hosts) == 0 {
		fmt.Println("  (local execution)")
	} else {
		for name := range hosts {
			fmt.Printf("  - %s", name)
			if len(hosts[name].Tags) > 0 {
				fmt.Printf(" [%s]", hosts[name].Tags)
			}
			fmt.Println()
		}
	}

	fmt.Println()
	fmt.Println("No changes made (dry run).")
}

// GetParallelFlagValues returns a function that can be used to get parallel flag values.
func GetParallelFlagValues(stream, verbose, quiet, failFast bool, maxParallel int, noLogs, dryRun bool) ParallelTaskOptions {
	return ParallelTaskOptions{
		Stream:      stream,
		Verbose:     verbose,
		Quiet:       quiet,
		FailFast:    failFast,
		MaxParallel: maxParallel,
		NoLogs:      noLogs,
		DryRun:      dryRun,
	}
}

// GetGlobalLogsConfig returns the logs config from global config for CLI commands.
func GetGlobalLogsConfig() config.LogsConfig {
	global, err := config.LoadGlobal()
	if err != nil {
		return config.LogsConfig{Dir: "~/.rr/logs", KeepRuns: 10}
	}
	return global.Logs
}

// PrintParallelHelp prints help text specific to parallel task execution.
func PrintParallelHelp() {
	fmt.Println(`Parallel Task Flags:
  --stream        Show real-time interleaved output with task prefixes
  --verbose       Show full output per task on completion
  --quiet         Show summary only
  --fail-fast     Stop execution on first task failure
  --max-parallel  Limit concurrent task execution (default: unlimited)
  --no-logs       Don't save output to log files
  --dry-run       Show execution plan without running

Parallel tasks run multiple subtasks concurrently across available hosts.
Each subtask is assigned to a host using work-stealing for optimal load balancing.

Example:
  rr test-all               Run all tests in parallel
  rr test-all --stream      See output in real-time
  rr test-all --fail-fast   Stop on first failure
  rr test-all --dry-run     See what would run

Log files are saved to ~/.rr/logs/<task>-<timestamp>/ by default.
Use 'rr logs' to view recent logs or 'rr logs clean' to remove old ones.`)
	fmt.Println()
}

// FormatParallelTaskHelp returns a formatted description for parallel task help.
func FormatParallelTaskHelp(task *config.TaskConfig, cfg *config.Config) string {
	if !config.IsParallelTask(task) {
		return ""
	}

	help := fmt.Sprintf("Runs %d tasks in parallel:\n", len(task.Parallel))
	for i, name := range task.Parallel {
		help += fmt.Sprintf("  %d. %s", i+1, name)
		if subtask, ok := cfg.Tasks[name]; ok && subtask.Description != "" {
			help += " - " + subtask.Description
		}
		help += "\n"
	}

	if task.Setup != "" {
		help += fmt.Sprintf("\nSetup (once per host): %s\n", task.Setup)
	}
	if task.FailFast {
		help += "\nStops on first failure (fail_fast: true)\n"
	}
	if task.MaxParallel > 0 {
		help += fmt.Sprintf("\nMax concurrent: %d\n", task.MaxParallel)
	}

	return help
}

// ExpandLogsDir expands ~ in a logs directory path.
func ExpandLogsDir(dir string) string {
	if len(dir) > 0 && dir[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return dir
		}
		return home + dir[1:]
	}
	return dir
}
