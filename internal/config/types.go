package config

import "time"

// CurrentConfigVersion is the schema version for the config file.
// Increment when making breaking changes to the config structure.
const CurrentConfigVersion = 1

// Config represents the complete .rr.yaml configuration file.
type Config struct {
	Version       int                   `yaml:"version" mapstructure:"version"`
	Hosts         map[string]Host       `yaml:"hosts" mapstructure:"hosts"`
	Default       string                `yaml:"default" mapstructure:"default"`
	LocalFallback bool                  `yaml:"local_fallback" mapstructure:"local_fallback"`
	ProbeTimeout  time.Duration         `yaml:"probe_timeout" mapstructure:"probe_timeout"`
	Sync          SyncConfig            `yaml:"sync" mapstructure:"sync"`
	Lock          LockConfig            `yaml:"lock" mapstructure:"lock"`
	Tasks         map[string]TaskConfig `yaml:"tasks" mapstructure:"tasks"`
	Output        OutputConfig          `yaml:"output" mapstructure:"output"`
	Monitor       MonitorConfig         `yaml:"monitor" mapstructure:"monitor"`
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
}

// SyncConfig controls file synchronization behavior.
type SyncConfig struct {
	// Exclude patterns for files/dirs not sent to remote (rsync syntax).
	Exclude []string `yaml:"exclude" mapstructure:"exclude"`

	// Preserve patterns for files/dirs not deleted on remote even if missing locally.
	Preserve []string `yaml:"preserve" mapstructure:"preserve"`

	// Flags are extra rsync flags to pass.
	Flags []string `yaml:"flags" mapstructure:"flags"`
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

	// Hosts restricts this task to specific hosts.
	Hosts []string `yaml:"hosts" mapstructure:"hosts"`

	// Env contains environment variables for this task.
	Env map[string]string `yaml:"env" mapstructure:"env"`
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

	// Thresholds for metric severity coloring.
	Thresholds ThresholdConfig `yaml:"thresholds" mapstructure:"thresholds"`

	// Exclude lists host names to exclude from the monitor dashboard.
	Exclude []string `yaml:"exclude" mapstructure:"exclude"`
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

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Version:       CurrentConfigVersion,
		Hosts:         make(map[string]Host),
		LocalFallback: false,
		ProbeTimeout:  2 * time.Second,
		Sync: SyncConfig{
			Exclude: []string{
				".git/",
				".venv/",
				"__pycache__/",
				"*.pyc",
				"node_modules/",
				".mypy_cache/",
				".pytest_cache/",
				".ruff_cache/",
				".DS_Store",
				"*.log",
			},
			Preserve: []string{
				".venv/",
				"node_modules/",
				"data/",
				".cache/",
			},
			Flags: []string{},
		},
		Lock: LockConfig{
			Enabled:     true,
			Timeout:     5 * time.Minute,
			WaitTimeout: 1 * time.Minute,
			Stale:       10 * time.Minute,
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
			Interval: "2s",
			Thresholds: ThresholdConfig{
				CPU: ThresholdValues{Warning: 70, Critical: 90},
				RAM: ThresholdValues{Warning: 70, Critical: 90},
				GPU: ThresholdValues{Warning: 70, Critical: 90},
			},
			Exclude: []string{},
		},
	}
}
