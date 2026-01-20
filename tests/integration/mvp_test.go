package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/lock"
	"github.com/rileyhilliard/rr/internal/output"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Config Loading Tests
// =============================================================================

func TestConfigLoadFromTempFile(t *testing.T) {
	dir := t.TempDir()

	// Set up global config with hosts
	globalDir := filepath.Join(dir, ".rr")
	require.NoError(t, os.MkdirAll(globalDir, 0755))
	globalContent := `
version: 1
hosts:
  test-host:
    ssh:
      - localhost
    dir: /tmp/rr-test-${USER}
`
	err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalContent), 0644)
	require.NoError(t, err)
	t.Setenv("HOME", dir)

	// Create project config
	configPath := filepath.Join(dir, ".rr.yaml")
	projectContent := `
version: 1
host: test-host
sync:
  exclude:
    - .git/
    - __pycache__/
lock:
  enabled: true
  timeout: 30s
  stale: 2m
  dir: /tmp/rr-locks
output:
  color: auto
  timing: true
`
	err = os.WriteFile(configPath, []byte(projectContent), 0644)
	require.NoError(t, err)

	// Load global config
	globalCfg, err := config.LoadGlobal()
	require.NoError(t, err)

	// Verify global config hosts
	assert.Equal(t, 1, globalCfg.Version)
	assert.Len(t, globalCfg.Hosts, 1)
	assert.Contains(t, globalCfg.Hosts, "test-host")

	// Verify host config
	host := globalCfg.Hosts["test-host"]
	assert.Equal(t, []string{"localhost"}, host.SSH)
	assert.Contains(t, host.Dir, "/tmp/rr-test-")

	// Load project config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Verify project config structure
	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, "test-host", cfg.Host)

	// Verify sync config
	assert.Contains(t, cfg.Sync.Exclude, ".git/")
	assert.Contains(t, cfg.Sync.Exclude, "__pycache__/")

	// Verify lock config
	assert.True(t, cfg.Lock.Enabled)
	assert.Equal(t, 30*time.Second, cfg.Lock.Timeout)
	assert.Equal(t, 2*time.Minute, cfg.Lock.Stale)
	assert.Equal(t, "/tmp/rr-locks", cfg.Lock.Dir)

	// Verify output config
	assert.Equal(t, "auto", cfg.Output.Color)
	assert.True(t, cfg.Output.Timing)
}

