package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/require"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/pkg/sshutil"
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

	// SubdirOffset is the caller's directory relative to the project root
	// ("backend", "backend/api"), empty when invoked from the root itself.
	// WorkDir is the project root regardless - it's the right sync root and
	// remote dir - but relative paths in a command were written against the
	// caller's cwd, so ad-hoc run/exec cd into this offset first.
	SubdirOffset string

	// ResultDetails accumulates extra keys (fallback, log_file, summary,
	// hint, path_rewrites, ...) merged into the final result envelope's
	// details map. Use AddResultDetail; nil until first write.
	ResultDetails map[string]interface{}

	// Internal state
	target     execTarget // local or remote, decided once in loadAndValidateConfig
	selector   *host.Selector
	signalChan chan os.Signal
	ctx        context.Context
	cancel     context.CancelFunc
	closeOnce  sync.Once
}

// AddResultDetail records an extra key for the final result envelope.
func (w *WorkflowContext) AddResultDetail(key string, value interface{}) {
	if w.ResultDetails == nil {
		w.ResultDetails = make(map[string]interface{})
	}
	w.ResultDetails[key] = value
}

// lostConnectionAsResult decides whether an exec error still ends in a result.
// A dropped connection cut off a command that ran, so the run reports a
// failed result with the error under details.error, keeping log_file and the
// partial output; it returns nil for that case. Any other error is returned
// as is, for the caller to fail with.
func (w *WorkflowContext) lostConnectionAsResult(err error) error {
	if !sshutil.IsConnectionLost(err) {
		return err
	}
	w.AddResultDetail("error", ErrorToJSON(err))
	if PrettyMode() {
		fmt.Fprintf(os.Stderr, "\n%s\n", err.Error())
	}
	return nil
}

