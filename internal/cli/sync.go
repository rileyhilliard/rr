package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
)

// SyncOptions holds options for the sync command.
type SyncOptions struct {
	Host         string        // Preferred host name
	Tag          string        // Filter hosts by tag
	ProbeTimeout time.Duration // Override SSH probe timeout (0 means use config default)
	DryRun       bool          // If true, show what would be synced without syncing
	WorkingDir   string        // Override local working directory
}

// Sync transfers files to the remote host without executing any command.
func Sync(opts SyncOptions) error {
	startTime := time.Now()
	phaseDisplay := ui.NewPhaseDisplay(os.Stdout)

	// Load config
	cfgPath, err := config.Find(Config())
	if err != nil {
		return err
	}
	if cfgPath == "" {
		return errors.New(errors.ErrConfig,
			"No config file found",
			"Run 'rr init' to create a .rr.yaml config file")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	if err := config.Validate(cfg); err != nil {
		return err
	}

	// Determine working directory
	workDir := opts.WorkingDir
	if workDir == "" {
		workDir, err = os.Getwd()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrExec,
				"Failed to get working directory",
				"Check directory permissions")
		}
	}

	// Create host selector
	selector := host.NewSelector(cfg.Hosts)
	defer selector.Close()

	// Set probe timeout (CLI flag overrides config)
	probeTimeout := cfg.ProbeTimeout
	if opts.ProbeTimeout > 0 {
		probeTimeout = opts.ProbeTimeout
	}
	if probeTimeout > 0 {
		selector.SetTimeout(probeTimeout)
	}

	// Phase 1: Connect
	connectStart := time.Now()
	spinner := ui.NewSpinner("Connecting")
	spinner.Start()

	preferredHost := opts.Host
	if preferredHost == "" {
		preferredHost = cfg.Default
	}

	// Connect - either by tag or by host/default
	var conn *host.Connection
	if opts.Tag != "" {
		conn, err = selector.SelectByTag(opts.Tag)
	} else {
		conn, err = selector.Select(preferredHost)
	}
	if err != nil {
		spinner.Fail()
		return err
	}
	spinner.Success()
	phaseDisplay.RenderSuccess("Connected to "+conn.Alias, time.Since(connectStart))

	// Phase 2: Sync
	syncStart := time.Now()
	spinner = ui.NewSpinner("Syncing files")
	spinner.Start()

	// Add dry-run flag if requested
	syncCfg := cfg.Sync
	if opts.DryRun {
		syncCfg.Flags = append(syncCfg.Flags, "--dry-run", "-v")
	}

	err = sync.Sync(conn, workDir, syncCfg, nil)
	if err != nil {
		spinner.Fail()
		return err
	}

	syncDuration := time.Since(syncStart)
	spinner.Success()

	totalDuration := time.Since(startTime)

	// Show summary
	fmt.Println()
	if opts.DryRun {
		fmt.Printf("%s Dry run completed in %.1fs\n",
			ui.SymbolComplete, totalDuration.Seconds())
	} else {
		fmt.Printf("%s Files synced to %s in %.1fs\n",
			ui.SymbolComplete, conn.Alias, syncDuration.Seconds())
	}

	return nil
}

// syncCommand is the implementation called by the cobra command.
func syncCommand(hostFlag, tagFlag, probeTimeoutFlag string, dryRun bool) error {
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

	return Sync(SyncOptions{
		Host:         hostFlag,
		Tag:          tagFlag,
		ProbeTimeout: probeTimeout,
		DryRun:       dryRun,
	})
}