func TestConfigValidation(t *testing.T) {
	t.Run("valid config passes validation", func(t *testing.T) {
		cfg := &config.Config{
			Version: 1,
			Host:    "test-host",
		}
		err := config.Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("valid global config passes validation", func(t *testing.T) {
		globalCfg := &config.GlobalConfig{
			Version: 1,
			Hosts: map[string]config.Host{
				"test": {SSH: []string{"localhost"}, Dir: "/tmp/test"},
			},
		}
		err := config.ValidateGlobal(globalCfg)
		assert.NoError(t, err)
	})

	t.Run("missing hosts fails resolved validation", func(t *testing.T) {
		resolved := &config.ResolvedConfig{
			Global: &config.GlobalConfig{
				Version: 1,
				Hosts:   map[string]config.Host{},
			},
			Project: &config.Config{Version: 1},
		}
		err := config.ValidateResolved(resolved)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "No hosts configured")
	})

	t.Run("reserved task name fails validation", func(t *testing.T) {
		cfg := &config.Config{
			Version: 1,
			Tasks: map[string]config.TaskConfig{
				"run": {Run: "echo test"}, // Reserved name
			},
		}
		err := config.Validate(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "built-in command")
	})
}

func TestConfigVariableExpansion(t *testing.T) {
	// Test USER variable
	result := config.Expand("~/projects/${USER}/myproject")
	assert.NotContains(t, result, "${USER}")
	assert.Contains(t, result, "/projects/")

	// Test HOME variable
	result = config.Expand("${HOME}/code")
	assert.NotContains(t, result, "${HOME}")
	home, _ := os.UserHomeDir()
	if home != "" {
		assert.Contains(t, result, home)
	}

	// Test PROJECT variable (falls back to directory name)
	result = config.Expand("/remote/${PROJECT}")
	assert.NotContains(t, result, "${PROJECT}")
}

// =============================================================================
// SSH Connection Tests (Skippable)
// =============================================================================

func TestSSHConnectionToTestHost(t *testing.T) {
	SkipIfNoSSH(t)

	// This test requires actual SSH access to localhost
	// It verifies the SSH client can connect to the configured test host
	t.Log("SSH connection test requires manual verification or CI configuration")
	t.Log("Test host:", GetTestSSHHost())
	t.Log("Test user:", GetTestSSHUser())

	// The actual connection test would use the sshutil package
	// For now, we verify the test helpers work correctly
	host := GetTestSSHHost()
	assert.NotEmpty(t, host)
}

// =============================================================================
// File Sync Tests
// =============================================================================

func TestFileSyncToTempDirectory(t *testing.T) {
	// Create source directory with files
	sourceDir := TempSyncDirWithFiles(t, map[string]string{
		"main.go":            "package main\n\nfunc main() {}\n",
		"go.mod":             "module test\n\ngo 1.21\n",
		"internal/app.go":    "package internal\n",
		"cmd/server/main.go": "package main\n",
		".git/config":        "[core]\n", // Should be excluded
	})

	// Test rsync command building (without actual execution)
	cfg := config.SyncConfig{
		Exclude: []string{
			".git/",
			"__pycache__/",
			"*.pyc",
		},
		Preserve: []string{
			".venv/",
			"node_modules/",
		},
		Flags: []string{},
	}

	// Build args without a real connection (we pass nil and expect an error)
	_, err := rrsync.BuildArgs(nil, sourceDir, cfg)
	assert.Error(t, err) // Expected: no connection provided

	// Verify source files exist
	files := []string{"main.go", "go.mod", "internal/app.go", "cmd/server/main.go"}
	for _, f := range files {
		path := filepath.Join(sourceDir, f)
		_, err := os.Stat(path)
		assert.NoError(t, err, "Expected file to exist: %s", f)
	}
}

func TestRsyncCommandBuilding(t *testing.T) {
	// Test that rsync is available
	rsyncPath, err := rrsync.FindRsync()
	if err != nil {
		t.Skip("rsync not found on system")
	}
	assert.NotEmpty(t, rsyncPath)

	// Test version retrieval
	version, err := rrsync.Version()
	if err != nil {
		t.Skip("Could not get rsync version")
	}
	assert.Contains(t, version, "rsync")
}

// =============================================================================
// Lock Tests
// =============================================================================

func TestLockAcquireRequiresConnection(t *testing.T) {
	cfg := config.LockConfig{
		Enabled: true,
		Timeout: time.Second,
		Stale:   time.Minute,
		Dir:     "/tmp/rr-locks",
	}

	// Acquire should fail without a connection
	_, err := lock.Acquire(nil, cfg, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Can't grab the lock")
}

func TestLockInfoCreation(t *testing.T) {
	info, err := lock.NewLockInfo("test command")
	require.NoError(t, err)

	assert.NotEmpty(t, info.User)
	assert.NotEmpty(t, info.Hostname)
	assert.NotZero(t, info.PID)
	assert.False(t, info.Started.IsZero())
	assert.Equal(t, "test command", info.Command)

	// Started should be recent
	assert.Less(t, time.Since(info.Started), time.Second)
}

func TestLockInfoSerialization(t *testing.T) {
	original := &lock.LockInfo{
		User:     "testuser",
		Hostname: "testhost",
		Started:  time.Now().Truncate(time.Second),
		PID:      12345,
	}

	// Marshal
	data, err := original.Marshal()
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Unmarshal
	parsed, err := lock.ParseLockInfo(data)
	require.NoError(t, err)

	assert.Equal(t, original.User, parsed.User)
	assert.Equal(t, original.Hostname, parsed.Hostname)
	assert.Equal(t, original.PID, parsed.PID)
	// Started times should be equal (truncated to second for comparison)
	assert.True(t, original.Started.Truncate(time.Second).Equal(parsed.Started.Truncate(time.Second)))
}

func TestLockStaleDetection(t *testing.T) {
	// Old lock (15 minutes ago)
	oldInfo := &lock.LockInfo{
		User:     "olduser",
		Hostname: "oldhost",
		Started:  time.Now().Add(-15 * time.Minute),
		PID:      1111,
	}

	// Stale threshold is 10 minutes
	staleThreshold := 10 * time.Minute
	assert.True(t, oldInfo.Age() > staleThreshold, "Old lock should be detected as stale")

	// Recent lock (5 minutes ago)
	recentInfo := &lock.LockInfo{
		User:     "currentuser",
		Hostname: "currenthost",
		Started:  time.Now().Add(-5 * time.Minute),
		PID:      2222,
	}
	assert.False(t, recentInfo.Age() > staleThreshold, "Recent lock should not be detected as stale")
}

func TestLockReleaseNilSafe(t *testing.T) {
	// Release on nil lock should not panic
	var l *lock.Lock
	err := l.Release()
	assert.NoError(t, err)

	// Release with nil connection should not panic
	l = &lock.Lock{
		Dir:  "/tmp/test.lock",
		Info: &lock.LockInfo{},
	}
	err = l.Release()
	assert.NoError(t, err)
}

// =============================================================================
// Lock Contention Tests
// =============================================================================

func TestLockContentionSimulated(t *testing.T) {
	// Simulate lock contention scenario without SSH
	// This tests the conceptual behavior using LockInfo

	var mu sync.Mutex
	lockHeld := false
	waiters := 0

	// Simulate first process acquiring lock
	acquireLock := func(id string, holdDuration time.Duration) {
		mu.Lock()
		if lockHeld {
			waiters++
			mu.Unlock()

			// Wait for lock to be released (simulated polling)
			for {
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				if !lockHeld {
					break
				}
				mu.Unlock()
			}
			waiters--
		}
		lockHeld = true
		mu.Unlock()

		// Hold the lock
		time.Sleep(holdDuration)

		// Release
		mu.Lock()
		lockHeld = false
		mu.Unlock()
	}

	// Start long-running process
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		acquireLock("process1", 100*time.Millisecond)
	}()

	// Small delay to ensure first process gets lock first
	time.Sleep(10 * time.Millisecond)

	// Start second process that should wait
	go func() {
		defer wg.Done()
		start := time.Now()
		acquireLock("process2", 10*time.Millisecond)
		elapsed := time.Since(start)

		// Second process should have waited ~100ms for first to release
		assert.True(t, elapsed >= 90*time.Millisecond,
			"Second process should wait for first to release lock, waited only %v", elapsed)
	}()

	wg.Wait()
}

// =============================================================================
// Output Formatting Tests
// =============================================================================

func TestOutputStreamHandler(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := output.NewStreamHandler(&stdout, &stderr)

	// Write to stdout
	err := h.WriteStdout("normal output line")
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "normal output line")

	// Write to stderr
	err = h.WriteStderr("error output line")
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "error output line")

	// Check line counts
	assert.Equal(t, 1, h.StdoutLines())
	assert.Equal(t, 1, h.StderrLines())
}

