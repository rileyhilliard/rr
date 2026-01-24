// Package deps handles task dependency resolution and execution planning.
package deps

import (
	"fmt"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
)

// ExecutionPlan represents the resolved order of task execution.
// Stages execute sequentially; tasks within a parallel stage run concurrently.
type ExecutionPlan struct {
	// Stages are executed in order. Each stage must complete before the next begins.
	Stages []Stage

	// TargetTask is the task that was requested (the one with dependencies).
	TargetTask string
}

// Stage represents a group of tasks that can be executed together.
type Stage struct {
	// Tasks are the task names to execute in this stage.
	Tasks []string

	// Parallel indicates whether tasks in this stage should run concurrently.
	// If false, tasks run sequentially in the order listed.
	Parallel bool
}

// ResolveOptions controls how dependency resolution behaves.
type ResolveOptions struct {
	// SkipDeps skips all dependencies and returns a plan with only the target task.
	SkipDeps bool

	// From starts execution from a specific task in the dependency chain.
	// All tasks before this one in the resolved order are skipped.
	From string
}

// Resolver builds execution plans from task dependency graphs.
type Resolver struct {
	tasks map[string]config.TaskConfig
}

// NewResolver creates a resolver for the given task configuration.
func NewResolver(tasks map[string]config.TaskConfig) *Resolver {
	return &Resolver{tasks: tasks}
}

// Resolve creates an execution plan for the given task.
// The plan includes all dependencies in the correct execution order,
// with parallel groups preserved from the dependency specification.
func (r *Resolver) Resolve(taskName string, opts ResolveOptions) (*ExecutionPlan, error) {
	task, ok := r.tasks[taskName]
	if !ok {
		return nil, fmt.Errorf("task '%s' not found", taskName)
	}

	plan := &ExecutionPlan{
		TargetTask: taskName,
	}

	// If skipping deps, return plan with just the target task
	if opts.SkipDeps {
		// Only add target if it has a command
		if task.Run != "" || len(task.Steps) > 0 {
			plan.Stages = []Stage{{Tasks: []string{taskName}, Parallel: false}}
		}
		return plan, nil
	}

	// Build the execution plan from dependencies
	seen := make(map[string]bool)
	if err := r.buildPlan(taskName, plan, seen); err != nil {
		return nil, err
	}

	// Add the target task itself if it has a command
	if task.Run != "" || len(task.Steps) > 0 {
		plan.Stages = append(plan.Stages, Stage{
			Tasks:    []string{taskName},
			Parallel: false,
		})
	}

	// Handle --from option by trimming stages
	if opts.From != "" {
		plan = r.trimFromTask(plan, opts.From)
		if plan == nil {
			return nil, fmt.Errorf("task '%s' not found in dependency chain", opts.From)
		}
	}

	// Deduplicate tasks across stages (a task should only run once)
	plan = r.deduplicate(plan)

	return plan, nil
}

// buildPlan recursively builds stages from task dependencies.
func (r *Resolver) buildPlan(taskName string, plan *ExecutionPlan, seen map[string]bool) error {
	if seen[taskName] {
		return nil // Already processed (dedup happens later)
	}
	seen[taskName] = true

	task, ok := r.tasks[taskName]
	if !ok {
		return fmt.Errorf("task '%s' not found", taskName)
	}

	// Process each dependency item
	for _, dep := range task.Depends {
		if dep.IsParallel() {
			// First, recursively resolve dependencies of tasks in this parallel group
			for _, pTask := range dep.Parallel {
				if err := r.buildPlan(pTask, plan, seen); err != nil {
					return err
				}
			}
			// Filter to only include tasks that have executable work
			var executableTasks []string
			for _, pTask := range dep.Parallel {
				pTaskCfg := r.tasks[pTask]
				if pTaskCfg.Run != "" || len(pTaskCfg.Steps) > 0 {
					executableTasks = append(executableTasks, pTask)
				}
			}
			// Only add parallel stage if there are executable tasks
			if len(executableTasks) > 0 {
				plan.Stages = append(plan.Stages, Stage{
					Tasks:    executableTasks,
					Parallel: true,
				})
			}
		} else {
			// Simple dependency - resolve its deps first, then add it
			if err := r.buildPlan(dep.Task, plan, seen); err != nil {
				return err
			}
			// Add sequential stage for this task
			depTask := r.tasks[dep.Task]
			if depTask.Run != "" || len(depTask.Steps) > 0 {
				plan.Stages = append(plan.Stages, Stage{
					Tasks:    []string{dep.Task},
					Parallel: false,
				})
			}
		}
	}

	return nil
}

// trimFromTask removes all stages before the specified task.
func (r *Resolver) trimFromTask(plan *ExecutionPlan, fromTask string) *ExecutionPlan {
	found := false
	startIdx := 0

	for i, stage := range plan.Stages {
		for _, task := range stage.Tasks {
			if task == fromTask {
				found = true
				startIdx = i
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return nil
	}

	return &ExecutionPlan{
		Stages:     plan.Stages[startIdx:],
		TargetTask: plan.TargetTask,
	}
}

// deduplicate removes duplicate task executions, keeping the first occurrence.
func (r *Resolver) deduplicate(plan *ExecutionPlan) *ExecutionPlan {
	seen := make(map[string]bool)
	result := &ExecutionPlan{
		TargetTask: plan.TargetTask,
	}

	for _, stage := range plan.Stages {
		newTasks := make([]string, 0, len(stage.Tasks))
		for _, task := range stage.Tasks {
			if !seen[task] {
				seen[task] = true
				newTasks = append(newTasks, task)
			}
		}
		if len(newTasks) > 0 {
			result.Stages = append(result.Stages, Stage{
				Tasks:    newTasks,
				Parallel: stage.Parallel,
			})
		}
	}

	return result
}

// TaskCount returns the total number of tasks in the plan.
func (p *ExecutionPlan) TaskCount() int {
	count := 0
	for _, stage := range p.Stages {
		count += len(stage.Tasks)
	}
	return count
}

// String returns a human-readable representation of the plan.
func (p *ExecutionPlan) String() string {
	if len(p.Stages) == 0 {
		return "empty plan"
	}

	var parts []string
	for i, stage := range p.Stages {
		var stageStr string
		if stage.Parallel && len(stage.Tasks) > 1 {
			stageStr = fmt.Sprintf("%d. [%s] (parallel)", i+1, strings.Join(stage.Tasks, ", "))
		} else {
			stageStr = fmt.Sprintf("%d. %s", i+1, strings.Join(stage.Tasks, ", "))
		}
		parts = append(parts, stageStr)
	}
	return strings.Join(parts, " -> ")
}