// setupSignalHandler registers interrupt handlers to ensure cleanup on Ctrl+C.
// Instead of calling os.Exit, it cancels the workflow context so in-flight
// commands (like remote SSH sessions) can send SIGINT to the remote process
// before the connection is torn down. The lock stays held until the caller's
// Close, after the command has stopped, so no other run starts on the host
// while it's still shutting down.
func (w *WorkflowContext) setupSignalHandler() {
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.signalChan = make(chan os.Signal, 2)
	signal.Notify(w.signalChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		if _, ok := <-w.signalChan; !ok {
			// Channel was closed by Close(), not a signal
			return
		}
		// Cancel context first so in-flight SSH commands can clean up
		w.cancel()

		// Second signal force-quits immediately (users expect double Ctrl+C
		// to kill). Local commands are killed first: exiting doesn't stop
		// them, since each runs in its own session.
		if _, ok := <-w.signalChan; !ok {
			return
		}
		host.KillLocalCommands()
		os.Exit(130)
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

// loadAndValidateConfig loads and validates both global and project config
// and decides the execution target (see execTarget).
func loadAndValidateConfig(ctx *WorkflowContext, opts WorkflowOptions) error {
	resolved, target, err := loadRunConfig(opts.Local, opts.Host, opts.Tag)
	if err != nil {
		return err
	}

	ctx.Resolved = resolved
	ctx.target = target
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
			ctx.SubdirOffset = subdirOffset(ctx.WorkDir)
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

// subdirOffset returns the current directory relative to projectRoot, or "" when
// the caller is at the root, the offset can't be determined, or it escapes the
// root. Both paths are symlink-resolved first: on macOS os.Getwd() reports
// /private/var while a discovered project root may be /var, and a naive
// filepath.Rel across that boundary yields a bogus "../../.." traversal.
func subdirOffset(projectRoot string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Clean(r)
		}
		return filepath.Clean(p)
	}

	rel, err := filepath.Rel(resolve(projectRoot), resolve(cwd))
	if err != nil || rel == "." || rel == "" {
		return ""
	}
	// A ".." component means cwd isn't under the project root at all.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// setupHostSelector creates and configures the host selector for a remote
// target. It uses ResolveHosts to determine which hosts this project can use.
// A local target never gets here (see connectLocalTarget).
func setupHostSelector(ctx *WorkflowContext, opts WorkflowOptions) {
	// Resolve local_fallback from project config (overrides global)
	localFallback := config.ResolveLocalFallback(ctx.Resolved)

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
	ctx.selector.SetLocalHost(config.LocalHost(ctx.Resolved))

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
			Dir:  config.ExpandRemote(h.Dir),
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

	ctx.selector.SetEventHandler(func(event host.ConnectionEvent) {
		switch event.Type {
		case host.EventFailed:
			status := mapProbeErrorToStatus(event.Error)
			connDisplay.AddAttempt(event.Alias, status, event.Latency, event.Message)
		case host.EventConnected:
			connDisplay.AddAttempt(event.Alias, ui.StatusSuccess, event.Latency, "")
		case host.EventLocalFallback:
			ctx.AddResultDetail("fallback", fallbackDetail{Reason: event.Reason})
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

	if ctx.Conn.LocalReason != "" {
		connDisplay.SuccessLocal(localRunDetail(ctx.Conn))
	} else {
		connDisplay.Success(ctx.Conn.Name, ctx.Conn.Alias)
	}

	return nil
}

func connectPhaseStructured(ctx *WorkflowContext, opts WorkflowOptions, preferredHost string, connectStart time.Time) error {
	reporter := ctx.GetReporter()
	reporter.PhaseStart("connect")

	// Local fallback (hosts unreachable) must be visible in structured
	// output too, not just in the pretty connection display.
	ctx.selector.SetEventHandler(func(event host.ConnectionEvent) {
		if event.Type == host.EventLocalFallback {
			WritePhaseEvent(PhaseEvent{
				Type:   "phase",
				Phase:  "connect",
				Status: "warn",
				Host:   event.Host,
				Details: map[string]interface{}{
					"local_fallback": true,
					"reason":         event.Reason,
					"message":        event.Message,
				},
			})
			ctx.AddResultDetail("fallback", fallbackDetail{Reason: event.Reason})
		}
	})

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

	reporter.PhaseComplete("connect", ctx.Conn.Name, time.Since(connectStart))
	return nil
}

// connectLocalTarget completes the connect phase for a local target
// (--local or local mode), and records the reason as details.local_reason
// on the result. Nothing is dialed and nothing went wrong, so it's a normal
// connect completion carrying the reason, not a fallback warning. With a
// local host the run goes through its connection (see execTarget).
func connectLocalTarget(ctx *WorkflowContext) {
	ctx.Conn = ctx.target.connection()
	ctx.AddResultDetail("local_reason", ctx.target.reason)

	if PrettyMode() {
		ctx.PhaseDisplay.RenderSuccess("Running locally ("+localRunDetail(ctx.Conn)+")", 0)
		return
	}
	emitLocalConnect(ctx.target)
}

// localRunDetail says why a run rr put on this machine by itself runs here,
// and on which local host if it has one, for "Running locally (<detail>)".
func localRunDetail(conn *host.Connection) string {
	detail := host.DescribeLocalReason(conn.LocalReason)
	if !conn.IsLocal {
		detail += ", on " + conn.Name
	}
	return detail
}

// syncPhase handles the file sync phase of the workflow.
func syncPhase(ctx *WorkflowContext, opts WorkflowOptions) error {
	reporter := ctx.GetReporter()

	if ctx.Conn.IsLocal {
		// Bare local execution, with no local host to run on.
		reporter.PhaseSkipped("sync", "local")
		return nil
	}
	if ctx.Conn.InPlace() {
		// A local host: it runs in the project dir, so there's nothing to sync.
		reporter.PhaseSkipped("sync", "in_place")
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

	err := rrsync.SyncWithOptions(ctx.Conn, ctx.WorkDir, syncCfg, nil, structuredSyncOptions(""))
	if err != nil {
		reporter.PhaseFailed("sync", err)
		return err
	}

	reporter.PhaseComplete("sync", ctx.Conn.Name, time.Since(syncStart))
	return nil
}

// syncOptions returns the sync callbacks for the active output mode.
func syncOptions() *rrsync.SyncOptions {
	if PrettyMode() {
		return prettySyncOptions()
	}
	return structuredSyncOptions("")
}

// structuredSyncOptions surfaces sync notices and warnings as phase events.
// hostName goes in the events' host field; parallel runs set it because they
// sync several hosts, single runs leave it empty.
func structuredSyncOptions(hostName string) *rrsync.SyncOptions {
	return &rrsync.SyncOptions{
		Invalidated: func(dir, lockfile string) {
			WritePhaseEvent(invalidatedEvent(hostName, dir, lockfile))
		},
		Warn: func(w rrsync.SyncWarning) {
			WritePhaseEvent(PhaseEvent{
				Type:    "phase",
				Phase:   "sync",
				Status:  "warn",
				Host:    hostName,
				Details: w.Details,
			})
		},
		Pruned: func(dir string) {
			WritePhaseEvent(PhaseEvent{
				Type:    "phase",
				Phase:   "sync",
				Status:  "pruned",
				Host:    hostName,
				Details: map[string]interface{}{"dir": dir},
			})
		},
	}
}

// invalidatedEvent is the sync phase event for a remote directory removed
// by lockfile invalidation.
func invalidatedEvent(hostName, dir, lockfile string) PhaseEvent {
	return PhaseEvent{
		Type:   "phase",
		Phase:  "sync",
		Status: "invalidated",
		Host:   hostName,
		Details: map[string]interface{}{
			"dir":      dir,
			"lockfile": lockfile,
		},
	}
}

// prettySyncOptions surfaces sync notices and warnings as printed lines.
func prettySyncOptions() *rrsync.SyncOptions {
	return &rrsync.SyncOptions{
		Invalidated: func(dir, lockfile string) {
			printSpinnerNotice(fmt.Sprintf("Invalidating stale %s (%s changed)", dir, lockfile))
		},
		Warn: func(w rrsync.SyncWarning) {
			ui.PrintWarning(w.Message)
		},
		Pruned: func(dir string) {
			muted := lipgloss.NewStyle().Foreground(ui.ColorMuted)
			fmt.Println(muted.Render("Pruned stale worktree dir " + dir))
		},
	}
}

// printSpinnerNotice prints a muted notice that arrives while the sync
// spinner is drawing, clearing the spinner's line first.
func printSpinnerNotice(msg string) {
	fmt.Print("\r\033[K")
	fmt.Println(lipgloss.NewStyle().Foreground(ui.ColorMuted).Render(msg))
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

	syncProgress := ui.NewInlineProgress("Syncing files", os.Stdout)
	syncProgress.SetUseFakeProgress(false) // Use real rsync progress
	progressWriter := ui.NewProgressWriter(syncProgress, nil)
	syncProgress.Start()

	err := rrsync.SyncWithOptions(ctx.Conn, ctx.WorkDir, syncCfg, progressWriter, prettySyncOptions())
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

	syncSpinner := ui.NewSpinner("Syncing files")
	syncSpinner.Start()

	err := rrsync.SyncWithOptions(ctx.Conn, ctx.WorkDir, syncCfg, nil, prettySyncOptions())
	if err != nil {
		syncSpinner.Fail()
		return err
	}

	syncSpinner.Success()
	ctx.PhaseDisplay.RenderSuccess("Files synced", time.Since(syncStart))
	return nil
}

// lockPhase handles the lock acquisition phase of the workflow. Bare local
// execution (--local, local mode or a fallback with no local host) takes no
// lock; everything else, a local host standing in for those included, locks
// its connection's host.
func lockPhase(ctx *WorkflowContext, opts WorkflowOptions) error {
	lockCfg := config.DefaultConfig().Lock
	if ctx.Resolved.Project != nil {
		lockCfg = ctx.Resolved.Project.Lock
	}

	if !lockCfg.Enabled || opts.SkipLock || ctx.Conn.IsLocal {
		return nil
	}

	hostName := ctx.Conn.Name
	lockStart := time.Now()
	acquireOpts := []lock.AcquireOption{
		lock.WithContext(ctx.Context()),
		lock.WithWarnFunc(lockWarn(hostName)),
	}

	if PrettyMode() {
		lockSpinner := ui.NewSpinner("Acquiring lock")
		lockSpinner.Start()

		var err error
		ctx.Lock, err = lock.Acquire(ctx.Conn, lockCfg, opts.Command, append(acquireOpts,
			lock.WithWaitFunc(func(holder *lock.LockInfo) {
				lockSpinner.SetLabel(lockWaitMessage(hostName, holder, lockCfg.Timeout))
			}))...)
		if err != nil {
			lockSpinner.Fail()
			return err
		}

		ctx.Lock.StartHeartbeat()
		recordLocalJob(ctx)
		lockSpinner.Success()
		ctx.PhaseDisplay.RenderSuccess("Lock acquired", time.Since(lockStart))
		return nil
	}

	reporter := ctx.GetReporter()
	reporter.PhaseStart("lock")

	var err error
	ctx.Lock, err = lock.Acquire(ctx.Conn, lockCfg, opts.Command, append(acquireOpts,
		lock.WithWaitFunc(func(holder *lock.LockInfo) {
			WritePhaseEvent(PhaseEvent{
				Type:   "phase",
				Phase:  "lock",
				Status: "waiting",
				Host:   hostName,
				Details: map[string]interface{}{
					"message":        lockWaitMessage(hostName, holder, lockCfg.Timeout),
					"holders":        holderDetails([]hostAttempt{{hostName: hostName, lockInfo: holder}}),
					"wait_timeout_s": lockCfg.Timeout.Seconds(),
				},
			})
		}))...)
	if err != nil {
		reporter.PhaseFailed("lock", err)
		return err
	}

	ctx.Lock.StartHeartbeat()
	recordLocalJob(ctx)
	reporter.PhaseComplete("lock", hostName, time.Since(lockStart))
	return nil
}

// lockWaitMessage says whose lock on hostName a run is waiting for, and for
// how long it will wait.
func lockWaitMessage(hostName string, holder *lock.LockInfo, timeout time.Duration) string {
	desc := "holder unknown"
	if holder != nil {
		desc = holder.Describe()
	}
	return fmt.Sprintf("Waiting up to %s for the lock on %s: %s", timeout, hostName, desc)
}

// recordLocalJob makes a local host record the process group of the job it
// starts in the lock just taken. The job runs in its own session, so if rr is
// killed it keeps running; with its group in the lock, the next run waits for
// it instead of taking the lock from the dead rr. Remote hosts don't need it.
func recordLocalJob(ctx *WorkflowContext) {
	client, ok := ctx.Conn.Client.(*host.LocalClient)
	if !ok || ctx.Lock == nil {
		return
	}
	lck, hostName := ctx.Lock, ctx.Conn.Name
	warn := lockWarn(hostName)
	client.SetOnStart(func(pgid int) {
		if err := lck.SetJobPGID(pgid); err != nil {
			warn(fmt.Sprintf("Warning: couldn't record the job in the lock on %s (%v); if rr is killed, another run may start before the job stops", hostName, err))
		}
	})
}

// SetupWorkflow performs the common workflow phases: load config, connect, lock, and sync.
// Returns a WorkflowContext that the caller uses for execution, and must Close() when done.
//
// A local target (--local or local mode) dials nothing and skips sync. With
// a local host it runs on that host and takes its lock, as a local fallback
// does; without one it runs bare, with no lock.
// When multiple hosts are configured, this function implements load balancing
// (see findAvailableHost):
//  1. Try each host with non-blocking lock acquisition
//  2. If a host is locked, immediately try the next host
//  3. If all hosts are locked or unreachable, local_fallback decides whether
//     to run locally, wait, or fail
//  4. Once a lock is acquired, sync files to that host
//
// The lock-before-sync order ensures we don't waste time syncing to a host we can't use.
func SetupWorkflow(opts WorkflowOptions) (*WorkflowContext, error) {
	pd := ui.NewPhaseDisplay(os.Stdout)
	ctx := &WorkflowContext{
		StartTime:    time.Now(),
		PhaseDisplay: pd,
		Reporter:     NewPhaseReporter(pd),
	}

	// Set up signal handler early to ensure cleanup on Ctrl+C
	ctx.setupSignalHandler()

	// Load and validate config, and decide local vs remote. This also
	// rejects --local with --tag before any config is read.
	if err := loadAndValidateConfig(ctx, opts); err != nil {
		ctx.Close()
		return nil, err
	}

	// Determine working directory
	if err := setupWorkDir(ctx, opts); err != nil {
		ctx.Close()
		return nil, err
	}

	if ctx.target.local {
		connectLocalTarget(ctx)
		if err := checkTaskHost(ctx, opts); err != nil {
			ctx.Close()
			return nil, err
		}
		if err := lockPhase(ctx, opts); err != nil {
			ctx.Close()
			return nil, err
		}
	} else if err := connectRemote(ctx, opts); err != nil {
		ctx.Close()
		return nil, err
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

// connectRemote picks a remote host and takes its lock. With several hosts
// and no --host/--tag it load-balances (connect + lock combined); otherwise
// it connects, then locks.
func connectRemote(ctx *WorkflowContext, opts WorkflowOptions) error {
	setupHostSelector(ctx, opts)

	if ctx.selector.HostCount() > 1 && opts.Host == "" && opts.Tag == "" {
		if err := setupWorkflowLoadBalanced(ctx, opts); err != nil {
			return err
		}
		// A fallback took no lock while picking; lock it now (a no-op for
		// bare local execution).
		if ctx.Conn.LocalReason != "" {
			return lockPhase(ctx, opts)
		}
		recordLocalJob(ctx)
		return nil
	}

	// Phase 1: Connect
	if err := connectPhase(ctx, opts); err != nil {
		return err
	}
	if err := checkTaskHost(ctx, opts); err != nil {
		return err
	}
	// Phase 2: Acquire lock (before sync)
	return lockPhase(ctx, opts)
}

// checkTaskHost refuses a task pinned to other hosts once its host is
// known, before that host's lock is waited on: a busy host would otherwise
// hold the run for lock.timeout only to refuse it. RunTask checks again
// after a load-balanced pick.
func checkTaskHost(ctx *WorkflowContext, opts WorkflowOptions) error {
	if opts.TaskName == "" || ctx.Resolved.Project == nil {
		return nil
	}
	task, ok := ctx.Resolved.Project.Tasks[opts.TaskName]
	if !ok || config.IsTaskHostAllowed(&task, ctx.Conn.Name) {
		return nil
	}
	return taskHostError(opts.TaskName, &task, ctx.Conn)
}

// ExecutePullPhase downloads files from remote after command execution.
// Pull happens regardless of command exit code - often you want test artifacts on failure.
// Errors are logged but don't fail the overall workflow.
//
// A run that ran in place (a local host, --local, a fallback) left its files
// in the project dir; they're copied from there to the dest, and the phase
// is reported skipped when the dest is the project dir itself.
func ExecutePullPhase(wf *WorkflowContext, pullItems []config.PullItem, dest string) {
	if len(pullItems) == 0 || wf.Conn == nil {
		return
	}

	pullStart := time.Now()
	pullOpts := rrsync.PullOptions{
		Patterns:    pullItems,
		DefaultDest: dest,
	}
	pull := rrsync.Pull
	if wf.Conn.InPlace() {
		if !rrsync.InPlaceCopyNeeded(wf.WorkDir, pullOpts) {
			reportPullSkipped(wf.Conn, "")
			return
		}
		pull = inPlacePull(wf.WorkDir)
	}
	if pullAndReport(wf.Conn, pullOpts, pull, "") && PrettyMode() {
		wf.PhaseDisplay.RenderSuccess("Files pulled", time.Since(pullStart))
	}
}

// inPlacePull returns a pullFunc that copies from srcDir on this machine,
// for a command that ran in place there.
func inPlacePull(srcDir string) pullFunc {
	return func(_ *host.Connection, opts rrsync.PullOptions, progress io.Writer) error {
		_, err := rrsync.PullInPlace(srcDir, opts, progress)
		return err
	}
}

// pullSkippedReason is the reason a pull phase that copied nothing reports:
// the command ran in place and the dest is the dir it ran in.
const pullSkippedReason = "same_dir"

// reportPullSkipped reports a pull phase with nothing to copy. task names
// the parallel subtask, empty for a single run.
func reportPullSkipped(conn *host.Connection, task string) {
	if PrettyMode() {
		label := "pull"
		if task != "" {
			label = fmt.Sprintf("pull %s (%s)", task, conn.Name)
		}
		ui.NewPhaseDisplay(os.Stdout).RenderSkipped(label, "files are already in the project dir")
		return
	}
	details := map[string]interface{}{"reason": pullSkippedReason}
	if task != "" {
		details["task"] = task
	}
	WritePhaseEvent(PhaseEvent{Type: "phase", Phase: "pull", Status: "skipped", Host: conn.Name, Details: details})
}

// pullFunc matches rrsync.Pull; tests swap in a fake.
type pullFunc func(conn *host.Connection, opts rrsync.PullOptions, progress io.Writer) error

// pullAndReport pulls files over conn and reports it as the pull phase:
// started, then complete or failed events in structured mode, a spinner and
// a failure line in pretty mode. A failed pull is reported, never returned,
// because pulls don't change a run's exit code. Returns whether it worked.
//
// task names the parallel subtask being pulled, empty for a single run.
// Several hosts pull in one parallel run, so a subtask's events carry the
// host throughout plus details.task; a single run's events match its other
// phases, with the host on complete only.
func pullAndReport(conn *host.Connection, opts rrsync.PullOptions, pull pullFunc, task string) bool {
	start := time.Now()

	if !PrettyMode() {
		ev := PhaseEvent{Type: "phase", Phase: "pull", Status: "started"}
		if task != "" {
			ev.Host = conn.Name
			ev.Details = map[string]interface{}{"task": task}
		}
		WritePhaseEvent(ev)
		if err := pull(conn, opts, nil); err != nil {
			ev.Status, ev.Error = "failed", err.Error()
			WritePhaseEvent(ev)
			return false
		}
		ev.Status, ev.Host, ev.Duration = "complete", conn.Name, time.Since(start).Seconds()
		WritePhaseEvent(ev)
		return true
	}

	label, failure := "Pulling files", "Pull failed"
	if task != "" {
		label = fmt.Sprintf("Pulling %s files (%s)", task, conn.Name)
		failure = "Pull failed for " + task
	}
	spinner := ui.NewSpinner(label)
	spinner.Start()
	if err := pull(conn, opts, nil); err != nil {
		spinner.Fail()
		fmt.Printf("%s %s: %s\n", ui.SymbolFail, failure, err.Error())
		return false
	}
	spinner.Success()
	return true
}

// requirementsPhase verifies that required tools are available on the remote.
func requirementsPhase(ctx *WorkflowContext, opts WorkflowOptions) error {
	// Skip for bare local execution or if explicitly disabled
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
	results, err := require.CheckAll(ctx.Conn.Client, &ctx.Conn.Host, reqs, require.GlobalCache(), ctx.Conn.Name)
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

	return missingRequirementsError(require.FormatMissing(missing), opts.TaskName)
}

// missingRequirementsError reports required tools missing on the remote.
// Only rr run and rr exec have --skip-requirements, so the suggestion
// mentions it only when no task is running (taskName is empty).
func missingRequirementsError(missing, taskName string) error {
	suggestion := "Run 'rr provision' to install missing tools, or use --skip-requirements to bypass."
	if taskName != "" {
		suggestion = "Run 'rr provision' to install missing tools, or remove them from 'require:' in your config."
	}
	return errors.New(errors.ErrDependency, "Missing required tools: "+missing, suggestion)
}

// lockWarn reports a lock warning for hostName (a stale or dead-holder lock
// was stolen): a lock warn event in structured mode, a warning line with
// --pretty. The lock package reports these only through this callback.
func lockWarn(hostName string) func(msg string) {
	return func(msg string) {
		if PrettyMode() {
			ui.PrintWarning(msg)
			return
		}
		WritePhaseEvent(PhaseEvent{
			Type:    "phase",
			Phase:   "lock",
			Status:  "warn",
			Host:    hostName,
			Details: map[string]interface{}{"message": msg},
		})
	}
}