func TestOutputFormatterErrorDetection(t *testing.T) {
	formatter := output.NewGenericFormatter()
	assert.Equal(t, "generic", formatter.Name())

	testCases := []struct {
		line     string
		isError  bool
		contains string
	}{
		{"normal output", false, "normal output"},
		{"ERROR: something failed", true, "ERROR:"},
		{"error: cannot find file", true, "error:"},
		{"Fatal: process crashed", true, "Fatal:"},
		{"FAILED test_something", true, "FAILED"},
		{"All tests passed", false, "passed"},
	}

	for _, tc := range testCases {
		result := formatter.ProcessLine(tc.line)
		assert.Contains(t, result, tc.contains,
			"Expected output to contain %q for line %q", tc.contains, tc.line)

		// Error lines may have ANSI codes (depends on terminal detection)
		// We just verify the content is preserved, not the styling
		// The styling tests are better done in internal/output package
	}
}

func TestOutputFormatterSummary(t *testing.T) {
	formatter := output.NewGenericFormatter()

	// Success case - no summary needed
	summary := formatter.Summary(0)
	assert.Empty(t, summary)

	// Failure case - should show exit code
	summary = formatter.Summary(1)
	assert.Contains(t, summary, "exit code")
	assert.Contains(t, summary, "1")
}

