package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
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
	lockHolder string         // Who holds the lock (if locked)
	lockInfo   *lock.LockInfo // Structured holder info (nil if unreadable)
}

// lockHolderDetail describes a lock holder in the result envelope.
type lockHolderDetail struct {
	Host        string  `json:"host"`
	User        string  `json:"user,omitempty"`
	Pid         int     `json:"pid,omitempty"`
	Command     string  `json:"command,omitempty"`
	AgeS        float64 `json:"age_s,omitempty"`
	SameMachine bool    `json:"same_machine"`
}

// fallbackDetail explains a local fallback in the result envelope.
type fallbackDetail struct {
	Reason  string             `json:"reason"` // all_hosts_locked
	WaitedS float64            `json:"waited_s,omitempty"`
	Holders []lockHolderDetail `json:"holders,omitempty"`
}

// findAvailableHostResult contains the result of finding an available host.
type findAvailableHostResult struct {
	conn       *host.Connection
	lock       *lock.Lock
	isLocal    bool
	fellBack   bool          // isLocal because all hosts were locked
	hostsState []hostAttempt // State of all hosts tried
}

// allLockedAction is the decision for the "every host is locked" scenario.
type allLockedAction int

const (
	// actionWaitThenError waits for a lock then errors on timeout (fallback disabled for busy hosts).
	actionWaitThenError allLockedAction = iota
	// actionFallbackImmediately goes local right away (always mode, no local holders).
	actionFallbackImmediately
	// actionWaitThenFallback waits first, then goes local on timeout (always
	// mode with a same-machine holder: the holder is likely the user's own
	// run, so waiting briefly beats silently running locally).
	actionWaitThenFallback
)

// resolveAllLockedAction decides what to do when every host is locked.
func resolveAllLockedAction(mode config.LocalFallbackMode, holders []lockHolderDetail) allLockedAction {
	if mode != config.LocalFallbackAlways {
		return actionWaitThenError
	}
	for _, h := range holders {
		if h.SameMachine {
			return actionWaitThenFallback
		}
	}
	return actionFallbackImmediately
}

// holderDetails converts locked-host attempts into envelope-ready holder info.
func holderDetails(lockedHosts []hostAttempt) []lockHolderDetail {
	details := make([]lockHolderDetail, 0, len(lockedHosts))
	for _, a := range lockedHosts {
		d := lockHolderDetail{Host: a.hostName}
		if a.lockInfo != nil {
			d.User = a.lockInfo.User
			d.Pid = a.lockInfo.PID
			d.Command = a.lockInfo.Command
			d.AgeS = a.lockInfo.Age().Round(time.Second).Seconds()
			d.SameMachine = a.lockInfo.SameMachine()
		}
		details = append(details, d)
	}
	return details
}

// describeHolders renders holder details for human-facing messages.
func describeHolders(holders []lockHolderDetail) string {
	parts := make([]string, 0, len(holders))
	for _, h := range holders {
		desc := h.Host
		if h.Command != "" {
			desc += fmt.Sprintf(": '%s'", h.Command)
		}
		if h.Pid != 0 {
			desc += fmt.Sprintf(" (pid %d", h.Pid)
			if h.SameMachine {
				desc += ", this machine"
			}
			desc += ")"
		}
		parts = append(parts, desc)
	}
	return strings.Join(parts, "; ")
}

// emitFallbackWarning makes a local fallback unmissable in both output modes.
func emitFallbackWarning(holders []lockHolderDetail, waited time.Duration) {
	if PrettyMode() {
		msg := "Falling back to LOCAL execution - all remote hosts are locked"
		if len(holders) > 0 {
			msg += " (" + describeHolders(holders) + ")"
		}
		ui.PrintWarning(msg)
		return
	}

	details := map[string]interface{}{
		"local_fallback": true,
		"reason":         "all_hosts_locked",
		"holders":        holders,
	}
	if waited > 0 {
		details["waited_s"] = waited.Seconds()
	}
	WritePhaseEvent(PhaseEvent{
		Type:    "phase",
		Phase:   "connect",
		Status:  "warn",
		Host:    "local",
		Details: details,
	})
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
	// Host order is determined by project config (hosts list order) or alphabetical for global hosts
	hostNames := ctx.selector.GetHostNames()

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
		lck, err := lock.TryAcquire(conn, lockCfg, opts.Command)
		if err == nil {
			// Got the lock
			lck.StartHeartbeat()
			return &findAvailableHostResult{
				conn:       conn,
				lock:       lck,
				hostsState: append(attempts, attempt),
			}, nil
		}

		if errors.Is(err, lock.ErrLocked) {
			// Host is locked, record who holds it and try next
			attempt.lockInfo = lock.GetLockInfo(conn, lockCfg)
			if attempt.lockInfo != nil {
				attempt.lockHolder = attempt.lockInfo.Describe()
			} else {
				attempt.lockHolder = lock.GetLockHolder(conn, lockCfg)
			}
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
		mode := config.ResolveLocalFallbackMode(ctx.Resolved)
		holders := holderDetails(lockedHosts)

		switch resolveAllLockedAction(mode, holders) {
		case actionFallbackImmediately:
			for _, a := range lockedHosts {
				if a.conn != nil {
					a.conn.Close()
				}
			}
			emitFallbackWarning(holders, 0)
			ctx.AddResultDetail("fallback", fallbackDetail{
				Reason:  "all_hosts_locked",
				Holders: holders,
			})
			return localFallbackResult(attempts), nil

		case actionWaitThenFallback:
			waitStart := time.Now()
			result, err := roundRobinWait(ctx, lockedHosts, lockCfg, opts.Command, attempts, holders)
			if err == nil {
				return result, nil
			}
			// Wait exhausted (or connections lost): fall back, loudly
			waited := time.Since(waitStart)
			emitFallbackWarning(holders, waited)
			ctx.AddResultDetail("fallback", fallbackDetail{
				Reason:  "all_hosts_locked",
				WaitedS: waited.Round(time.Second).Seconds(),
				Holders: holders,
			})
			return localFallbackResult(attempts), nil

		default:
			// Fallback disabled for busy hosts: wait, then error
			return roundRobinWait(ctx, lockedHosts, lockCfg, opts.Command, attempts, holders)
		}
	}

	// No hosts could be connected to at all
	return nil, buildConnectionError(attempts)
}

