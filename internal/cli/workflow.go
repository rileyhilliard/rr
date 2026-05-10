package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/require"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
	"golang.org/x/term"
)

// WorkflowOptions configures workflow setup behavior.
type WorkflowOptions struct {
	Host             string        // Preferred host name
	Tag              string        // Filter hosts by tag
	ProbeTimeout     time.Duration // Override SSH probe timeout
	SkipSync         bool          // Skip file sync phase
	SkipLock         bool          // Skip lock acquisition
	SkipRequirements bool          // Skip requirement checks
	WorkingDir       string        // Override local working directory
	Quiet            bool          // Minimize output
	Local            bool          // Force local execution (skip remote hosts)
	Command          string        // Command being run (stored in lock for monitoring)
	TaskName         string        // Task name for task-specific requirements
}

// WorkflowContext holds state from workflow setup for use during execution.
type WorkflowContext struct {
	Resolved     *config.ResolvedConfig
	Conn         *host.Connection
	Lock         *lock.Lock
	WorkDir      string
	PhaseDisplay *ui.PhaseDisplay
	Reporter     PhaseReporter
	StartTime    time.Time

	// Internal state
	selector   *host.Selector
	signalChan chan os.Signal
	ctx        context.Context
	cancel     context.CancelFunc
	closeOnce  sync.Once
}

// setupSignalHandler registers interrupt handlers to ensure cleanup on Ctrl+C.
// Instead of calling os.Exit, it cancels the workflow context so in-flight
// commands (like remote SSH sessions) can send SIGINT to the remote process
// before the connection is torn down.
func (w *WorkflowContext) setupSignalHandler() {
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.signalChan = make(chan os.Signal, 2)
	signal.Notify(w.signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		_, ok := <-w.signalChan
		if !ok {
			// Channel was closed by Close(), not a signal
			return
		}
		// Cancel context first so in-flight SSH commands can clean up
		w.cancel()

		// Second signal force-quits immediately (users expect double Ctrl+C to kill)
		go func() {
			_, ok := <-w.signalChan
			if !ok {
				return
			}
			os.Exit(130)
		}()

		w.Close()
	}()
}

// GetReporter returns the workflow's phase reporter, lazily initializing if needed.
func (w *WorkflowContext) GetReporter() PhaseReporter {
	if w.Reporter != nil {
		return w.Reporter
	}
	if w.PhaseDisplay != nil {
		w.Reporter = NewPhaseReporter(w.PhaseDisplay)
		return w.Reporter
	}
	w.Reporter = &StructuredReporter{}
	return w.Reporter
}

// Context returns the workflow's cancellable context. This context is cancelled
// when the user sends SIGINT/SIGTERM, allowing callers to propagate cancellation
// to remote commands.
func (w *WorkflowContext) Context() context.Context {
	if w.ctx == nil {
		return context.Background()
	}
	return w.ctx
}

// Close releases workflow resources. Safe to call multiple times.
func (w *WorkflowContext) Close() {
	w.closeOnce.Do(func() {
		// Cancel context to signal in-flight operations
		if w.cancel != nil {
			w.cancel()
		}
		// Stop listening for signals
		if w.signalChan != nil {
			signal.Stop(w.signalChan)
			close(w.signalChan)
		}
		if w.Lock != nil {
			w.Lock.Release() //nolint:errcheck // Lock release errors are non-fatal
		}
		if w.selector != nil {
			w.selector.Close()
		}
	})
}

// loadAndValidateConfig loads and validates both global and project config.
func loadAndValidateConfig(ctx *WorkflowContext) error {
	resolved, err := config.LoadResolved(Config())
	if err != nil {
		return err
	}

	// Validate the resolved configuration
	if err := config.ValidateResolved(resolved); err != nil {
		return err
	}

	ctx.Resolved = resolved
	return nil
}

