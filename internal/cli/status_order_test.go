package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// writeStatusConfigs writes a global config with a local host (dev) and two
// unreachable remote hosts (aaa, zzz), plus a project config with the given
// hosts line, and runs the test from the project dir.
func writeStatusConfigs(t *testing.T, projectHosts string) {
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
  aaa:
    ssh: [rr-status-test-aaa.invalid]
    dir: ~/rr/aaa
  dev:
    local: true
  zzz:
    ssh: [rr-status-test-zzz.invalid]
    dir: ~/rr/zzz
`
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(global), 0o644))

	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	project := "version: 1\n" + projectHosts
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte(project), 0o644))
	t.Chdir(projectDir)
}

// runStatusJSON runs rr status in structured mode (what agents read) and
// decodes the envelope's data.
func runStatusJSON(t *testing.T) StatusOutput {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = statusCommand() })
	require.NoError(t, err)

	var env struct {
		Success bool         `json:"success"`
		Data    StatusOutput `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), out)
	require.True(t, env.Success, out)
	return env.Data
}

func statusHostNames(out StatusOutput) []string {
	names := make([]string, 0, len(out.Hosts))
	for _, h := range out.Hosts {
		names = append(names, h.Name)
	}
	return names
}

// Regression: status picked "Selected" by iterating a Go map (random) and
// listed every global host. It must list the project's hosts in the order
// runs try them, and select the first reachable one in that order.
func TestStatus_FollowsProjectHostOrder(t *testing.T) {
	tests := []struct {
		name         string
		projectHosts string
		wantOrder    []string
		wantScope    string
	}{
		{
			name:         "local host first",
			projectHosts: "hosts: [dev, aaa]\n",
			wantOrder:    []string{"dev", "aaa"},
			wantScope:    statusScopeProject,
		},
		{
			name:         "unreachable host first is listed first, the next reachable one is selected",
			projectHosts: "hosts: [zzz, dev]\n",
			wantOrder:    []string{"zzz", "dev"},
			wantScope:    statusScopeProject,
		},
		{
			name:         "singular host key",
			projectHosts: "host: dev\n",
			wantOrder:    []string{"dev"},
			wantScope:    statusScopeProject,
		},
		{
			name:         "no project host list uses every global host alphabetically",
			projectHosts: "",
			wantOrder:    []string{"aaa", "dev", "zzz"},
			wantScope:    statusScopeGlobal,
		},
		{
			name:         "local mode runs on the local host and tries no other",
			projectHosts: "local_fallback: true\n",
			wantOrder:    []string{"dev"},
			wantScope:    statusScopeLocal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeStatusConfigs(t, tt.projectHosts)

			got := runStatusJSON(t)

			assert.Equal(t, tt.wantOrder, statusHostNames(got), "hosts are listed in preference order, scoped to the project")
			assert.Equal(t, tt.wantScope, got.Scope)
			require.NotNil(t, got.Selected)
			assert.Equal(t, &Selected{Host: "dev", Alias: host.LocalAlias}, got.Selected, "the only reachable host is selected")
			for name := range got.Project.RemoteDirs {
				assert.Contains(t, tt.wantOrder, name, "project mapping lists only the hosts in scope")
			}
		})
	}
}

// Local mode with no local host configured: runs go to this machine as the
// bare "local" target, so that's what status reports, not a remote host.
func TestStatus_LocalModeWithoutLocalHost(t *testing.T) {
	writeStatusConfigs(t, "local_fallback: true\n")
	global := "version: 1\nhosts:\n  aaa:\n    ssh: [rr-status-test-aaa.invalid]\n    dir: ~/rr/aaa\n"
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".rr", "config.yaml"), []byte(global), 0o644))

	got := runStatusJSON(t)

	assert.Equal(t, []string{host.LocalAlias}, statusHostNames(got))
	assert.Equal(t, statusScopeLocal, got.Scope)
	assert.Equal(t, &Selected{Host: host.LocalAlias, Alias: host.LocalAlias}, got.Selected)
}

// Local mode with no global hosts at all used to fail with "No hosts
// configured". Runs go to this machine, so status reports it.
func TestStatus_LocalModeWithNoGlobalHosts(t *testing.T) {
	writeStatusConfigs(t, "local_fallback: true\n")
	require.NoError(t, os.WriteFile(filepath.Join(os.Getenv("HOME"), ".rr", "config.yaml"), []byte("version: 1\n"), 0o644))

	got := runStatusJSON(t)

	assert.Equal(t, []string{host.LocalAlias}, statusHostNames(got))
	assert.Equal(t, statusScopeLocal, got.Scope)
	assert.Equal(t, &Selected{Host: host.LocalAlias, Alias: host.LocalAlias}, got.Selected)
}

// The global-hosts fallback is for a skipped nested config only. A --config
// path that doesn't exist is an error, not a silent switch to global hosts.
func TestStatus_ExplicitMissingConfigFails(t *testing.T) {
	writeStatusConfigs(t, "hosts: [dev]\n")
	cfgFile = filepath.Join(t.TempDir(), "missing.yaml")
	t.Cleanup(func() { cfgFile = "" })

	var err error
	captureStdout(t, func() { err = statusCommand() })

	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrConfigNotFound), "got %v", err)
}

func TestStatus_ProjectNamesUnknownHost(t *testing.T) {
	writeStatusConfigs(t, "hosts: [dev, nope]\n")

	var err error
	captureStdout(t, func() { err = statusCommand() })

	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrHostNotFound), "got %v", err)
	assert.Contains(t, err.Error(), "nope")
}

func TestStatus_TextNamesWhereTheOrderComesFrom(t *testing.T) {
	tests := []struct {
		name         string
		projectHosts string
		want         string
	}{
		{"project list", "hosts: [dev]\n", "Hosts from .rr.yaml, in the order runs try them"},
		{"global list", "", "Set 'hosts:' in .rr.yaml"},
		{"local mode", "local_fallback: true\n", "Runs use this machine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeStatusConfigs(t, tt.projectHosts)
			prettyMode = true
			t.Cleanup(func() { prettyMode = false })
			var err error
			out := captureStdout(t, func() { err = statusCommand() })
			require.NoError(t, err)
			assert.Contains(t, out, "Selected: dev")
			assert.Contains(t, out, tt.want)
		})
	}
}

// With several reachable hosts, the earliest in order wins, not the
// alphabetically first.
func TestFindSelectedHost_FirstReachableInOrder(t *testing.T) {
	healthy := func(name string) probeResult {
		return probeResult{HostName: name, Aliases: []host.ProbeResult{{SSHAlias: name + "-lan", Success: true}}}
	}
	down := probeResult{HostName: "down", Aliases: []host.ProbeResult{{SSHAlias: "down-lan"}}}

	assert.Equal(t, "zeta", findSelectedHost([]probeResult{down, healthy("zeta"), healthy("alpha")}).Host)
	assert.Equal(t, "alpha", findSelectedHost([]probeResult{healthy("alpha"), healthy("zeta")}).Host)
}
