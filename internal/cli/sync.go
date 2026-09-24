package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	gosync "sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
)

// SyncOptions holds options for the sync command.
type SyncOptions struct {
	Host         string        // Preferred host name
	Tag          string        // Filter hosts by tag
	ProbeTimeout time.Duration // Override SSH probe timeout (0 means use config default)
	DryRun       bool          // If true, show what would be synced without syncing
	SkipLock     bool          // If true, skip locking
	WorkingDir   string        // Override local working directory
}

// Sync transfers files to the remote host without executing any command.
// With --pretty each phase shows a spinner; otherwise phases are JSON events
// on stderr, ending in a result event (see reportSyncDone).
func Sync(opts SyncOptions) error {
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

	// Determine working directory
	workDir := opts.WorkingDir
	if workDir == "" {
		workDir, err = os.Getwd()
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrExec,
				"Can't figure out what directory you're in",
				"This is unusual - check your directory permissions.")
		}
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
	connect := startSyncCmdPhase(phaseDisplay, "connect", "Connecting")

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
		connect.fail(err)
		return err
	}
	connect.complete(conn.Name, "Connected to "+conn.Alias)

	// Phase 2: Acquire lock (skip for dry-run and local connections)
	lockCfg := config.DefaultConfig().Lock
	if resolved.Project != nil {
		lockCfg = resolved.Project.Lock
	}

	if lockCfg.Enabled && !opts.DryRun && !opts.SkipLock && !conn.IsLocal {
		locking := startSyncCmdPhase(phaseDisplay, "lock", "Acquiring lock")

		lck, err := lock.Acquire(conn, lockCfg, "sync", lock.WithWarnFunc(lockWarn(conn.Name)))
		if err != nil {
			locking.fail(err)
			return err
		}
		defer lck.Release() //nolint:errcheck // Lock release errors are non-fatal

		locking.complete(conn.Name, "Lock acquired")
	}

	// Phase 3: Sync
	syncing := startSyncCmdPhase(phaseDisplay, "sync", "Syncing files")

	// Use project sync config if available, otherwise use defaults
	syncCfg := config.DefaultConfig().Sync
	if resolved.Project != nil {
		syncCfg = resolved.Project.Sync
	}
	// A dry run itemizes its changes so it can say what it would transfer
	// and delete (copy first to avoid mutating the shared config slice).
	// SyncWithOptions adds --dry-run itself.
	var progress io.Writer
	var itemized *lockedBuffer
	if opts.DryRun {
		syncCfg.Flags = append(slices.Clone(syncCfg.Flags), "--itemize-changes")
		itemized = &lockedBuffer{}
		progress = itemized
	}

	err = rrsync.SyncWithOptions(conn, workDir, syncCfg, progress, syncCommandOptions(opts.DryRun))
	if err != nil {
		syncing.fail(err)
		return err
	}
	syncing.complete(conn.Name, "")

	var preview *syncPreview
	if opts.DryRun {
		p := parseItemizedChanges(itemized.String())
		preview = &p
	}
	reportSyncDone(conn, preview, time.Since(syncing.start), time.Since(startTime))
	return nil
}

// syncCmdPhase is one phase of rr sync: a spinner with --pretty, started and
// complete or failed events otherwise.
type syncCmdPhase struct {
	name    string
	start   time.Time
	pd      *ui.PhaseDisplay
	spinner *ui.Spinner // nil in structured mode
	events  *StructuredReporter
}

func startSyncCmdPhase(pd *ui.PhaseDisplay, name, label string) *syncCmdPhase {
	p := &syncCmdPhase{name: name, start: time.Now(), pd: pd}
	if PrettyMode() {
		p.spinner = ui.NewSpinner(label)
		p.spinner.Start()
	} else {
		p.events = &StructuredReporter{}
		p.events.PhaseStart(name)
	}
	return p
}

// complete ends the phase on hostName. With --pretty, a non-empty doneMsg
// is printed as a success line after the spinner's.
func (p *syncCmdPhase) complete(hostName, doneMsg string) {
	if p.spinner == nil {
		p.events.PhaseComplete(p.name, hostName, time.Since(p.start))
		return
	}
	p.spinner.Success()
	if doneMsg != "" {
		p.pd.RenderSuccess(doneMsg, time.Since(p.start))
	}
}

func (p *syncCmdPhase) fail(err error) {
	if p.spinner == nil {
		p.events.PhaseFailed(p.name, err)
		return
	}
	p.spinner.Fail()
}

