package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
)

// ReservedTaskNames are command names that cannot be used as task names.
var ReservedTaskNames = map[string]bool{
	"run":        true,
	"exec":       true,
	"sync":       true,
	"init":       true,
	"setup":      true,
	"status":     true,
	"monitor":    true,
	"doctor":     true,
	"help":       true,
	"version":    true,
	"completion": true,
	"update":     true,
	"host":       true,
	"unlock":     true,
	"tasks":      true,
	"clean":      true,
}

// ValidationOption controls validation behavior.
type ValidationOption func(*validationContext)

type validationContext struct {
	// No options currently needed for project config validation
}

// Validate checks the project config for errors and returns structured error messages.
func Validate(cfg *Config, opts ...ValidationOption) error {
	ctx := &validationContext{}
	for _, opt := range opts {
		opt(ctx)
	}

	// Check version
	if cfg.Version > CurrentConfigVersion {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("This config is from the future (version %d, but rr only knows up to %d)", cfg.Version, CurrentConfigVersion),
			"Grab the latest rr: https://github.com/rileyhilliard/rr/releases")
	}

	// Validate Host reference (if set) - should just be a name, no special chars
	if cfg.Host != "" {
		if err := validateHostReference(cfg.Host); err != nil {
			return err
		}
	}

	// Validate Hosts list (if set)
	if len(cfg.Hosts) > 0 {
		seen := make(map[string]bool)
		for _, h := range cfg.Hosts {
			if err := validateHostReference(h); err != nil {
				return err
			}
			if seen[h] {
				return errors.New(errors.ErrConfig,
					fmt.Sprintf("Duplicate host '%s' in hosts list", h),
					"Each host should only appear once in the hosts list.")
			}
			seen[h] = true
		}
	}

	// Check for reserved task names
	for name := range cfg.Tasks {
		if ReservedTaskNames[name] {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("Can't use '%s' as a task name - that's a built-in command", name),
				fmt.Sprintf("Pick a different name, like 'my-%s' or 'do-%s'.", name, name))
		}

		if err := validateTask(name, cfg.Tasks[name]); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check your task config in .rr.yaml.")
		}
	}

	// Validate output config
	if err := validateOutput(cfg.Output); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check the 'output' section in your .rr.yaml.")
	}

	// Validate lock config
	if err := validateLock(cfg.Lock); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check the 'lock' section in your .rr.yaml.")
	}

	// Validate monitor config (no hosts to validate against in project config)
	if err := validateMonitorConfig(cfg.Monitor); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check the 'monitor' section in your .rr.yaml.")
	}

	// Validate parallel task references (must come after individual task validation)
	if err := validateParallelTaskRefs(cfg); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check your parallel task configuration in .rr.yaml.")
	}

	// Validate project-level require list
	if err := validateRequireList("project", cfg.Require); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check the 'require' section in your .rr.yaml.")
	}

	// Validate dependency references and detect cycles
	if err := ValidateDependencyGraph(cfg); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check your task dependencies in .rr.yaml.")
	}

	return nil
}

// validateHostReference checks that a host reference is just a name (no special chars).
func validateHostReference(host string) error {
	// Host reference should be a simple name - no @ (user@host), no / (paths)
	if strings.Contains(host, "@") {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Host reference '%s' looks like an SSH string, not a host name", host),
			"Use just the host name here. Configure SSH connection details in ~/.rr/config.yaml.")
	}
	if strings.Contains(host, "/") {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Host reference '%s' contains a path separator", host),
			"Use just the host name here, not a path.")
	}
	return nil
}

// ValidateGlobal checks the global config for errors.
func ValidateGlobal(cfg *GlobalConfig) error {
	if cfg == nil {
		return errors.New(errors.ErrConfig,
			"Global config is nil",
			"This is unexpected - try reloading the configuration.")
	}

	// Check version
	if cfg.Version > CurrentGlobalConfigVersion {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Global config is from the future (version %d, but rr only knows up to %d)", cfg.Version, CurrentGlobalConfigVersion),
			"Grab the latest rr: https://github.com/rileyhilliard/rr/releases")
	}

	// Validate each host
	for name := range cfg.Hosts {
		if err := validateHost(name, cfg.Hosts[name]); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check your host config in ~/.rr/config.yaml.")
		}
	}

	return nil
}

