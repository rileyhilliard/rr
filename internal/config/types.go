package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CurrentConfigVersion is the schema version for the config file.
// Increment when making breaking changes to the config structure.
const CurrentConfigVersion = 1

// CurrentGlobalConfigVersion is the schema version for the global config file.
const CurrentGlobalConfigVersion = 1

// GlobalConfig represents the global ~/.rr/config.yaml configuration file.
// This contains personal host configurations that shouldn't be shared with a team.
type GlobalConfig struct {
	Version  int             `yaml:"version" mapstructure:"version"`
	Hosts    map[string]Host `yaml:"hosts" mapstructure:"hosts"`
	Defaults GlobalDefaults  `yaml:"defaults" mapstructure:"defaults"`
	Logs     LogsConfig      `yaml:"logs" mapstructure:"logs"`
}

// LocalFallbackMode controls when rr falls back to local execution.
type LocalFallbackMode string

const (
	// LocalFallbackNever disables local fallback entirely.
	LocalFallbackNever LocalFallbackMode = "never"
	// LocalFallbackOnUnreachable falls back only when hosts are down or
	// unconfigured; hosts that are merely busy (locked) cause a wait/error.
	LocalFallbackOnUnreachable LocalFallbackMode = "on-unreachable"
	// LocalFallbackAlways falls back when hosts are unreachable OR all locked.
	LocalFallbackAlways LocalFallbackMode = "always"
)

// Enabled reports whether any form of local fallback is allowed.
func (m LocalFallbackMode) Enabled() bool {
	return m == LocalFallbackOnUnreachable || m == LocalFallbackAlways
}

// Valid reports whether the mode is one of the recognized values.
func (m LocalFallbackMode) Valid() bool {
	switch m {
	case LocalFallbackNever, LocalFallbackOnUnreachable, LocalFallbackAlways:
		return true
	}
	return false
}

// GlobalDefaults contains default settings for host selection and connection.
type GlobalDefaults struct {
	// ProbeTimeout is how long to wait when probing SSH hosts.
	ProbeTimeout time.Duration `yaml:"probe_timeout" mapstructure:"probe_timeout"`

	// LocalFallback controls falling back to local execution: never,
	// on-unreachable, or always. Booleans are accepted for backwards
	// compatibility (true = always, false = never).
	LocalFallback LocalFallbackMode `yaml:"local_fallback" mapstructure:"local_fallback"`

	// RewritePaths controls rewriting local absolute paths in commands and
	// task args to their remote equivalents before execution. Unset means
	// enabled; set to false to pass paths through untouched.
	RewritePaths *bool `yaml:"rewrite_paths,omitempty" mapstructure:"rewrite_paths"`
}

// ProjectDefaults contains default settings applied to all tasks in a project.
// These are merged with host-level and task-level settings.
type ProjectDefaults struct {
	// Setup commands run before each task command.
	// These are prepended after host setup_commands but before the task command.
	// Useful for sourcing environments, setting shell options, etc.
	Setup []string `yaml:"setup" mapstructure:"setup"`

	// Env contains environment variables applied to all tasks.
	// These override host env but are overridden by task-specific env.
	Env map[string]string `yaml:"env" mapstructure:"env"`
}

// Config represents the project-level .rr.yaml configuration file.
// This is shareable with the team and doesn't contain host connection details.
type Config struct {
	Version       int                `yaml:"version" mapstructure:"version"`
	Host          string             `yaml:"host,omitempty" mapstructure:"host"`   // Single host reference (backwards compat)
	Hosts         []string           `yaml:"hosts,omitempty" mapstructure:"hosts"` // Multiple host references for load balancing
	LocalFallback *LocalFallbackMode `yaml:"local_fallback,omitempty" mapstructure:"local_fallback"`
	RewritePaths  *bool              `yaml:"rewrite_paths,omitempty" mapstructure:"rewrite_paths"` // overrides defaults.rewrite_paths

	Defaults ProjectDefaults       `yaml:"defaults" mapstructure:"defaults"`
	Sync     SyncConfig            `yaml:"sync" mapstructure:"sync"`
	Lock     LockConfig            `yaml:"lock" mapstructure:"lock"`
	Tasks    map[string]TaskConfig `yaml:"tasks" mapstructure:"tasks"`
	Output   OutputConfig          `yaml:"output" mapstructure:"output"`
	Monitor  MonitorConfig         `yaml:"monitor" mapstructure:"monitor"`

	// Require lists tools that must be available on remote hosts.
	// Checked before sync; uses built-in installers when available.
	Require []string `yaml:"require,omitempty" mapstructure:"require"`
}

