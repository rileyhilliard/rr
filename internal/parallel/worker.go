package parallel

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/lock"
	rrsync "github.com/rileyhilliard/rr/internal/sync"
)

// hostWorker executes tasks on a specific host.
type hostWorker struct {
	orchestrator *Orchestrator
	hostName     string
	host         config.Host
	conn         *host.Connection
	connMu       sync.Mutex
	hostLock     *lock.Lock
	resultChan   chan<- TaskResult
	failed       *bool
	failedMu     *sync.Mutex
}

// executeTaskWithRequeue runs a single task on the host, returning whether the task
// should be re-queued (e.g., if the host is unavailable).
// Returns (result, shouldRequeue). If shouldRequeue is true, the result should be ignored
// and the task should be sent to another host.
func (w *hostWorker) executeTaskWithRequeue(ctx context.Context, task TaskInfo) (TaskResult, bool) {
	result := TaskResult{
		TaskName:  task.Name,
		TaskIndex: task.Index,
		Command:   task.Command,
		Host:      w.hostName,
		StartTime: time.Now(),
	}

	// Notify output manager or dashboard: task is syncing (connecting, syncing files, waiting for lock)
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskSyncing(task.Name, task.Index, w.hostName)
	}
	w.orchestrator.notifyTaskSyncing(task.Name, task.Index, w.hostName)

	// Ensure we have a connection - if this fails due to host unavailability,
	// re-queue the task. But if the context was cancelled or timed out,
	// don't re-queue since that's an intentional stop.
	if err := w.ensureConnection(ctx); err != nil {
		// Check if context was cancelled or timed out - don't re-queue in that case
		if ctx.Err() != nil ||
			stderrors.Is(err, context.Canceled) ||
			stderrors.Is(err, context.DeadlineExceeded) {
			result.Error = err
			result.ExitCode = 1
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			w.notifyComplete(result)
			return result, false
		}
		// Connection failed due to host unavailability - re-queue the task
		return result, true
	}

	// From here, we have a connection. Execute normally.
	return w.executeTaskInternal(ctx, task, result), false
}

// executeTaskInternal executes a task after connection has been established.
func (w *hostWorker) executeTaskInternal(ctx context.Context, task TaskInfo, result TaskResult) TaskResult {

	// Ensure host is synced (once per host)
	if err := w.ensureSync(ctx); err != nil {
		result.Error = err
		result.ExitCode = 1
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		w.notifyComplete(result)
		return result
	}

	// Run setup command if configured (once per host, after sync)
	if err := w.ensureSetup(ctx); err != nil {
		result.Error = err
		result.ExitCode = 1
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		w.notifyComplete(result)
		return result
	}

	// Update lock command to show current task in monitor.
	// We hold connMu briefly to safely read hostLock (Close() may nil it concurrently).
	// Error is intentionally ignored: UpdateCommand is best-effort for monitoring visibility
	// and failure doesn't affect task execution. The lock itself remains valid.
	w.connMu.Lock()
	hostLock := w.hostLock
	w.connMu.Unlock()
	if hostLock != nil {
		_ = hostLock.UpdateCommand(task.Name)
	}

	// Notify output manager or dashboard: task is now actually executing
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskExecuting(task.Name, task.Index)
	}
	w.orchestrator.notifyTaskExecuting(task.Name, task.Index)

	// Execute the task with timeout if configured
	execCtx := ctx
	var cancel context.CancelFunc
	if w.orchestrator.config.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, w.orchestrator.config.Timeout)
		defer cancel()
	}

	// Capture output
	var outputBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	// Build the command
	cmd := task.Command
	workDir := task.WorkDir
	if workDir == "" && w.host.Dir != "" {
		workDir = config.ExpandRemote(w.host.Dir)
	}

	// Execute the command
	exitCode, err := w.execCommand(execCtx, cmd, task.Env, workDir, &outputBuf, &stderrBuf)

	result.ExitCode = exitCode
	result.Error = err
	result.Output = append(outputBuf.Bytes(), stderrBuf.Bytes()...)
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	// Stream output if in stream mode
	if w.orchestrator.outputMgr != nil {
		// Output line by line for stream mode
		for _, line := range bytes.Split(outputBuf.Bytes(), []byte("\n")) {
			if len(line) > 0 {
				w.orchestrator.outputMgr.TaskOutput(task.Name, task.Index, line, false)
			}
		}
		for _, line := range bytes.Split(stderrBuf.Bytes(), []byte("\n")) {
			if len(line) > 0 {
				w.orchestrator.outputMgr.TaskOutput(task.Name, task.Index, line, true)
			}
		}
	}

	w.notifyComplete(result)
	return result
}

