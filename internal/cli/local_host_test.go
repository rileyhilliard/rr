package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// localHostWorkflow builds a workflow context whose selector holds hosts in
// order, with locks under a temp dir.
func localHostWorkflow(t *testing.T, hosts map[string]config.Host, order []string, fallback config.LocalFallbackMode) (*WorkflowContext, config.LockConfig) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	lockCfg := config.LockConfig{
		Enabled:     true,
		Timeout:     time.Second,
		WaitTimeout: time.Nanosecond,
		Stale:       time.Minute,
		Dir:         filepath.Join(t.TempDir(), "locks"),
	}
	project := &config.Config{Lock: lockCfg, Hosts: order}
	if fallback != "" {
		project.LocalFallback = &fallback
	}
	sel := host.NewSelector(hosts)
	sel.SetHostOrder(order)
	sel.SetTimeout(2 * time.Second)
	ctx := &WorkflowContext{
		Resolved: &config.ResolvedConfig{
			Global:  &config.GlobalConfig{Version: 1, Hosts: hosts},
			Project: project,
		},
		selector: sel,
	}
	t.Cleanup(ctx.Close)
	return ctx, lockCfg
}

func TestFindAvailableHost_LocalHostFirstInOrder(t *testing.T) {
	hosts := map[string]config.Host{
		"dev":    {Local: true, Dir: t.TempDir()},
		"remote": {SSH: []string{"nonexistent-host-a-xxxx"}, Dir: "~"},
	}
	ctx, lockCfg := localHostWorkflow(t, hosts, []string{"dev", "remote"}, "")

	result, err := findAvailableHost(ctx, WorkflowOptions{Command: "go test ./..."})
	require.NoError(t, err)
	ctx.Lock = result.lock

	assert.Equal(t, "dev", result.conn.Name)
	assert.False(t, result.isLocal, "a local host is a host, not a fallback")
	assert.False(t, result.fellBack)
	require.NotNil(t, result.lock, "the local host is locked like any host")
	assert.Len(t, result.hostsState, 1, "the remote after it is never tried")

	_, statErr := os.Stat(filepath.Join(lockCfg.Dir, "rr.lock"))
	assert.NoError(t, statErr)
}

func TestFindAvailableHost_LocalHostAfterUnreachableRemote(t *testing.T) {
	hosts := map[string]config.Host{
		"dev":    {Local: true, Dir: t.TempDir()},
		"remote": {SSH: []string{"nonexistent-host-a-xxxx"}, Dir: "~"},
	}
	ctx, _ := localHostWorkflow(t, hosts, []string{"remote", "dev"}, "")

	result, err := findAvailableHost(ctx, WorkflowOptions{Command: "go test ./..."})
	require.NoError(t, err)
	ctx.Lock = result.lock

	assert.Equal(t, "dev", result.conn.Name)
	require.Len(t, result.hostsState, 2)
	assert.Equal(t, "remote", result.hostsState[0].hostName)
	assert.Error(t, result.hostsState[0].connErr)
	assert.NotNil(t, result.lock)
}

// When the local host is busy, falling back to local execution would put a
// second run on the machine its lock protects, so rr waits for a host and
// errors on timeout instead, whatever local_fallback says.
func TestFindAvailableHost_LockedLocalHostDoesNotFallBack(t *testing.T) {
	hosts := map[string]config.Host{"dev": {Local: true, Dir: t.TempDir()}}
	ctx, lockCfg := localHostWorkflow(t, hosts, []string{"dev"}, config.LocalFallbackAlways)

	holderConn, err := ctx.selector.SelectHost("dev")
	require.NoError(t, err)
	holder, err := lock.TryAcquire(holderConn, lockCfg, "rr test (other run)")
	require.NoError(t, err)
	defer holder.Release()

	result, err := findAvailableHost(ctx, WorkflowOptions{Command: "rr test"})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.IsCode(err, errors.ErrLock), "got %v", err)
	assert.Contains(t, err.Error(), "All hosts are locked")
}