// Host defines a remote machine and its connection settings.
type Host struct {
	// SSH connection strings, tried in order until one succeeds.
	// Can be: hostname, user@hostname, or SSH config alias.
	SSH []string `yaml:"ssh" mapstructure:"ssh"`

	// Dir is the working directory on remote (where files sync to).
	// Supports variable expansion: ${PROJECT}, ${USER}, ${HOME}, and ~.
	Dir string `yaml:"dir" mapstructure:"dir"`

	// Tags for filtering hosts with --tag flag.
	Tags []string `yaml:"tags" mapstructure:"tags"`

	// Env contains environment variables specific to this host.
	Env map[string]string `yaml:"env" mapstructure:"env"`

	// Shell specifies how to invoke the shell for commands.
	// Default uses $SHELL -l -c (user's login shell) to ensure PATH is set up.
	// Use "sh -c" for minimal shell without profile loading.
	// Format: "<shell> <flags> <command-flag>" where the command will be appended.
	Shell string `yaml:"shell,omitempty" mapstructure:"shell"`

	// SetupCommands are run before each command (e.g., "source ~/.zshrc").
	// These are prepended to the actual command with && separators.
	SetupCommands []string `yaml:"setup_commands,omitempty" mapstructure:"setup_commands"`

	// Require lists tools that must be available on this host.
	// Uses built-in installers when available (go, node, cargo, etc.).
	Require []string `yaml:"require,omitempty" mapstructure:"require"`
}

// LockfileInvalidation maps a lockfile to remote directories that should be
// deleted when the lockfile has been modified since the last sync. This
// handles the case where a lockfile (e.g. bun.lock) changes locally but the
// corresponding install directory (e.g. node_modules/) is preserved on remote
// and never re-installed because the package manager sees no changes.
type LockfileInvalidation struct {
	// Lockfile is a filename relative to the project root (e.g. "bun.lock").
	Lockfile string `yaml:"lockfile" mapstructure:"lockfile"`

	// Dirs are remote directories to delete when the lockfile changes
	// (e.g. ["node_modules/", "frontend/node_modules/"]).
	Dirs []string `yaml:"dirs" mapstructure:"dirs"`
}

// SyncConfig controls file synchronization behavior.
type SyncConfig struct {
	// RespectGitignore adds --filter=':- .gitignore' to rsync so that
	// .gitignore patterns are applied as exclude rules during sync.
	RespectGitignore bool `yaml:"respect_gitignore" mapstructure:"respect_gitignore"`

	// Exclude patterns for files/dirs not sent to remote (rsync syntax).
	Exclude []string `yaml:"exclude" mapstructure:"exclude"`

	// Preserve patterns for files/dirs not deleted on remote even if missing locally.
	Preserve []string `yaml:"preserve" mapstructure:"preserve"`

	// Flags are extra rsync flags to pass.
	Flags []string `yaml:"flags" mapstructure:"flags"`

	// Invalidations maps lockfiles to remote directories to delete when the
	// lockfile changes. Prevents stale install directories (node_modules, .venv,
	// etc.) from being used after a lockfile update.
	Invalidations []LockfileInvalidation `yaml:"invalidations" mapstructure:"invalidations"`

	// WorktreeIsolation gives each linked git worktree its own remote
	// directory (${PROJECT} becomes "<repo>@<worktree>"). Defaults to true;
	// set false to share the main checkout's remote directory.
	WorktreeIsolation *bool `yaml:"worktree_isolation,omitempty" mapstructure:"worktree_isolation"`
}