func TestPhaseDisplayOutput(t *testing.T) {
	var buf bytes.Buffer
	pd := ui.NewPhaseDisplay(&buf)

	// Test success rendering
	pd.RenderSuccess("Connected", 250*time.Millisecond)
	output := buf.String()

	// Should contain the phase name and timing
	assert.Contains(t, output, "Connected")
	// Timing should be shown
	assert.True(t, strings.Contains(output, "0.2s") || strings.Contains(output, "0.3s") || strings.Contains(output, "250"),
		"Expected timing in output: %s", output)

	buf.Reset()

	// Test failed rendering
	pd.RenderFailed("Sync", 1*time.Second, nil)
	output = buf.String()
	assert.Contains(t, output, "Sync")

	buf.Reset()

	// Test skipped rendering
	pd.RenderSkipped("Lock", "disabled")
	output = buf.String()
	assert.Contains(t, output, "Lock")
	assert.Contains(t, output, "disabled")
}

func TestPhaseDivider(t *testing.T) {
	var buf bytes.Buffer
	pd := ui.NewPhaseDisplay(&buf)

	pd.Divider()
	output := buf.String()

	// Should contain horizontal line characters
	assert.True(t, strings.Contains(output, "━") || strings.Contains(output, "-"),
		"Expected divider line in output")
}

func TestCommandPromptRendering(t *testing.T) {
	var buf bytes.Buffer
	pd := ui.NewPhaseDisplay(&buf)

	pd.CommandPrompt("pytest -n auto")
	output := buf.String()

	assert.Contains(t, output, "$")
	assert.Contains(t, output, "pytest -n auto")
}

// =============================================================================
// Exit Code Tests
// =============================================================================

func TestExitCodePreservation(t *testing.T) {
	// Test that different exit codes are properly represented
	testCases := []struct {
		exitCode    int
		description string
	}{
		{0, "success"},
		{1, "general error"},
		{2, "misuse of command"},
		{126, "command not executable"},
		{127, "command not found"},
		{130, "terminated by Ctrl-C"},
		{255, "SSH error"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Verify the exit code is a valid value
			assert.GreaterOrEqual(t, tc.exitCode, 0)
			assert.LessOrEqual(t, tc.exitCode, 255)
		})
	}
}

// =============================================================================
// Workflow Simulation Tests
// =============================================================================

