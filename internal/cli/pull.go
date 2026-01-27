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

// PullOptions holds options for the pull command.
type PullOptions struct {
	Patterns     []string      // Remote paths or glob patterns to pull
	Dest         string        // Local destination directory (default: current directory)
	Host         string        // Preferred host name
	Tag          string        // Filter hosts by tag
	ProbeTimeout time.Duration // Override SSH probe timeout (0 means use config default)
	DryRun       bool          // If true, show what would be pulled without pulling
}

// Pull downloads files from the remote host to the local machine.
func Pull(opts PullOptions) error {
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

	// Phase 2: Pull
	pullStart := time.Now()
	spinner = ui.NewSpinner("Pulling files")
	spinner.Start()

	// Convert string patterns to PullItems
	pullItems := make([]config.PullItem, len(opts.Patterns))
	for i, p := range opts.Patterns {
		pullItems[i] = config.PullItem{Src: p}
	}

	// Build pull options
	pullOpts := sync.PullOptions{
		Patterns:    pullItems,
		DefaultDest: opts.Dest,
	}

	// Add dry-run flag if requested
	if opts.DryRun {
		pullOpts.Flags = append(pullOpts.Flags, "--dry-run", "-v")
	}

	err = sync.Pull(conn, pullOpts, nil)
	if err != nil {
		spinner.Fail()
		return err
	}

	pullDuration := time.Since(pullStart)
	spinner.Success()

	totalDuration := time.Since(startTime)

	// Show summary
	fmt.Println()
	if opts.DryRun {
		fmt.Printf("%s Dry run completed in %.1fs\n",
			ui.SymbolComplete, totalDuration.Seconds())
	} else {
		destStr := opts.Dest
		if destStr == "" {
			destStr = "current directory"
		}
		fmt.Printf("%s Files pulled from %s to %s in %.1fs\n",
			ui.SymbolComplete, conn.Alias, destStr, pullDuration.Seconds())
	}

	return nil
}

// pullCommand is the implementation called by the cobra command.
func pullCommand(patterns []string, destFlag, hostFlag, tagFlag, probeTimeoutFlag string, dryRun bool) error {
	if len(patterns) == 0 {
		return errors.New(errors.ErrExec,
			"What should I pull?",
			"Usage: rr pull <pattern> [pattern...]  (e.g., rr pull coverage.xml)")
	}

	probeTimeout, err := ParseProbeTimeout(probeTimeoutFlag)
	if err != nil {
		return err
	}

	return Pull(PullOptions{
		Patterns:     patterns,
		Dest:         destFlag,
		Host:         hostFlag,
		Tag:          tagFlag,
		ProbeTimeout: probeTimeout,
		DryRun:       dryRun,
	})
}
