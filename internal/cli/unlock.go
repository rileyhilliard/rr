package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	All  bool   // Unlock all configured hosts
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

	// Get current working directory for project hash
	workDir, err := os.Getwd()
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrExec,
			"Can't determine current directory",
			"Check your directory permissions.")
	}
	projectHash := hashProject(workDir)

	// Determine which hosts to unlock
	var hostsToUnlock []string

	if opts.All {
		// Unlock all configured hosts
		for name := range globalCfg.Hosts {
			hostsToUnlock = append(hostsToUnlock, name)
		}
		sort.Strings(hostsToUnlock)
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
		// No host specified - use default or show picker
		if globalCfg.Defaults.Host != "" {
			hostsToUnlock = []string{globalCfg.Defaults.Host}
		} else if len(globalCfg.Hosts) == 1 {
			// Only one host, use it
			for name := range globalCfg.Hosts {
				hostsToUnlock = []string{name}
			}
		} else {
			// Multiple hosts, no default - show picker
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

	// Process each host
	var successCount, notLockedCount, failCount int

	for _, hostName := range hostsToUnlock {
		hostCfg := globalCfg.Hosts[hostName]
		result := unlockHost(hostName, hostCfg, lockCfg, projectHash)

		switch result {
		case unlockResultSuccess:
			successCount++
		case unlockResultNotLocked:
			notLockedCount++
		case unlockResultFailed:
			failCount++
		}
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

	if failCount > 0 {
		return errors.New(errors.ErrLock,
			"Some hosts failed to unlock",
			"Check the SSH connection and try again.")
	}

	return nil
}

type unlockResult int

const (
	unlockResultSuccess unlockResult = iota
	unlockResultNotLocked
	unlockResultFailed
)

// unlockHost attempts to release the lock on a single host.
func unlockHost(hostName string, hostCfg config.Host, lockCfg config.LockConfig, projectHash string) unlockResult {
	if len(hostCfg.SSH) == 0 {
		fmt.Printf("%s %s: no SSH connections configured\n", ui.SymbolFail, hostName)
		return unlockResultFailed
	}

	// Build lock directory path
	baseDir := lockCfg.Dir
	if baseDir == "" {
		baseDir = "/tmp"
	}
	lockDir := filepath.Join(baseDir, fmt.Sprintf("rr-%s.lock", projectHash))

	// Try to connect using the first available SSH alias
	spinner := ui.NewSpinner(fmt.Sprintf("Connecting to %s", hostName))
	spinner.Start()

	var conn *host.Connection
	var connErr error
	for _, sshAlias := range hostCfg.SSH {
		client, latency, err := host.ProbeAndConnect(sshAlias, 10*time.Second)
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
		spinner.Fail()
		fmt.Printf("  Could not connect: %v\n", connErr)
		return unlockResultFailed
	}
	defer conn.Close()
	spinner.Success()

	// Check if lock exists
	if !lock.IsLocked(conn, lockCfg, projectHash) {
		fmt.Printf("%s %s: no lock held\n", ui.SymbolPending, hostName)
		return unlockResultNotLocked
	}

	// Get lock holder info before releasing
	holder := lock.GetLockHolder(conn, lockCfg, projectHash)

	// Release the lock
	err := lock.ForceRelease(conn, lockDir)
	if err != nil {
		fmt.Printf("%s %s: failed to release lock: %v\n", ui.SymbolFail, hostName, err)
		return unlockResultFailed
	}

	if holder != "" && holder != "unknown" {
		fmt.Printf("%s %s: lock released (was held by %s)\n", ui.SymbolSuccess, hostName, holder)
	} else {
		fmt.Printf("%s %s: lock released\n", ui.SymbolSuccess, hostName)
	}
	return unlockResultSuccess
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
		if h == globalCfg.Defaults.Host {
			label += " (default)"
		}
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
