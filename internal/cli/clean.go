package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/clean"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
)

// CleanOptions holds options for the clean command.
type CleanOptions struct {
	Host         string        // Specific host to clean (empty for all branch-enabled hosts)
	DryRun       bool          // Show what would be removed without deleting
	ProbeTimeout time.Duration // SSH probe timeout
}

// cleanCommand discovers and removes stale per-branch directories.
func cleanCommand(opts CleanOptions) error {
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	if len(globalCfg.Hosts) == 0 {
		return errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add a host with 'rr host add' first.")
	}

	// Determine which hosts to clean
	hostsToClean, err := selectHostsForClean(globalCfg, opts.Host)
	if err != nil {
		return err
	}

	// Get local branches for comparison
	localBranches, err := config.ListLocalBranches()
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrExec,
			"Failed to list local git branches",
			"Make sure you're in a git repository.")
	}

	probeTimeout := opts.ProbeTimeout
	if probeTimeout == 0 {
		probeTimeout = globalCfg.Defaults.ProbeTimeout
		if probeTimeout == 0 {
			probeTimeout = 2 * time.Second
		}
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Process each host
	var totalStale []hostStaleResult
	for _, hostName := range hostsToClean {
		hostCfg := globalCfg.Hosts[hostName]

		// Check if this host uses ${BRANCH} — must use the raw template,
		// since Dir has already been expanded by LoadGlobal.
		_, hasBranch := config.ExpandRemoteGlob(hostCfg.DirTemplate)
		if !hasBranch {
			fmt.Printf("%s %s: dir template has no ${BRANCH}, skipping\n", dimStyle.Render("·"), hostName)
			continue
		}

		// Connect to host
		spinner := ui.NewSpinner(fmt.Sprintf("Scanning %s for stale directories", hostName))
		spinner.Start()

		conn, connErr := connectToHost(hostCfg, probeTimeout)
		if connErr != nil {
			spinner.Fail()
			fmt.Printf("  %s Could not connect: %v\n", ui.SymbolWarning, connErr)
			continue
		}
		staleDirs, discoverErr := clean.Discover(conn.Client, hostCfg.DirTemplate, localBranches)
		conn.Close()
		if discoverErr != nil {
			spinner.Fail()
			fmt.Printf("  %s Discovery failed: %v\n", ui.SymbolWarning, discoverErr)
			continue
		}

		spinner.Success()

		if len(staleDirs) == 0 {
			fmt.Printf("  No stale directories found\n")
		}

		totalStale = append(totalStale, hostStaleResult{
			hostName: hostName,
			hostCfg:  hostCfg,
			stale:    staleDirs,
		})
	}

	// Collect all stale dirs across hosts
	var allStale int
	for i := range totalStale {
		allStale += len(totalStale[i].stale)
	}

	if allStale == 0 {
		fmt.Printf("\n%s No stale directories to clean\n", ui.SymbolSuccess)
		return nil
	}

	// Display results
	fmt.Println()
	for i := range totalStale {
		r := &totalStale[i]
		if len(r.stale) == 0 {
			continue
		}
		fmt.Printf("  %s:\n", r.hostName)
		for _, dir := range r.stale {
			size := dir.DiskUsage
			if size == "" || size == "?" {
				size = "?"
			}
			fmt.Printf("    %s  %s  %s\n",
				dir.Path,
				dimStyle.Render("(no local branch)"),
				dimStyle.Render("— "+size))
		}
		fmt.Println()
	}

	if opts.DryRun {
		fmt.Printf("%s Dry run: would remove %d stale director%s\n",
			ui.SymbolPending, allStale, pluralize(allStale))
		return nil
	}

	// Confirm deletion
	var confirm bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Remove %d stale director%s?", allStale, pluralize(allStale))).
				Description("This cannot be undone").
				Value(&confirm),
		),
	)
	if err := form.Run(); err != nil {
		return nil
	}
	if !confirm {
		fmt.Println("Cancelled.")
		return nil
	}

	// Delete stale dirs
	var totalRemoved, totalFailed int
	for i := range totalStale {
		r := &totalStale[i]
		if len(r.stale) == 0 {
			continue
		}

		conn, connErr := connectToHost(r.hostCfg, probeTimeout)
		if connErr != nil {
			fmt.Printf("%s %s: reconnection failed: %v\n", ui.SymbolFail, r.hostName, connErr)
			totalFailed += len(r.stale)
			continue
		}
		removed, errs := clean.Remove(conn.Client, r.hostCfg.DirTemplate, r.stale)
		conn.Close()

		totalRemoved += len(removed)
		totalFailed += len(errs)
		for _, e := range errs {
			fmt.Printf("  %s %s\n", ui.SymbolWarning, e)
		}
	}

	if totalRemoved > 0 {
		fmt.Printf("%s Removed %d stale director%s\n", ui.SymbolSuccess, totalRemoved, pluralize(totalRemoved))
	}
	if totalFailed > 0 {
		return errors.New(errors.ErrExec,
			fmt.Sprintf("Failed to remove %d director%s", totalFailed, pluralize(totalFailed)),
			"Check host connectivity and permissions.")
	}

	return nil
}

type hostStaleResult struct {
	hostName string
	hostCfg  config.Host
	stale    []clean.StaleDir
}

// selectHostsForClean determines which hosts to clean.
func selectHostsForClean(globalCfg *config.GlobalConfig, preferredHost string) ([]string, error) {
	if preferredHost != "" {
		if _, exists := globalCfg.Hosts[preferredHost]; !exists {
			var available []string
			for k := range globalCfg.Hosts {
				available = append(available, k)
			}
			sort.Strings(available)
			return nil, errors.New(errors.ErrConfig,
				fmt.Sprintf("Host '%s' not found", preferredHost),
				fmt.Sprintf("Available hosts: %s", strings.Join(available, ", ")))
		}
		return []string{preferredHost}, nil
	}

	// Default: all hosts (we filter by ${BRANCH} presence later)
	var hosts []string
	for name := range globalCfg.Hosts {
		hosts = append(hosts, name)
	}
	sort.Strings(hosts)
	return hosts, nil
}

// connectToHost tries each SSH alias for a host until one connects.
func connectToHost(hostCfg config.Host, probeTimeout time.Duration) (*host.Connection, error) {
	var lastErr error
	for _, sshAlias := range hostCfg.SSH {
		client, latency, err := host.ProbeAndConnect(sshAlias, probeTimeout)
		if err == nil {
			return &host.Connection{
				Alias:   sshAlias,
				Client:  client,
				Host:    hostCfg,
				Latency: latency,
			}, nil
		}
		lastErr = err
	}
	return nil, errors.WrapWithCode(lastErr, errors.ErrSSH,
		fmt.Sprintf("All SSH aliases unreachable: %v", hostCfg.SSH),
		"Check that the host is online and SSH is configured correctly.")
}

func pluralize(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
