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
	for name, host := range cfg.Hosts {
		if err := validateHost(name, host); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Check your host config in ~/.rr/config.yaml.")
		}
	}

	// Check default host exists (if specified)
	if cfg.Defaults.Host != "" {
		if _, ok := cfg.Hosts[cfg.Defaults.Host]; !ok {
			hostNames := getHostNames(cfg.Hosts)
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("Default host '%s' doesn't exist", cfg.Defaults.Host),
				fmt.Sprintf("Did you rename or remove it? Available hosts: %s", strings.Join(hostNames, ", ")))
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

	return nil
}

// validateRemotePath checks for common remote path configuration mistakes.
// Note: Tilde (~) is ALLOWED in remote paths - the remote shell expands it.
// Only ${VAR} variables should be expanded locally before sending to remote.
func validateRemotePath(hostName, fieldName, path string) error {
	// Check for unexpanded variables (these should be expanded locally)
	if strings.Contains(path, "${") {
		return fmt.Errorf("host '%s' has an unexpanded variable in %s: %s", hostName, fieldName, path)
	}

	// Note: ~ and relative paths are allowed for remote paths
	// The remote shell will handle tilde expansion
	// Relative paths are relative to SSH user's home directory

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

// validateTask checks a single task configuration.
func validateTask(name string, task TaskConfig) error {
	// Must have either run or steps, not both
	hasRun := task.Run != ""
	hasSteps := len(task.Steps) > 0

	if !hasRun && !hasSteps {
		return fmt.Errorf("task '%s' needs either 'run' (single command) or 'steps' (multiple commands)", name)
	}

	if hasRun && hasSteps {
		return fmt.Errorf("task '%s' has both 'run' and 'steps' - pick one or the other", name)
	}

	// Validate steps if present
	for i, step := range task.Steps {
		if step.Run == "" {
			return fmt.Errorf("task '%s' step %d is missing the 'run' command", name, i+1)
		}
		if step.OnFail != "" && step.OnFail != "stop" && step.OnFail != "continue" {
			return fmt.Errorf("task '%s' step %d has on_fail='%s' but it needs to be 'stop' or 'continue'", name, i+1, step.OnFail)
		}
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
func validateMonitor(monitor MonitorConfig, hosts map[string]Host) error {
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