// LockConfig controls the distributed lock behavior to prevent concurrent executions.
type LockConfig struct {
	// Enabled toggles locking on/off.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Timeout is how long to wait for a lock before giving up.
	Timeout time.Duration `yaml:"timeout" mapstructure:"timeout"`

	// WaitTimeout is how long to round-robin through hosts when all are locked.
	// Only applies when multiple hosts are configured and local_fallback is false.
	// If local_fallback is true, we immediately fall back to local when all hosts are locked.
	WaitTimeout time.Duration `yaml:"wait_timeout" mapstructure:"wait_timeout"`

	// Stale is when to consider a lock stale (holder probably crashed).
	Stale time.Duration `yaml:"stale" mapstructure:"stale"`

	// Dir is the directory where lock files are stored on the remote.
	Dir string `yaml:"dir" mapstructure:"dir"`
}

// TaskConfig defines a named task (command sequence).
type TaskConfig struct {
	// Description shown in rr --help.
	Description string `yaml:"description" mapstructure:"description"`

	// Run is the command to execute (for simple single-command tasks).
	Run string `yaml:"run" mapstructure:"run"`

	// Steps for multi-step tasks (mutually exclusive with Run).
	Steps []TaskStep `yaml:"steps" mapstructure:"steps"`

	// Depends lists tasks that must complete before this task runs.
	// Can be simple task names or parallel groups.
	// Simple: depends: [lint, test]
	// With parallel groups: depends: [{parallel: [lint, typecheck]}, test]
	Depends []DependencyItem `yaml:"depends" mapstructure:"depends"`

	// Hosts restricts this task to specific hosts.
	Hosts []string `yaml:"hosts" mapstructure:"hosts"`

	// Env contains environment variables for this task.
	Env map[string]string `yaml:"env" mapstructure:"env"`

	// Parallel is a list of task names to run concurrently.
	// When set, this task becomes a parallel orchestrator and Run/Steps are ignored.
	Parallel []string `yaml:"parallel" mapstructure:"parallel"`

	// Setup is a command that runs once per host before any subtasks execute on that host.
	// Only applies to parallel tasks. Useful for dependency installation, database migrations,
	// or other one-time setup that shouldn't be repeated for each subtask.
	// Setup failure aborts all subtasks on that host.
	Setup string `yaml:"setup,omitempty" mapstructure:"setup"`

	// FailFast stops parallel/dependency execution on first failure when true.
	FailFast bool `yaml:"fail_fast" mapstructure:"fail_fast"`

	// MaxParallel limits concurrent task execution. 0 means unlimited.
	// Only applies when Parallel is set.
	MaxParallel int `yaml:"max_parallel" mapstructure:"max_parallel"`

	// Timeout is the maximum duration for this task (e.g., "10m", "1h").
	// Applies to individual tasks and parallel orchestrators.
	Timeout string `yaml:"timeout" mapstructure:"timeout"`

	// Output controls how task output is displayed: "progress", "stream", "verbose", "quiet".
	// Overrides the global output settings for this task.
	Output string `yaml:"output" mapstructure:"output"`

	// Require lists additional tools needed for this specific task.
	// Combined with project and host requirements.
	Require []string `yaml:"require,omitempty" mapstructure:"require"`

	// Pull lists files or patterns to download from remote after command execution.
	// Can be simple strings (patterns) or objects with src/dest fields.
	// Simple: pull: [coverage.xml, htmlcov/]
	// With destinations: pull: [{src: dist/*.whl, dest: ./artifacts/}]
	Pull []PullItem `yaml:"pull,omitempty" mapstructure:"pull"`

	// ForwardArgs appends extra CLI arguments to each subtask's run command.
	// Only valid for parallel tasks where all subtasks use a single run command (not steps).
	// Enables: rr test-backend -k bond  (forwards "-k bond" to each subtask)
	ForwardArgs bool `yaml:"forward_args" mapstructure:"forward_args"`
}

// DependencyItem represents a single dependency which can be either
// a simple task name (string) or a parallel group of tasks.
type DependencyItem struct {
	// Task is set when this is a simple task reference (from a string in YAML).
	Task string `yaml:"-"`

	// Parallel is set when this is a group of tasks to run concurrently.
	Parallel []string `yaml:"parallel,omitempty" mapstructure:"parallel"`
}

