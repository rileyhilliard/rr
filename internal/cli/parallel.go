package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/output/formatters"
	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/rileyhilliard/rr/internal/parallel/logs"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/util"
	"github.com/rileyhilliard/rr/pkg/sshutil"
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
	Args        []string      // Extra args forwarded to subtasks when forward_args is true
}

// RunParallelTask executes a parallel task group.
// Returns the aggregate exit code and any error.
func RunParallelTask(opts ParallelTaskOptions) (int, error) {
	// Load and validate config, and decide local vs remote
	resolved, target, err := loadRunConfig(opts.Local, opts.Host, opts.Tag)
	if err != nil {
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

	rewriteForwardArgs(resolved, task, &opts)

	// Build TaskInfo for each flattened subtask
	tasks, err := buildSubtaskInfos(resolved.Project, task, flattenedNames, opts.Args)
	if err != nil {
		return 1, err
	}

	// Resolve hosts - hostOrder preserves priority from config (none for a
	// local target)
	hostOrder, hosts, err := resolveTargetHosts(resolved, target, opts.Host)
	if err != nil {
		return 1, err
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

	// Every host-restricted subtask needs at least one of its hosts in the
	// run. Without this check the scheduler would have nowhere to send it and
	// the run would end with a "no available host" failure after the other
	// subtasks finished; with --host pointing at a disallowed host the old
	// scheduler ran it there anyway.
	if !target.local {
		if err := checkSubtaskHosts(tasks, hostOrder); err != nil {
			return 1, err
		}
	}

	// If dry run, just show the plan
	if opts.DryRun {
		renderDryRunPlan(opts.TaskName, task.Parallel, flattenedNames, tasks, hosts, task.Setup, resolved.Project.Tasks)
		return 0, nil
	}

	// Structured mode prints no human progress: stdout carries only the
	// subtasks' output, and progress goes out as events on stderr. An explicit
	// stream or verbose mode still prints task output.
	outputMode := determineOutputMode(opts, task)
	if !PrettyMode() && (outputMode == parallel.OutputProgress || outputMode == parallel.OutputQuiet) {
		outputMode = parallel.OutputNone
	}

	// Build parallel config. Workers sync through the same callbacks as a
	// single run, so invalidation, provenance and prune notices show up.
	syncNotices := &parallelSyncNotices{}
	parallelCfg := parallel.Config{
		MaxParallel: task.MaxParallel,
		FailFast:    task.FailFast,
		OutputMode:  outputMode,
		SaveLogs:    !opts.NoLogs,
		Setup:       task.Setup,
		SyncOptions: syncNotices.optionsFor,
	}
	if !PrettyMode() {
		parallelCfg.OnRequeue = requeuedEvent
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

	if target.local && !PrettyMode() {
		emitLocalConnect(target.reason)
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
	syncNotices.flush()
	if err != nil {
		return 1, err
	}

	// Pull every subtask's files, pass or fail: a failed shard's junit and
	// coverage files are what you need to debug it. Skipped on Ctrl+C.
	if ctx.Err() == nil {
		pullSubtaskFiles(tasks, result, hosts, rrsync.Pull)
	}

	return renderParallelResult(result, logWriter, opts.TaskName, target.reason), nil
}

// renderParallelResult writes the logs and reports the run's result.
// localReason is the target's reason when it was local (see execTarget),
// recorded as details.local_reason; empty for a remote target.
func renderParallelResult(result *parallel.Result, logWriter *logs.LogWriter, taskName, localReason string) int {
	if logWriter != nil {
		writeTaskLogs(logWriter, result, taskName)
	}

	logDir := ""
	if logWriter != nil {
		logDir = logWriter.Dir()
	}

	outcomes := parseTaskOutcomes(result)
	noTests := tasksWithoutTests(result, outcomes)

	if PrettyMode() {
		parallel.RenderSummary(result, outcomes, logDir)
		if len(noTests) > 0 {
			warnNoTests(map[string]interface{}{
				"no_tests":       true,
				"no_tests_tasks": noTests,
			})
		}
	} else {
		exitCode := 0
		if result.Failed > 0 {
			exitCode = 1
		}
		details := map[string]interface{}{
			"total":  result.Passed + result.Failed,
			"passed": result.Passed,
			"failed": result.Failed,
		}
		if logDir != "" {
			details["log_dir"] = logDir
		}
		if localReason != "" {
			details["local_reason"] = localReason
		}
		if result.Failed > 0 {
			details["failures"] = extractTaskFailures(result, outcomes, logDir)
		}
		// no_tests stays a bool here as it is for single runs - one key, one
		// type, so consumers can branch on it without sniffing. The subtask
		// names go in their own field.
		if len(noTests) > 0 {
			details["no_tests"] = true
			details["no_tests_tasks"] = noTests
		}
		WritePhaseEvent(PhaseEvent{
			Type:     "result",
			Status:   map[bool]string{true: "success", false: "failed"}[result.Failed == 0],
			ExitCode: &exitCode,
			Details:  details,
		})
	}

	if result.Failed > 0 {
		return 1
	}
	return 0
}

const maxOutputTailLines = 20
const maxFailureMessageLen = 500

// parseTaskOutcomes parses each subtask's output once (see
// formatters.ParseRunOutcome). The result is index-aligned with
// result.TaskResults.
func parseTaskOutcomes(result *parallel.Result) []formatters.Outcome {
	outcomes := make([]formatters.Outcome, len(result.TaskResults))
	for i := range result.TaskResults {
		tr := &result.TaskResults[i]
		outcomes[i] = formatters.ParseRunOutcome(tr.Command, tr.Output)
	}
	return outcomes
}

// tasksWithoutTests names the subtasks whose runner collected zero tests.
// Sharded suites make this easy to miss: the aggregate says "3 passed" while
// one shard's path filter matched nothing. Reported per subtask, never fatal -
// forwarded filters (forward_args) legitimately leave some shards empty.
func tasksWithoutTests(result *parallel.Result, outcomes []formatters.Outcome) []string {
	var names []string
	for i := range result.TaskResults {
		if outcomes[i].NoTests {
			names = append(names, result.TaskResults[i].TaskName)
		}
	}
	return names
}

// extractTaskFailures builds structured failure info for machine-mode output.
// When logDir is non-empty, each failure carries the path of its saved log.
func extractTaskFailures(result *parallel.Result, outcomes []formatters.Outcome, logDir string) []map[string]interface{} {
	var failures []map[string]interface{}
	for i := range result.TaskResults {
		tr := &result.TaskResults[i]
		if tr.Success() {
			continue
		}
		entry := map[string]interface{}{
			"task":      tr.TaskName,
			"host":      tr.Host,
			"exit_code": tr.ExitCode,
		}
		if tr.Error != nil {
			entry["error"] = tr.Error.Error()
		}
		if logDir != "" {
			entry["log_file"] = logs.TaskLogPath(logDir, tr.TaskName, tr.TaskIndex)
		}

		if parsed := outcomes[i].Failures; len(parsed) > 0 {
			entry["tests"] = failureEntries(parsed)
		} else if len(tr.Output) > 0 {
			lines := strings.Split(strings.TrimSpace(string(tr.Output)), "\n")
			start := 0
			if len(lines) > maxOutputTailLines {
				start = len(lines) - maxOutputTailLines
			}
			entry["output_tail"] = strings.Join(lines[start:], "\n")
		}
		failures = append(failures, entry)
	}
	return failures
}

// rewriteForwardArgs rewrites local absolute path args to project-relative
// form so they resolve after each worker cds into its host's project dir.
// Skipped for --local runs, where local paths are already correct.
func rewriteForwardArgs(resolved *config.ResolvedConfig, task *config.TaskConfig, opts *ParallelTaskOptions) {
	if !task.ForwardArgs || len(opts.Args) == 0 || opts.Local ||
		resolved.ProjectRoot == "" || !config.ResolveRewritePaths(resolved) {
		return
	}
	rewritten, n := RewriteArgsToRelative(opts.Args, resolved.ProjectRoot)
	if n > 0 {
		opts.Args = rewritten
		announcePathRewrites(n, resolved.ProjectRoot, ".")
	}
}

// checkSubtaskHosts fails when a restricted subtask has none of its allowed
// hosts among the hosts selected for this run.
func checkSubtaskHosts(tasks []parallel.TaskInfo, hostOrder []string) error {
	for _, t := range tasks {
		if len(t.AllowedHosts) == 0 {
			continue
		}
		ok := false
		for _, h := range hostOrder {
			if t.AllowsHost(h) {
				ok = true
				break
			}
		}
		if !ok {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("Subtask '%s' can't run on the selected host(s): %s", t.Name, util.JoinOrNone(hostOrder)),
				fmt.Sprintf("This subtask is restricted to: %s. Drop --host/--tag or include one of those hosts.", util.JoinOrNone(t.AllowedHosts)))
		}
	}
	return nil
}

// buildSubtaskInfos constructs the TaskInfo list for each flattened subtask name.
// When forwardTask.ForwardArgs is true, args are substituted into each
// subtask's {args} placeholder or appended (shell-quoted) to simple commands.
// {args:-default} defaults apply even when no args are forwarded.
// Multi-step subtasks cannot accept forwarded args.
func buildSubtaskInfos(proj *config.Config, forwardTask *config.TaskConfig, flattenedNames []string, args []string) ([]parallel.TaskInfo, error) {
	tasks := make([]parallel.TaskInfo, 0, len(flattenedNames))
	for i, subtaskName := range flattenedNames {
		subtask, err := config.GetTask(proj, subtaskName)
		if err != nil {
			return nil, err
		}

		cmd := subtask.Run
		if cmd == "" && len(subtask.Steps) > 0 {
			cmd = buildStepsCommand(subtask.Steps)
		}

		if forwardTask.ForwardArgs && len(args) > 0 && len(subtask.Steps) > 0 {
			return nil, errors.New(errors.ErrConfig,
				fmt.Sprintf("subtask '%s' uses steps and cannot accept forwarded args", subtaskName),
				"remove forward_args from the parent task or convert the subtask to a single run command")
		}

		if subtask.Run != "" {
			forwarded := args
			if !forwardTask.ForwardArgs {
				forwarded = nil // still expands {args:-default} placeholders
			}
			cmd, err = exec.ApplyTaskArgs(subtask.Run, forwarded)
			if err != nil {
				return nil, errors.New(errors.ErrConfig,
					fmt.Sprintf("subtask '%s' is a compound command and has no {args} placeholder for forwarded args", subtaskName),
					"Add an {args} placeholder to the subtask's run command where the arguments belong.")
			}
		}

		tasks = append(tasks, parallel.TaskInfo{
			Name:         subtaskName,
			Index:        i,
			Command:      cmd,
			Env:          subtask.Env,
			AllowedHosts: subtask.Hosts,
			Pull:         subtask.Pull,
		})
	}
	return tasks, nil
}

// requeuedEvent reports a subtask moved off a host that became unavailable;
// the host is dropped for the rest of the run. The reason is connection_lost
// when the host's connection died mid-run, connect_failed when the host was
// never reached.
func requeuedEvent(taskName, hostName string, cause error) {
	reason := "connect_failed"
	if sshutil.IsConnectionLost(cause) {
		reason = "connection_lost"
	}
	details := map[string]interface{}{
		"reason": reason,
		"task":   taskName,
	}
	if cause != nil {
		details["error"] = ErrorToJSON(cause)
	}
	WritePhaseEvent(PhaseEvent{
		Type:    "phase",
		Phase:   "connect",
		Status:  "warn",
		Host:    hostName,
		Details: details,
	})
}

// parallelSyncNotices hands parallel workers the same sync callbacks a single
// run uses. Workers sync concurrently, so callbacks are serialized. Structured
// mode emits the sync phase events right away, with the host set since each
// host syncs once for all its subtasks. Pretty mode holds the lines until
// flush, because printing under the live progress display garbles it.
type parallelSyncNotices struct {
	mu      sync.Mutex
	pending []func()
}

// optionsFor returns the sync options for one host's sync.
func (n *parallelSyncNotices) optionsFor(hostName string) *rrsync.SyncOptions {
	if !PrettyMode() {
		base := structuredSyncOptions(hostName)
		return &rrsync.SyncOptions{
			Invalidated: func(dir, lockfile string) { n.now(func() { base.Invalidated(dir, lockfile) }) },
			Warn:        func(w rrsync.SyncWarning) { n.now(func() { base.Warn(w) }) },
			Pruned:      func(dir string) { n.now(func() { base.Pruned(dir) }) },
		}
	}
	base := prettySyncOptions()
	return &rrsync.SyncOptions{
		Invalidated: func(dir, lockfile string) { n.later(func() { base.Invalidated(dir, lockfile) }) },
		Warn:        func(w rrsync.SyncWarning) { n.later(func() { base.Warn(w) }) },
		Pruned:      func(dir string) { n.later(func() { base.Pruned(dir) }) },
	}
}

func (n *parallelSyncNotices) now(fn func()) {
	n.mu.Lock()
	defer n.mu.Unlock()
	fn()
}

func (n *parallelSyncNotices) later(fn func()) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pending = append(n.pending, fn)
}

// flush prints any held pretty-mode notices. Call it after the run's live
// display has closed.
func (n *parallelSyncNotices) flush() {
	n.mu.Lock()
	pending := n.pending
	n.pending = nil
	n.mu.Unlock()
	for _, fn := range pending {
		fn()
	}
}

// pullSubtaskFiles runs each subtask's `pull:` after the whole parallel run
// has finished, whatever the subtask's exit code. Pulls run one at a time in
// subtask order, from the host the subtask ran on, through the alias that
// reached it. Each subtask's files land in <dest>/<stem>/, where <stem> is
// its log file's name without .log (see subtaskPullDir), so shards with the
// same output paths don't overwrite each other locally. Subtasks that never
// reached a remote host (local runs, no host available) are skipped. A
// failed pull is reported but doesn't change the run's exit code, same as
// single tasks.
func pullSubtaskFiles(tasks []parallel.TaskInfo, result *parallel.Result, hosts map[string]config.Host, pull pullFunc) {
	ranOn := make(map[int]*parallel.TaskResult, len(result.TaskResults))
	for i := range result.TaskResults {
		ranOn[result.TaskResults[i].TaskIndex] = &result.TaskResults[i]
	}

	for _, t := range tasks {
		tr, ok := ranOn[t.Index]
		if !ok || len(t.Pull) == 0 {
			continue
		}
		hostCfg, remote := hosts[tr.Host]
		if !remote {
			continue // "local" or "none": nothing on a remote to pull
		}
		if tr.Alias == "" {
			continue // never connected (e.g. cancelled by fail-fast): nothing ran there
		}
		conn := &host.Connection{Name: tr.Host, Alias: tr.Alias, Host: hostCfg}
		pullAndReport(conn, rrsync.PullOptions{Patterns: subtaskPullItems(t.Pull, subtaskPullDir(t))}, pull, t.Name)
	}
}

// subtaskPullDir names the directory a subtask's pulled files land in: the
// stem of its log file, <name>_<index> with the characters logs replaces in
// file names (/ \ : * ? " < > |) turned into '-'. The index is unique within
// the run and the stem ends in it, so no two subtasks share a directory, even
// a task listed twice or one named like another's stem, and a name with a
// slash stays one level deep.
func subtaskPullDir(t parallel.TaskInfo) string {
	return strings.TrimSuffix(filepath.Base(logs.TaskLogPath("", t.Name, t.Index)), ".log")
}

// subtaskPullItems rewrites a subtask's pull items so each lands in
// <dest>/<dir>/ (./<dir>/ when dest is unset).
func subtaskPullItems(items []config.PullItem, dir string) []config.PullItem {
	out := make([]config.PullItem, 0, len(items))
	for _, item := range items {
		dest := item.Dest
		if dest == "" {
			dest = "."
		}
		out = append(out, config.PullItem{Src: item.Src, Dest: filepath.Join(dest, dir)})
	}
	return out
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
		case config.TaskOutputStream:
			return parallel.OutputStream
		case config.TaskOutputVerbose:
			return parallel.OutputVerbose
		case config.TaskOutputQuiet:
			return parallel.OutputQuiet
		case config.TaskOutputProgress:
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