// syncPreview is what a dry run would change on the remote.
type syncPreview struct {
	Transfer []string // paths rsync would send or create
	Delete   []string // remote paths rsync would delete
}

// parseItemizedChanges reads rsync --itemize-changes output. Each change is
// "<flags> <path>": flags starting with '<' or '>' (file sent), 'c' (created,
// such as a directory or symlink) or 'h' (hard link) followed by the file
// type (f, d, L, D, S) mean the path would be transferred, and "*deleting"
// means it would be deleted. Attribute-only updates ('.' flags), progress
// lines, and rsync's own messages are skipped.
func parseItemizedChanges(out string) syncPreview {
	preview := syncPreview{Transfer: []string{}, Delete: []string{}}
	for _, line := range strings.Split(out, "\n") {
		flags, path, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch {
		case flags == "*deleting":
			preview.Delete = append(preview.Delete, strings.TrimLeft(path, " "))
		case len(flags) >= 9 && strings.IndexByte("<>ch", flags[0]) >= 0 && strings.IndexByte("fdLDS", flags[1]) >= 0:
			preview.Transfer = append(preview.Transfer, path)
		}
	}
	return preview
}

// reportSyncDone reports a finished rr sync. preview is set on a dry run.
// Structured mode emits a result event on stderr, host and duration set and
// details.dry_run always present; a dry run adds details.transfer and
// details.delete, the paths it would send and remove. With --pretty it
// prints the preview, then the summary line.
func reportSyncDone(conn *host.Connection, preview *syncPreview, syncDuration, totalDuration time.Duration) {
	if !PrettyMode() {
		details := map[string]interface{}{"dry_run": preview != nil}
		if preview != nil {
			details["transfer"] = preview.Transfer
			details["delete"] = preview.Delete
		}
		WritePhaseEvent(PhaseEvent{
			Type:     "result",
			Status:   "success",
			Host:     conn.Name,
			Duration: totalDuration.Seconds(),
			Details:  details,
		})
		return
	}

	fmt.Println()
	if preview == nil {
		fmt.Printf("%s Files synced to %s in %.1fs\n",
			ui.SymbolComplete, conn.Alias, syncDuration.Seconds())
		return
	}
	printPathList("Would transfer:", preview.Transfer)
	printPathList("Would delete:", preview.Delete)
	if len(preview.Transfer) == 0 && len(preview.Delete) == 0 {
		fmt.Println("Nothing to sync: the remote is up to date.")
	}
	fmt.Println()
	fmt.Printf("%s Dry run completed in %.1fs\n",
		ui.SymbolComplete, totalDuration.Seconds())
}

// printPathList prints a heading and its paths, or nothing when empty.
func printPathList(heading string, paths []string) {
	if len(paths) == 0 {
		return
	}
	fmt.Println(heading)
	for _, p := range paths {
		fmt.Println("  " + p)
	}
}

// lockedBuffer collects rsync output. SyncWithOptions streams rsync's
// stdout and stderr into it from separate goroutines.
type lockedBuffer struct {
	mu  gosync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// syncCommandOptions returns the sync options for rr sync. A dry run deletes
// nothing, so its invalidation notice says what it would remove: a "Would
// invalidate" line with --pretty, the usual invalidated event with
// details.dry_run set otherwise.
func syncCommandOptions(dryRun bool) *rrsync.SyncOptions {
	opts := syncOptions()
	if !dryRun {
		return opts
	}
	opts.DryRun = true
	if PrettyMode() {
		opts.Invalidated = func(dir, lockfile string) {
			printSpinnerNotice(fmt.Sprintf("Would invalidate stale %s (%s changed)", dir, lockfile))
		}
		return opts
	}
	opts.Invalidated = func(dir, lockfile string) {
		ev := invalidatedEvent("", dir, lockfile)
		ev.Details["dry_run"] = true
		WritePhaseEvent(ev)
	}
	return opts
}

// syncCommand is the implementation called by the cobra command.
func syncCommand(hostFlag, tagFlag, probeTimeoutFlag string, dryRun bool) error {
	probeTimeout, err := ParseProbeTimeout(probeTimeoutFlag)
	if err != nil {
		return err
	}

	return Sync(SyncOptions{
		Host:         hostFlag,
		Tag:          tagFlag,
		ProbeTimeout: probeTimeout,
		DryRun:       dryRun,
	})
}