// PullItem represents a file or pattern to pull from the remote host.
// Can be a simple string (path/pattern pulled to current directory)
// or an object with src and dest fields.
type PullItem struct {
	// Src is the remote path or glob pattern to pull.
	// Set from string format or object format.
	Src string `yaml:"src,omitempty" mapstructure:"src"`

	// Dest is the local destination directory.
	// If empty, files are pulled to the current directory.
	Dest string `yaml:"dest,omitempty" mapstructure:"dest"`
}

// UnmarshalYAML handles both string and object dependency formats.
// String format: "lint" becomes DependencyItem{Task: "lint"}
// Object format: {parallel: [lint, typecheck]} becomes DependencyItem{Parallel: [lint, typecheck]}
func (d *DependencyItem) UnmarshalYAML(value *yaml.Node) error {
	// If it's a scalar (string), treat as a simple task name
	if value.Kind == yaml.ScalarNode {
		d.Task = value.Value
		return nil
	}

	// Otherwise, expect an object with "parallel" key
	type depItem DependencyItem
	return value.Decode((*depItem)(d))
}

// DependencyItemFromInterface converts a generic interface (from viper/mapstructure)
// to a DependencyItem. Handles both string and map formats.
// Returns an error if the input type is unsupported or contains invalid elements.
func DependencyItemFromInterface(v interface{}) (DependencyItem, error) {
	switch val := v.(type) {
	case string:
		return DependencyItem{Task: val}, nil
	case map[string]interface{}:
		d := DependencyItem{}
		parallel, ok := val["parallel"]
		if !ok {
			return DependencyItem{}, fmt.Errorf("dependency map must contain 'parallel' key")
		}
		switch p := parallel.(type) {
		case []interface{}:
			for i, item := range p {
				s, ok := item.(string)
				if !ok {
					return DependencyItem{}, fmt.Errorf("parallel[%d]: expected string, got %T", i, item)
				}
				d.Parallel = append(d.Parallel, s)
			}
		case []string:
			d.Parallel = p
		default:
			return DependencyItem{}, fmt.Errorf("parallel: expected array of strings, got %T", parallel)
		}
		return d, nil
	default:
		return DependencyItem{}, fmt.Errorf("dependency: expected string or map, got %T", v)
	}
}

// IsParallel returns true if this dependency is a parallel group.
func (d *DependencyItem) IsParallel() bool {
	return len(d.Parallel) > 0
}

// UnmarshalYAML handles both string and object pull item formats.
// String format: "coverage.xml" becomes PullItem{Src: "coverage.xml"}
// Object format: {src: "dist/*.whl", dest: "./artifacts/"} becomes PullItem{Src: "dist/*.whl", Dest: "./artifacts/"}
func (p *PullItem) UnmarshalYAML(value *yaml.Node) error {
	// If it's a scalar (string), treat as a simple source path
	if value.Kind == yaml.ScalarNode {
		if value.Value == "" {
			return fmt.Errorf("pull item cannot be empty")
		}
		p.Src = value.Value
		return nil
	}

	// Otherwise, expect an object with "src" and optionally "dest"
	type pullItem PullItem
	if err := value.Decode((*pullItem)(p)); err != nil {
		return err
	}
	if p.Src == "" {
		return fmt.Errorf("pull item must have 'src' field")
	}
	return nil
}

// PullItemFromInterface converts a generic interface (from viper/mapstructure)
// to a PullItem. Handles both string and map formats.
func PullItemFromInterface(v interface{}) (PullItem, error) {
	switch val := v.(type) {
	case string:
		if strings.TrimSpace(val) == "" {
			return PullItem{}, fmt.Errorf("pull item cannot be empty string")
		}
		return PullItem{Src: val}, nil
	case map[string]interface{}:
		p := PullItem{}
		if src, ok := val["src"]; ok {
			if s, ok := src.(string); ok {
				p.Src = s
			} else {
				return PullItem{}, fmt.Errorf("pull src: expected string, got %T", src)
			}
		}
		if dest, ok := val["dest"]; ok {
			if d, ok := dest.(string); ok {
				p.Dest = d
			} else {
				return PullItem{}, fmt.Errorf("pull dest: expected string, got %T", dest)
			}
		}
		if p.Src == "" {
			return PullItem{}, fmt.Errorf("pull item must have 'src' field")
		}
		return p, nil
	default:
		return PullItem{}, fmt.Errorf("pull item: expected string or map, got %T", v)
	}
}