// ensureConnection establishes an SSH connection to the host if needed.
func (w *hostWorker) ensureConnection(_ context.Context) error {
	w.connMu.Lock()
	defer w.connMu.Unlock()

	if w.conn != nil {
		return nil
	}

	// Create a selector for this specific host
	hosts := map[string]config.Host{w.hostName: w.host}
	selector := host.NewSelector(hosts)

	// Try to connect
	conn, err := selector.Select(w.hostName)
	if err != nil {
		return err
	}

	w.conn = conn
	return nil
}

// ensureSync syncs files to the host and acquires a lock if not already done.
// The lock is held for the lifetime of the worker to prevent conflicts with
// other rr processes while parallel tasks are running on this host.
func (w *hostWorker) ensureSync(_ context.Context) error {
	// Check if already synced (and locked)
	if w.orchestrator.markHostSynced(w.hostName) {
		return nil
	}

	// Skip sync for local connections
	if w.conn != nil && w.conn.IsLocal {
		return nil
	}

	// Use project root if available, otherwise fall back to cwd.
	// This ensures parallel tasks sync from the correct directory when
	// run from a subdirectory (matching the single-task workflow behavior).
	workDir := resolveWorkDir(w.orchestrator.resolved)
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return errors.Wrap(err, "resolve working directory for sync")
		}
	}

	// Acquire lock before syncing
	lockCfg := config.DefaultConfig().Lock
	if w.orchestrator.resolved != nil && w.orchestrator.resolved.Project != nil {
		lockCfg = w.orchestrator.resolved.Project.Lock
	}

	if lockCfg.Enabled && w.conn != nil {
		// Acquire lock with placeholder - UpdateCommand is called per-task with actual task name
		hostLock, err := lock.Acquire(w.conn, lockCfg, "starting...")
		if err != nil {
			return err
		}
		w.hostLock = hostLock
	}

	// Get sync config
	syncCfg := config.DefaultConfig().Sync
	if w.orchestrator.resolved != nil && w.orchestrator.resolved.Project != nil {
		syncCfg = w.orchestrator.resolved.Project.Sync
	}

	// Perform sync
	return rrsync.Sync(w.conn, workDir, syncCfg, nil)
}

// ensureSetup runs the setup command once per host after sync.
// Setup failures abort all subsequent tasks on this host.
func (w *hostWorker) ensureSetup(ctx context.Context) error {
	// No setup configured
	if w.orchestrator.config.Setup == "" {
		return nil
	}

	// Check if setup already attempted for this host
	alreadyAttempted, previousErr := w.orchestrator.checkHostSetup(w.hostName)
	if alreadyAttempted {
		// Return the same error (or nil) as the first attempt
		return previousErr
	}

	// Get working directory for setup command
	workDir := ""
	if w.host.Dir != "" {
		workDir = config.ExpandRemote(w.host.Dir)
	}

	// Execute setup command
	var stdout, stderr bytes.Buffer
	exitCode, err := w.execCommand(ctx, w.orchestrator.config.Setup, nil, workDir, &stdout, &stderr)

	var setupErr error
	if err != nil {
		setupErr = fmt.Errorf("setup command failed: %w", err)
	} else if exitCode != 0 {
		output := stdout.String() + stderr.String()
		setupErr = fmt.Errorf("setup command failed with exit code %d: %s", exitCode, output)
	}

	// Record result so subsequent tasks on this host get the same outcome
	w.orchestrator.recordHostSetup(w.hostName, setupErr)

	return setupErr
}

// execCommand executes a command on the host with output capture.
func (w *hostWorker) execCommand(
	ctx context.Context,
	cmd string,
	env map[string]string,
	workDir string,
	stdout, stderr *bytes.Buffer,
) (int, error) {
	// Build full command with env and workdir
	fullCmd := buildFullCommand(cmd, env, workDir, w.host.SetupCommands)

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return 1, ctx.Err()
	default:
	}

	// Execute via SSH
	if w.conn == nil || w.conn.Client == nil {
		return 1, fmt.Errorf("no SSH connection available for host %s", w.hostName)
	}

	return w.conn.Client.ExecStreamContext(ctx, fullCmd, stdout, stderr)
}

// buildFullCommand constructs the command with setup commands, env, and workdir.
func buildFullCommand(cmd string, env map[string]string, workDir string, setupCommands []string) string {
	var parts []string

	// Add setup commands
	parts = append(parts, setupCommands...)

	// Add cd to work directory
	if workDir != "" {
		parts = append(parts, "cd "+workDir)
	}

	// Build env prefix
	envPrefix := ""
	for k, v := range env {
		envPrefix += "export " + k + "=" + shellQuote(v) + "; "
	}

	// Add command with env
	parts = append(parts, envPrefix+cmd)

	// Join with &&
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += " && "
		}
		result += part
	}

	return result
}

