package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	rrerrors "github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/ui"
)

// hostAttempt tracks the result of trying to connect and lock a host.
type hostAttempt struct {
	hostName   string
	conn       *host.Connection
	connErr    error
	lockHolder string // Who holds the lock (if locked)
}

// findAvailableHostResult contains the result of finding an available host.
type findAvailableHostResult struct {
	conn       *host.Connection
	lock       *lock.Lock
	isLocal    bool
	hostsState []hostAttempt // State of all hosts tried
}

// findAvailableHost tries to find a host that is both connectable and not locked.
// It iterates through hosts in alphabetical order, trying to connect and acquire
// a non-blocking lock on each.
//
// The function implements load balancing by:
// 1. Trying each host sequentially with non-blocking lock acquisition
// 2. If a host is locked, immediately trying the next host
// 3. If all hosts are locked and local_fallback is true, returning local
// 4. If all hosts are locked and local_fallback is false, round-robin waiting
//
// Returns:
//   - result with conn, lock, and state information on success
//   - error if no host is available (after timeout if waiting)
func findAvailableHost(ctx *WorkflowContext, opts WorkflowOptions) (*findAvailableHostResult, error) {
	projectHash := hashProject(ctx.WorkDir)
	hostNames := ctx.selector.GetHostNames()

	// Get default host from resolution order
	defaultHost := ""
	if ctx.Resolved.Project != nil && ctx.Resolved.Project.Host != "" {
		defaultHost = ctx.Resolved.Project.Host
	} else if ctx.Resolved.Global.Defaults.Host != "" {
		defaultHost = ctx.Resolved.Global.Defaults.Host
	}

	// Put default host first if configured
	if defaultHost != "" {
		hostNames = reorderWithDefault(hostNames, defaultHost)
	}

	if len(hostNames) == 0 {
		return nil, rrerrors.New(rrerrors.ErrConfig,
			"No hosts configured",
			"Add at least one host with 'rr host add'.")
	}

	// Get lock config from project or use defaults
	lockCfg := config.DefaultConfig().Lock
	if ctx.Resolved.Project != nil {
		lockCfg = ctx.Resolved.Project.Lock
	}

	// Track state for each host
	attempts := make([]hostAttempt, 0, len(hostNames))
	var lockedHosts []hostAttempt

	// Phase 1: Try each host with non-blocking lock
	for _, hostName := range hostNames {
		attempt := hostAttempt{hostName: hostName}

		// Try to connect
		conn, err := ctx.selector.SelectHost(hostName)
		if err != nil {
			attempt.connErr = err
			attempts = append(attempts, attempt)
			continue
		}
		attempt.conn = conn

		// Skip lock for local connections
		if conn.IsLocal {
			return &findAvailableHostResult{
				conn:       conn,
				lock:       nil,
				isLocal:    true,
				hostsState: append(attempts, attempt),
			}, nil
		}

		// Skip lock if disabled
		if !lockCfg.Enabled || opts.SkipLock {
			return &findAvailableHostResult{
				conn:       conn,
				lock:       nil,
				hostsState: append(attempts, attempt),
			}, nil
		}

		// Try non-blocking lock acquisition
		lck, err := lock.TryAcquire(conn, lockCfg, projectHash)
		if err == nil {
			// Got the lock
			return &findAvailableHostResult{
				conn:       conn,
				lock:       lck,
				hostsState: append(attempts, attempt),
			}, nil
		}

		if errors.Is(err, lock.ErrLocked) {
			// Host is locked, record who holds it and try next
			attempt.lockHolder = lock.GetLockHolder(conn, lockCfg, projectHash)
			lockedHosts = append(lockedHosts, attempt)
			attempts = append(attempts, attempt)
			// Keep connection open for potential round-robin
			continue
		}

		// Other error (SSH issues, permissions, etc.)
		conn.Close()
		attempt.connErr = err
		attempts = append(attempts, attempt)
	}

	// Phase 2: All hosts tried - handle "all locked" scenario
	if len(lockedHosts) > 0 {
		// If local_fallback is enabled, go local immediately
		if ctx.Resolved.Global.Defaults.LocalFallback {
			// Close all locked host connections
			for _, a := range lockedHosts {
				if a.conn != nil {
					a.conn.Close()
				}
			}
			return &findAvailableHostResult{
				conn: &host.Connection{
					Name:    "local",
					Alias:   "local",
					IsLocal: true,
				},
				lock:       nil,
				isLocal:    true,
				hostsState: attempts,
			}, nil
		}

		// Otherwise, round-robin wait for a host to become available
		return roundRobinWait(ctx, lockedHosts, lockCfg, projectHash, attempts)
	}

	// No hosts could be connected to at all
	return nil, buildConnectionError(attempts)
}

