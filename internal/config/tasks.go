package config

import (
	"fmt"

	"github.com/rileyhilliard/rr/internal/errors"
)

// OnFail constants define what happens when a step fails.
const (
	OnFailStop     = "stop"     // Default: stop execution on failure
	OnFailContinue = "continue" // Continue to next step on failure
)

// GetTask returns a task by name from the config.
// Returns an error if the task doesn't exist or is invalid.
func GetTask(cfg *Config, name string) (*TaskConfig, error) {
	if cfg == nil {
		return nil, errors.New(errors.ErrConfig,
			"Config hasn't been loaded yet",
			"This is unexpected - load a config before looking up tasks.")
	}

	if cfg.Tasks == nil {
		return nil, errors.New(errors.ErrConfig,
			"No tasks defined in config",
			"Add some tasks to your .rr.yaml under 'tasks:' or just run commands directly with 'rr run'.")
	}

	task, ok := cfg.Tasks[name]
	if !ok {
		available := getTaskNames(cfg.Tasks)
		hint := "Check your .rr.yaml for available tasks."
		if len(available) > 0 {
			hint = fmt.Sprintf("Available tasks: %s", formatList(available))
		}
		return nil, errors.New(errors.ErrConfig,
			fmt.Sprintf("No task named '%s'", name),
			hint)
	}

	return &task, nil
}

// GetTaskWithMergedEnv returns a task with environment variables merged.
// Task env takes precedence over host env.
func GetTaskWithMergedEnv(cfg *Config, taskName string, host *Host) (*TaskConfig, map[string]string, error) {
	task, err := GetTask(cfg, taskName)
	if err != nil {
		return nil, nil, err
	}

	// Start with empty env
	mergedEnv := make(map[string]string)

	// Add host env first (lowest precedence)
	if host != nil {
		for k, v := range host.Env {
			mergedEnv[k] = v
		}
	}

	// Add task env (highest precedence)
	for k, v := range task.Env {
		mergedEnv[k] = v
	}

	return task, mergedEnv, nil
}

// TaskNames returns a list of all task names in the config.
func TaskNames(cfg *Config) []string {
	if cfg == nil || cfg.Tasks == nil {
		return nil
	}
	return getTaskNames(cfg.Tasks)
}

// getTaskNames returns a list of task names from a task map.
func getTaskNames(tasks map[string]TaskConfig) []string {
	names := make([]string, 0, len(tasks))
	for name := range tasks {
		names = append(names, name)
	}
	return names
}

// formatList formats a string slice as a comma-separated list.
func formatList(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	result := items[0]
	for i := 1; i < len(items); i++ {
		result += ", " + items[i]
	}
	return result
}

// IsTaskHostAllowed checks if a task is allowed to run on a given host.
// If the task has no host restrictions, returns true for any host.
func IsTaskHostAllowed(task *TaskConfig, hostName string) bool {
	if task == nil || len(task.Hosts) == 0 {
		return true // No restrictions
	}

	for _, allowed := range task.Hosts {
		if allowed == hostName {
			return true
		}
	}
	return false
}

// GetStepOnFail returns the on_fail behavior for a step.
// Defaults to "stop" if not specified.
func GetStepOnFail(step TaskStep) string {
	if step.OnFail == "" {
		return OnFailStop
	}
	return step.OnFail
}