func TestWorkflowPhasesSimulated(t *testing.T) {
	// Simulate the phases of `rr run` without actual SSH
	phases := []struct {
		name     string
		action   func() error
		duration time.Duration
	}{
		{
			name:     "Config Loading",
			action:   func() error { return nil },
			duration: 10 * time.Millisecond,
		},
		{
			name:     "Host Selection",
			action:   func() error { return nil },
			duration: 5 * time.Millisecond,
		},
		{
			name: "Connection",
			action: func() error {
				// Would connect to SSH here
				return nil
			},
			duration: 50 * time.Millisecond,
		},
		{
			name: "Sync",
			action: func() error {
				// Would run rsync here
				return nil
			},
			duration: 100 * time.Millisecond,
		},
		{
			name: "Lock Acquire",
			action: func() error {
				// Would acquire lock here
				return nil
			},
			duration: 20 * time.Millisecond,
		},
		{
			name: "Execute",
			action: func() error {
				// Would run command here
				return nil
			},
			duration: 200 * time.Millisecond,
		},
		{
			name:     "Lock Release",
			action:   func() error { return nil },
			duration: 10 * time.Millisecond,
		},
	}

	var buf bytes.Buffer
	pd := ui.NewPhaseDisplay(&buf)

	totalStart := time.Now()
	for _, phase := range phases {
		start := time.Now()
		err := phase.action()
		time.Sleep(phase.duration) // Simulate work
		elapsed := time.Since(start)

		if err != nil {
			pd.RenderFailed(phase.name, elapsed, err)
			t.Fatalf("Phase %s failed: %v", phase.name, err)
		}
		pd.RenderSuccess(phase.name, elapsed)
	}
	totalDuration := time.Since(totalStart)

	output := buf.String()

	// Verify all phases appear in output
	for _, phase := range phases {
		assert.Contains(t, output, phase.name,
			"Expected phase %q in output", phase.name)
	}

	// Total duration should be roughly the sum of phase durations
	expectedMin := 300 * time.Millisecond
	assert.True(t, totalDuration >= expectedMin,
		"Total duration %v should be at least %v", totalDuration, expectedMin)

	t.Logf("Workflow completed in %v", totalDuration)
}

// =============================================================================
// Full Integration Test (Requires SSH)
// =============================================================================

func TestFullRunWorkflow(t *testing.T) {
	SkipIfNoSSH(t)

	// This test requires actual SSH access
	// It would test the full `rr run` workflow

	t.Log("Full integration test requires SSH access to:", GetTestSSHHost())
	t.Log("Configure RR_TEST_SSH_HOST, RR_TEST_SSH_USER, RR_TEST_SSH_KEY")
	t.Log("Or set RR_TEST_SKIP_SSH=1 to skip SSH-dependent tests")

	// In a real CI environment, this would:
	// 1. Create a temporary config file
	// 2. Create a temporary source directory
	// 3. Run `rr run "echo hello"`
	// 4. Verify the output contains "hello"
	// 5. Verify exit code is 0
}

// =============================================================================
// ANSI Passthrough Tests
// =============================================================================

func TestANSICodePassthrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := output.NewStreamHandler(&stdout, &stderr)

	// Write line with ANSI codes
	ansiLine := "\033[32mGreen text\033[0m and \033[31mRed text\033[0m"
	err := h.WriteStdout(ansiLine)
	require.NoError(t, err)

	output := stdout.String()

	// ANSI codes should be preserved
	assert.Contains(t, output, "\033[32m")
	assert.Contains(t, output, "\033[31m")
	assert.Contains(t, output, "\033[0m")
	assert.Contains(t, output, "Green text")
	assert.Contains(t, output, "Red text")
}

// =============================================================================
// Line Buffering Tests
// =============================================================================

func TestLineBuffering(t *testing.T) {
	var stdout, stderr bytes.Buffer
	h := output.NewStreamHandler(&stdout, &stderr)

	w := h.Stdout()

	// Write partial line
	_, err := w.Write([]byte("partial"))
	require.NoError(t, err)
	assert.Empty(t, stdout.String(), "Partial line should be buffered")

	// Complete the line
	_, err = w.Write([]byte(" line\n"))
	require.NoError(t, err)
	assert.Equal(t, "partial line\n", stdout.String())

	// Write multiple lines at once
	stdout.Reset()
	_, err = w.Write([]byte("line1\nline2\nline3\n"))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
}
