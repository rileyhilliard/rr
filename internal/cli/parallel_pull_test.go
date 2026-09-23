package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/parallel"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parsePhaseEvents decodes one PhaseEvent per stderr line.
func parsePhaseEvents(t *testing.T, out string) []PhaseEvent {
	t.Helper()
	var events []PhaseEvent
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev PhaseEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev), "bad event line: %s", line)
		events = append(events, ev)
	}
	return events
}

func TestBuildSubtaskInfos_CarriesPull(t *testing.T) {
	pull := []config.PullItem{{Src: "junit.xml"}, {Src: "coverage/", Dest: "reports"}}
	proj := &config.Config{Tasks: map[string]config.TaskConfig{
		"shard-1": {Run: "pytest", Pull: pull},
		"shard-2": {Run: "pytest"},
	}}
	parent := &config.TaskConfig{Parallel: []string{"shard-1", "shard-2"}}

	infos, err := buildSubtaskInfos(proj, parent, []string{"shard-1", "shard-2"}, nil)
	require.NoError(t, err)
	require.Len(t, infos, 2)
	assert.Equal(t, pull, infos[0].Pull)
	assert.Empty(t, infos[1].Pull)
}

func TestSubtaskPullItems(t *testing.T) {
	tests := []struct {
		name    string
		items   []config.PullItem
		subtask string
		want    []config.PullItem
	}{
		{
			name:    "no dest lands in ./<subtask>",
			items:   []config.PullItem{{Src: "junit.xml"}},
			subtask: "shard-1",
			want:    []config.PullItem{{Src: "junit.xml", Dest: "shard-1"}},
		},
		{
			name:    "dest gets the subtask appended",
			items:   []config.PullItem{{Src: "coverage/*.xml", Dest: "./reports/"}},
			subtask: "shard-2",
			want:    []config.PullItem{{Src: "coverage/*.xml", Dest: filepath.Join("reports", "shard-2")}},
		},
		{
			name:    "absolute dest",
			items:   []config.PullItem{{Src: "out.log", Dest: "/tmp/rr-out"}},
			subtask: "lint",
			want:    []config.PullItem{{Src: "out.log", Dest: "/tmp/rr-out/lint"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, subtaskPullItems(tt.items, tt.subtask))
		})
	}
}

// pullCall records one call to the injected pull function.
type pullCall struct {
	name, alias, dir string
	patterns         []config.PullItem
}

// TestPullSubtaskFiles covers the C2 contract: every subtask that ran on a
// remote host and has pull config gets pulled, pass or fail, one after
// another in subtask order, into <dest>/<subtask>/.
func TestPullSubtaskFiles(t *testing.T) {
	oldPretty := prettyMode
	defer func() { prettyMode = oldPretty }()
	prettyMode = false

	hosts := map[string]config.Host{
		"box-a": {Dir: "~/rr/proj"},
		"box-b": {Dir: "/srv/proj"},
	}
	tasks := []parallel.TaskInfo{
		{Name: "shard-1", Index: 0, Pull: []config.PullItem{{Src: "junit.xml"}}},
		{Name: "shard-2", Index: 1, Pull: []config.PullItem{{Src: "junit.xml", Dest: "reports"}}},
		{Name: "no-pull", Index: 2},
		{Name: "never-ran", Index: 3, Pull: []config.PullItem{{Src: "junit.xml"}}},
	}
	// Results arrive in completion order, not task order.
	result := &parallel.Result{TaskResults: []parallel.TaskResult{
		{TaskName: "no-pull", TaskIndex: 2, Host: "box-a", Alias: "a-lan"},
		{TaskName: "shard-2", TaskIndex: 1, Host: "box-b", Alias: "b-vpn", ExitCode: 1},
		{TaskName: "never-ran", TaskIndex: 3, Host: "none", ExitCode: 1},
		{TaskName: "shard-1", TaskIndex: 0, Host: "box-a", Alias: "a-lan"},
	}}

	var calls []pullCall
	pull := func(conn *host.Connection, opts rrsync.PullOptions, _ io.Writer) error {
		calls = append(calls, pullCall{name: conn.Name, alias: conn.Alias, dir: conn.Host.Dir, patterns: opts.Patterns})
		if conn.Name == "box-b" {
			return fmt.Errorf("rsync: no such file junit.xml")
		}
		return nil
	}

	stderr := captureStderr(t, func() {
		pullSubtaskFiles(tasks, result, hosts, pull)
	})

	require.Len(t, calls, 2, "only remote subtasks with pull config are pulled")
	assert.Equal(t, pullCall{name: "box-a", alias: "a-lan", dir: "~/rr/proj",
		patterns: []config.PullItem{{Src: "junit.xml", Dest: "shard-1"}}}, calls[0])
	assert.Equal(t, pullCall{name: "box-b", alias: "b-vpn", dir: "/srv/proj",
		patterns: []config.PullItem{{Src: "junit.xml", Dest: filepath.Join("reports", "shard-2")}}}, calls[1],
		"a failed subtask is still pulled")

	events := parsePhaseEvents(t, stderr)
	require.Len(t, events, 4)
	for _, ev := range events {
		assert.Equal(t, "phase", ev.Type)
		assert.Equal(t, "pull", ev.Phase)
	}
	assert.Equal(t, "started", events[0].Status)
	assert.Equal(t, "box-a", events[0].Host)
	assert.Equal(t, "shard-1", events[0].Details["task"])
	assert.Equal(t, "complete", events[1].Status)
	assert.Equal(t, "shard-1", events[1].Details["task"])
	assert.Equal(t, "started", events[2].Status)
	assert.Equal(t, "failed", events[3].Status)
	assert.Equal(t, "box-b", events[3].Host)
	assert.Equal(t, "shard-2", events[3].Details["task"])
	assert.Contains(t, events[3].Error, "no such file")
}