// ValidateResolved checks the combined global and project configuration.
func ValidateResolved(r *ResolvedConfig) error {
	if r == nil {
		return errors.New(errors.ErrConfig,
			"Resolved config is nil",
			"This is unexpected - try reloading the configuration.")
	}

	// Validate global config - must have at least one host
	if r.Global == nil {
		return errors.New(errors.ErrConfig,
			"Global config not loaded",
			"This is unexpected - try running the command again.")
	}

	if len(r.Global.Hosts) == 0 {
		return errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add hosts to ~/.rr/config.yaml or run 'rr host add'.")
	}

	if err := ValidateGlobal(r.Global); err != nil {
		return err
	}

	// Validate project config if present
	if r.Project != nil {
		if err := Validate(r.Project); err != nil {
			return err
		}

		// Validate project's Host reference exists in global (if set)
		if r.Project.Host != "" {
			if _, ok := r.Global.Hosts[r.Project.Host]; !ok {
				hostNames := getHostNames(r.Global.Hosts)
				return errors.New(errors.ErrConfig,
					fmt.Sprintf("Project references host '%s' which doesn't exist in global config", r.Project.Host),
					fmt.Sprintf("Available hosts: %s. Add it to ~/.rr/config.yaml or change the host in .rr.yaml.", strings.Join(hostNames, ", ")))
			}
		}

		// Validate project's Hosts list references exist in global (if set)
		for _, h := range r.Project.Hosts {
			if _, ok := r.Global.Hosts[h]; !ok {
				hostNames := getHostNames(r.Global.Hosts)
				return errors.New(errors.ErrConfig,
					fmt.Sprintf("Project references host '%s' which doesn't exist in global config", h),
					fmt.Sprintf("Available hosts: %s. Add it to ~/.rr/config.yaml or remove it from .rr.yaml.", strings.Join(hostNames, ", ")))
			}
		}
	}

	return nil
}

// validateHost checks a single host configuration.
func validateHost(name string, host Host) error {
	if len(host.SSH) == 0 {
		return fmt.Errorf("host '%s' needs at least one SSH connection (like 'user@hostname')", name)
	}

	for i, ssh := range host.SSH {
		if strings.TrimSpace(ssh) == "" {
			return fmt.Errorf("host '%s' has an empty SSH entry at position %d", name, i)
		}
	}

	if host.Dir == "" {
		return fmt.Errorf("host '%s' needs a 'dir' - that's where your code will sync to", name)
	}

	// Validate remote path (allows ~ for remote shell expansion)
	if err := validateRemotePath(name, "dir", host.Dir); err != nil {
		return err
	}

	// Validate shell format if specified
	if host.Shell != "" {
		if err := validateShellFormat(name, host.Shell); err != nil {
			return err
		}
	}

	// Validate require list
	if err := validateRequireList(fmt.Sprintf("host '%s'", name), host.Require); err != nil {
		return err
	}

	return nil
}

// validateRemotePath checks for common remote path configuration mistakes.
// Note: Tilde (~) is ALLOWED in remote paths - the remote shell expands it.
// This runs AFTER variable expansion (${PROJECT}, ${USER}, ${HOME}, ${BRANCH})
// in parseGlobalConfig, so any remaining ${} indicates an unknown variable.
func validateRemotePath(hostName, fieldName, path string) error {
	if strings.Contains(path, "${") {
		return fmt.Errorf("host '%s' has an unrecognized variable in %s: %s (supported: ${PROJECT}, ${USER}, ${HOME}, ${BRANCH})", hostName, fieldName, path)
	}

	return nil
}

// validateShellFormat checks that the shell configuration looks correct.
func validateShellFormat(hostName, shell string) error {
	// Shell should end with a command flag like "-c"
	parts := strings.Fields(shell)
	if len(parts) == 0 {
		return fmt.Errorf("host '%s' has an empty shell setting", hostName)
	}

	lastPart := parts[len(parts)-1]
	if !strings.HasPrefix(lastPart, "-") {
		return fmt.Errorf("host '%s' shell should end with a flag like '-c'. Got '%s' - try 'bash -l -c' or 'zsh -c'", hostName, shell)
	}

	return nil
}

// validateSteps checks that all steps in a task are valid.
func validateSteps(name string, steps []TaskStep) error {
	for i, step := range steps {
		if step.Run == "" {
			return fmt.Errorf("task '%s' step %d is missing the 'run' command", name, i+1)
		}
		if step.OnFail != "" && step.OnFail != "stop" && step.OnFail != "continue" {
			return fmt.Errorf("task '%s' step %d has on_fail='%s' but it needs to be 'stop' or 'continue'", name, i+1, step.OnFail)
		}
	}
	return nil
}