// TaskNames returns all task names referenced by this dependency.
func (d *DependencyItem) TaskNames() []string {
	if d.Task != "" {
		return []string{d.Task}
	}
	return d.Parallel
}

// TaskStep is a single step in a multi-step task.
type TaskStep struct {
	// Name identifies this step in output.
	Name string `yaml:"name" mapstructure:"name"`

	// Run is the command to execute.
	Run string `yaml:"run" mapstructure:"run"`

	// OnFail controls behavior when step fails: "stop" (default) or "continue".
	OnFail string `yaml:"on_fail" mapstructure:"on_fail"`
}

// OutputConfig controls terminal output formatting.
type OutputConfig struct {
	// Color mode: "auto", "always", or "never".
	// "auto" disables color when output is piped.
	Color string `yaml:"color" mapstructure:"color"`

	// Format for command output: "auto", "generic", "pytest", "jest", "go", "cargo".
	Format string `yaml:"format" mapstructure:"format"`

	// Timing shows timing for each phase.
	Timing bool `yaml:"timing" mapstructure:"timing"`

	// Verbosity level: "quiet", "normal", or "verbose".
	Verbosity string `yaml:"verbosity" mapstructure:"verbosity"`
}

// MonitorConfig controls the resource monitoring dashboard.
type MonitorConfig struct {
	// Interval between metric updates (e.g., "2s", "5s").
	Interval string `yaml:"interval" mapstructure:"interval"`

	// Timeout for per-host connection and collection (e.g., "8s", "10s").
	// Shorter timeouts provide faster feedback when hosts are unreachable.
	Timeout string `yaml:"timeout" mapstructure:"timeout"`

	// Thresholds for metric severity coloring.
	Thresholds ThresholdConfig `yaml:"thresholds" mapstructure:"thresholds"`

	// Exclude lists host names to exclude from the monitor dashboard.
	Exclude []string `yaml:"exclude" mapstructure:"exclude"`

	// Alerts controls threshold alerting (bell, card flash, command hook).
	Alerts AlertsConfig `yaml:"alerts" mapstructure:"alerts"`
}

// AlertsConfig controls threshold alerting in the monitor dashboard.
// An alert fires when a metric crosses its configured critical threshold and
// re-arms only after the metric falls back below the warning threshold.
type AlertsConfig struct {
	// Enabled turns threshold alerting on. Default false.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Bell rings the terminal bell when a new alert fires. Default true.
	Bell bool `yaml:"bell" mapstructure:"bell"`

	// Flash renders an alerting host's card border in the critical color.
	// Default true.
	Flash bool `yaml:"flash" mapstructure:"flash"`

	// Cooldown is the minimum time between re-fires for the same host and
	// metric (e.g., "60s", "5m"). Default "60s".
	Cooldown string `yaml:"cooldown" mapstructure:"cooldown"`

	// OnAlert is an optional local shell command run when an alert fires.
	// It receives RR_HOST, RR_METRIC, and RR_VALUE in its environment.
	OnAlert string `yaml:"on_alert" mapstructure:"on_alert"`
}

// ThresholdConfig defines warning and critical thresholds for metrics.
type ThresholdConfig struct {
	CPU ThresholdValues `yaml:"cpu" mapstructure:"cpu"`
	RAM ThresholdValues `yaml:"ram" mapstructure:"ram"`
	GPU ThresholdValues `yaml:"gpu" mapstructure:"gpu"`
}

// ThresholdValues contains the percentage thresholds for a metric type.
type ThresholdValues struct {
	// Warning threshold percentage (default 70).
	Warning int `yaml:"warning" mapstructure:"warning"`

	// Critical threshold percentage (default 90).
	Critical int `yaml:"critical" mapstructure:"critical"`
}

