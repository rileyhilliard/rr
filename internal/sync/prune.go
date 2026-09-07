package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/util"
)

// Worktree isolation gives every linked worktree its own remote directory
// ("<repo>@<worktree>" beside "<repo>"), each with its own preserved
// node_modules and .venv. Nothing removed those directories when the local
// worktree went away, so a machine that churns through worktrees fills the
// remote disk. Pruning compares the remote "<repo>@*" siblings with the
// worktrees git still knows about locally and removes the rest.

// worktreeSuffixRe matches what sanitizeWorktreeName can produce. Anything
// else beside the project dir is not ours to delete.
var worktreeSuffixRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// PruneOptions tunes a prune pass.
type PruneOptions struct {
	// DryRun lists what would be removed without removing it.
	DryRun bool
	// Pruned is called with each remote directory removed (or, with DryRun,
	// each that would be). Nil means silent.
	Pruned func(remoteDir string)
}

// PruneStaleWorktrees removes remote per-worktree sync directories for
// worktrees that no longer exist locally. It returns the directories removed
// (or, in dry-run mode, the candidates). localDir is the tree being synced.
//
// It does nothing when: the connection is local or nil; the host dir does
// not end in ${PROJECT} (the remote layout is not "<parent>/<repo>@<wt>");
// or git cannot list the local worktrees (an unknown live set must never be
// treated as empty).
func PruneStaleWorktrees(conn *host.Connection, localDir string, opts PruneOptions) ([]string, error) {
	if conn == nil || conn.IsLocal || conn.Client == nil {
		return nil, nil
	}
	remoteDir := strings.TrimSuffix(config.ExpandRemote(conn.Host.Dir), "/")
	if path.Base(remoteDir) != config.ProjectName() {
		return nil, nil
	}
	live, ok := config.ListWorktreeNames(localDir)
	if !ok {
		return nil, nil
	}
	hostname, _ := os.Hostname()

	stale, err := staleWorktreeDirs(conn, remoteDir, config.ProjectBaseName(), live, hostname)
	if err != nil || len(stale) == 0 {
		return nil, err
	}

	return pruneDirs(conn, stale, opts)
}

// pruneDirs removes the given directories and reports each one, stopping at
// the first failure. A directory is reported only once it is actually gone,
// so a failed rm never surfaces as a "pruned" event; in dry-run mode nothing
// is removed and every candidate is reported.
func pruneDirs(conn *host.Connection, stale []string, opts PruneOptions) ([]string, error) {
	var done []string
	for _, dir := range stale {
		if !opts.DryRun {
			if err := removeRemoteDir(conn, dir); err != nil {
				return done, err
			}
		}
		done = append(done, dir)
		if opts.Pruned != nil {
			opts.Pruned(dir)
		}
	}
	return done, nil
}

// staleWorktreeDirs lists "<parent>/<base>@<wt>" directories beside remoteDir
// whose <wt> is not in live. The current remote dir is never a candidate.
// A directory whose provenance marker names another machine is kept: that
// machine's worktrees are not in our live set, so we cannot judge them.
// Directories without a marker (synced by an rr that predates markers) are
// treated as ours.
func staleWorktreeDirs(conn *host.Connection, remoteDir, base string, live map[string]bool, hostname string) ([]string, error) {
	parent := path.Dir(remoteDir)
	if base == "" || parent == "." || parent == "/" {
		return nil, nil
	}

	// ls the siblings by name; the glob is outside the quotes on purpose.
	listCmd := fmt.Sprintf("cd %s 2>/dev/null && ls -1d %s@* 2>/dev/null",
		util.ShellQuotePreserveTilde(parent), util.ShellQuote(base))
	stdout, _, _, err := conn.Client.Exec(listCmd)
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrSync,
			"Couldn't list remote worktree directories",
			"Check the SSH connection.")
	}

	current := path.Base(remoteDir)
	var stale []string
	for _, line := range strings.Split(string(stdout), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || name == current || !strings.HasPrefix(name, base+"@") {
			continue
		}
		wt := name[len(base)+1:]
		if !worktreeSuffixRe.MatchString(wt) || live[wt] {
			continue
		}
		dir := parent + "/" + name
		if owner := markerHostname(conn, dir); owner != "" && owner != hostname {
			continue
		}
		stale = append(stale, dir)
	}
	return stale, nil
}

// markerHostname reads the provenance marker in a remote dir and returns the
// hostname that last synced it, or "" when there is no readable marker.
func markerHostname(conn *host.Connection, remoteDir string) string {
	catCmd := fmt.Sprintf("cat %s 2>/dev/null", util.ShellQuotePreserveTilde(remoteDir+"/"+sourceMarkerFile))
	stdout, _, exitCode, err := conn.Client.Exec(catCmd)
	if err != nil || exitCode != 0 {
		return ""
	}
	var m SourceMarker
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(stdout))), &m); err != nil {
		return ""
	}
	return m.Hostname
}

// removeRemoteDir deletes one remote directory. The path must be a
// "<repo>@<worktree>" directory (the only thing prune ever produces); the
// check is a last guard against a malformed path reaching rm -rf.
func removeRemoteDir(conn *host.Connection, dir string) error {
	base := path.Base(dir)
	if !strings.Contains(base, "@") || path.Dir(dir) == "/" || path.Dir(dir) == "." {
		return errors.New(errors.ErrSync,
			fmt.Sprintf("Refusing to remove %s", dir),
			"Prune only removes <repo>@<worktree> directories beside the project's remote dir.")
	}
	rmCmd := fmt.Sprintf("rm -rf %s", util.ShellQuotePreserveTilde(dir))
	_, stderr, exitCode, err := conn.Client.Exec(rmCmd)
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrSync,
			fmt.Sprintf("Failed to remove stale remote directory %s", dir),
			"Check SSH connection and remote permissions.")
	}
	if exitCode != 0 {
		return errors.New(errors.ErrSync,
			fmt.Sprintf("Failed to remove stale remote directory %s", dir),
			fmt.Sprintf("Remote error: %s", strings.TrimSpace(string(stderr))))
	}
	return nil
}
