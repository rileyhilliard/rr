package cli

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
	"golang.org/x/term"
)

// WorkflowOptions configures workflow setup behavior.
type WorkflowOptions struct {
	Host         string        // Preferred host name
	Tag          string        // Filter hosts by tag
	ProbeTimeout time.Duration // Override SSH probe timeout
	SkipSync     bool          // Skip file sync phase
	SkipLock     bool          // Skip lock acquisition
	WorkingDir   string        // Override local working directory
	Quiet        bool          // Minimize output
}

// WorkflowContext holds state from workflow setup for use during execution.
type WorkflowContext struct {
	Resolved     *config.ResolvedConfig
	Conn         *host.Connection
	Lock         *lock.Lock
	WorkDir      string
	PhaseDisplay *ui.PhaseDisplay
	StartTime    time.Time

	// Internal state
	selector   *host.Selector
	signalChan chan os.Signal
	closeOnce  sync.Once
}

// setupSignalHandler registers interrupt handlers to ensure cleanup on Ctrl+C.
func (w *WorkflowContext) setupSignalHandler() {
	w.signalChan = make(chan os.Signal, 1)
	signal.Notify(w.signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig, ok := <-w.signalChan
		if !ok {
			// Channel was closed by Close(), not a signal
			return
		}
		// Received actual signal, clean up and exit
		_ = sig
		w.Close()
		os.Exit(130) // 128 + SIGINT(2) = 130, standard exit code for Ctrl+C
	}()
}

// Close releases workflow resources. Safe to call multiple times.
func (w *WorkflowContext) Close() {
	w.closeOnce.Do(func() {
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
func setupWorkDir(ctx *WorkflowContext, opts WorkflowOptions) error {
	ctx.WorkDir = opts.WorkingDir
	if ctx.WorkDir == "" {
		var err error
		ctx.WorkDir, err = os.Getwd()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrExec,
				"Can't figure out what directory you're in",
				"This is unusual - check your directory permissions.")
		}
	}
	return nil
}

// setupHostSelector creates and configures the host selector.
func setupHostSelector(ctx *WorkflowContext, opts WorkflowOptions) {
	ctx.selector = host.NewSelector(ctx.Resolved.Global.Hosts)
	ctx.selector.SetLocalFallback(ctx.Resolved.Global.Defaults.LocalFallback)

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

	// Get default host from resolution order
	defaultHost := ""
	if ctx.Resolved.Project != nil && ctx.Resolved.Project.Host != "" {
		defaultHost = ctx.Resolved.Project.Host
	} else if ctx.Resolved.Global.Defaults.Host != "" {
		defaultHost = ctx.Resolved.Global.Defaults.Host
	}

	hostInfos := ctx.selector.HostInfo(defaultHost)
	uiHosts := make([]ui.HostInfo, len(hostInfos))
	for i, h := range hostInfos {
		uiHosts[i] = ui.HostInfo{
			Name:    h.Name,
			SSH:     h.SSH,
			Dir:     h.Dir,
			Tags:    h.Tags,
			Default: h.Default,
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
	connDisplay := ui.NewConnectionDisplay(os.Stdout)
	connDisplay.SetQuiet(opts.Quiet)
	connDisplay.Start()

	// Resolve preferred host using resolution order
	preferredHost := opts.Host
	if preferredHost == "" {
		// Use config.ResolveHost to get the host following resolution order
		hostName, _, err := config.ResolveHost(ctx.Resolved, "")
		if err == nil {
			preferredHost = hostName
		}
	}

	// Interactive host selection
	var err error
	preferredHost, err = selectHostInteractively(ctx, preferredHost, opts.Quiet)
	if err != nil {
		return err
	}

	// Track connection status for output
	var usedLocalFallback bool

	// Set up event handler for connection progress
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

	// Connect - either by tag or by host/default
	if opts.Tag != "" {
		ctx.Conn, err = ctx.selector.SelectByTag(opts.Tag)
	} else {
		ctx.Conn, err = ctx.selector.Select(preferredHost)
	}
	if err != nil {
		connDisplay.Fail(err.Error())
		return err
	}

	// Show connection result
	if usedLocalFallback {
		connDisplay.SuccessLocal()
	} else {
		connDisplay.Success(ctx.Conn.Name, ctx.Conn.Alias)
	}

	return nil
}

// syncPhase handles the file sync phase of the workflow.
func syncPhase(ctx *WorkflowContext, opts WorkflowOptions) error {
	if ctx.Conn.IsLocal {
		ctx.PhaseDisplay.RenderSkipped("Sync", "local")
		return nil
	}
	if opts.SkipSync {
		ctx.PhaseDisplay.RenderSkipped("Sync", "skipped")
		return nil
	}

	syncStart := time.Now()

	if !opts.Quiet {
		return syncWithProgress(ctx, syncStart)
	}
	return syncQuiet(ctx, syncStart)
}

// syncWithProgress syncs files with progress bar display.
func syncWithProgress(ctx *WorkflowContext, syncStart time.Time) error {
	syncProgress := ui.NewInlineProgress("Syncing files", os.Stdout)
	progressWriter := ui.NewProgressWriter(syncProgress, nil)
	syncProgress.Start()

	// Use project sync config if available, otherwise use defaults
	syncCfg := config.DefaultConfig().Sync
	if ctx.Resolved.Project != nil {
		syncCfg = ctx.Resolved.Project.Sync
	}

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
	syncSpinner := ui.NewSpinner("Syncing files")
	syncSpinner.Start()

	// Use project sync config if available, otherwise use defaults
	syncCfg := config.DefaultConfig().Sync
	if ctx.Resolved.Project != nil {
		syncCfg = ctx.Resolved.Project.Sync
	}

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
	// Use project lock config if available, otherwise use defaults
	lockCfg := config.DefaultConfig().Lock
	if ctx.Resolved.Project != nil {
		lockCfg = ctx.Resolved.Project.Lock
	}

	if !lockCfg.Enabled || opts.SkipLock || ctx.Conn.IsLocal {
		return nil
	}

	lockStart := time.Now()
	lockSpinner := ui.NewSpinner("Acquiring lock")
	lockSpinner.Start()

	projectHash := hashProject(ctx.WorkDir)
	var err error
	ctx.Lock, err = lock.Acquire(ctx.Conn, lockCfg, projectHash)
	if err != nil {
		lockSpinner.Fail()
		return err
	}

	lockSpinner.Success()
	ctx.PhaseDisplay.RenderSuccess("Lock acquired", time.Since(lockStart))
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
	ctx := &WorkflowContext{
		StartTime:    time.Now(),
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
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

	// Phase 3: Sync (same for both paths)
	if err := syncPhase(ctx, opts); err != nil {
		ctx.Close()
		return nil, err
	}

	return ctx, nil
}
