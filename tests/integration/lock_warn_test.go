package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/cli"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLockSteal_ReportedAsEvent plants a lock held by a dead rr process on
// this machine. rr steals it right away, and in structured mode the notice
// is one lock warn event naming the host, with nothing else on stderr.
func TestLockSteal_ReportedAsEvent(t *testing.T) {
	conn := GetSSHConnection(t)
	home := setupParallelSSHHome(t)
	// Unique per run, so two test processes sharing the SSH host don't touch
	// each other's lock or synced files.
	suffix := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	lockDir := "/tmp/rr-lockwarn-" + suffix
	projDir := "/tmp/rr-lockwarn-proj-" + suffix
	t.Cleanup(func() {
		CleanupRemoteDir(t, conn, lockDir)
		CleanupRemoteDir(t, conn, projDir)
	})

	globalCfg := fmt.Sprintf("version: 1\nhosts:\n  h:\n    ssh: [%s]\n    dir: %s\n", parallelTestAlias, projDir)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(globalCfg), 0o644))
	projectCfg := fmt.Sprintf("version: 1\nhost: h\nlock:\n  dir: %s\ntasks:\n  x:\n    run: \"true\"\n  both:\n    parallel: [x]\n", lockDir)

	tests := []struct {
		name string
		run  func() (int, error)
	}{
		{"rr run", func() (int, error) { return cli.Run(cli.RunOptions{Command: "true", Quiet: true}) }},
		{"parallel task", func() (int, error) {
			return cli.RunParallelTask(cli.ParallelTaskOptions{TaskName: "both", NoLogs: true})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(TempSyncDirWithFiles(t, map[string]string{".rr.yaml": projectCfg}))
			plantDeadHolderLock(t, conn, lockDir)

			var code int
			var runErr error
			lines := captureStderrLines(t, func() { code, runErr = tt.run() })

			require.NoError(t, runErr)
			assert.Equal(t, 0, code)
			var warns int
			for _, line := range lines {
				var ev map[string]interface{}
				require.NoError(t, json.Unmarshal([]byte(line), &ev), "non-JSON line on stderr: %q", line)
				if ev["phase"] == "lock" && ev["status"] == "warn" {
					warns++
					assert.Equal(t, "h", ev["host"])
					assert.Contains(t, ev["details"].(map[string]interface{})["message"], "dead local process")
				}
			}
			assert.Equal(t, 1, warns)
		})
	}
}

// plantDeadHolderLock creates <lockDir>/rr.lock held by an rr process on
// this machine that has already exited.
func plantDeadHolderLock(t *testing.T, conn *host.Connection, lockDir string) {
	t.Helper()
	proc := exec.Command("true")
	require.NoError(t, proc.Run())

	info, err := lock.NewLockInfo("rr earlier-run")
	require.NoError(t, err)
	info.PID = proc.Process.Pid
	data, err := info.Marshal()
	require.NoError(t, err)

	cmd := fmt.Sprintf("mkdir -p %[1]s/rr.lock && cat > %[1]s/rr.lock/info.json <<'EOF'\n%[2]s\nEOF", lockDir, data)
	_, stderr, code, err := conn.Client.Exec(cmd)
	require.NoError(t, err)
	require.Equal(t, 0, code, string(stderr))
}
