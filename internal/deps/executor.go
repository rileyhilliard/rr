package deps

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
)

// Executor runs execution plans for task dependencies.
type Executor struct {
	resolved *config.ResolvedConfig
	conn     *host.Connection
	opts     ExecutorOptions
}

// ExecutorOptions configures how the executor runs tasks.
type ExecutorOptions struct {
	// FailFast stops execution on first task failure when true.
	FailFast bool

	// Quiet minimizes output when true.
	Quiet bool

	// Stdout and Stderr for task output.
	Stdout io.Writer
	Stderr io.Writer

	// SetupCommands are prepended to each command.
	SetupCommands []string

	// WorkDir is the remote working directory.
	WorkDir string

	// StageHandler is called before and after each stage.
	StageHandler StageHandler
}

// StageHandler receives callbacks during execution.
type StageHandler interface {
	OnStageStart(stageNum, totalStages int, stage Stage)
	OnStageComplete(stageNum, totalStages int, stage Stage, result *StageResult, duration time.Duration)
	OnTaskStart(taskName string, parallel bool)
	OnTaskComplete(taskName string, exitCode int, duration time.Duration)
}

// ExecutionResult contains the result of running an execution plan.
type ExecutionResult struct {
	// StageResults contains results for each stage.
	StageResults []*StageResult

	// TotalDuration is the overall execution time.
	TotalDuration time.Duration

	// FailedStage is the index of the first failed stage (-1 if none).
	FailedStage int

	// FailFast indicates if execution was stopped early due to failure.
	FailFast bool
}

// Success returns true if all stages completed successfully.
func (r *ExecutionResult) Success() bool {
	return r.FailedStage == -1
}

// ExitCode returns the first non-zero exit code, or 0 if all passed.
func (r *ExecutionResult) ExitCode() int {
	for _, sr := range r.StageResults {
		if sr.ExitCode != 0 {
			return sr.ExitCode
		}
	}
	return 0
}

// StageResult contains the result of running a single stage.
type StageResult struct {
	// Stage is the stage that was executed.
	Stage Stage

	// TaskResults maps task names to their results.
	TaskResults map[string]*TaskExecutionResult

	// ExitCode is the first non-zero exit code from tasks in this stage.
	ExitCode int

	// Duration is how long the stage took.
	Duration time.Duration
}

// Success returns true if all tasks in the stage completed successfully.
func (r *StageResult) Success() bool {
	return r.ExitCode == 0
}

// TaskExecutionResult contains the result of running a single task.
type TaskExecutionResult struct {
	TaskName string
	ExitCode int
	Duration time.Duration
	Error    error
}

// NewExecutor creates an executor for running dependency plans.
func NewExecutor(resolved *config.ResolvedConfig, conn *host.Connection, opts ExecutorOptions) *Executor {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	return &Executor{
		resolved: resolved,
		conn:     conn,
		opts:     opts,
	}
}

// Execute runs all stages in the plan.
func (e *Executor) Execute(ctx context.Context, plan *ExecutionPlan) (*ExecutionResult, error) {
	start := time.Now()
	result := &ExecutionResult{
		StageResults: make([]*StageResult, 0, len(plan.Stages)),
		FailedStage:  -1,
	}

	for i, stage := range plan.Stages {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		// Notify handler that stage is starting
		if e.opts.StageHandler != nil {
			e.opts.StageHandler.OnStageStart(i+1, len(plan.Stages), stage)
		}

		stageStart := time.Now()
		stageResult, err := e.executeStage(ctx, stage)
		stageResult.Duration = time.Since(stageStart)

		// Notify handler that stage completed
		if e.opts.StageHandler != nil {
			e.opts.StageHandler.OnStageComplete(i+1, len(plan.Stages), stage, stageResult, stageResult.Duration)
		}

		if err != nil {
			return result, err
		}

		result.StageResults = append(result.StageResults, stageResult)

		if !stageResult.Success() {
			if result.FailedStage == -1 {
				result.FailedStage = i
			}
			if e.opts.FailFast {
				result.FailFast = true
				break
			}
		}
	}

	result.TotalDuration = time.Since(start)
	return result, nil
}

