package cli

import (
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
)

// SyncOptions holds options for the sync command.
type SyncOptions struct {
	Host         string        // Preferred host name
	Tag          string        // Filter hosts by tag
	ProbeTimeout time.Duration // Override SSH probe timeout (0 means use config default)
	DryRun       bool          // If true, show what would be synced without syncing
	SkipLock     bool          // If true, skip locking
	WorkingDir   string        // Override local working directory
}

// Sync transfers files to the remote host without executing any command.
func Sync(opts SyncOptions) error {
	startTime := time.Now()
	phaseDisplay := ui.NewPhaseDisplay(os.Stdout)

	// Load resolved config (global + project)
	resolved, err := config.LoadResolved(Config())
	if err != nil {
		return err
	}

	if err := config.ValidateResolved(resolved); err != nil {
		return err
	}

	// Determine working directory
	workDir := opts.WorkingDir
	if workDir == "" {
		workDir, err = os.Getwd()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrExec,
				"Can't figure out what directory you're in",
				"This is unusual - check your directory permissions.")
		}
	}

	// Create host selector with proper priority order
	hostOrder, projectHosts, err := config.ResolveHosts(resolved, opts.Host)
	if err != nil {
		// Fall back to all global hosts if resolution fails
		projectHosts = resolved.Global.Hosts
	}
	selector := host.NewSelector(projectHosts)
	selector.SetHostOrder(hostOrder)
	defer selector.Close()

	// Set probe timeout (CLI flag overrides config)
	probeTimeout := resolved.Global.Defaults.ProbeTimeout
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

	// Resolve preferred host using resolution order
	preferredHost := opts.Host
	if preferredHost == "" {
		preferredHost, _, _ = config.ResolveHost(resolved, "")
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

	// Phase 2: Acquire lock (skip for dry-run and local connections)
	lockCfg := config.DefaultConfig().Lock
	if resolved.Project != nil {
		lockCfg = resolved.Project.Lock
	}

	if lockCfg.Enabled && !opts.DryRun && !opts.SkipLock && !conn.IsLocal {
		lockStart := time.Now()
		lockSpinner := ui.NewSpinner("Acquiring lock")
		lockSpinner.Start()

		lck, err := lock.Acquire(conn, lockCfg, "sync")
		if err != nil {
			lockSpinner.Fail()
			return err
		}
		defer lck.Release() //nolint:errcheck // Lock release errors are non-fatal

		lockSpinner.Success()
		phaseDisplay.RenderSuccess("Lock acquired", time.Since(lockStart))
	}

	// Phase 3: Sync
	syncStart := time.Now()
	spinner = ui.NewSpinner("Syncing files")
	spinner.Start()

	// Use project sync config if available, otherwise use defaults
	syncCfg := config.DefaultConfig().Sync
	if resolved.Project != nil {
		syncCfg = resolved.Project.Sync
	}
	// Add dry-run flag if requested (copy first to avoid mutating shared config slice)
	if opts.DryRun {
		syncCfg.Flags = append(slices.Clone(syncCfg.Flags), "--dry-run", "-v")
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
	probeTimeout, err := ParseProbeTimeout(probeTimeoutFlag)
	if err != nil {
		return err
	}

	return Sync(SyncOptions{
		Host:         hostFlag,
		Tag:          tagFlag,
		ProbeTimeout: probeTimeout,
		DryRun:       dryRun,
	})
}
