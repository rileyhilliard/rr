package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/cli"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/parallel"
	"github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parallelTestAlias is the SSH alias parallel-run tests put in their own
// ~/.ssh/config. Parallel workers connect by alias (like a real host entry),
// and rsync reuses the same alias, so both need to resolve it.
const parallelTestAlias = "rr-parallel-test"

// setupParallelSSHHome points HOME at a temp dir holding an SSH config that
// maps parallelTestAlias to the test SSH server, so the orchestrator's host
// selector and rsync both reach it the way they would a real configured host.
// Returns the fake HOME. Call after GetSSHConnection, which resets
// sync.SSHConfigFile in its own cleanup.
func setupParallelSSHHome(t *testing.T) string {
	t.Helper()
	RequireSSH(t)

	sshHost, sshPort := parseHostPort(GetTestSSHHost())
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))

	sshConfig := fmt.Sprintf("Host %s\n  HostName %s\n", parallelTestAlias, sshHost)
	if sshPort != "" {
		sshConfig += fmt.Sprintf("  Port %s\n", sshPort)
	}
	if user := GetTestSSHUser(); user != "" {
		sshConfig += fmt.Sprintf("  User %s\n", user)
	}
	if key := GetTestSSHKey(); key != "" {
		sshConfig += fmt.Sprintf("  IdentityFile %s\n", key)
	}
	sshConfig += "  StrictHostKeyChecking no\n  UserKnownHostsFile /dev/null\n"

	configPath := filepath.Join(sshDir, "config")
	require.NoError(t, os.WriteFile(configPath, []byte(sshConfig), 0600))

	t.Setenv("HOME", home)
	sshutil.StrictHostKeyChecking = false
	sync.SSHConfigFile = configPath
	t.Cleanup(func() {
		sshutil.StrictHostKeyChecking = true
		sync.SSHConfigFile = ""
	})
	return home
}

