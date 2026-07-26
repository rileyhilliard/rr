package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/rileyhilliard/rr/internal/parallel/logs"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/internal/util"
)

// RunOptions holds options for the run command.
type RunOptions struct {
	Command          string
	Host             string        // Preferred host name
	Tag              string        // Filter hosts by tag
	ProbeTimeout     time.Duration // Override SSH probe timeout (0 means use config default)
	SkipSync         bool          // If true, skip sync phase (used by exec)
	SkipLock         bool          // If true, skip locking
	SkipRequirements bool          // If true, skip requirement checks
	DryRun           bool          // If true, show what would be done without doing it
	WorkingDir       string        // Override local working directory
	RemoteCWD        string        // Subdirectory to cd into on remote before running (relative to host.Dir)
	Quiet            bool          // If true, minimize output (no individual connection attempts)
	Local            bool          // If true, force local execution (skip remote hosts)
	Pull             []string      // Patterns to pull from remote after command completes
	PullDest         string        // Destination directory for pulled files
	Tail             int           // Print the last N lines of the run log after completion
	LogName          string        // Run log directory prefix (defaults to "run")
}

// Run syncs files and executes a command on the remote host.
// This is the main workflow that ties together all subsystems.
func Run(opts RunOptions) (int, error) {
	// Setup common workflow phases (config, connect, sync, lock)
	wf, err := SetupWorkflow(WorkflowOptions{
		Host:             opts.Host,
		Tag:              opts.Tag,
		ProbeTimeout:     opts.ProbeTimeout,
		SkipSync:         opts.SkipSync,
		SkipLock:         opts.SkipLock,
		SkipRequirements: opts.SkipRequirements,
		WorkingDir:       opts.WorkingDir,
		Quiet:            opts.Quiet,
		Local:            opts.Local,
		Command:          opts.Command,
	})
	if err != nil {
		return 1, err
	}
	defer wf.Close()

	// Phase 4: Execute command
	wf.Reporter.Divider()
	wf.Reporter.CommandPrompt(opts.Command)
	if PrettyMode() {
		fmt.Println()
	}

	// Set up output streaming - in structured mode, pass raw stdout/stderr
	streamHandler := output.NewStreamHandler(os.Stdout, os.Stderr)
	if PrettyMode() {
		streamHandler.SetFormatter(output.NewGenericFormatter())
	}

	// Tee raw output into a per-run log file (best-effort).
	logName := opts.LogName
	if logName == "" {
		logName = "run"
	}
	logPath, closeLog := setupRunLog(wf, logName, streamHandler)
	defer closeLog()

	execStart := time.Now()
	var exitCode int
	remoteProjectDir := ""

	if wf.Conn.IsLocal {
		exitCode, err = exec.ExecuteLocal(opts.Command, wf.WorkDir, streamHandler.Stdout(), streamHandler.Stderr())
	} else {
		remoteProjectDir = config.ExpandRemote(wf.Conn.Host.Dir)
		fullCmd, cmdErr := buildRemoteRunCommand(wf, opts, remoteProjectDir)
		if cmdErr != nil {
			return 1, cmdErr
		}
		exitCode, err = wf.Conn.Client.ExecStreamContext(wf.Context(), fullCmd, streamHandler.Stdout(), streamHandler.Stderr())
	}
	execDuration := time.Since(execStart)

	if wf.Context().Err() != nil {
		return 130, nil
	}

	if err != nil {
		return 1, err
	}

	// Release lock early
	if wf.Lock != nil {
		wf.Lock.Release() //nolint:errcheck // Lock release errors are non-fatal
	}

	// Phase 5: Pull files (if requested)
	if len(opts.Pull) > 0 {
		pullItems := make([]config.PullItem, len(opts.Pull))
		for i, p := range opts.Pull {
			pullItems[i] = config.PullItem{Src: p}
		}
		ExecutePullPhase(wf, pullItems, opts.PullDest)
	}

	// Post-failure hint: detect local-machine assumptions (paths that only
	// exist here, git commands against the synced snapshot).
	failureHint := ""
	if exitCode != 0 && !wf.Conn.IsLocal {
		failureHint = buildFailureHint(opts.Command, streamHandler.GetStderrCapture(), wf.WorkDir, remoteProjectDir, wf.Conn.Name)
		if failureHint != "" {
			wf.AddResultDetail("hint", failureHint)
		}
	}

	// Record test summary/failures from the run log and note broken pipes.
	attachRunOutcome(wf, opts.Command, logPath, exitCode)
	if streamHandler.BrokenPipe() {
		wf.AddResultDetail("broken_pipe", true)
	}

	// In structured mode, emit result and return - no decorations
	if !PrettyMode() {
		wf.Reporter.CommandComplete(exitCode, wf.Conn.Name, time.Since(wf.StartTime), execDuration, wf.ResultDetails)
		printLogTail(logPath, opts.Tail)
		return exitCode, nil
	}

	// Pretty mode: check for failures, test summaries, etc.
	failureExplained, retry := explainRunFailure(wf, opts, streamHandler, exitCode, execDuration)
	if retry {
		wf.Close()
		return Run(opts)
	}

	wf.PhaseDisplay.ThinDivider()
	renderFinalStatus(wf.PhaseDisplay, exitCode, time.Since(wf.StartTime), execDuration, wf.Conn.Name)

	if failureHint != "" {
		fmt.Printf("\n%s\n", lipgloss.NewStyle().Foreground(ui.ColorMuted).Render(failureHint))
	} else if exitCode != 0 && !failureExplained {
		renderFailureHelp(exitCode, opts.Command, wf.Conn.Name)
	}

	printLogTail(logPath, opts.Tail)

	return exitCode, nil
}

