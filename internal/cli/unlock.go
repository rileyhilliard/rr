package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/ui"
)

// UnlockOptions holds options for the unlock command.
type UnlockOptions struct {
	Host string // Specific host to unlock (empty for default or picker)
	All  bool   // Unlock all hosts in the project (or all global hosts)
}

// hostUnlockOutcome records the result of unlocking a single host.
type hostUnlockOutcome struct {
	Host   string `json:"host"`
	Status string `json:"status"` // released | not_locked | failed
	Holder string `json:"holder,omitempty"`
	Error  string `json:"error,omitempty"`
}

// unlockCommand releases the lock on one or more remote hosts.
func unlockCommand(opts UnlockOptions) error {
	// Load global config for hosts
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	if len(globalCfg.Hosts) == 0 {
		return errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add a host with 'rr host add' first.")
	}

	// Load project config for lock settings (if available)
	var lockCfg config.LockConfig
	cfgPath, _ := config.Find(Config())
	if cfgPath != "" {
		if projectCfg, err := config.Load(cfgPath); err == nil {
			lockCfg = projectCfg.Lock
		} else {
			lockCfg = config.DefaultConfig().Lock
		}
	} else {
		lockCfg = config.DefaultConfig().Lock
	}

	// Determine which hosts to unlock
	var hostsToUnlock []string

	if opts.All {
		// Unlock the project's hosts; all global hosts when no project scopes them
		hostsToUnlock = hostsForUnlockAll(globalCfg)
	} else if opts.Host != "" {
		// Specific host provided
		if _, exists := globalCfg.Hosts[opts.Host]; !exists {
			var available []string
			for k := range globalCfg.Hosts {
				available = append(available, k)
			}
			sort.Strings(available)
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("Host '%s' not found", opts.Host),
				fmt.Sprintf("Available hosts: %s", strings.Join(available, ", ")))
		}
		hostsToUnlock = []string{opts.Host}
	} else {
		// No host specified - use single host or show picker
		if len(globalCfg.Hosts) == 1 {
			// Only one host, use it
			for name := range globalCfg.Hosts {
				hostsToUnlock = []string{name}
			}
		} else {
			// Multiple hosts - show picker (pretty mode only; structured
			// callers must name a host or pass --all)
			if !PrettyMode() {
				return errors.New(errors.ErrConfig,
					"Multiple hosts configured - specify which to unlock",
					"Use 'rr unlock <host>' or 'rr unlock --all'.")
			}
			selectedHost, err := pickHostForUnlock(globalCfg)
			if err != nil {
				return err
			}
			if selectedHost == "" {
				fmt.Println("Cancelled.")
				return nil
			}
			hostsToUnlock = []string{selectedHost}
		}
	}

	// Process hosts concurrently - each needs its own SSH probe
	showSpinner := PrettyMode() && len(hostsToUnlock) == 1
	outcomes := make([]hostUnlockOutcome, len(hostsToUnlock))
	var wg sync.WaitGroup
	for i, hostName := range hostsToUnlock {
		wg.Add(1)
		go func(i int, hostName string) {
			defer wg.Done()
			outcomes[i] = unlockHost(hostName, globalCfg.Hosts[hostName], lockCfg, showSpinner)
		}(i, hostName)
	}
	wg.Wait()

	var successCount, notLockedCount, failCount int
	for _, o := range outcomes {
		switch o.Status {
		case "released":
			successCount++
		case "not_locked":
			notLockedCount++
		case "failed":
			failCount++
		}
	}

	if PrettyMode() {
		for _, o := range outcomes {
			printUnlockOutcome(o)
		}

		// Summary for --all
		if opts.All && len(hostsToUnlock) > 1 {
			fmt.Println()
			if successCount > 0 {
				fmt.Printf("Released locks on %d host(s)\n", successCount)
			}
			if notLockedCount > 0 {
				fmt.Printf("%d host(s) had no lock\n", notLockedCount)
			}
			if failCount > 0 {
				fmt.Printf("%d host(s) failed\n", failCount)
			}
		}
	} else {
		_ = WriteJSONSuccess(os.Stdout, map[string]interface{}{
			"hosts":      outcomes,
			"released":   successCount,
			"not_locked": notLockedCount,
			"failed":     failCount,
		})
	}

	if failCount > 0 {
		return errors.New(errors.ErrLock,
			"Some hosts failed to unlock",
			"Check the SSH connection and try again.")
	}

	return nil
}

