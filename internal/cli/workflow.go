package cli

import (
	"os"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
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
	Config       *config.Config
	Conn         *host.Connection
	Lock         *lock.Lock
	WorkDir      string
	PhaseDisplay *ui.PhaseDisplay
	StartTime    time.Time

	// Internal state
	selector *host.Selector
}

// Close releases workflow resources.
func (w *WorkflowContext) Close() {
	if w.Lock != nil {
		w.Lock.Release() //nolint:errcheck // Lock release errors are non-fatal
	}
	if w.selector != nil {
		w.selector.Close()
	}
}

// SetupWorkflow performs the common workflow phases: load config, connect, sync, and lock.
// Returns a WorkflowContext that the caller uses for execution, and must Close() when done.
func SetupWorkflow(opts WorkflowOptions) (*WorkflowContext, error) {
	ctx := &WorkflowContext{
		StartTime:    time.Now(),
		PhaseDisplay: ui.NewPhaseDisplay(os.Stdout),
	}

	// Load config
	cfgPath, err := config.Find(Config())
	if err != nil {
		return nil, err
	}
	if cfgPath == "" {
		return nil, errors.New(errors.ErrConfig,
			"No config file found",
			"Run 'rr init' to create a .rr.yaml config file")
	}

	ctx.Config, err = config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	if err := config.Validate(ctx.Config); err != nil {
		return nil, err
	}

	// Determine working directory
	ctx.WorkDir = opts.WorkingDir
	if ctx.WorkDir == "" {
		ctx.WorkDir, err = os.Getwd()
		if err != nil {
			return nil, errors.WrapWithCode(err, errors.ErrExec,
				"Failed to get working directory",
				"Check directory permissions")
		}
	}

	// Create host selector
	ctx.selector = host.NewSelector(ctx.Config.Hosts)

	// Enable local fallback if configured
	ctx.selector.SetLocalFallback(ctx.Config.LocalFallback)

	// Set probe timeout (CLI flag overrides config)
	probeTimeout := ctx.Config.ProbeTimeout
	if opts.ProbeTimeout > 0 {
		probeTimeout = opts.ProbeTimeout
	}
	if probeTimeout > 0 {
		ctx.selector.SetTimeout(probeTimeout)
	}

	// Phase 1: Connect
	connDisplay := ui.NewConnectionDisplay(os.Stdout)
	connDisplay.SetQuiet(opts.Quiet)
	connDisplay.Start()

	preferredHost := opts.Host
	if preferredHost == "" {
		preferredHost = ctx.Config.Default
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
		ctx.Close()
		return nil, err
	}

	// Show connection result
	if usedLocalFallback {
		connDisplay.SuccessLocal()
	} else {
		connDisplay.Success(ctx.Conn.Name, ctx.Conn.Alias)
	}

	// Phase 2: Sync (unless skipped or local)
	if ctx.Conn.IsLocal {
		ctx.PhaseDisplay.RenderSkipped("Sync", "local")
	} else if !opts.SkipSync {
		syncStart := time.Now()
		syncSpinner := ui.NewSpinner("Syncing files")
		syncSpinner.Start()

		err = sync.Sync(ctx.Conn, ctx.WorkDir, ctx.Config.Sync, nil)
		if err != nil {
			syncSpinner.Fail()
			ctx.Close()
			return nil, err
		}

		syncSpinner.Success()
		ctx.PhaseDisplay.RenderSuccess("Files synced", time.Since(syncStart))
	} else {
		ctx.PhaseDisplay.RenderSkipped("Sync", "skipped")
	}

	// Phase 3: Acquire lock (unless disabled or local)
	if ctx.Config.Lock.Enabled && !opts.SkipLock && !ctx.Conn.IsLocal {
		lockStart := time.Now()
		lockSpinner := ui.NewSpinner("Acquiring lock")
		lockSpinner.Start()

		projectHash := hashProject(ctx.WorkDir)
		ctx.Lock, err = lock.Acquire(ctx.Conn, ctx.Config.Lock, projectHash)
		if err != nil {
			lockSpinner.Fail()
			ctx.Close()
			return nil, err
		}

		lockSpinner.Success()
		ctx.PhaseDisplay.RenderSuccess("Lock acquired", time.Since(lockStart))
	}

	return ctx, nil
}
