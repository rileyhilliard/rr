//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signalCountingScript records each SIGINT and SIGTERM it gets in dir/ints
// and dir/terms, writes its pid to dir/pid once the traps are set, and
// exits 0 about a second after the first signal. That second is the window
// in which a signal rr forwarded would arrive on top of the first.
func signalCountingScript(dir string) string {
	ints := filepath.Join(dir, "ints")
	terms := filepath.Join(dir, "terms")
	return `trap 'echo int >> "` + ints + `"; got=1' INT
trap 'echo term >> "` + terms + `"; got=1' TERM
echo $$ > "` + filepath.Join(dir, "pid") + `"
while [ -z "$got" ]; do sleep 0.05; done
i=0; while [ $i -lt 20 ]; do sleep 0.05; i=$((i+1)); done
exit 0`
}

// startBareLocalRR runs `rr run --local script` with no local host
// configured, in a process group of its own, as the shell puts a foreground
// job. The command rr starts shares that group, so a signal sent to the
// group reaches rr and the command together, as Ctrl+C in a terminal does.
// It returns once the command has written its pid.
func startBareLocalRR(t *testing.T, script, pidFile string) *rrProcess {
	t.Helper()
	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".rr.yaml"), []byte("version: 1\n"), 0o644))

	p := &rrProcess{cmd: exec.Command(os.Args[0], "run", "--local", script), stderr: &lockedBuffer{}, done: make(chan error, 1)}
	p.cmd.Dir = projectDir
	p.cmd.Env = append(os.Environ(), runCLIEnv+"=1", "SHELL=/bin/sh")
	p.cmd.Stdout = &lockedBuffer{}
	p.cmd.Stderr = p.stderr
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, p.cmd.Start())
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
	})

	require.Eventually(t, func() bool { return writtenPid(pidFile) > 0 },
		10*time.Second, 10*time.Millisecond, "the command started; stderr:\n%s", p.stderr.String())
	return p
}

// writtenPid returns the pid written to path, or 0 if it isn't fully
// written yet.
func writtenPid(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasSuffix(string(data), "\n") {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// readPid returns the pid written to path, failing the test if there's none.
func readPid(t *testing.T, path string) int {
	t.Helper()
	pid := writtenPid(path)
	require.NotZero(t, pid, "pid in %s", path)
	return pid
}

// signalLines returns the lines the script wrote to path, or none if it
// wrote nothing there.
func signalLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Fields(string(data))
}

// One Ctrl+C on a bare local run reaches the command once. The terminal
// already delivers it to the whole foreground group, the command included,
// so rr passing it on as well would be a second SIGINT: pytest's teardown
// gets a second KeyboardInterrupt and docker compose force-kills.
func TestRun_BareLocalCtrlCInterruptsOnce(t *testing.T) {
	dir := t.TempDir()
	p := startBareLocalRR(t, signalCountingScript(dir), filepath.Join(dir, "pid"))

	// Ctrl+C reaches every process in the foreground group at once, in no
	// set order. Delivering it to the command first and to rr once the
	// command has handled it is one such order, and the one where a second
	// SIGINT from rr can't merge with the first while that's still pending.
	ints := filepath.Join(dir, "ints")
	require.NoError(t, syscall.Kill(readPid(t, filepath.Join(dir, "pid")), syscall.SIGINT))
	require.Eventually(t, func() bool { return len(signalLines(t, ints)) > 0 },
		5*time.Second, 10*time.Millisecond, "the command handled the Ctrl+C")
	require.NoError(t, p.cmd.Process.Signal(syscall.SIGINT))
	_ = p.wait(t, 10*time.Second)

	assert.Equal(t, []string{"int"}, signalLines(t, ints),
		"the command got exactly one SIGINT; stderr:\n%s", p.stderr.String())
}

// A SIGTERM sent to rr alone, which the command never sees, still stops the
// command: rr interrupts it.
func TestRun_BareLocalSIGTERMStopsCommand(t *testing.T) {
	dir := t.TempDir()
	p := startBareLocalRR(t, signalCountingScript(dir), filepath.Join(dir, "pid"))

	require.NoError(t, p.cmd.Process.Signal(syscall.SIGTERM))
	_ = p.wait(t, 10*time.Second)

	assert.Equal(t, []string{"int"}, signalLines(t, filepath.Join(dir, "ints")),
		"rr interrupted the command; stderr:\n%s", p.stderr.String())
}
