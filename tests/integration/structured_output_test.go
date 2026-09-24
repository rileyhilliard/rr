package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rr binary for subprocess tests, built once per package run. It lives
// outside t.TempDir() so every test can share it; TestMain removes it.
var (
	rrBinOnce gosync.Once
	rrBinDir  string
	rrBinPath string
	rrBinErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if rrBinDir != "" {
		_ = os.RemoveAll(rrBinDir)
	}
	os.Exit(code)
}

// buildRRBinary builds ./cmd/rr once and returns the binary's path. Tests
// that check what rr prints should run this binary as a subprocess: that
// catches every writer, whatever fd it captured, which swapping os.Stderr
// in-process doesn't.
func buildRRBinary(t *testing.T) string {
	t.Helper()
	rrBinOnce.Do(func() {
		rrBinDir, rrBinErr = os.MkdirTemp("", "rr-bin-")
		if rrBinErr != nil {
			return
		}
		rrBinPath = filepath.Join(rrBinDir, "rr")
		out, err := exec.Command("go", "build", "-o", rrBinPath, "github.com/rileyhilliard/rr/cmd/rr").CombinedOutput()
		if err != nil {
			rrBinErr = fmt.Errorf("go build failed: %w\n%s", err, out)
		}
	})
	require.NoError(t, rrBinErr)
	return rrBinPath
}

// seedKnownHosts writes the test server's host keys to <home>/.ssh/known_hosts.
//
// The child rr doesn't inherit the in-process sshutil.StrictHostKeyChecking =
// false that setupParallelSSHHome sets, and rr's Go SSH client ignores
// StrictHostKeyChecking in ~/.ssh/config: it always checks
// ~/.ssh/known_hosts. Seeding that file with ssh-keyscan keeps the child on
// rr's default, strict path, so any host-key warning it would print to a real
// user shows up here too. (--no-strict-host-key-checking would also work but
// skips that path.) rsync goes through system ssh, which the temp ssh config
// already tells not to check.
func seedKnownHosts(t *testing.T, home string) {
	t.Helper()
	sshHost, sshPort := parseHostPort(GetTestSSHHost())
	args := []string{"-T", "5"}
	if sshPort != "" {
		args = append(args, "-p", sshPort)
	}
	args = append(args, sshHost)
	var stderr bytes.Buffer
	cmd := exec.Command("ssh-keyscan", args...)
	cmd.Stderr = &stderr
	keys, err := cmd.Output()
	require.NoError(t, err, "ssh-keyscan failed: %s", stderr.String())
	require.NotEmpty(t, keys, "ssh-keyscan returned no host keys: %s", stderr.String())
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), keys, 0o600))
}

// runRRBinary runs the rr binary in dir with HOME set to home and returns
// stdout, stderr, and the exit code.
func runRRBinary(t *testing.T, bin, dir, home string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else {
		require.NoError(t, err, "failed to start rr")
	}
	return stdout.String(), stderr.String(), code
}

// TestStructuredOutput_Contract runs the real rr binary in its default
// (structured) mode and checks the output contract: every stderr line is a
// JSON event, and stdout carries only the command's own output.
func TestStructuredOutput_Contract(t *testing.T) {
	// Build before setupParallelSSHHome points HOME at a temp dir, or go
	// build fills that dir with a read-only module cache.
	RequireSSH(t)
	bin := buildRRBinary(t)
	conn := GetSSHConnection(t)
	RequireRemoteRsync(t, conn)
	home := setupParallelSSHHome(t)
	seedKnownHosts(t, home)

	remoteDir := fmt.Sprintf("/tmp/rr-structured-%d", time.Now().UnixNano())
	lockDir := remoteDir + "-locks"
	t.Cleanup(func() {
		CleanupRemoteDir(t, conn, remoteDir)
		CleanupRemoteDir(t, conn, lockDir)
	})

	globalCfg := fmt.Sprintf("version: 1\nhosts:\n  h:\n    ssh: [%s]\n    dir: %s\n", parallelTestAlias, remoteDir)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".rr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".rr", "config.yaml"), []byte(globalCfg), 0o644))

	projectCfg := fmt.Sprintf(`version: 1
host: h
lock:
  dir: %s
tasks:
  a:
    run: echo a
  b:
    run: echo b
  deps:
    depends: [a, b]
  steps:
    steps:
      - name: one
        run: echo one
      - name: two
        run: echo two
  both:
    parallel: [a, b]
`, lockDir)
	projectDir := TempSyncDirWithFiles(t, map[string]string{".rr.yaml": projectCfg})

	tests := []struct {
		name       string
		args       []string
		setup      func(t *testing.T)
		wantStdout string
	}{
		{name: "run", args: []string{"run", "echo out"}, wantStdout: "out\n"},
		{name: "depends task", args: []string{"deps"}, wantStdout: "a\nb\n"},
		{name: "multi-step task", args: []string{"steps"}, wantStdout: "one\ntwo\n"},
		// A parallel task prints nothing on stdout without --stream; the
		// subtasks' output goes to the log dir named in the result event.
		{name: "parallel task", args: []string{"both"}, wantStdout: ""},
		{name: "sync", args: []string{"sync"}, wantStdout: ""},
		{
			name: "run steals dead-holder lock", args: []string{"run", "echo out"}, wantStdout: "out\n",
			setup: func(t *testing.T) { plantDeadHolderLock(t, conn, lockDir) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}

			stdout, stderr, code := runRRBinary(t, bin, projectDir, home, tt.args...)
			require.Equal(t, 0, code, "rr %v failed\nstdout:\n%s\nstderr:\n%s", tt.args, stdout, stderr)

			lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
			require.NotEmpty(t, stderr, "structured mode should emit events on stderr")
			for _, line := range lines {
				var ev map[string]interface{}
				assert.NoError(t, json.Unmarshal([]byte(line), &ev), "non-JSON line on stderr: %q", line)
			}

			assert.Equal(t, tt.wantStdout, stdout, "stdout should be only the command's output")
		})
	}
}