// explainRunFailure renders pretty-mode failure explanations: missing-tool
// detection (with optional auto-fix) and parsed test summaries. Returns
// whether the failure was explained and whether the caller should retry the
// whole run after a successful tool fix.
func explainRunFailure(wf *WorkflowContext, opts RunOptions, streamHandler *output.StreamHandler, exitCode int, execDuration time.Duration) (explained, retry bool) {
	if exitCode == 0 {
		return false, false
	}

	var sshClient exec.SSHExecer
	if !wf.Conn.IsLocal && wf.Conn.Client != nil {
		sshClient = wf.Conn.Client
	}

	missingTool := exec.DetectMissingTool(opts.Command, streamHandler.GetStderrCapture(), exitCode, sshClient, wf.Conn.Name)
	if missingTool != nil {
		fmt.Println()
		fmt.Printf("%s %s\n\n", ui.SymbolFail, missingTool.Error())
		fmt.Println(missingTool.Suggestion)

		if !wf.Conn.IsLocal && wf.Conn.Client != nil {
			configPath, _ := config.Find(Config())
			if configPath != "" {
				fixResult, _ := HandleMissingTool(missingTool, wf.Conn.Client, configPath)
				if fixResult != nil && fixResult.ShouldRetry {
					wf.PhaseDisplay.ThinDivider()
					renderFinalStatus(wf.PhaseDisplay, exitCode, time.Since(wf.StartTime), execDuration, wf.Conn.Name)
					return true, true
				}
			}
		}
		return true, false
	}

	if provider, ok := streamHandler.GetFormatter().(output.TestSummaryProvider); ok {
		failures := provider.GetTestFailures()
		if len(failures) > 0 {
			passed, failed, skipped, errors := provider.GetTestCounts()
			summary := &ui.TestSummary{
				Passed:   passed,
				Failed:   failed,
				Skipped:  skipped,
				Errors:   errors,
				Failures: make([]ui.TestFailure, len(failures)),
			}
			for i, f := range failures {
				summary.Failures[i] = ui.TestFailure{
					TestName: f.TestName,
					File:     f.File,
					Line:     f.Line,
					Message:  f.Message,
				}
			}
			fmt.Println()
			fmt.Print(ui.FormatDivider(ui.DividerWidth))
			fmt.Println()
			fmt.Print(ui.RenderSummary(summary, exitCode))
			return true, false
		}
	}

	return false, false
}

// buildRemoteRunCommand prepares a command for remote execution: local path
// rewriting, foreign-path checks, project setup prepends, and --cwd handling.
func buildRemoteRunCommand(wf *WorkflowContext, opts RunOptions, remoteProjectDir string) (string, error) {
	cmd := opts.Command

	// Rewrite local absolute paths to their remote equivalents so commands
	// authored against the local checkout work on the mirror.
	if config.ResolveRewritePaths(wf.Resolved) {
		rewritten, n := RewriteLocalPaths(cmd, wf.WorkDir, remoteProjectDir)
		if n > 0 {
			cmd = rewritten
			reportPathRewrites(wf, n, wf.WorkDir, remoteProjectDir)
		}
		if err := checkForeignPaths(wf, cmd, remoteProjectDir); err != nil {
			return "", err
		}
	}

	if len(wf.Resolved.Project.Defaults.Setup) > 0 {
		cmd = strings.Join(wf.Resolved.Project.Defaults.Setup, " && ") + " && " + cmd
	}

	// --cwd prepends a cd into a subdirectory of the remote project root.
	// Reject paths that escape the project root via ../ traversal.
	if opts.RemoteCWD != "" {
		resolved := path.Join(remoteProjectDir, opts.RemoteCWD)
		if !strings.HasPrefix(resolved+"/", remoteProjectDir+"/") {
			return "", errors.New(errors.ErrConfig,
				fmt.Sprintf("--cwd '%s' escapes the remote project root", opts.RemoteCWD),
				"use a path relative to the project root without '..' components")
		}
		subdir := util.ShellQuotePreserveTilde(resolved)
		cmd = fmt.Sprintf("cd %s && %s", subdir, cmd)
	}

	return exec.BuildRemoteCommand(cmd, &wf.Conn.Host), nil
}