// setupWorkDir determines the working directory.
// Uses the project root (where .rr.yaml is located) as the default,
// allowing rr to work correctly from subdirectories.
func setupWorkDir(ctx *WorkflowContext, opts WorkflowOptions) error {
	ctx.WorkDir = opts.WorkingDir
	if ctx.WorkDir == "" {
		// Use project root if available, otherwise fall back to cwd
		if ctx.Resolved != nil && ctx.Resolved.ProjectRoot != "" {
			ctx.WorkDir = ctx.Resolved.ProjectRoot
		} else {
			var err error
			ctx.WorkDir, err = os.Getwd()
			if err != nil {
				return errors.WrapWithCode(err, errors.ErrExec,
					"Can't figure out what directory you're in",
					"This is unusual - check your directory permissions.")
			}
		}
	}
	return nil
}

// setupHostSelector creates and configures the host selector.
// It uses ResolveHosts to determine which hosts this project can use.
func setupHostSelector(ctx *WorkflowContext, opts WorkflowOptions) {
	// Resolve local_fallback from project config (overrides global)
	localFallback := config.ResolveLocalFallback(ctx.Resolved)

	// If --local flag is set, force local execution (empty hosts + local fallback)
	if opts.Local {
		ctx.selector = host.NewSelector(make(map[string]config.Host))
		ctx.selector.SetLocalFallback(true)
		return
	}

	// Get the hosts this project is allowed to use
	// (respects project.Hosts list if specified, otherwise uses all global hosts)
	// Empty hosts with nil error indicates local-only mode
	hostOrder, projectHosts, err := config.ResolveHosts(ctx.Resolved, opts.Host)
	if err != nil {
		// Fall back to all global hosts if resolution fails
		ctx.selector = host.NewSelector(ctx.Resolved.Global.Hosts)
	} else {
		ctx.selector = host.NewSelector(projectHosts)
		ctx.selector.SetHostOrder(hostOrder)
	}
	ctx.selector.SetLocalFallback(localFallback)

	probeTimeout := ctx.Resolved.Global.Defaults.ProbeTimeout
	if opts.ProbeTimeout > 0 {
		probeTimeout = opts.ProbeTimeout
	}
	if probeTimeout > 0 {
		ctx.selector.SetTimeout(probeTimeout)
	}
}

// selectHostInteractively shows a host picker if needed.
func selectHostInteractively(ctx *WorkflowContext, preferredHost string, quiet bool) (string, error) {
	if preferredHost != "" || ctx.selector.HostCount() <= 1 || quiet || !term.IsTerminal(int(os.Stdin.Fd())) {
		return preferredHost, nil
	}

	// Get host info for picker (no default host - list order determines priority)
	hostInfos := ctx.selector.HostInfo()
	uiHosts := make([]ui.HostInfo, len(hostInfos))
	for i, h := range hostInfos {
		uiHosts[i] = ui.HostInfo{
			Name: h.Name,
			SSH:  h.SSH,
			Dir:  h.Dir,
			Tags: h.Tags,
		}
	}

	selected, err := ui.PickHost(uiHosts)
	if err != nil {
		return "", errors.WrapWithCode(err, errors.ErrExec, "Host selection failed", "Try again or use --host flag")
	}
	if selected == nil {
		return "", errors.New(errors.ErrExec, "No host selected", "Use --host flag to specify a host")
	}
	return selected.Name, nil
}

// connectPhase handles the connection phase of the workflow.
func connectPhase(ctx *WorkflowContext, opts WorkflowOptions) error {
	connectStart := time.Now()

	// Resolve preferred host using resolution order
	preferredHost := opts.Host
	if preferredHost == "" {
		hostName, _, err := config.ResolveHost(ctx.Resolved, "")
		if err == nil {
			preferredHost = hostName
		}
	}

	if PrettyMode() {
		return connectPhasePretty(ctx, opts, preferredHost, connectStart)
	}
	return connectPhaseStructured(ctx, opts, preferredHost, connectStart)
}

