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
}

// ValidationOption controls validation behavior.
type ValidationOption func(*validationContext)

type validationContext struct {
	allowNoHosts bool
}

// AllowNoHosts allows configs without hosts (for 'rr init').
func AllowNoHosts() ValidationOption {
	return func(ctx *validationContext) {
		ctx.allowNoHosts = true
	}
}

// Validate checks the config for errors and returns structured error messages.
func Validate(cfg *Config, opts ...ValidationOption) error {
	ctx := &validationContext{}
	for _, opt := range opts {
		opt(ctx)
	}

	// Check version
	if cfg.Version > CurrentConfigVersion {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Config version %d is newer than supported version %d", cfg.Version, CurrentConfigVersion),
			"Update rr to the latest version: 'rr update' or check https://github.com/rileyhilliard/rr/releases")
	}

	// Check hosts exist (unless explicitly allowed)
	if !ctx.allowNoHosts && len(cfg.Hosts) == 0 {
		return errors.New(errors.ErrConfig,
			"No hosts defined in config",
			"Add at least one host under 'hosts:' in your .rr.yaml, or run 'rr init' to create one")
	}

	// Validate each host
	for name, host := range cfg.Hosts {
		if err := validateHost(name, host); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Fix the host configuration in your .rr.yaml")
		}
	}

	// Check default host exists (if specified)
	if cfg.Default != "" && cfg.Default != "auto" {
		if _, ok := cfg.Hosts[cfg.Default]; !ok {
			hostNames := getHostNames(cfg.Hosts)
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("Default host '%s' not found", cfg.Default),
				fmt.Sprintf("Available hosts: %s", strings.Join(hostNames, ", ")))
		}
	}

	// Check for reserved task names
	for name := range cfg.Tasks {
		if ReservedTaskNames[name] {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("'%s' is a reserved command name and cannot be used as a task", name),
				"Rename your task to something else (e.g., 'my-"+name+"')")
		}

		if err := validateTask(name, cfg.Tasks[name]); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Fix the task configuration in your .rr.yaml")
		}
	}

	// Validate output config
	if err := validateOutput(cfg.Output); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Fix the output configuration in your .rr.yaml")
	}

	// Validate lock config
	if err := validateLock(cfg.Lock); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Fix the lock configuration in your .rr.yaml")
	}

	// Validate monitor config
	if err := validateMonitor(cfg.Monitor, cfg.Hosts); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig, err.Error(), "Fix the monitor configuration in your .rr.yaml")
	}

	return nil
}

// validateHost checks a single host configuration.
func validateHost(name string, host Host) error {
	if len(host.SSH) == 0 {
		return fmt.Errorf("host '%s': missing ssh connection strings", name)
	}

	for i, ssh := range host.SSH {
		if strings.TrimSpace(ssh) == "" {
			return fmt.Errorf("host '%s': ssh[%d] is empty", name, i)
		}
	}

	if host.Dir == "" {
		return fmt.Errorf("host '%s': missing dir (working directory)", name)
	}

	return nil
}

// validateTask checks a single task configuration.
func validateTask(name string, task TaskConfig) error {
	// Must have either run or steps, not both
	hasRun := task.Run != ""
	hasSteps := len(task.Steps) > 0

	if !hasRun && !hasSteps {
		return fmt.Errorf("task '%s': must have either 'run' or 'steps'", name)
	}

	if hasRun && hasSteps {
		return fmt.Errorf("task '%s': cannot have both 'run' and 'steps'", name)
	}

	// Validate steps if present
	for i, step := range task.Steps {
		if step.Run == "" {
			return fmt.Errorf("task '%s': step %d missing 'run' command", name, i+1)
		}
		if step.OnFail != "" && step.OnFail != "stop" && step.OnFail != "continue" {
			return fmt.Errorf("task '%s': step %d has invalid on_fail '%s' (must be 'stop' or 'continue')", name, i+1, step.OnFail)
		}
	}

	return nil
}

// validateOutput checks output configuration.
func validateOutput(out OutputConfig) error {
	validColors := map[string]bool{"auto": true, "always": true, "never": true, "": true}
	if !validColors[out.Color] {
		return fmt.Errorf("output.color must be 'auto', 'always', or 'never' (got '%s')", out.Color)
	}

	validFormats := map[string]bool{
		"auto": true, "generic": true, "pytest": true,
		"jest": true, "go": true, "cargo": true, "": true,
	}
	if !validFormats[out.Format] {
		return fmt.Errorf("output.format must be one of: auto, generic, pytest, jest, go, cargo (got '%s')", out.Format)
	}

	validVerbosity := map[string]bool{"quiet": true, "normal": true, "verbose": true, "": true}
	if !validVerbosity[out.Verbosity] {
		return fmt.Errorf("output.verbosity must be 'quiet', 'normal', or 'verbose' (got '%s')", out.Verbosity)
	}

	return nil
}

// validateLock checks lock configuration.
func validateLock(lock LockConfig) error {
	if lock.Timeout < 0 {
		return fmt.Errorf("lock.timeout cannot be negative")
	}
	if lock.Stale < 0 {
		return fmt.Errorf("lock.stale cannot be negative")
	}
	if lock.Enabled && lock.Timeout > 0 && lock.Stale > 0 && lock.Timeout > lock.Stale {
		return fmt.Errorf("lock.timeout (%v) should be less than lock.stale (%v)", lock.Timeout, lock.Stale)
	}
	return nil
}

// validateMonitor checks monitor configuration.
func validateMonitor(monitor MonitorConfig, hosts map[string]Host) error {
	// Validate interval format if specified
	if monitor.Interval != "" {
		if _, err := time.ParseDuration(monitor.Interval); err != nil {
			return fmt.Errorf("monitor.interval '%s' is not a valid duration (try '2s', '5s', '1m')", monitor.Interval)
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

	// Validate excluded hosts exist (warning only, don't fail validation)
	// This allows excluding hosts that might be temporarily removed from config
	for _, excluded := range monitor.Exclude {
		if _, ok := hosts[excluded]; !ok && len(hosts) > 0 {
			// Just validate that it's not empty, don't require it to exist
			if strings.TrimSpace(excluded) == "" {
				return fmt.Errorf("monitor.exclude contains an empty entry")
			}
		}
	}

	return nil
}

// validateThresholds checks a threshold configuration for a single metric type.
func validateThresholds(name string, thresh ThresholdValues) error {
	// Only validate if non-zero (0 means use default)
	if thresh.Warning < 0 || thresh.Warning > 100 {
		return fmt.Errorf("monitor.thresholds.%s.warning must be between 0 and 100 (got %d)", name, thresh.Warning)
	}
	if thresh.Critical < 0 || thresh.Critical > 100 {
		return fmt.Errorf("monitor.thresholds.%s.critical must be between 0 and 100 (got %d)", name, thresh.Critical)
	}
	// Warning should be less than critical (if both are non-zero)
	if thresh.Warning > 0 && thresh.Critical > 0 && thresh.Warning >= thresh.Critical {
		return fmt.Errorf("monitor.thresholds.%s.warning (%d) should be less than critical (%d)", name, thresh.Warning, thresh.Critical)
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
