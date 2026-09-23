package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// killSSHSession kills the sshd process serving the command's own session,
// which drops the connection mid-command. It walks up from the shell because
// rr may wrap the command in more than one shell.
const killSSHSession = `p=$$; while [ "$p" -gt 1 ]; do ` +
	`case "$(ps -o comm= -p "$p")" in sshd*) kill -9 "$p"; break;; esac; ` +
	`p=$(ps -o ppid= -p "$p" | tr -d ' '); done; sleep 5`

// TestConnectionLost_EndsInResult checks the contract for a single run whose
// connection drops mid-command: a result event with exit_code -1, the error
// under details.error, and log_file still set where the path keeps one, with
// rr exiting 255 (-1).
func TestConnectionLost_EndsInResult(t *testing.T) {
	home := setupParallelSSHHome(t)
	globalCfg := fmt.Sprintf("version: 1\nhosts:\n  h:\n    ssh: [%s]\n    dir: /tmp/rr-conn-lost\n", parallelTestAlias)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(globalCfg), 0o644))

	projectCfg := fmt.Sprintf(`version: 1
host: h
tasks:
  first:
    run: %q
  second:
    run: "true"
    depends: [first]
`, killSSHSession)

	tests := []struct {
		name    string
		run     func() (int, error)
		wantLog bool
	}{
		// SkipLock: the dead connection can't release its lock, and since the
		// holder is this still-running test process, the next subtest would
		// wait for it to go stale.
		{"rr run", func() (int, error) {
			return cli.Run(cli.RunOptions{Command: killSSHSession, Quiet: true, SkipLock: true})
		}, true},
		// Dependency chains don't write a run log.
		{"task with depends", func() (int, error) {
			return cli.RunTask(cli.TaskOptions{TaskName: "second", Quiet: true, SkipLock: true})
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(TempSyncDirWithFiles(t, map[string]string{".rr.yaml": projectCfg}))

			var code int
			var runErr error
			stderr := captureStderrLines(t, func() { code, runErr = tt.run() })

			require.NoError(t, runErr, "a dropped connection ends in a result, not an error envelope")
			assert.Equal(t, -1, code)

			result := findResultEvent(t, stderr)
			assert.EqualValues(t, -1, result["exit_code"])
			details, _ := result["details"].(map[string]interface{})
			errDetail, ok := details["error"].(map[string]interface{})
			require.True(t, ok, "result must carry details.error; got %v", details)
			assert.Equal(t, "SSH_CONNECTION_FAILED", errDetail["code"])
			assert.Contains(t, errDetail["message"], "Lost the connection")
			if tt.wantLog {
				assert.NotEmpty(t, details["log_file"])
			}
		})
	}
}

// captureStderrLines runs fn with os.Stderr redirected and returns what it
// wrote, line by line.
func captureStderrLines(t *testing.T, fn func()) []string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	func() {
		defer func() { os.Stderr = orig }()
		fn()
	}()
	require.NoError(t, w.Close())
	out := <-done

	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// findResultEvent returns the final "type":"result" event from stderr lines.
func findResultEvent(t *testing.T, lines []string) map[string]interface{} {
	t.Helper()
	for _, line := range lines {
		var ev map[string]interface{}
		if json.Unmarshal([]byte(line), &ev) == nil && ev["type"] == "result" {
			return ev
		}
	}
	require.Fail(t, "no result event", "stderr:\n%s", strings.Join(lines, "\n"))
	return nil
}