func connectPhasePretty(ctx *WorkflowContext, opts WorkflowOptions, preferredHost string, _ time.Time) error {
	connDisplay := ui.NewConnectionDisplay(os.Stdout)
	connDisplay.SetQuiet(opts.Quiet)
	connDisplay.Start()

	// Interactive host selection
	var err error
	preferredHost, err = selectHostInteractively(ctx, preferredHost, opts.Quiet)
	if err != nil {
		return err
	}

	var usedLocalFallback bool

	ctx.selector.SetEventHandler(func(event host.ConnectionEvent) {
		switch event.Type {
		case host.EventFailed:
			status := mapProbeErrorToStatus(event.Error)
			connDisplay.AddAttempt(event.Alias, status, event.Latency, event.Message)
		case host.EventConnected:
			connDisplay.AddAttempt(event.Alias, ui.StatusSuccess, event.Latency, "")
		case host.EventLocalFallback:
			usedLocalFallback = true
		}
	})

	if opts.Tag != "" {
		ctx.Conn, err = ctx.selector.SelectByTag(opts.Tag)
	} else {
		ctx.Conn, err = ctx.selector.Select(preferredHost)
	}
	if err != nil {
		connDisplay.Fail(err.Error())
		return err
	}

	if usedLocalFallback {
		connDisplay.SuccessLocal()
	} else {
		connDisplay.Success(ctx.Conn.Name, ctx.Conn.Alias)
	}

	return nil
}

func connectPhaseStructured(ctx *WorkflowContext, opts WorkflowOptions, preferredHost string, connectStart time.Time) error {
	reporter := ctx.GetReporter()
	reporter.PhaseStart("connect")

	var err error
	if opts.Tag != "" {
		ctx.Conn, err = ctx.selector.SelectByTag(opts.Tag)
	} else {
		ctx.Conn, err = ctx.selector.Select(preferredHost)
	}
	if err != nil {
		reporter.PhaseFailed("connect", err)
		return err
	}

	host := ctx.Conn.Name
	if ctx.Conn.IsLocal {
		host = "local"
	}
	reporter.PhaseComplete("connect", host, time.Since(connectStart))
	return nil
}

// syncPhase handles the file sync phase of the workflow.
func syncPhase(ctx *WorkflowContext, opts WorkflowOptions) error {
	reporter := ctx.GetReporter()

	if ctx.Conn.IsLocal {
		reporter.PhaseSkipped("sync", "local")
		return nil
	}
	if opts.SkipSync {
		reporter.PhaseSkipped("sync", "skipped")
		return nil
	}

	syncStart := time.Now()

	if !PrettyMode() {
		return syncStructured(ctx, syncStart)
	}
	if !opts.Quiet {
		return syncWithProgress(ctx, syncStart)
	}
	return syncQuiet(ctx, syncStart)
}

// syncStructured syncs files without UI and emits structured events.
func syncStructured(ctx *WorkflowContext, syncStart time.Time) error {
	reporter := ctx.GetReporter()
	reporter.PhaseStart("sync")

	syncCfg := resolveSyncConfig(ctx)

	if err := rrsync.InvalidateStaleDirectories(ctx.Conn, ctx.WorkDir, syncCfg.Invalidations); err != nil {
		reporter.PhaseFailed("sync", err)
		return err
	}

	err := rrsync.Sync(ctx.Conn, ctx.WorkDir, syncCfg, nil)
	if err != nil {
		reporter.PhaseFailed("sync", err)
		return err
	}

	reporter.PhaseComplete("sync", ctx.Conn.Name, time.Since(syncStart))
	return nil
}

// resolveSyncConfig returns the sync config to use, falling back to defaults.
func resolveSyncConfig(ctx *WorkflowContext) config.SyncConfig {
	if ctx.Resolved.Project != nil {
		return ctx.Resolved.Project.Sync
	}
	return config.DefaultConfig().Sync
}

// syncWithProgress syncs files with progress bar display.
func syncWithProgress(ctx *WorkflowContext, syncStart time.Time) error {
	syncCfg := resolveSyncConfig(ctx)

	// Delete stale remote directories when lockfiles have changed before syncing.
	if err := rrsync.InvalidateStaleDirectories(ctx.Conn, ctx.WorkDir, syncCfg.Invalidations); err != nil {
		return err
	}

	syncProgress := ui.NewInlineProgress("Syncing files", os.Stdout)
	syncProgress.SetUseFakeProgress(false) // Use real rsync progress
	progressWriter := ui.NewProgressWriter(syncProgress, nil)
	syncProgress.Start()

	err := rrsync.Sync(ctx.Conn, ctx.WorkDir, syncCfg, progressWriter)
	if err != nil {
		syncProgress.Fail()
		return err
	}

	syncProgress.Success()
	ctx.PhaseDisplay.RenderSuccess("Files synced", time.Since(syncStart))
	return nil
}