// hostsForUnlockAll returns the hosts --all should target: the project's
// resolved host list when a project config scopes them, otherwise every
// global host.
func hostsForUnlockAll(globalCfg *config.GlobalConfig) []string {
	if resolved, err := config.LoadResolved(Config()); err == nil && resolved.ProjectRoot != "" {
		if names, _, err := config.ResolveHosts(resolved, ""); err == nil && len(names) > 0 {
			sorted := append([]string(nil), names...)
			sort.Strings(sorted)
			return sorted
		}
	}

	var names []string
	for name := range globalCfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// unlockHost attempts to release the lock on a single host.
func unlockHost(hostName string, hostCfg config.Host, lockCfg config.LockConfig, showSpinner bool) hostUnlockOutcome {
	outcome := hostUnlockOutcome{Host: hostName}

	if len(hostCfg.SSH) == 0 {
		outcome.Status = "failed"
		outcome.Error = "no SSH connections configured"
		return outcome
	}

	// Get lock directory path
	lockDir := lock.LockDir(lockCfg)

	var spinner *ui.Spinner
	if showSpinner {
		spinner = ui.NewSpinner(fmt.Sprintf("Connecting to %s", hostName))
		spinner.Start()
	}

	// Try to connect using the first available SSH alias
	var conn *host.Connection
	var connErr error
	for _, sshAlias := range hostCfg.SSH {
		client, latency, err := host.ProbeAndConnect(sshAlias, 5*time.Second)
		if err == nil {
			conn = &host.Connection{
				Name:    hostName,
				Alias:   sshAlias,
				Client:  client,
				Host:    hostCfg,
				Latency: latency,
			}
			break
		}
		connErr = err
	}

	if conn == nil {
		if spinner != nil {
			spinner.Fail()
		}
		outcome.Status = "failed"
		outcome.Error = fmt.Sprintf("could not connect: %v", connErr)
		return outcome
	}
	defer conn.Close()
	if spinner != nil {
		spinner.Success()
	}

	// Check if lock exists
	if !lock.IsLocked(conn, lockCfg) {
		outcome.Status = "not_locked"
		return outcome
	}

	// Get lock holder info before releasing
	if info := lock.GetLockInfo(conn, lockCfg); info != nil {
		outcome.Holder = info.Describe()
	} else {
		outcome.Holder = lock.GetLockHolder(conn, lockCfg)
	}

	// Release the lock
	if err := lock.ForceRelease(conn, lockDir); err != nil {
		outcome.Status = "failed"
		outcome.Error = fmt.Sprintf("failed to release lock: %v", err)
		return outcome
	}

	outcome.Status = "released"
	return outcome
}

// printUnlockOutcome renders one host's unlock result in pretty mode.
func printUnlockOutcome(o hostUnlockOutcome) {
	switch o.Status {
	case "released":
		if o.Holder != "" && o.Holder != "unknown" {
			fmt.Printf("%s %s: lock released (was held by %s)\n", ui.SymbolSuccess, o.Host, o.Holder)
		} else {
			fmt.Printf("%s %s: lock released\n", ui.SymbolSuccess, o.Host)
		}
	case "not_locked":
		fmt.Printf("%s %s: no lock held\n", ui.SymbolPending, o.Host)
	default:
		fmt.Printf("%s %s: %s\n", ui.SymbolFail, o.Host, o.Error)
	}
}

// pickHostForUnlock shows a host picker for the unlock command.
func pickHostForUnlock(globalCfg *config.GlobalConfig) (string, error) {
	var hostNames []string
	for k := range globalCfg.Hosts {
		hostNames = append(hostNames, k)
	}
	sort.Strings(hostNames)

	options := make([]huh.Option[string], len(hostNames))
	for i, h := range hostNames {
		label := h
		if hostCfg, ok := globalCfg.Hosts[h]; ok && len(hostCfg.SSH) > 0 {
			label += " - " + hostCfg.SSH[0]
		}
		options[i] = huh.NewOption(label, h)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select host to unlock").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return "", errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't get your selection",
			"Try again or use: rr unlock <host>")
	}

	return selected, nil
}
