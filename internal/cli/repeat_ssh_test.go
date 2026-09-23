package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/util"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repeatSSHAlias is the SSH alias the --repeat tests put in their own
// ~/.ssh/config, pointing at the RR_TEST_SSH_HOST server.
const repeatSSHAlias = "rr-repeat-test"

// withTestSSHHost points HOME at a temp dir whose ~/.ssh/config maps
// repeatSSHAlias to the RR_TEST_SSH_* server, so host selection and rsync
// reach it like a configured host, and returns a client for setting up the
// remote. Skips when the SSH test server isn't configured.
func withTestSSHHost(t *testing.T) (home string, client *sshutil.Client) {
	t.Helper()
	addr, key := os.Getenv("RR_TEST_SSH_HOST"), os.Getenv("RR_TEST_SSH_KEY")
	if addr == "" || key == "" {
		t.Skip("RR_TEST_SSH_HOST and RR_TEST_SSH_KEY not set (see scripts/ci-ssh-server.sh)")
	}

	sshutil.StrictHostKeyChecking = false
	t.Cleanup(func() { sshutil.StrictHostKeyChecking = true })

	client, err := sshutil.Dial(addr, 10*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	hostName, port := addr, ""
	if i := strings.LastIndex(addr, ":"); i != -1 {
		hostName, port = addr[:i], addr[i+1:]
	}
	sshConfig := fmt.Sprintf("Host %s\n  HostName %s\n  IdentityFile %s\n", repeatSSHAlias, hostName, key)
	if port != "" {
		sshConfig += fmt.Sprintf("  Port %s\n", port)
	}
	if user := os.Getenv("RR_TEST_SSH_USER"); user != "" {
		sshConfig += fmt.Sprintf("  User %s\n", user)
	}
	sshConfig += "  StrictHostKeyChecking no\n  UserKnownHostsFile /dev/null\n"

	home = t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ssh"), 0o700))
	configPath := filepath.Join(home, ".ssh", "config")
	require.NoError(t, os.WriteFile(configPath, []byte(sshConfig), 0o600))
	rrsync.SSHConfigFile = configPath
	t.Cleanup(func() { rrsync.SSHConfigFile = "" })
	return home, client
}

// TestRepeat_StructuredSyncNotices checks that `rr run --repeat` and
// `rr <task> --repeat` sync through the same callbacks as a single run: in
// structured mode a lockfile invalidation is a sync event on stderr, and
// nothing but the command's own output reaches stdout.
func TestRepeat_StructuredSyncNotices(t *testing.T) {
	tests := []struct {
		name string
		run  func() (int, error)
	}{
		{name: "rr run --repeat", run: func() (int, error) { return runRepeated("true", 2, "", "", false) }},
		{name: "rr <task> --repeat", run: func() (int, error) { return runTaskRepeated("noop", 2, "", "", false) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withStructuredOutput(t)
			home, client := withTestSSHHost(t)

			remoteDir := fmt.Sprintf("/tmp/rr-cli-repeat-%d", time.Now().UnixNano())
			t.Cleanup(func() { _, _, _, _ = client.Exec("rm -rf " + util.ShellQuote(remoteDir)) })
			nodeModules := util.ShellQuote(remoteDir + "/node_modules")
			_, stderr, code, err := client.Exec("mkdir -p " + nodeModules + " && touch -d '2000-01-01' " + nodeModules)
			require.NoError(t, err)
			require.Equal(t, 0, code, "remote setup: %s", stderr)

			require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(fmt.Sprintf(`version: 1
hosts:
  box:
    ssh: [%s]
    dir: %s
`, repeatSSHAlias, remoteDir)), 0o644))
			dir := inProject(t, `version: 1
hosts: [box]
lock:
  enabled: false
sync:
  preserve: [node_modules/]
  invalidations:
    - lockfile: package-lock.json
      dirs: [node_modules/]
tasks:
  noop:
    run: "true"
`)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644))

			var exitCode int
			var runErr error
			stdout, stderrOut := runCaptured(t, func() { exitCode, runErr = tt.run() })

			require.NoError(t, runErr)
			assert.Equal(t, 0, exitCode)
			assert.Empty(t, stdout, "structured mode must not print plain text on stdout")
			invalidated := eventsWith(parseEvents(t, stderrOut), "sync", "invalidated")
			require.Len(t, invalidated, 1, "the host syncs once for both runs")
			assert.Equal(t, "box", invalidated[0].Host)
			assert.Equal(t, "node_modules/", invalidated[0].Details["dir"])
		})
	}
}