// syncQuiet syncs files with minimal output (spinner only).
func syncQuiet(ctx *WorkflowContext, syncStart time.Time) error {
	syncCfg := resolveSyncConfig(ctx)

	// Delete stale remote directories when lockfiles have changed before syncing.
	if err := rrsync.InvalidateStaleDirectories(ctx.Conn, ctx.WorkDir, syncCfg.Invalidations); err != nil {
		return err
	}

	syncSpinner := ui.NewSpinner("Syncing files")
	syncSpinner.Start()

	err := rrsync.Sync(ctx.Conn, ctx.WorkDir, syncCfg, nil)
	if err != nil {
		syncSpinner.Fail()
		return err
	}

	syncSpinner.Success()
	ctx.PhaseDisplay.RenderSuccess("Files synced", time.Since(syncStart))
	return nil
}

// lockPhase handles the lock acquisition phase of the workflow.
func lockPhase(ctx *WorkflowContext, opts WorkflowOptions) error {
	lockCfg := config.DefaultConfig().Lock
	if ctx.Resolved.Project != nil {
		lockCfg = ctx.Resolved.Project.Lock
	}

	if !lockCfg.Enabled || opts.SkipLock || ctx.Conn.IsLocal {
		return nil
	}

	lockStart := time.Now()

	if PrettyMode() {
		lockSpinner := ui.NewSpinner("Acquiring lock")
		lockSpinner.Start()

		var err error
		ctx.Lock, err = lock.Acquire(ctx.Conn, lockCfg, opts.Command)
		if err != nil {
			lockSpinner.Fail()
			return err
		}

		ctx.Lock.StartHeartbeat()
		lockSpinner.Success()
		ctx.PhaseDisplay.RenderSuccess("Lock acquired", time.Since(lockStart))
		return nil
	}

	reporter := ctx.GetReporter()
	reporter.PhaseStart("lock")

	var err error
	ctx.Lock, err = lock.Acquire(ctx.Conn, lockCfg, opts.Command)
	if err != nil {
		reporter.PhaseFailed("lock", err)
		return err
	}

	ctx.Lock.StartHeartbeat()
	reporter.PhaseComplete("lock", ctx.Conn.Name, time.Since(lockStart))
	return nil
}

// SetupWorkflow performs the common workflow phases: load config, connect, lock, and sync.
// Returns a WorkflowContext that the caller uses for execution, and must Close() when done.
//
// When multiple hosts are configured, this function implements load balancing:
// 1. Try each host with non-blocking lock acquisition
// 2. If a host is locked, immediately try the next host
// 3. If all hosts are locked and local_fallback is true, run locally
// 4. If all hosts are locked and local_fallback is false, round-robin wait
// 5. Once a lock is acquired, sync files to that host
//
// The lock-before-sync order ensures we don't waste time syncing to a host we can't use.
func SetupWorkflow(opts WorkflowOptions) (*WorkflowContext, error) {
	// Validate mutually exclusive flags
	if err := ValidateLocalAndTag(opts.Local, opts.Tag); err != nil {
		return nil, err
	}

	pd := ui.NewPhaseDisplay(os.Stdout)
	ctx := &WorkflowContext{
		StartTime:    time.Now(),
		PhaseDisplay: pd,
		Reporter:     NewPhaseReporter(pd),
	}

	// Set up signal handler early to ensure cleanup on Ctrl+C
	ctx.setupSignalHandler()

	// Load and validate config
	if err := loadAndValidateConfig(ctx); err != nil {
		return nil, err
	}

	// Determine working directory
	if err := setupWorkDir(ctx, opts); err != nil {
		return nil, err
	}

	// Create host selector
	setupHostSelector(ctx, opts)

	// Check if we have multiple hosts (load balancing scenario)
	hostCount := ctx.selector.HostCount()
	useLoadBalancing := hostCount > 1 && opts.Host == "" && opts.Tag == ""

	if useLoadBalancing {
		// Multi-host: use load-balanced workflow (Connect + Lock combined, then Sync)
		if err := setupWorkflowLoadBalanced(ctx, opts); err != nil {
			ctx.Close()
			return nil, err
		}
	} else {
		// Single host or explicit host/tag: use original workflow order
		// Phase 1: Connect
		if err := connectPhase(ctx, opts); err != nil {
			ctx.Close()
			return nil, err
		}

		// Phase 2: Acquire lock (moved before sync for consistency)
		if err := lockPhase(ctx, opts); err != nil {
			ctx.Close()
			return nil, err
		}
	}

	// Phase 3: Check requirements (before sync)
	if err := requirementsPhase(ctx, opts); err != nil {
		ctx.Close()
		return nil, err
	}

	// Phase 4: Sync (same for both paths)
	if err := syncPhase(ctx, opts); err != nil {
		ctx.Close()
		return nil, err
	}

	return ctx, nil
}