// roundRobinWait cycles through locked hosts until one becomes available or timeout.
func roundRobinWait(ctx *WorkflowContext, lockedHosts []hostAttempt, lockCfg config.LockConfig, projectHash string, allAttempts []hostAttempt) (*findAvailableHostResult, error) {
	waitTimeout := lockCfg.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = 1 * time.Minute // Default
	}

	startTime := time.Now()
	spinner := ui.NewSpinner("Waiting for available host")
	spinner.Start()

	// Track which hosts we're cycling through
	for {
		elapsed := time.Since(startTime)
		if elapsed >= waitTimeout {
			spinner.Fail()
			// Close all connections
			for _, a := range lockedHosts {
				if a.conn != nil {
					a.conn.Close()
				}
			}
			return nil, buildAllHostsLockedError(lockedHosts, waitTimeout)
		}

		// Try each locked host
		for i, attempt := range lockedHosts {
			if attempt.conn == nil {
				continue
			}

			// Try to acquire lock
			lck, err := lock.TryAcquire(attempt.conn, lockCfg, projectHash)
			if err == nil {
				spinner.Success()
				// Close other connections
				for j, a := range lockedHosts {
					if j != i && a.conn != nil {
						a.conn.Close()
					}
				}
				return &findAvailableHostResult{
					conn:       attempt.conn,
					lock:       lck,
					hostsState: allAttempts,
				}, nil
			}

			if !errors.Is(err, lock.ErrLocked) {
				// Connection died or other error - remove from rotation
				attempt.conn.Close()
				lockedHosts[i].conn = nil
			}
		}

		// Check if all connections are dead
		aliveCount := 0
		for _, a := range lockedHosts {
			if a.conn != nil {
				aliveCount++
			}
		}
		if aliveCount == 0 {
			spinner.Fail()
			return nil, rrerrors.New(rrerrors.ErrSSH,
				"All host connections lost while waiting",
				"Check network connectivity and try again.")
		}

		// Wait before next round
		time.Sleep(2 * time.Second)
	}
}

// buildConnectionError builds an error message for when no hosts could connect.
func buildConnectionError(attempts []hostAttempt) error {
	if len(attempts) == 0 {
		return rrerrors.New(rrerrors.ErrSSH,
			"No hosts configured",
			"Add at least one host to your .rr.yaml file.")
	}

	// Build list of hosts and their errors
	var failedHosts []string
	for _, a := range attempts {
		if a.connErr != nil {
			failedHosts = append(failedHosts, a.hostName)
		}
	}

	if len(failedHosts) == 1 {
		return rrerrors.New(rrerrors.ErrSSH,
			fmt.Sprintf("Couldn't connect to host '%s'", failedHosts[0]),
			"Check if the host is reachable and your SSH configuration.")
	}

	return rrerrors.New(rrerrors.ErrSSH,
		fmt.Sprintf("Couldn't connect to any host (tried: %v)", failedHosts),
		"Check if your hosts are reachable and your SSH configuration.")
}

// buildAllHostsLockedError builds an error message for when all hosts are locked.
func buildAllHostsLockedError(lockedHosts []hostAttempt, timeout time.Duration) error {
	holders := make([]string, 0, len(lockedHosts))
	for _, a := range lockedHosts {
		holder := a.lockHolder
		if holder == "" {
			holder = "unknown"
		}
		holders = append(holders, fmt.Sprintf("%s (held by %s)", a.hostName, holder))
	}

	return rrerrors.New(rrerrors.ErrLock,
		fmt.Sprintf("All hosts are locked - timed out after %s", timeout),
		fmt.Sprintf("Locked hosts: %v. Wait for them to finish or use --force-unlock if stale.", holders))
}

// setupWorkflowLoadBalanced performs workflow setup with load balancing.
// This is an alternative to the original SetupWorkflow that supports
// distributing work across multiple hosts.
//
// The key difference is the phase order:
// 1. Connect + Lock (combined, iterating through hosts)
// 2. Sync (only to the host we got)
//
// This avoids syncing to a host we can't lock.
func setupWorkflowLoadBalanced(ctx *WorkflowContext, opts WorkflowOptions) error {
	// Show connection display
	connDisplay := ui.NewConnectionDisplay(os.Stdout)
	connDisplay.SetQuiet(opts.Quiet)
	connDisplay.Start()

	// Set up event handler for connection progress
	ctx.selector.SetEventHandler(func(event host.ConnectionEvent) {
		switch event.Type {
		case host.EventFailed:
			status := mapProbeErrorToStatus(event.Error)
			connDisplay.AddAttempt(event.Alias, status, event.Latency, event.Message)
		case host.EventConnected:
			connDisplay.AddAttempt(event.Alias, ui.StatusSuccess, event.Latency, "")
		}
	})

	// Find an available host (connect + lock)
	result, err := findAvailableHost(ctx, opts)
	if err != nil {
		connDisplay.Fail(err.Error())
		return err
	}

	ctx.Conn = result.conn
	ctx.Lock = result.lock

	// Show connection result
	if result.isLocal {
		connDisplay.SuccessLocal()
	} else {
		connDisplay.Success(ctx.Conn.Name, ctx.Conn.Alias)
	}

	return nil
}

// reorderWithDefault moves the default host to the front of the list.
// If defaultHost is not in the list, returns the original slice unchanged.
func reorderWithDefault(hostNames []string, defaultHost string) []string {
	if defaultHost == "" {
		return hostNames
	}

	// Find the default host in the list
	defaultIdx := -1
	for i, name := range hostNames {
		if name == defaultHost {
			defaultIdx = i
			break
		}
	}

	// Not found, return unchanged
	if defaultIdx == -1 {
		return hostNames
	}

	// Already first, nothing to do
	if defaultIdx == 0 {
		return hostNames
	}

	// Move default to front
	result := make([]string, len(hostNames))
	result[0] = defaultHost
	copy(result[1:], hostNames[:defaultIdx])
	copy(result[1+defaultIdx:], hostNames[defaultIdx+1:])
	return result
}