// validateTask checks a single task configuration.
func validateTask(name string, task TaskConfig) error {
	hasRun := task.Run != ""
	hasSteps := len(task.Steps) > 0
	hasParallel := len(task.Parallel) > 0
	hasDepends := len(task.Depends) > 0

	// Parallel tasks are mutually exclusive with run and steps
	if hasParallel {
		if hasRun {
			return fmt.Errorf("task '%s' has both 'parallel' and 'run' - parallel tasks can't have a run command", name)
		}
		if hasSteps {
			return fmt.Errorf("task '%s' has both 'parallel' and 'steps' - parallel tasks can't have steps", name)
		}
		if hasDepends {
			return fmt.Errorf("task '%s' has both 'parallel' and 'depends' - parallel tasks can't have dependencies (use depends inside the subtasks instead)", name)
		}
		// Parallel-specific validation is done separately after all tasks are known
		return nil
	}

	// Tasks with dependencies can optionally have a run command or steps
	// (depends-only tasks just orchestrate their dependencies)
	if hasDepends {
		// Can have run, steps, or neither - but not both
		if hasRun && hasSteps {
			return fmt.Errorf("task '%s' has both 'run' and 'steps' - pick one or the other", name)
		}
		return validateSteps(name, task.Steps)
	}

	// Non-parallel, non-depends tasks: must have either run or steps, not both
	if !hasRun && !hasSteps {
		return fmt.Errorf("task '%s' needs either 'run' (single command), 'steps' (multiple commands), 'parallel' (concurrent subtasks), or 'depends' (task dependencies)", name)
	}

	if hasRun && hasSteps {
		return fmt.Errorf("task '%s' has both 'run' and 'steps' - pick one or the other", name)
	}

	// Validate steps if present
	if err := validateSteps(name, task.Steps); err != nil {
		return err
	}

	// Validate task-level require list
	if err := validateRequireList(fmt.Sprintf("task '%s'", name), task.Require); err != nil {
		return err
	}

	return nil
}

// validateOutput checks output configuration.
func validateOutput(out OutputConfig) error {
	validColors := map[string]bool{"auto": true, "always": true, "never": true, "": true}
	if !validColors[out.Color] {
		return fmt.Errorf("output.color '%s' isn't valid - use 'auto', 'always', or 'never'", out.Color)
	}

	validFormats := map[string]bool{
		"auto": true, "generic": true, "pytest": true,
		"jest": true, "go": true, "cargo": true, "": true,
	}
	if !validFormats[out.Format] {
		return fmt.Errorf("output.format '%s' isn't valid - try: auto, generic, pytest, jest, go, or cargo", out.Format)
	}

	validVerbosity := map[string]bool{"quiet": true, "normal": true, "verbose": true, "": true}
	if !validVerbosity[out.Verbosity] {
		return fmt.Errorf("output.verbosity '%s' isn't valid - use 'quiet', 'normal', or 'verbose'", out.Verbosity)
	}

	return nil
}

// validateLock checks lock configuration.
func validateLock(lock LockConfig) error {
	if lock.Timeout < 0 {
		return fmt.Errorf("lock.timeout can't be negative - that doesn't make sense")
	}
	if lock.Stale < 0 {
		return fmt.Errorf("lock.stale can't be negative - that doesn't make sense")
	}
	if lock.Enabled && lock.Timeout > 0 && lock.Stale > 0 && lock.Timeout > lock.Stale {
		return fmt.Errorf("lock.timeout (%v) is longer than lock.stale (%v) - you'd timeout before the lock expires", lock.Timeout, lock.Stale)
	}
	return nil
}

// validateMonitorConfig checks monitor configuration without host validation.
// Used for project config where hosts are defined separately in global config.
func validateMonitorConfig(monitor MonitorConfig) error {
	// Validate interval format if specified
	if monitor.Interval != "" {
		if _, err := time.ParseDuration(monitor.Interval); err != nil {
			return fmt.Errorf("monitor.interval '%s' doesn't look like a valid duration - try something like '2s', '5s', or '1m'", monitor.Interval)
		}
	}

	// Validate thresholds
	if err := validateThresholds("cpu", monitor.Thresholds.CPU); err != nil {
		return err
	}
	if err := validateThresholds("ram", monitor.Thresholds.RAM); err != nil {
		return err
	}
	if err := validateThresholds("gpu", monitor.Thresholds.GPU); err != nil {
		return err
	}

	// Validate exclude entries aren't empty (can't validate against hosts here)
	for _, excluded := range monitor.Exclude {
		if strings.TrimSpace(excluded) == "" {
			return fmt.Errorf("monitor.exclude has an empty entry - remove it or add a host name")
		}
	}

	return nil
}

