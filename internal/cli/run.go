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
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
)

// RunOptions holds options for the run command.
type RunOptions struct {
	Command    string
	Host       string // Preferred host name
	SkipSync   bool   // If true, skip sync phase (used by exec)
	SkipLock   bool   // If true, skip locking
	DryRun     bool   // If true, show what would be done without doing it
	WorkingDir string // Override local working directory
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

	// Phase 1: Connect
	connectStart := time.Now()
	spinner := ui.NewSpinner("Connecting")
	spinner.Start()

	preferredHost := opts.Host
	if preferredHost == "" {
		preferredHost = cfg.Default
	}

	conn, err := selector.Select(preferredHost)
	if err != nil {
		spinner.Fail()
		return 1, err
	}
	spinner.Success()
	phaseDisplay.RenderSuccess("Connected to "+conn.Alias, time.Since(connectStart))

	// Phase 2: Sync (unless skipped)
	var syncDuration time.Duration
	if !opts.SkipSync {
		syncStart := time.Now()
		spinner = ui.NewSpinner("Syncing files")
		spinner.Start()

		err = sync.Sync(conn, workDir, cfg.Sync, nil)
		if err != nil {
			spinner.Fail()
			return 1, err
		}

		syncDuration = time.Since(syncStart)
		spinner.Success()
		phaseDisplay.RenderSuccess("Files synced", syncDuration)
	} else {
		phaseDisplay.RenderSkipped("Sync", "skipped")
	}

	// Phase 3: Acquire lock (unless disabled)
	var lck *lock.Lock
	if cfg.Lock.Enabled && !opts.SkipLock {
		lockStart := time.Now()
		spinner = ui.NewSpinner("Acquiring lock")
		spinner.Start()

		lck, err = lock.Acquire(conn, cfg.Lock, projectHash)
		if err != nil {
			spinner.Fail()
			return 1, err
		}
		defer lck.Release() //nolint:errcheck // Lock release errors are non-fatal in cleanup

		spinner.Success()
		phaseDisplay.RenderSuccess("Lock acquired", time.Since(lockStart))
	}

	// Phase 4: Execute command
	phaseDisplay.Divider()

	// Build the command to run in the remote directory
	remoteDir := config.Expand(conn.Host.Dir)
	fullCmd := fmt.Sprintf("cd %q && %s", remoteDir, opts.Command)

	// Show the command being run
	phaseDisplay.CommandPrompt(opts.Command)
	fmt.Println()

	// Set up output streaming
	streamHandler := output.NewStreamHandler(os.Stdout, os.Stderr)
	streamHandler.SetFormatter(output.NewGenericFormatter())

	execStart := time.Now()
	exitCode, err := conn.Client.ExecStream(fullCmd, streamHandler.Stdout(), streamHandler.Stderr())
	execDuration := time.Since(execStart)

	if err != nil {
		return 1, err
	}

	// Release lock early if command completed
	if lck != nil {
		lck.Release() //nolint:errcheck // Lock release errors are non-fatal
	}

	// Show summary
	phaseDisplay.ThinDivider()
	renderSummary(phaseDisplay, exitCode, time.Since(startTime), execDuration, conn.Alias)

	return exitCode, nil
}

// renderSummary displays the final execution summary.
func renderSummary(pd *ui.PhaseDisplay, exitCode int, totalTime, execTime time.Duration, host string) {
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

// runCommand is the actual implementation called by the cobra command.
func runCommand(args []string, hostFlag string) error {
	if len(args) == 0 {
		return errors.New(errors.ErrExec,
			"No command specified",
			"Usage: rr run <command>")
	}

	// Join all args as the command (handles "rr run make test")
	cmd := strings.Join(args, " ")

	exitCode, err := Run(RunOptions{
		Command: cmd,
		Host:    hostFlag,
	})

	if err != nil {
		return err
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}

	return nil
}