// ExecutePullPhase downloads files from remote after command execution.
// Pull happens regardless of command exit code - often you want test artifacts on failure.
// Errors are logged but don't fail the overall workflow.
func ExecutePullPhase(wf *WorkflowContext, pullItems []config.PullItem, dest string) {
	if len(pullItems) == 0 || wf.Conn == nil || wf.Conn.IsLocal {
		return
	}

	pullStart := time.Now()
	pullOpts := rrsync.PullOptions{
		Patterns:    pullItems,
		DefaultDest: dest,
	}

	if !PrettyMode() {
		reporter := wf.GetReporter()
		reporter.PhaseStart("pull")
		pullErr := rrsync.Pull(wf.Conn, pullOpts, nil)
		if pullErr != nil {
			reporter.PhaseFailed("pull", pullErr)
		} else {
			reporter.PhaseComplete("pull", wf.Conn.Name, time.Since(pullStart))
		}
		return
	}

	spinner := ui.NewSpinner("Pulling files")
	spinner.Start()

	pullErr := rrsync.Pull(wf.Conn, pullOpts, nil)
	if pullErr != nil {
		spinner.Fail()
		fmt.Printf("\n%s Pull failed: %s\n", ui.SymbolFail, pullErr.Error())
	} else {
		spinner.Success()
		wf.PhaseDisplay.RenderSuccess("Files pulled", time.Since(pullStart))
	}
}

// requirementsPhase verifies that required tools are available on the remote.
func requirementsPhase(ctx *WorkflowContext, opts WorkflowOptions) error {
	// Skip for local execution or if explicitly disabled
	if ctx.Conn.IsLocal || opts.SkipRequirements {
		return nil
	}

	// Gather requirements from all sources
	var projectReqs, taskReqs []string
	if ctx.Resolved.Project != nil {
		projectReqs = ctx.Resolved.Project.Require
		if opts.TaskName != "" {
			if task, ok := ctx.Resolved.Project.Tasks[opts.TaskName]; ok {
				taskReqs = task.Require
			}
		}
	}
	hostReqs := ctx.Conn.Host.Require

	// Merge requirements (project, host, task) with deduplication
	reqs := require.Merge(projectReqs, hostReqs, taskReqs)
	if len(reqs) == 0 {
		return nil
	}

	// Check requirements with caching
	results, err := require.CheckAll(ctx.Conn.Client, reqs, require.GlobalCache(), ctx.Conn.Name)
	if err != nil {
		return err
	}

	// Filter to missing requirements
	missing := require.FilterMissing(results)
	if len(missing) == 0 {
		if PrettyMode() && !opts.Quiet {
			ctx.PhaseDisplay.RenderSuccess("Requirements verified", 0)
		}
		return nil
	}

	// Report missing requirements with actionable suggestion
	missingStr := require.FormatMissing(missing)
	return errors.New(errors.ErrExec,
		"Missing required tools: "+missingStr,
		"Run 'rr provision' to install missing tools, or use --skip-requirements to bypass.")
}
