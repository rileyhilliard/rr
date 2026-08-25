package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/util"
)

// sourceMarkerFile is written to the remote sync root after every sync,
// recording where the tree came from. rsync protects it via a filter rule
// in BuildArgs so --delete never removes it.
const sourceMarkerFile = ".rr-source"

// SourceMarker records the provenance of a remote sync directory.
type SourceMarker struct {
	SourcePath string    `json:"source_path"`
	Hostname   string    `json:"hostname"`
	Branch     string    `json:"branch,omitempty"`
	Head       string    `json:"head,omitempty"`
	Worktree   string    `json:"worktree,omitempty"`
	SyncedAt   time.Time `json:"synced_at"`
}

// SyncWarning is a non-fatal condition surfaced during sync.
type SyncWarning struct {
	Code    string                 // e.g. "source_mismatch"
	Message string                 // human-readable
	Details map[string]interface{} // structured payload for phase events
}

// SyncOptions carries optional sync behavior.
type SyncOptions struct {
	// Warn receives non-fatal warnings (nil to ignore).
	Warn func(SyncWarning)
}

// markerRemotePath returns the remote path of the provenance marker.
func markerRemotePath(conn *host.Connection) string {
	remoteDir := strings.TrimSuffix(config.ExpandRemote(conn.Host.Dir), "/")
	return remoteDir + "/" + sourceMarkerFile
}

// checkSourceMarker warns when the remote dir was last synced from a
// different local tree or machine - the observable version of what used to
// be a silent clobber.
func checkSourceMarker(conn *host.Connection, localDir string, opts *SyncOptions) {
	if opts == nil || opts.Warn == nil || conn == nil || conn.Client == nil {
		return
	}

	catCmd := fmt.Sprintf("cat %s 2>/dev/null", util.ShellQuotePreserveTilde(markerRemotePath(conn)))
	stdout, _, exitCode, err := conn.Client.Exec(catCmd)
	if err != nil || exitCode != 0 {
		return // no marker (first sync or old rr) - nothing to compare
	}

	var prev SourceMarker
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(stdout))), &prev); err != nil {
		return
	}

	hostname, _ := os.Hostname()
	current := filepath.Clean(localDir)
	if filepath.Clean(prev.SourcePath) == current && prev.Hostname == hostname {
		return
	}

	prevDesc := prev.SourcePath
	if prev.Worktree != "" {
		prevDesc = fmt.Sprintf("worktree %s (%s)", prev.Worktree, prev.SourcePath)
	}
	if prev.Branch != "" {
		prevDesc += fmt.Sprintf(" on branch %s", prev.Branch)
	}

	remoteDir := config.ExpandRemote(conn.Host.Dir)
	opts.Warn(SyncWarning{
		Code: "source_mismatch",
		Message: fmt.Sprintf("remote %s was last synced from %s; now syncing from %s - the previous tree's changes there will be replaced",
			remoteDir, prevDesc, current),
		Details: map[string]interface{}{
			"code":              "source_mismatch",
			"remote_dir":        remoteDir,
			"previous_source":   prev.SourcePath,
			"previous_branch":   prev.Branch,
			"previous_worktree": prev.Worktree,
			"previous_hostname": prev.Hostname,
			"current_source":    current,
		},
	})
}

// writeSourceMarker records this sync's provenance on the remote.
// Best-effort: failures never fail the sync.
func writeSourceMarker(conn *host.Connection, localDir string) {
	if conn == nil || conn.Client == nil {
		return
	}

	marker := buildSourceMarker(localDir)
	data, err := json.Marshal(marker)
	if err != nil {
		return
	}

	writeCmd := fmt.Sprintf("cat > %s << 'RRSOURCE'\n%s\nRRSOURCE",
		util.ShellQuotePreserveTilde(markerRemotePath(conn)), string(data))
	_, _, _, _ = conn.Client.Exec(writeCmd)
}

// buildSourceMarker gathers local provenance (path, hostname, git state).
func buildSourceMarker(localDir string) SourceMarker {
	hostname, _ := os.Hostname()
	marker := SourceMarker{
		SourcePath: filepath.Clean(localDir),
		Hostname:   hostname,
		SyncedAt:   time.Now(),
	}

	if wt := config.DetectWorktree(); wt.IsLinked {
		marker.Worktree = wt.Name
	}

	// Branch and HEAD are best-effort context
	if out, err := exec.Command("git", "-C", localDir, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		marker.Branch = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", localDir, "rev-parse", "HEAD").Output(); err == nil {
		marker.Head = strings.TrimSpace(string(out))
	}

	return marker
}