// writeLocalHostConfigs writes a global config with a local host "dev" and a
// project config that uses it, and returns the project dir and lock dir.
func writeLocalHostConfigs(t *testing.T) (projectDir, lockDir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	global := `version: 1
hosts:
  dev:
    local: true
    setup_commands:
      - export FROM_SETUP=setup-ok
    env:
      FROM_HOST: env-ok
`
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(global), 0o644))

	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	lockBase := filepath.Join(t.TempDir(), "locks")
	project := `version: 1
hosts: [dev]
lock:
  dir: ` + lockBase + `
tasks:
  check:
    run: test -d ` + filepath.Join(lockBase, "rr.lock") + ` && echo "$FROM_HOST" > task.out
`
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "sub"), 0o755))
	return projectDir, filepath.Join(lockBase, "rr.lock")
}

// rr run on a local host: runs in the project dir (here, the caller's
// subdirectory of it) with the host's setup_commands, under the host lock,
// and reports the host by name.
func TestRun_LocalHost(t *testing.T) {
	projectDir, lockDir := writeLocalHostConfigs(t)
	t.Chdir(filepath.Join(projectDir, "sub"))

	var exitCode int
	var err error
	var events string
	captureStdout(t, func() {
		events = captureStderr(t, func() {
			exitCode, err = Run(RunOptions{
				Command: `test -d '` + lockDir + `' && echo "$FROM_SETUP" > run.out && pwd -P > pwd.out`,
			})
		})
	})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, events)

	got, err := os.ReadFile(filepath.Join(projectDir, "sub", "run.out"))
	require.NoError(t, err, "the command runs in the caller's dir of the local checkout")
	assert.Equal(t, "setup-ok\n", string(got))
	pwd, err := os.ReadFile(filepath.Join(projectDir, "sub", "pwd.out"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(projectDir, "sub")+"\n", string(pwd))

	assert.Contains(t, events, `"host":"dev"`)
	assert.Contains(t, events, `"phase":"lock"`, "the lock phase runs")
	assert.NotContains(t, events, `"local_fallback"`)
	_, statErr := os.Stat(lockDir)
	assert.True(t, os.IsNotExist(statErr), "the lock is released after the run")
}

func TestRunTask_LocalHost(t *testing.T) {
	projectDir, _ := writeLocalHostConfigs(t)
	t.Chdir(projectDir)

	var exitCode int
	var err error
	out := captureStdout(t, func() {
		captureStderr(t, func() {
			exitCode, err = RunTask(TaskOptions{TaskName: "check"})
		})
	})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, out)

	got, err := os.ReadFile(filepath.Join(projectDir, "task.out"))
	require.NoError(t, err)
	assert.Equal(t, "env-ok\n", string(got), "host env reaches the task")
}