// renderFinalStatus displays the final execution status line.
func renderFinalStatus(_ *ui.PhaseDisplay, exitCode int, totalTime, execTime time.Duration, host string) {
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

	// Summary line: [symbol] Completed on [host] in [time]
	if exitCode == 0 {
		fmt.Printf("%s Completed on %s %s\n",
			symbolStyle.Render(symbol),
			host,
			mutedStyle.Render(fmt.Sprintf("(%.1fs total, %.1fs exec)",
				totalTime.Seconds(), execTime.Seconds())),
		)
	} else {
		fmt.Printf("%s Failed on %s with exit code %d %s\n",
			symbolStyle.Render(symbol),
			host,
			exitCode,
			mutedStyle.Render(fmt.Sprintf("(%.1fs)", totalTime.Seconds())),
		)
	}
}

// renderFailureHelp displays contextual help for command failures.
// This is shown when the failure wasn't already explained (e.g., missing tool).
func renderFailureHelp(exitCode int, command, host string) {
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	var hint string
	switch exitCode {
	case 1:
		hint = "General error. Check command output above for details."
	case 2:
		hint = "Misuse or command failed. Check if a dependency is missing or command syntax is wrong."
	case 126:
		hint = "Command found but not executable. Check file permissions on remote."
	case 127:
		hint = "Command not found. The tool may not be installed or not in PATH."
	case 128:
		hint = "Invalid exit argument. The command may have a bug."
	case 130:
		hint = "Interrupted by Ctrl+C."
	case 137:
		hint = "Killed (likely OOM). The remote may have run out of memory."
	case 139:
		hint = "Segmentation fault. The command crashed."
	case 143:
		hint = "Terminated by SIGTERM."
	default:
		if exitCode > 128 && exitCode < 165 {
			signal := exitCode - 128
			hint = fmt.Sprintf("Killed by signal %d.", signal)
		}
	}

	if hint != "" {
		fmt.Printf("\n%s\n", mutedStyle.Render(hint))
	}

	// Always show recovery suggestions for non-trivial failures
	if exitCode != 130 && exitCode != 143 { // Skip for user interrupts
		fmt.Printf("\n%s\n", mutedStyle.Render("Troubleshooting:"))
		fmt.Printf("%s\n", mutedStyle.Render(fmt.Sprintf("  - Run the command directly: ssh %s %q", host, command)))
		fmt.Printf("%s\n", mutedStyle.Render("  - Check remote logs or environment"))
		fmt.Printf("%s\n", mutedStyle.Render("  - Run 'rr doctor' to verify configuration"))
	}
}

// mapProbeErrorToStatus converts a probe error to a ConnectionStatus for display.
func mapProbeErrorToStatus(err error) ui.ConnectionStatus {
	if err == nil {
		return ui.StatusSuccess
	}

	// Check if it's a ProbeError with a specific reason
	if probeErr, ok := err.(*host.ProbeError); ok {
		switch probeErr.Reason {
		case host.ProbeFailTimeout:
			return ui.StatusTimeout
		case host.ProbeFailRefused:
			return ui.StatusRefused
		case host.ProbeFailUnreachable:
			return ui.StatusUnreachable
		case host.ProbeFailAuth:
			return ui.StatusAuthFailed
		default:
			return ui.StatusFailed
		}
	}

	return ui.StatusFailed
}

// runCmdFlags carries the cobra flag values for run/exec into the shared
// command implementation.
type runCmdFlags struct {
	Verb             string // "run" or "exec" - used in messages and validation
	Host             string
	Tag              string
	ProbeTimeout     string
	Local            bool
	SkipRequirements bool
	SkipSync         bool // exec: don't sync before running
	Repeat           int
	Pull             []string
	PullDest         string
	RemoteCWD        string
	Tail             int
}