// validateMonitor checks monitor configuration with host validation.
// Used when we have access to the hosts map (from global config).
func validateMonitor(monitor MonitorConfig, _ map[string]Host) error {
	// First run the basic validation
	if err := validateMonitorConfig(monitor); err != nil {
		return err
	}

	// Then validate excluded hosts exist (warning only, don't fail validation)
	// This allows excluding hosts that might be temporarily removed from config
	// Note: the empty check is already done in validateMonitorConfig

	return nil
}

// validateThresholds checks a threshold configuration for a single metric type.
func validateThresholds(name string, thresh ThresholdValues) error {
	// Only validate if non-zero (0 means use default)
	if thresh.Warning < 0 || thresh.Warning > 100 {
		return fmt.Errorf("monitor.thresholds.%s.warning needs to be 0-100 (got %d)", name, thresh.Warning)
	}
	if thresh.Critical < 0 || thresh.Critical > 100 {
		return fmt.Errorf("monitor.thresholds.%s.critical needs to be 0-100 (got %d)", name, thresh.Critical)
	}
	// Warning should be less than critical (if both are non-zero)
	if thresh.Warning > 0 && thresh.Critical > 0 && thresh.Warning >= thresh.Critical {
		return fmt.Errorf("monitor.thresholds.%s.warning (%d%%) is higher than critical (%d%%) - should be the other way around", name, thresh.Warning, thresh.Critical)
	}
	return nil
}

// getHostNames returns a sorted list of host names.
func getHostNames(hosts map[string]Host) []string {
	names := make([]string, 0, len(hosts))
	for name := range hosts {
		names = append(names, name)
	}
	return names
}

// IsReservedTaskName checks if a name is reserved.
func IsReservedTaskName(name string) bool {
	return ReservedTaskNames[name]
}

// ValidateParallelTasks validates parallel task references after individual tasks are validated.
// This checks:
// - All referenced tasks exist
// - No circular references (task A -> task B -> task A)
// Nested parallel tasks ARE allowed - they get flattened at execution time.
func ValidateParallelTasks(cfg *Config) error {
	if cfg == nil || cfg.Tasks == nil {
		return nil
	}

	for name := range cfg.Tasks {
		task := cfg.Tasks[name]
		if len(task.Parallel) == 0 {
			continue
		}

		// Check each referenced task exists
		for _, ref := range task.Parallel {
			_, ok := cfg.Tasks[ref]
			if !ok {
				available := getTaskNames(cfg.Tasks)
				return fmt.Errorf("parallel task '%s' references non-existent task '%s'. Available tasks: %s",
					name, ref, strings.Join(available, ", "))
			}

			// No self-reference (direct)
			if ref == name {
				return fmt.Errorf("parallel task '%s' can't reference itself", name)
			}
		}

		// Check for cycles in parallel task references
		if err := detectParallelCycle(name, cfg.Tasks); err != nil {
			return err
		}
	}

	return nil
}

// detectParallelCycle detects circular references in parallel task definitions.
// Uses DFS to find cycles like: A -> B -> C -> A
func detectParallelCycle(startTask string, tasks map[string]TaskConfig) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var path []string

	var dfs func(taskName string) error
	dfs = func(taskName string) error {
		if inStack[taskName] {
			// Found a cycle - build the cycle path for the error message
			cycleStart := -1
			for i, p := range path {
				if p == taskName {
					cycleStart = i
					break
				}
			}
			cyclePath := make([]string, len(path[cycleStart:])+1)
			copy(cyclePath, path[cycleStart:])
			cyclePath[len(cyclePath)-1] = taskName
			return fmt.Errorf("circular reference in parallel tasks: %s", strings.Join(cyclePath, " -> "))
		}

		if visited[taskName] {
			return nil
		}

		task, ok := tasks[taskName]
		if !ok {
			return nil // Task doesn't exist - caught elsewhere
		}

		// Only traverse parallel references
		if len(task.Parallel) == 0 {
			return nil
		}

		visited[taskName] = true
		inStack[taskName] = true
		path = append(path, taskName)

		for _, ref := range task.Parallel {
			if err := dfs(ref); err != nil {
				return err
			}
		}

		inStack[taskName] = false
		path = path[:len(path)-1]
		return nil
	}

	return dfs(startTask)
}