func TestStatus_LocalHost(t *testing.T) {
	hosts := map[string]config.Host{"dev": {Local: true}}

	results := probeAllHosts(hosts)
	require.Contains(t, results, "dev")
	require.Len(t, results["dev"].Aliases, 1)
	assert.True(t, results["dev"].Aliases[0].Success, "a local host is always reachable")
	assert.Equal(t, host.LocalAlias, results["dev"].Aliases[0].SSHAlias)
	assert.Equal(t, &Selected{Host: "dev", Alias: host.LocalAlias}, findSelectedHost(results))

	mapping := buildProjectMapping(&config.GlobalConfig{Hosts: hosts})
	assert.NotContains(t, mapping.RemoteDirs, "dev", "a local host has no remote dir")
	assert.Equal(t, []string{"dev"}, mapping.InPlaceHosts)
	data, err := json.Marshal(mapping)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"in_place_hosts":["dev"]`)

	out := captureStdout(t, func() {
		require.NoError(t, outputStatusText(results, findSelectedHost(results), mapping))
	})
	assert.Contains(t, out, "runs in place on dev")
}

func TestConnectDoctorHosts_LocalHostIsNotDialed(t *testing.T) {
	hosts := map[string]config.Host{"dev": {Local: true}}
	dial := func(alias string, _ time.Duration) (*sshutil.Client, time.Duration, error) {
		t.Fatalf("dialed %s for a local host", alias)
		return nil, 0, nil
	}
	conns, dialErrs := connectDoctorHosts([]string{"dev"}, hosts, dial)
	defer closeDoctorConnections(conns)

	assert.Empty(t, dialErrs)
	require.Contains(t, conns, "dev")
	assert.Equal(t, host.LocalAlias, conns["dev"].Alias)
	assert.NotNil(t, conns["dev"].Client, "--path and --requirements checks run through it")
}

func TestUnlockHost_LocalHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	hostCfg := config.Host{Local: true}
	lockCfg := config.LockConfig{Enabled: true, Timeout: time.Second, Stale: time.Minute, Dir: filepath.Join(t.TempDir(), "locks")}

	assert.Equal(t, "not_locked", unlockHost("dev", hostCfg, lockCfg, false).Status)

	held, err := lock.TryAcquire(host.NewLocalHostConnection("dev", hostCfg), lockCfg, "rr test")
	require.NoError(t, err)
	defer held.StopHeartbeat()

	outcome := unlockHost("dev", hostCfg, lockCfg, false)
	assert.Equal(t, "released", outcome.Status, outcome.Error)
	assert.Contains(t, outcome.Holder, "rr test")
	_, statErr := os.Stat(filepath.Join(lockCfg.Dir, "rr.lock"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestCheckHosts_LocalHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local client uses a POSIX shell")
	}
	hosts := map[string]config.Host{"dev": {Local: true, Require: []string{"sh"}}}
	results := checkHosts([]string{"dev"}, hosts, nil, ProvisionOptions{})
	require.Len(t, results, 1)
	r := results[0]
	require.NoError(t, r.connErr)
	assert.True(t, r.connected, "a local host needs no SSH connection")
	require.NoError(t, r.checkErr)
	require.Len(t, r.results, 1)
	assert.True(t, r.results[0].Satisfied, "sh is on this machine")
}

// Prune checks nothing on a local host, so it reports it skipped, with the
// reason, in both output modes.
func TestPrune_LocalHostIsSkipped(t *testing.T) {
	outcome := pruneHost("dev", config.Host{Local: true}, t.TempDir(), false)
	assert.Equal(t, "skipped", outcome.Status)
	assert.Equal(t, "in_place", outcome.Reason)
	assert.Empty(t, outcome.Error)

	out := captureStdout(t, func() { printPruneOutcome(outcome, false) })
	assert.Contains(t, out, "dev: skipped (local host, runs in place; nothing synced to prune)")

	projectDir, _ := writeLocalHostConfigs(t)
	t.Chdir(projectDir)
	withStructuredOutput(t)
	var err error
	out = captureStdout(t, func() { err = pruneCommand(PruneOptions{}) })
	require.NoError(t, err)
	var envelope struct {
		Data struct {
			Hosts []hostPruneOutcome `json:"hosts"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &envelope), out)
	assert.Equal(t, []hostPruneOutcome{{Host: "dev", Status: "skipped", Reason: "in_place"}}, envelope.Data.Hosts)
}

// host list --json gives a local host an empty ssh_aliases list, not null,
// and every host picker labels it "local".
func TestHostListAndPickers_LocalHost(t *testing.T) {
	cfg := &config.GlobalConfig{Hosts: map[string]config.Host{
		"dev":  {Local: true},
		"mini": {SSH: []string{"mini-lan", "mini-ts"}, Dir: "~/rr"},
	}}
	out := captureStdout(t, func() { require.NoError(t, outputHostListJSON(cfg, nil, "/cfg")) })
	assert.Contains(t, out, `"ssh_aliases": []`)
	assert.NotContains(t, out, `"ssh_aliases": null`)

	assert.Equal(t, "dev - local", hostPickerLabel("dev", cfg.Hosts["dev"]))
	assert.Equal(t, "mini - mini-lan", hostPickerLabel("mini", cfg.Hosts["mini"]))
	assert.Equal(t, "bare", hostPickerLabel("bare", config.Host{}))
}