// executeStage runs all tasks in a stage.
func (e *Executor) executeStage(ctx context.Context, stage Stage) (*StageResult, error) {
	result := &StageResult{
		Stage:       stage,
		TaskResults: make(map[string]*TaskExecutionResult),
	}

	if stage.Parallel && len(stage.Tasks) > 1 {
		return e.executeParallel(ctx, stage)
	}

	// Sequential execution
	for _, taskName := range stage.Tasks {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		taskResult := e.executeTask(ctx, taskName)
		result.TaskResults[taskName] = taskResult

		if taskResult.ExitCode != 0 && result.ExitCode == 0 {
			result.ExitCode = taskResult.ExitCode
		}

		if taskResult.Error != nil {
			return result, taskResult.Error
		}

		// For sequential stages with fail_fast, stop on first failure
		if taskResult.ExitCode != 0 && e.opts.FailFast {
			break
		}
	}

	return result, nil
}

// executeParallel runs all tasks in the stage concurrently.
func (e *Executor) executeParallel(ctx context.Context, stage Stage) (*StageResult, error) {
	result := &StageResult{
		Stage:       stage,
		TaskResults: make(map[string]*TaskExecutionResult),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error

	// Create a cancelable context for fail-fast behavior
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, taskName := range stage.Tasks {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			// Check if we should abort
			select {
			case <-ctx.Done():
				mu.Lock()
				result.TaskResults[name] = &TaskExecutionResult{
					TaskName: name,
					ExitCode: -1,
					Error:    ctx.Err(),
				}
				mu.Unlock()
				return
			default:
			}

			taskResult := e.executeTask(ctx, name)

			mu.Lock()
			result.TaskResults[name] = taskResult
			if taskResult.ExitCode != 0 && result.ExitCode == 0 {
				result.ExitCode = taskResult.ExitCode
			}
			if taskResult.Error != nil && firstError == nil {
				firstError = taskResult.Error
			}
			mu.Unlock()

			// Cancel other tasks if fail-fast
			if e.opts.FailFast && taskResult.ExitCode != 0 {
				cancel()
			}
		}(taskName)
	}

	wg.Wait()
	return result, firstError
}

// executeTask runs a single task.
func (e *Executor) executeTask(_ context.Context, taskName string) *TaskExecutionResult {
	start := time.Now()
	result := &TaskExecutionResult{
		TaskName: taskName,
	}

	// Notify handler that task is starting
	if e.opts.StageHandler != nil {
		e.opts.StageHandler.OnTaskStart(taskName, false)
	}

	// Get the task config
	task, ok := e.resolved.Project.Tasks[taskName]
	if !ok {
		result.Error = fmt.Errorf("task '%s' not found", taskName)
		result.ExitCode = 1
		return result
	}

	// Get host config for env merging
	var hostCfg *config.Host
	if !e.conn.IsLocal {
		if h, ok := e.resolved.Global.Hosts[e.conn.Name]; ok {
			hostCfg = &h
		}
	}

	// Merge environment variables
	_, mergedEnv, err := config.GetTaskWithMergedEnv(e.resolved.Project, taskName, hostCfg)
	if err != nil {
		result.Error = err
		result.ExitCode = 1
		return result
	}

	// Execute the task
	execOpts := &exec.TaskExecOptions{
		SetupCommands: e.opts.SetupCommands,
	}

	taskResult, err := exec.ExecuteTask(e.conn, &task, nil, mergedEnv, e.opts.WorkDir, e.opts.Stdout, e.opts.Stderr, execOpts)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err
		result.ExitCode = 1
	} else {
		result.ExitCode = taskResult.ExitCode
	}

	// Notify handler that task completed
	if e.opts.StageHandler != nil {
		e.opts.StageHandler.OnTaskComplete(taskName, result.ExitCode, result.Duration)
	}

	return result
}