// runCommand is the shared implementation behind 'rr run' and 'rr exec'.
func runCommand(args []string, f runCmdFlags) error {
	if len(args) == 0 {
		return errors.New(errors.ErrExec,
			"What should I run?",
			fmt.Sprintf("Usage: rr %s <command>  (e.g., rr %s \"make test\")", f.Verb, f.Verb))
	}

	if err := validateLeadingArg(args, f.Verb); err != nil {
		return err
	}

	probeTimeout, err := ParseProbeTimeout(f.ProbeTimeout)
	if err != nil {
		return err
	}

	// Join all args as the command (handles "rr run make test")
	cmd := strings.Join(args, " ")

	// If --repeat is specified, use parallel execution
	if f.Repeat > 1 {
		exitCode, err := runRepeated(cmd, f.Repeat, f.Host, f.Tag, f.Local)
		if err != nil {
			return err
		}
		if exitCode != 0 {
			return errors.NewExitError(exitCode)
		}
		return nil
	}

	exitCode, err := Run(RunOptions{
		Command:          cmd,
		Host:             f.Host,
		Tag:              f.Tag,
		ProbeTimeout:     probeTimeout,
		SkipSync:         f.SkipSync,
		SkipRequirements: f.SkipRequirements,
		Quiet:            Quiet(),
		Local:            f.Local,
		Pull:             f.Pull,
		PullDest:         f.PullDest,
		RemoteCWD:        f.RemoteCWD,
		Tail:             f.Tail,
		LogName:          f.Verb,
	})

	if err != nil {
		return err
	}

	if exitCode != 0 {
		return errors.NewExitError(exitCode)
	}

	return nil
}

// runRepeated runs a command N times in parallel across available hosts.
// Used for flake detection - run the same test multiple times to surface intermittent failures.
func runRepeated(cmd string, repeatCount int, hostFlag, tagFlag string, localFlag bool) (int, error) {
	// Load and validate config
	resolved, err := config.LoadResolved(Config())
	if err != nil {
		return 1, err
	}

	if err := config.ValidateResolved(resolved); err != nil {
		return 1, err
	}

	// Create N synthetic tasks with the same command
	tasks := make([]parallel.TaskInfo, repeatCount)
	for i := 0; i < repeatCount; i++ {
		tasks[i] = parallel.TaskInfo{
			Name:    fmt.Sprintf("run-%d", i+1),
			Index:   i,
			Command: cmd,
		}
	}

	// Resolve hosts
	hostOrder, hosts, err := config.ResolveHosts(resolved, hostFlag)
	if err != nil {
		return 1, err
	}

	// Handle --local flag
	if localFlag {
		// --local and --tag are mutually exclusive
		if err := ValidateLocalAndTag(localFlag, tagFlag); err != nil {
			return 1, err
		}
		hosts = make(map[string]config.Host)
		hostOrder = nil
	}

	// Filter by tag if specified
	if tagFlag != "" {
		hosts, hostOrder = filterHostsByTag(hosts, hostOrder, tagFlag)
		if len(hosts) == 0 {
			return 1, errors.New(errors.ErrConfig,
				fmt.Sprintf("No hosts found with tag '%s'", tagFlag),
				"Check your host tags in ~/.rr/config.yaml.")
		}
	}

	// Build parallel config
	parallelCfg := parallel.Config{
		OutputMode: parallel.OutputProgress,
		SaveLogs:   true,
	}

	// Set up log writer
	var logWriter *logs.LogWriter
	logDir := resolved.Global.Logs.Dir
	if logDir == "" {
		logDir = "~/.rr/logs"
	}
	logWriter, err = logs.NewLogWriter(logDir, "repeat")
	if err != nil {
		logWriter = nil
	} else {
		parallelCfg.LogDir = logWriter.Dir()
	}

	// Cleanup old logs
	_ = logs.Cleanup(resolved.Global.Logs)

	// Create orchestrator
	orchestrator := parallel.NewOrchestrator(tasks, hosts, hostOrder, resolved, parallelCfg)

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		writeTaskLogs(logWriter, result, "repeat")
	}

	// Render summary
	logDirPath := ""
	if logWriter != nil {
		logDirPath = logWriter.Dir()
	}
	parallel.RenderSummary(result, logDirPath)

	// Return aggregate exit code
	if result.Failed > 0 {
		return 1, nil
	}
	return 0, nil
}