func TestPullSubtaskFiles_LocalRunSkips(t *testing.T) {
	tasks := []parallel.TaskInfo{{Name: "a", Pull: []config.PullItem{{Src: "x"}}}}
	result := &parallel.Result{TaskResults: []parallel.TaskResult{{TaskName: "a", Host: "local"}}}
	called := false
	pullSubtaskFiles(tasks, result, nil, func(*host.Connection, rrsync.PullOptions, io.Writer) error {
		called = true
		return nil
	})
	assert.False(t, called)
}

// TestParallelSyncNotices_Structured checks parallel syncs emit the same
// sync phase events as single runs, with the host set since sync runs once
// per host rather than per subtask.
func TestParallelSyncNotices_Structured(t *testing.T) {
	oldPretty := prettyMode
	defer func() { prettyMode = oldPretty }()
	prettyMode = false

	notices := &parallelSyncNotices{}
	stderr := captureStderr(t, func() {
		opts := notices.optionsFor("box-a")
		opts.Invalidated("node_modules", "package-lock.json")
		opts.Warn(rrsync.SyncWarning{Message: "different source", Details: map[string]interface{}{"reason": "source_mismatch"}})
		opts.Pruned("~/rr/proj@old")
	})

	events := parsePhaseEvents(t, stderr)
	require.Len(t, events, 3)
	want := []string{"invalidated", "warn", "pruned"}
	for i, ev := range events {
		assert.Equal(t, "sync", ev.Phase)
		assert.Equal(t, want[i], ev.Status)
		assert.Equal(t, "box-a", ev.Host)
	}
	assert.Equal(t, "node_modules", events[0].Details["dir"])
	assert.Equal(t, "source_mismatch", events[1].Details["reason"])
	assert.Equal(t, "~/rr/proj@old", events[2].Details["dir"])
}

// TestParallelSyncNotices_PrettyDeferred checks pretty-mode notices wait
// for flush, so they don't print over the live progress display.
func TestParallelSyncNotices_PrettyDeferred(t *testing.T) {
	oldPretty := prettyMode
	defer func() { prettyMode = oldPretty }()
	prettyMode = true

	notices := &parallelSyncNotices{}
	during := captureStdout(t, func() {
		opts := notices.optionsFor("box-a")
		opts.Invalidated("node_modules", "package-lock.json")
		opts.Pruned("~/rr/proj@old")
	})
	assert.Empty(t, during)

	after := captureStdout(t, notices.flush)
	assert.Contains(t, after, "Invalidating stale node_modules (package-lock.json changed)")
	assert.Contains(t, after, "Pruned stale worktree dir ~/rr/proj@old")

	assert.Empty(t, captureStdout(t, notices.flush), "flush prints each notice once")
}
