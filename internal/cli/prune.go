package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
)

// PruneOptions holds options for the prune command.
type PruneOptions struct {
	Host   string // Specific host (empty for every project host)
	DryRun bool   // List candidates without removing them
}

// hostPruneOutcome records the result of pruning one host.
type hostPruneOutcome struct {
	Host    string   `json:"host"`
	Status  string   `json:"status"` // pruned | clean | failed
	Removed []string `json:"removed,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// pruneCommand removes remote "<repo>@<worktree>" sync directories whose
// worktree no longer exists locally. Syncs do this on their own (see
// sync.prune_worktrees); the command exists for hosts that have not been
// synced to since a worktree was removed, and for --dry-run inspection.
func pruneCommand(opts PruneOptions) error {
	resolved, err := config.LoadResolved(Config())
	if err != nil {
		return err
	}
	if resolved.ProjectRoot == "" {
		return errors.New(errors.ErrConfig,
			"No project found",
			"Run rr prune from inside a project with a .rr.yaml.")
	}

	// ResolveHosts validates --host against the global config and returns
	// just that host when set.
	hostOrder, hosts, err := config.ResolveHosts(resolved, opts.Host)
	if err != nil {
		return err
	}
	if len(hostOrder) == 0 {
		return errors.New(errors.ErrConfig,
			"No remote hosts to prune",
			"Prune only applies to remote hosts. Add hosts to ~/.rr/config.yaml.")
	}

	outcomes := make([]hostPruneOutcome, 0, len(hostOrder))
	for _, name := range hostOrder {
		outcomes = append(outcomes, pruneHost(name, hosts[name], resolved.ProjectRoot, opts.DryRun))
	}

	failed := 0
	for _, o := range outcomes {
		if o.Status == "failed" {
			failed++
		}
	}

	if PrettyMode() {
		for _, o := range outcomes {
			printPruneOutcome(o, opts.DryRun)
		}
	} else {
		_ = WriteJSONSuccess(os.Stdout, map[string]interface{}{
			"hosts":   outcomes,
			"dry_run": opts.DryRun,
			"failed":  failed,
		})
	}

	if failed > 0 {
		return errors.New(errors.ErrSync,
			"Some hosts could not be pruned",
			"Check the SSH connection and try again.")
	}
	return nil
}

// pruneHost connects to one host and prunes it.
func pruneHost(hostName string, hostCfg config.Host, projectRoot string, dryRun bool) hostPruneOutcome {
	outcome := hostPruneOutcome{Host: hostName}

	var conn *host.Connection
	var connErr error
	for _, alias := range hostCfg.SSH {
		client, latency, err := host.ProbeAndConnect(alias, 5*time.Second)
		if err == nil {
			conn = &host.Connection{Name: hostName, Alias: alias, Client: client, Host: hostCfg, Latency: latency}
			break
		}
		connErr = err
	}
	if conn == nil {
		outcome.Status = "failed"
		outcome.Error = fmt.Sprintf("could not connect: %v", connErr)
		return outcome
	}
	defer conn.Close()

	removed, err := rrsync.PruneStaleWorktrees(conn, projectRoot, rrsync.PruneOptions{DryRun: dryRun})
	if err != nil {
		outcome.Status = "failed"
		outcome.Error = err.Error()
		return outcome
	}
	outcome.Removed = removed
	if len(removed) == 0 {
		outcome.Status = "clean"
	} else {
		outcome.Status = "pruned"
	}
	return outcome
}

func printPruneOutcome(o hostPruneOutcome, dryRun bool) {
	switch o.Status {
	case "pruned":
		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Printf("%s %s: %s %d stale worktree dir(s)\n", ui.SymbolSuccess, o.Host, verb, len(o.Removed))
		for _, d := range o.Removed {
			fmt.Printf("    %s\n", d)
		}
	case "clean":
		fmt.Printf("%s %s: no stale worktree dirs\n", ui.SymbolPending, o.Host)
	default:
		fmt.Printf("%s %s: %s\n", ui.SymbolFail, o.Host, o.Error)
	}
}