// LogsConfig controls log file retention for parallel task execution.
type LogsConfig struct {
	// Dir is the directory where log files are stored.
	// Default: ~/.rr/logs
	Dir string `yaml:"dir" mapstructure:"dir"`

	// KeepRuns is the number of recent runs to keep logs for.
	// Default: 10. Set to 0 to disable run-based cleanup.
	KeepRuns int `yaml:"keep_runs" mapstructure:"keep_runs"`

	// KeepDays is the number of days to keep logs.
	// Default: 0 (disabled). When set, logs older than this are deleted.
	KeepDays int `yaml:"keep_days" mapstructure:"keep_days"`

	// MaxSizeMB is the maximum total size of logs in megabytes.
	// Default: 0 (disabled). When set, oldest logs are deleted to stay under limit.
	MaxSizeMB int `yaml:"max_size_mb" mapstructure:"max_size_mb"`
}

// DefaultGlobalConfig returns a GlobalConfig with sensible defaults.
func DefaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Version: CurrentGlobalConfigVersion,
		Hosts:   make(map[string]Host),
		Defaults: GlobalDefaults{
			ProbeTimeout:  2 * time.Second,
			LocalFallback: LocalFallbackNever,
		},
		Logs: LogsConfig{
			Dir:      "~/.rr/logs",
			KeepRuns: 10,
		},
	}
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Version: CurrentConfigVersion,
		Host:    "",
		Sync: SyncConfig{
			RespectGitignore: true,
			// Bare patterns (no trailing slash) for .git, .venv, and
			// node_modules: in git worktrees .git is a FILE and
			// node_modules may be a symlink. Dir-only patterns miss those
			// and rsync then fails replacing a remote dir with a file
			// (partial transfer, exit 23).
			Exclude: []string{
				".git",
				".venv",
				"__pycache__/",
				"*.pyc",
				"node_modules",
				".mypy_cache/",
				".pytest_cache/",
				".ruff_cache/",
				".DS_Store",
				"*.log",
				".claude/",
				".cursor/",
				".aider/",
				".copilot/",
			},
			Preserve: []string{
				".venv/",
				"node_modules/",
				"data/",
				".cache/",
			},
			Flags: []string{},
			Invalidations: []LockfileInvalidation{
				{Lockfile: "bun.lock", Dirs: []string{"node_modules/"}},
				{Lockfile: "package-lock.json", Dirs: []string{"node_modules/"}},
				{Lockfile: "yarn.lock", Dirs: []string{"node_modules/"}},
				{Lockfile: "pnpm-lock.yaml", Dirs: []string{"node_modules/"}},
				{Lockfile: "poetry.lock", Dirs: []string{".venv/"}},
				{Lockfile: "Pipfile.lock", Dirs: []string{".venv/"}},
			},
		},
		Lock: LockConfig{
			Enabled:     true,
			Timeout:     5 * time.Minute,
			WaitTimeout: 1 * time.Minute,
			Stale:       3 * time.Minute,
			Dir:         "/tmp/rr-locks",
		},
		Tasks: make(map[string]TaskConfig),
		Output: OutputConfig{
			Color:     "auto",
			Format:    "auto",
			Timing:    true,
			Verbosity: "normal",
		},
		Monitor: MonitorConfig{
			Interval: "1s",
			Timeout:  "8s",
			Thresholds: ThresholdConfig{
				CPU: ThresholdValues{Warning: 70, Critical: 90},
				RAM: ThresholdValues{Warning: 70, Critical: 90},
				GPU: ThresholdValues{Warning: 70, Critical: 90},
			},
			Exclude: []string{},
			Alerts: AlertsConfig{
				Enabled:  false,
				Bell:     true,
				Flash:    true,
				Cooldown: "60s",
			},
		},
	}
}

// IsParallelTask returns true if the task is configured to run subtasks in parallel.
func IsParallelTask(task *TaskConfig) bool {
	return len(task.Parallel) > 0
}

// HasDependencies returns true if the task has dependencies that must run first.
func HasDependencies(task *TaskConfig) bool {
	return len(task.Depends) > 0
}
