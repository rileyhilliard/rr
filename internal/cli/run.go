package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
	Quiet            bool          // If true, minimize output (no individual connection attempts)
	Local            bool          // If true, force local execution (skip remote hosts)
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
	wf.PhaseDisplay.Divider()

	// Show the command being run
	wf.PhaseDisplay.CommandPrompt(opts.Command)
	fmt.Println()

	// Set up output streaming
	streamHandler := output.NewStreamHandler(os.Stdout, os.Stderr)
	streamHandler.SetFormatter(output.NewGenericFormatter())

	execStart := time.Now()
	var exitCode int

	if wf.Conn.IsLocal {
		// Local execution
		exitCode, err = exec.ExecuteLocal(opts.Command, wf.WorkDir, streamHandler.Stdout(), streamHandler.Stderr())
	} else {
		// Remote execution - build command with shell config, setup commands, and working directory
		// Prepend project defaults setup to the command for consistency with task execution
		cmd := opts.Command
		if len(wf.Resolved.Project.Defaults.Setup) > 0 {
			cmd = strings.Join(wf.Resolved.Project.Defaults.Setup, " && ") + " && " + cmd
		}
		fullCmd := exec.BuildRemoteCommand(cmd, &wf.Conn.Host)
		exitCode, err = wf.Conn.Client.ExecStream(fullCmd, streamHandler.Stdout(), streamHandler.Stderr())
	}
	execDuration := time.Since(execStart)

	if err != nil {
		return 1, err
	}

	// Release lock early if command completed (wf.Close() will also release, but early release is cleaner)
	if wf.Lock != nil {
		wf.Lock.Release() //nolint:errcheck // Lock release errors are non-fatal
	}

	// Check for command-not-found and other special exit codes
	failureExplained := false
	if exitCode != 0 {
		// Check for command not found (exit code 127)
		// Pass SSH client for PATH probing if available (remote execution only)
		var sshClient exec.SSHExecer
		if !wf.Conn.IsLocal && wf.Conn.Client != nil {
			sshClient = wf.Conn.Client
		}

		// Try to detect a missing tool error
		missingTool := exec.DetectMissingTool(opts.Command, streamHandler.GetStderrCapture(), exitCode, sshClient, wf.Conn.Name)
		if missingTool != nil {
			failureExplained = true
			// Show the error message
			fmt.Println()
			fmt.Printf("%s %s\n\n", ui.SymbolFail, missingTool.Error())
			fmt.Println(missingTool.Suggestion)

			// Offer interactive fix if we're on a remote host with SSH client
			if !wf.Conn.IsLocal && wf.Conn.Client != nil {
				// Get config path for potential updates
				configPath, _ := config.Find(Config())
				if configPath != "" {
					fixResult, _ := HandleMissingTool(missingTool, wf.Conn.Client, configPath)
					if fixResult != nil && fixResult.ShouldRetry {
						// User wants to retry - show final status then indicate retry
						wf.PhaseDisplay.ThinDivider()
						renderFinalStatus(wf.PhaseDisplay, exitCode, time.Since(wf.StartTime), execDuration, wf.Conn.Name)

						// Close current workflow and retry
						wf.Close()
						return Run(opts)
					}
				}
			}
		} else if provider, ok := streamHandler.GetFormatter().(output.TestSummaryProvider); ok {
			// Check for test failures and render summary if available
			failures := provider.GetTestFailures()
			if len(failures) > 0 {
				failureExplained = true
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
			}
		}
	}

	// Show final status
	wf.PhaseDisplay.ThinDivider()
	renderFinalStatus(wf.PhaseDisplay, exitCode, time.Since(wf.StartTime), execDuration, wf.Conn.Name)

	// Show contextual help for unexplained failures
	if exitCode != 0 && !failureExplained {
		renderFailureHelp(exitCode, opts.Command, wf.Conn.Name)
	}

	return exitCode, nil
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

// runCommand is the actual implementation called by the cobra command.
func runCommand(args []string, hostFlag, tagFlag, probeTimeoutFlag string, localFlag, skipRequirementsFlag bool, repeatCount int) error {
	if len(args) == 0 {
		return errors.New(errors.ErrExec,
			"What should I run?",
			"Usage: rr run <command>  (e.g., rr run \"make test\")")
	}

	probeTimeout, err := ParseProbeTimeout(probeTimeoutFlag)
	if err != nil {
		return err
	}

	// Join all args as the command (handles "rr run make test")
	cmd := strings.Join(args, " ")

	// If --repeat is specified, use parallel execution
	if repeatCount > 1 {
		exitCode, err := runRepeated(cmd, repeatCount, hostFlag, tagFlag, localFlag)
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
		Host:             hostFlag,
		Tag:              tagFlag,
		ProbeTimeout:     probeTimeout,
		SkipRequirements: skipRequirementsFlag,
		Quiet:            Quiet(),
		Local:            localFlag,
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