// FlattenParallelTasks expands nested parallel task references into a flat list.
// Given a parallel task that references other parallel tasks, this returns
// the final list of executable (non-parallel) task names.
//
// Tasks are expanded exactly as listed - if a task appears multiple times,
// it will appear multiple times in the result. This allows running the same
// task multiple times (e.g., for flake detection across parallel workers).
//
// Example:
//
//	test-opendata: {parallel: [test-opendata-1, test-opendata-2]}
//	test-backend: {parallel: [test-backend-1, test-backend-2]}
//	test: {parallel: [test-opendata, test-backend, test-frontend]}
//
// FlattenParallelTasks("test", tasks) returns:
// [test-opendata-1, test-opendata-2, test-backend-1, test-backend-2, test-frontend]
func FlattenParallelTasks(taskName string, tasks map[string]TaskConfig) ([]string, error) {
	task, ok := tasks[taskName]
	if !ok {
		return nil, fmt.Errorf("task '%s' not found", taskName)
	}

	if len(task.Parallel) == 0 {
		return nil, fmt.Errorf("task '%s' is not a parallel task", taskName)
	}

	// Track tasks in current path for cycle detection
	inPath := make(map[string]bool)
	var result []string

	var expand func(name string) error
	expand = func(name string) error {
		if inPath[name] {
			return fmt.Errorf("circular reference detected at task '%s'", name)
		}
		inPath[name] = true
		defer func() { inPath[name] = false }()

		t, ok := tasks[name]
		if !ok {
			return fmt.Errorf("referenced task '%s' not found", name)
		}

		if len(t.Parallel) > 0 {
			// This is a parallel task - recursively expand its references
			for _, ref := range t.Parallel {
				if err := expand(ref); err != nil {
					return err
				}
			}
		} else {
			// This is an executable task - add to result
			result = append(result, name)
		}
		return nil
	}

	for _, ref := range task.Parallel {
		if err := expand(ref); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// validateParallelTaskRefs is called from Validate to check parallel task references.
func validateParallelTaskRefs(cfg *Config) error {
	return ValidateParallelTasks(cfg)
}

// validateRequireList checks that a require list has no empty entries.
func validateRequireList(context string, reqs []string) error {
	for i, req := range reqs {
		if strings.TrimSpace(req) == "" {
			return fmt.Errorf("%s has an empty require entry at position %d", context, i+1)
		}
	}
	return nil
}

// ValidateDependencyGraph validates task dependency references and detects cycles.
// It checks:
// - All dependency references point to existing tasks
// - No self-references
// - No circular dependencies (A depends on B, B depends on A)
func ValidateDependencyGraph(cfg *Config) error {
	if cfg == nil || cfg.Tasks == nil {
		return nil
	}

	// First pass: validate all dependency references exist
	for name := range cfg.Tasks {
		task := cfg.Tasks[name]
		if err := validateDependencies(name, task, cfg.Tasks); err != nil {
			return err
		}
	}

	// Second pass: detect circular dependencies using DFS
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var path []string

	var detectCycle func(taskName string) error
	detectCycle = func(taskName string) error {
		if inStack[taskName] {
			// Found a cycle - build the cycle path for the error message
			cycleStart := -1
			for i, p := range path {
				if p == taskName {
					cycleStart = i
					break
				}
			}
			cyclePath := make([]string, len(path[cycleStart:])+1)
			copy(cyclePath, path[cycleStart:])
			cyclePath[len(cyclePath)-1] = taskName
			return fmt.Errorf("circular dependency detected: %s", strings.Join(cyclePath, " -> "))
		}

		if visited[taskName] {
			return nil
		}

		visited[taskName] = true
		inStack[taskName] = true
		path = append(path, taskName)

		task, ok := cfg.Tasks[taskName]
		if !ok {
			// Task doesn't exist - already caught in validateDependencies
			return nil
		}

		for _, dep := range task.Depends {
			for _, depName := range dep.TaskNames() {
				if err := detectCycle(depName); err != nil {
					return err
				}
			}
		}

		inStack[taskName] = false
		path = path[:len(path)-1]
		return nil
	}

	for name := range cfg.Tasks {
		// Reset per-traversal state but keep visited across iterations
		inStack = make(map[string]bool)
		path = nil
		if err := detectCycle(name); err != nil {
			return err
		}
	}

	return nil
}

// validateDependencies validates the dependency references for a single task.
func validateDependencies(taskName string, task TaskConfig, allTasks map[string]TaskConfig) error {
	for i, dep := range task.Depends {
		taskNames := dep.TaskNames()
		if len(taskNames) == 0 {
			return fmt.Errorf("task '%s' has an empty dependency at position %d", taskName, i+1)
		}

		for _, depName := range taskNames {
			// Check for self-reference
			if depName == taskName {
				return fmt.Errorf("task '%s' can't depend on itself", taskName)
			}

			// Check dependency exists
			if _, ok := allTasks[depName]; !ok {
				available := getTaskNames(allTasks)
				return fmt.Errorf("task '%s' depends on non-existent task '%s'. Available tasks: %s",
					taskName, depName, strings.Join(available, ", "))
			}
		}
	}
	return nil
}
