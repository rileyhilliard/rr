package parallel

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
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

// executeTask runs a single task on the host.
func (w *hostWorker) executeTask(ctx context.Context, task TaskInfo) TaskResult {
	result := TaskResult{
		TaskName:  task.Name,
		Host:      w.hostName,
		StartTime: time.Now(),
	}

	// Notify output manager: task is syncing (connecting, syncing files, waiting for lock)
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskSyncing(task.Name, w.hostName)
	}

	// Ensure we have a connection
	if err := w.ensureConnection(ctx); err != nil {
		result.Error = err
		result.ExitCode = 1
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		w.notifyComplete(task.Name, result)
		return result
	}

	// Ensure host is synced (once per host)
	if err := w.ensureSync(ctx); err != nil {
		result.Error = err
		result.ExitCode = 1
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		w.notifyComplete(task.Name, result)
		return result
	}

	// Notify output manager: task is now actually executing
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskExecuting(task.Name)
	}

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
				w.orchestrator.outputMgr.TaskOutput(task.Name, line, false)
			}
		}
		for _, line := range bytes.Split(stderrBuf.Bytes(), []byte("\n")) {
			if len(line) > 0 {
				w.orchestrator.outputMgr.TaskOutput(task.Name, line, true)
			}
		}
	}

	w.notifyComplete(task.Name, result)
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

	// Get working directory
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Acquire lock before syncing
	lockCfg := config.DefaultConfig().Lock
	if w.orchestrator.resolved != nil && w.orchestrator.resolved.Project != nil {
		lockCfg = w.orchestrator.resolved.Project.Lock
	}

	if lockCfg.Enabled && w.conn != nil {
		projectHash := hashWorkDir(workDir)
		hostLock, err := lock.Acquire(w.conn, lockCfg, projectHash)
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

	return w.conn.Client.ExecStream(fullCmd, stdout, stderr)
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

// notifyComplete notifies the output manager that a task completed.
func (w *hostWorker) notifyComplete(taskName string, result TaskResult) {
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskCompleted(taskName, result)
	}
}

// Close releases the lock and closes the worker's connection.
func (w *hostWorker) Close() error {
	w.connMu.Lock()
	defer w.connMu.Unlock()

	// Release the lock first
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

// hashWorkDir creates a hash of the working directory for lock naming.
func hashWorkDir(workDir string) string {
	// Use a simple hash for the project identifier
	// This matches how the CLI does it in workflow.go
	h := uint32(0)
	for _, c := range workDir {
		h = h*31 + uint32(c)
	}
	return string(rune('a'+h%26)) + string(rune('a'+(h>>5)%26)) + string(rune('a'+(h>>10)%26)) + string(rune('a'+(h>>15)%26))
}

// localWorker executes tasks locally without SSH.
type localWorker struct {
	orchestrator *Orchestrator
}

// executeTask runs a single task locally.
func (w *localWorker) executeTask(ctx context.Context, task TaskInfo) TaskResult {
	result := TaskResult{
		TaskName:  task.Name,
		Host:      "local",
		StartTime: time.Now(),
	}

	// Notify output manager: local tasks go straight to executing (no sync needed)
	if w.orchestrator.outputMgr != nil {
		w.orchestrator.outputMgr.TaskSyncing(task.Name, "local")
		w.orchestrator.outputMgr.TaskExecuting(task.Name)
	}

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
				w.orchestrator.outputMgr.TaskOutput(task.Name, line, false)
			}
		}
		w.orchestrator.outputMgr.TaskCompleted(task.Name, result)
	}

	return result
}
