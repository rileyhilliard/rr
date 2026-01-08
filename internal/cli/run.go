package cli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
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
)

// RunOptions holds options for the run command.
type RunOptions struct {
	Command      string
	Host         string        // Preferred host name
	Tag          string        // Filter hosts by tag
	ProbeTimeout time.Duration // Override SSH probe timeout (0 means use config default)
	SkipSync     bool          // If true, skip sync phase (used by exec)
	SkipLock     bool          // If true, skip locking
	DryRun       bool          // If true, show what would be done without doing it
	WorkingDir   string        // Override local working directory
	Quiet        bool          // If true, minimize output (no individual connection attempts)
}

// Run syncs files and executes a command on the remote host.
// This is the main workflow that ties together all subsystems.
func Run(opts RunOptions) (int, error) {
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

	// Track connection status for output
	var usedLocalFallback bool

	// Set up event handler for connection progress with visual feedback
	selector.SetEventHandler(func(event host.ConnectionEvent) {
		switch event.Type {
		case host.EventTrying:
			// Don't show "trying" - only show results
		case host.EventFailed:
			// Map probe error to connection status
			status := mapProbeErrorToStatus(event.Error)
			connDisplay.AddAttempt(event.Alias, status, event.Latency, event.Message)
		case host.EventConnected:
			connDisplay.AddAttempt(event.Alias, ui.StatusSuccess, event.Latency, "")
		case host.EventLocalFallback:
			usedLocalFallback = true
		}
	})

	// Connect - either by tag or by host/default
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

	// Show connection result
	if usedLocalFallback {
		connDisplay.SuccessLocal()
	} else {
		connDisplay.Success(conn.Name, conn.Alias)
	}

	// Phase 2: Sync (unless skipped or local)
	var syncDuration time.Duration
	if conn.IsLocal {
		// Local execution - no sync needed
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

	// Phase 4: Execute command
	phaseDisplay.Divider()

	// Show the command being run
	phaseDisplay.CommandPrompt(opts.Command)
	fmt.Println()

	// Set up output streaming
	streamHandler := output.NewStreamHandler(os.Stdout, os.Stderr)
	streamHandler.SetFormatter(output.NewGenericFormatter())

	execStart := time.Now()
	var exitCode int

	if conn.IsLocal {
		// Local execution
		exitCode, err = exec.ExecuteLocal(opts.Command, workDir, streamHandler.Stdout(), streamHandler.Stderr())
	} else {
		// Remote execution - build command with cd to remote directory
		remoteDir := config.Expand(conn.Host.Dir)
		fullCmd := fmt.Sprintf("cd %q && %s", remoteDir, opts.Command)
		exitCode, err = conn.Client.ExecStream(fullCmd, streamHandler.Stdout(), streamHandler.Stderr())
	}
	execDuration := time.Since(execStart)

	if err != nil {
		return 1, err
	}

	// Release lock early if command completed
	if lck != nil {
		lck.Release() //nolint:errcheck // Lock release errors are non-fatal
	}

	// Check for test failures and render summary if available
	if exitCode != 0 {
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
			}
		}
	}

	// Show final status
	phaseDisplay.ThinDivider()
	renderFinalStatus(phaseDisplay, exitCode, time.Since(startTime), execDuration, conn.Alias)

	return exitCode, nil
}

// renderFinalStatus displays the final execution status line.
func renderFinalStatus(pd *ui.PhaseDisplay, exitCode int, totalTime, execTime time.Duration, host string) {
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

// hashProject creates a short hash of the project path for lock identification.
func hashProject(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", h[:8]) // First 16 hex chars
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
func runCommand(args []string, hostFlag, tagFlag, probeTimeoutFlag string) error {
	if len(args) == 0 {
		return errors.New(errors.ErrExec,
			"No command specified",
			"Usage: rr run <command>")
	}

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

	// Join all args as the command (handles "rr run make test")
	cmd := strings.Join(args, " ")

	exitCode, err := Run(RunOptions{
		Command:      cmd,
		Host:         hostFlag,
		Tag:          tagFlag,
		ProbeTimeout: probeTimeout,
	})

	if err != nil {
		return err
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}