// shellQuote quotes a string for safe shell use.
// Uses single quotes with proper escaping to prevent command injection.
func shellQuote(s string) string {
	// Use single quotes and escape any embedded single quotes
	// This is safe for POSIX shells: 'foo'\''bar' -> foo'bar
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// notifyComplete notifies the output manager and dashboard that a task completed.
func (w *hostWorker) notifyComplete(result TaskResult) {
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskCompleted(result)
	}
	w.orchestrator.notifyTaskCompleted(result)
}

// Close releases the lock and closes the worker's connection.
// Holds connMu to synchronize with executeTask's access to hostLock.
func (w *hostWorker) Close() error {
	w.connMu.Lock()
	defer w.connMu.Unlock()

	// Release the lock first (error ignored - best effort cleanup)
	if w.hostLock != nil {
		_ = w.hostLock.Release()
		w.hostLock = nil
	}

	if w.conn != nil {
		err := w.conn.Close()
		w.conn = nil
		return err
	}
	return nil
}

// localWorker executes tasks locally without SSH.
type localWorker struct {
	orchestrator *Orchestrator
}

// executeTask runs a single task locally.
func (w *localWorker) executeTask(ctx context.Context, task TaskInfo) TaskResult {
	result := TaskResult{
		TaskName:  task.Name,
		TaskIndex: task.Index,
		Command:   task.Command,
		Host:      "local",
		StartTime: time.Now(),
	}

	// Notify output manager or dashboard: local tasks go straight to executing (no sync needed)
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskSyncing(task.Name, task.Index, "local")
	}
	w.orchestrator.notifyTaskSyncing(task.Name, task.Index, "local")

	// Run setup command if configured (once for local execution)
	if err := w.ensureSetup(ctx); err != nil {
		result.Error = err
		result.ExitCode = 1
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		if w.orchestrator.outputMgr != nil {
			w.orchestrator.outputMgr.TaskCompleted(result)
		}
		return result
	}

	// Now executing
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskExecuting(task.Name, task.Index)
	}
	w.orchestrator.notifyTaskExecuting(task.Name, task.Index)

	// Execute with timeout if configured
	execCtx := ctx
	var cancel context.CancelFunc
	if w.orchestrator.config.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, w.orchestrator.config.Timeout)
		defer cancel()
	}

	// Capture output
	var outputBuf bytes.Buffer

	// Run the command locally
	cmd := exec.CommandContext(execCtx, "sh", "-c", task.Command)
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf

	// Set working directory if specified
	if task.WorkDir != "" {
		cmd.Dir = task.WorkDir
	}

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range task.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	err := cmd.Run()
	result.Output = outputBuf.Bytes()
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
			result.Error = err
		}
	}

	// Stream output if in stream mode
	if w.orchestrator.outputMgr != nil {
		for _, line := range bytes.Split(outputBuf.Bytes(), []byte("\n")) {
			if len(line) > 0 {
				w.orchestrator.outputMgr.TaskOutput(task.Name, task.Index, line, false)
			}
		}
		w.orchestrator.outputMgr.TaskCompleted(result)
	}
	w.orchestrator.notifyTaskCompleted(result)

	return result
}

// ensureSetup runs the setup command once for local execution.
func (w *localWorker) ensureSetup(ctx context.Context) error {
	// No setup configured
	if w.orchestrator.config.Setup == "" {
		return nil
	}

	// Check if setup already attempted (use "local" as the host name)
	alreadyAttempted, previousErr := w.orchestrator.checkHostSetup("local")
	if alreadyAttempted {
		// Return the same error (or nil) as the first attempt
		return previousErr
	}

	// Execute setup command locally
	cmd := exec.CommandContext(ctx, "sh", "-c", w.orchestrator.config.Setup)
	var outputBuf bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf

	var setupErr error
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			setupErr = fmt.Errorf("setup command failed with exit code %d: %s", exitErr.ExitCode(), outputBuf.String())
		} else {
			setupErr = fmt.Errorf("setup command failed: %w", err)
		}
	}

	// Record result so subsequent tasks get the same outcome
	w.orchestrator.recordHostSetup("local", setupErr)

	return setupErr
}

// resolveWorkDir returns the project root from the resolved config if available,
// or an empty string if not set. Callers should fall back to os.Getwd() when empty.
func resolveWorkDir(resolved *config.ResolvedConfig) string {
	if resolved != nil && resolved.ProjectRoot != "" {
		return resolved.ProjectRoot
	}
	return ""
}