// localFallbackResult builds the local-execution result used when all hosts
// are locked and fallback is allowed.
func localFallbackResult(attempts []hostAttempt) *findAvailableHostResult {
	return &findAvailableHostResult{
		conn: &host.Connection{
			Name:    "local",
			Alias:   "local",
			IsLocal: true,
		},
		lock:       nil,
		isLocal:    true,
		fellBack:   true,
		hostsState: attempts,
	}
}

// roundRobinWait cycles through locked hosts until one becomes available or timeout.
func roundRobinWait(_ *WorkflowContext, lockedHosts []hostAttempt, lockCfg config.LockConfig, command string, allAttempts []hostAttempt, holders []lockHolderDetail) (*findAvailableHostResult, error) {
	waitTimeout := lockCfg.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = 1 * time.Minute // Default
	}

	waitMsg := waitMessage(holders, waitTimeout)
	startTime := time.Now()
	var spinner *ui.Spinner
	if PrettyMode() {
		spinner = ui.NewSpinner(waitMsg)
		spinner.Start()
	} else {
		WritePhaseEvent(PhaseEvent{
			Type:   "phase",
			Phase:  "connect",
			Status: "waiting",
			Details: map[string]interface{}{
				"message":        waitMsg,
				"holders":        holders,
				"wait_timeout_s": waitTimeout.Seconds(),
			},
		})
	}

	for {
		elapsed := time.Since(startTime)
		if elapsed >= waitTimeout {
			if spinner != nil {
				spinner.Fail()
			}
			for _, a := range lockedHosts {
				if a.conn != nil {
					a.conn.Close()
				}
			}
			return nil, buildAllHostsLockedError(lockedHosts, waitTimeout)
		}

		for i, attempt := range lockedHosts {
			if attempt.conn == nil {
				continue
			}

			lck, err := lock.TryAcquire(attempt.conn, lockCfg, command)
			if err == nil {
				lck.StartHeartbeat()
				if spinner != nil {
					spinner.Success()
				}
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
				attempt.conn.Close()
				lockedHosts[i].conn = nil
			}
		}

		aliveCount := 0
		for _, a := range lockedHosts {
			if a.conn != nil {
				aliveCount++
			}
		}
		if aliveCount == 0 {
			if spinner != nil {
				spinner.Fail()
			}
			return nil, rrerrors.New(rrerrors.ErrSSH,
				"All host connections lost while waiting",
				"Check network connectivity and try again.")
		}

		time.Sleep(2 * time.Second)
	}
}

// waitMessage says exactly what the round-robin wait is waiting on.
func waitMessage(holders []lockHolderDetail, timeout time.Duration) string {
	for _, h := range holders {
		if h.SameMachine {
			desc := fmt.Sprintf("pid %d", h.Pid)
			if h.Command != "" {
				desc = fmt.Sprintf("pid %d: %s", h.Pid, h.Command)
			}
			return fmt.Sprintf("All hosts locked by your own runs (%s); waiting up to %s", desc, timeout)
		}
	}
	return fmt.Sprintf("All hosts locked; waiting up to %s", timeout)
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
		fmt.Sprintf("Locked hosts: %v. Wait for them to finish, or run 'rr unlock --all' if they're stuck.", holders))
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
	if !PrettyMode() {
		return setupWorkflowLoadBalancedStructured(ctx, opts)
	}

	connDisplay := ui.NewConnectionDisplay(os.Stdout)
	connDisplay.SetQuiet(opts.Quiet)
	connDisplay.Start()

	ctx.selector.SetEventHandler(func(event host.ConnectionEvent) {
		switch event.Type {
		case host.EventFailed:
			status := mapProbeErrorToStatus(event.Error)
			connDisplay.AddAttempt(event.Alias, status, event.Latency, event.Message)
		case host.EventConnected:
			connDisplay.AddAttempt(event.Alias, ui.StatusSuccess, event.Latency, "")
		}
	})

	result, err := findAvailableHost(ctx, opts)
	if err != nil {
		connDisplay.Fail(err.Error())
		return err
	}

	ctx.Conn = result.conn
	ctx.Lock = result.lock

	if result.isLocal {
		connDisplay.SuccessLocal()
	} else {
		connDisplay.Success(ctx.Conn.Name, ctx.Conn.Alias)
	}

	return nil
}

func setupWorkflowLoadBalancedStructured(ctx *WorkflowContext, opts WorkflowOptions) error {
	connectStart := time.Now()
	reporter := ctx.GetReporter()
	reporter.PhaseStart("connect")

	result, err := findAvailableHost(ctx, opts)
	if err != nil {
		reporter.PhaseFailed("connect", err)
		return err
	}

	ctx.Conn = result.conn
	ctx.Lock = result.lock

	host := ctx.Conn.Name
	if result.isLocal {
		host = "local"
	}
	reporter.PhaseComplete("connect", host, time.Since(connectStart))

	return nil
}