// runOrchestrator runs tasks on a single test host and returns the result.
func runOrchestrator(t *testing.T, tasks []parallel.TaskInfo, h config.Host, resolved *config.ResolvedConfig, cfg parallel.Config) *parallel.Result {
	t.Helper()
	hosts := map[string]config.Host{"test-host": h}
	orch := parallel.NewOrchestrator(tasks, hosts, []string{"test-host"}, resolved, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := orch.Run(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

// projectConfig returns a project config with locking off (the tests run
// several hosts against one SSH server, which share a lock path) and the
// given sync config.
func projectConfig(syncCfg config.SyncConfig) *config.Config {
	proj := config.DefaultConfig()
	proj.Lock.Enabled = false
	proj.Sync = syncCfg
	return proj
}

// TestParallelSyncUsesSyncOptions checks that a parallel worker's sync goes
// through the caller's SyncOptions: the callback is asked for the host's
// options and lockfile invalidation reports through it, the same as a
// single-task sync.
func TestParallelSyncUsesSyncOptions(t *testing.T) {
	conn := GetSSHConnection(t)
	RequireRemoteRsync(t, conn)
	setupParallelSSHHome(t)

	remoteDir := conn.Host.Dir
	t.Cleanup(func() { CleanupRemoteDir(t, conn, remoteDir) })

	// A remote node_modules older than the local lockfile is stale. It's in
	// preserve, so only lockfile invalidation (not rsync --delete) removes it.
	EnsureRemoteDir(t, conn, remoteDir+"/node_modules")
	CreateRemoteFile(t, conn, remoteDir+"/node_modules/stale.txt", "old install")
	_, _, code, err := conn.Client.Exec(fmt.Sprintf("touch -d '2000-01-01' %q", remoteDir+"/node_modules"))
	require.NoError(t, err)
	require.Equal(t, 0, code)

	localDir := TempSyncDirWithFiles(t, map[string]string{
		"package-lock.json": `{"lockfileVersion": 3}`,
		"app.txt":           "synced by parallel worker",
	})

	syncCfg := config.DefaultConfig().Sync
	syncCfg.Preserve = append(syncCfg.Preserve, "node_modules/")
	syncCfg.Invalidations = []config.LockfileInvalidation{
		{Lockfile: "package-lock.json", Dirs: []string{"node_modules"}},
	}
	resolved := &config.ResolvedConfig{Project: projectConfig(syncCfg), ProjectRoot: localDir}

	var (
		mu          gosync.Mutex
		askedFor    []string
		invalidated []string
	)
	cfg := parallel.Config{
		OutputMode: parallel.OutputQuiet,
		SyncOptions: func(hostName string) *sync.SyncOptions {
			mu.Lock()
			askedFor = append(askedFor, hostName)
			mu.Unlock()
			return &sync.SyncOptions{
				Invalidated: func(dir, lockfile string) {
					mu.Lock()
					invalidated = append(invalidated, dir+":"+lockfile)
					mu.Unlock()
				},
			}
		},
	}

	tasks := []parallel.TaskInfo{
		{Name: "read-a", Index: 0, Command: "cat app.txt"},
		{Name: "read-b", Index: 1, Command: "cat app.txt"},
	}
	h := config.Host{SSH: []string{parallelTestAlias}, Dir: remoteDir}
	result := runOrchestrator(t, tasks, h, resolved, cfg)

	for _, tr := range result.TaskResults {
		assert.Contains(t, string(tr.Output), "synced by parallel worker", "subtask %s output", tr.TaskName)
	}
	require.Equal(t, 2, result.Passed, "both subtasks should pass")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"test-host"}, askedFor, "host syncs once, through the caller's options")
	assert.Equal(t, []string{"node_modules:package-lock.json"}, invalidated)
	assert.False(t, RemoteDirExists(t, conn, remoteDir+"/node_modules"), "stale node_modules should be removed")
}

// TestParallelRunRemoteDirWithShellMetacharacters checks that subtasks run
// in a remote dir whose path has a space, a quote, and a $ in it.
func TestParallelRunRemoteDirWithShellMetacharacters(t *testing.T) {
	conn := GetSSHConnection(t)
	RequireRemoteRsync(t, conn)
	setupParallelSSHHome(t)

	remoteDir := fmt.Sprintf("/tmp/rr it's $HOME dir-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _, _, _ = conn.Client.Exec("rm -rf '" + strings.ReplaceAll(remoteDir, "'", `'\''`) + "'") })

	localDir := TempSyncDirWithFiles(t, map[string]string{"marker.txt": "metachar dir ok"})
	resolved := &config.ResolvedConfig{
		Project:     projectConfig(config.DefaultConfig().Sync),
		ProjectRoot: localDir,
	}

	tasks := []parallel.TaskInfo{
		{Name: "pwd", Index: 0, Command: "pwd"},
		{Name: "cat", Index: 1, Command: "cat marker.txt"},
	}
	h := config.Host{SSH: []string{parallelTestAlias}, Dir: remoteDir}
	result := runOrchestrator(t, tasks, h, resolved, parallel.Config{OutputMode: parallel.OutputQuiet})

	outputs := map[string]string{}
	for _, tr := range result.TaskResults {
		outputs[tr.TaskName] = string(tr.Output)
	}
	require.Equal(t, 2, result.Passed, "both subtasks should pass, output: %v", outputs)
	assert.Contains(t, outputs["pwd"], remoteDir, "subtask should run in the literal remote dir")
	assert.Contains(t, outputs["cat"], "metachar dir ok", "synced file should be readable from the remote dir")
}

// TestParallelEnvExpandsLikeSingleTasks checks defaults.env values reach a
// remote parallel subtask the way they reach a single task: $HOME expands on
// the remote, while quotes and backticks arrive literally.
func TestParallelEnvExpandsLikeSingleTasks(t *testing.T) {
	conn := GetSSHConnection(t)
	RequireRemoteRsync(t, conn)
	setupParallelSSHHome(t)

	remoteDir := conn.Host.Dir
	t.Cleanup(func() { CleanupRemoteDir(t, conn, remoteDir) })

	remoteHome, _, code, err := conn.Client.Exec("printf %s \"$HOME\"")
	require.NoError(t, err)
	require.Equal(t, 0, code)

	proj := projectConfig(config.DefaultConfig().Sync)
	proj.Defaults.Env = map[string]string{
		"RR_BIN": "$HOME/bin",
		"RR_MSG": "say \"hi\" `nope`",
	}
	resolved := &config.ResolvedConfig{Project: proj, ProjectRoot: TempSyncDirWithFiles(t, map[string]string{"a.txt": "a"})}

	tasks := []parallel.TaskInfo{{Name: "env", Index: 0, Command: `printf '%s|%s' "$RR_BIN" "$RR_MSG"`}}
	h := config.Host{SSH: []string{parallelTestAlias}, Dir: remoteDir}
	result := runOrchestrator(t, tasks, h, resolved, parallel.Config{OutputMode: parallel.OutputQuiet})

	require.Len(t, result.TaskResults, 1)
	tr := result.TaskResults[0]
	require.Equal(t, 0, tr.ExitCode, string(tr.Output))
	assert.Equal(t, string(remoteHome)+"/bin|say \"hi\" `nope`", strings.TrimSpace(string(tr.Output)))
}

// TestParallelSubtaskPull runs a parallel task through the CLI entry point
// and checks each subtask's `pull:` lands in <dest>/<subtask>/ after the run,
// including for the subtask that failed. Both subtasks write the same
// relative path on different hosts, so the per-subtask dirs are what keep
// them apart locally.
func TestParallelSubtaskPull(t *testing.T) {
	conn := GetSSHConnection(t)
	RequireRemoteRsync(t, conn)
	home := setupParallelSSHHome(t)

	remoteBase := fmt.Sprintf("/tmp/rr-parallel-pull-%d", time.Now().UnixNano())
	t.Cleanup(func() { CleanupRemoteDir(t, conn, remoteBase) })

	globalCfg := fmt.Sprintf(`version: 1
hosts:
  host-a:
    ssh: [%[1]s]
    dir: %[2]s/a
  host-b:
    ssh: [%[1]s]
    dir: %[2]s/b
`, parallelTestAlias, remoteBase)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(globalCfg), 0644))

	pullDest := filepath.Join(t.TempDir(), "artifacts")
	projectCfg := fmt.Sprintf(`version: 1
hosts: [host-a, host-b]
lock:
  enabled: false
tasks:
  shard-pass:
    run: mkdir -p out && echo from-pass > out/report.txt
    hosts: [host-a]
    pull:
      - src: out/report.txt
        dest: %[1]s
  shard-fail:
    run: mkdir -p out && echo from-fail > out/report.txt && exit 3
    hosts: [host-b]
    pull:
      - src: out/report.txt
        dest: %[1]s
  shards:
    parallel: [shard-pass, shard-fail]
`, pullDest)
	projectDir := TempSyncDirWithFiles(t, map[string]string{".rr.yaml": projectCfg})
	t.Chdir(projectDir)

	exitCode, err := cli.RunParallelTask(cli.ParallelTaskOptions{
		TaskName: "shards",
		Quiet:    true,
		NoLogs:   true,
	})
	require.NoError(t, err)
	assert.NotEqual(t, 0, exitCode, "a failed subtask should fail the run")

	tests := []struct {
		subtask string
		want    string
	}{
		{subtask: "shard-pass", want: "from-pass"},
		{subtask: "shard-fail", want: "from-fail"},
	}
	for _, tt := range tests {
		t.Run(tt.subtask, func(t *testing.T) {
			got, err := os.ReadFile(filepath.Join(pullDest, tt.subtask, "report.txt"))
			require.NoError(t, err, "expected %s's report pulled into %s/%s/", tt.subtask, pullDest, tt.subtask)
			assert.Equal(t, tt.want+"\n", string(got))
		})
	}
	_, err = os.Stat(filepath.Join(pullDest, "report.txt"))
	assert.True(t, os.IsNotExist(err), "files should land under the subtask dir, not dest itself")
}
