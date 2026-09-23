package doctor

import (
	"fmt"
	"path/filepath"

	"github.com/rileyhilliard/rr/internal/config"
)

// ConfigFileCheck verifies that a config file exists.
type ConfigFileCheck struct {
	ConfigPath string // Explicit path, or empty to search
}

func (c *ConfigFileCheck) Name() string     { return "config_file" }
func (c *ConfigFileCheck) Category() string { return "CONFIG" }

func (c *ConfigFileCheck) Run() CheckResult {
	path, err := config.Find(c.ConfigPath)
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("Error finding config: %v", err),
			Suggestion: "Check file permissions or run 'rr init' to create a config",
		}
	}

	// No project config is a warning: rr runs without .rr.yaml, using every
	// host from the global config.
	if path == "" {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusWarn,
			Message:    "No project config (.rr.yaml) found; using global hosts only",
			Suggestion: "Run 'rr init' to create a .rr.yaml config file",
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: fmt.Sprintf("Config file: %s", filepath.Base(path)),
	}
}

func (c *ConfigFileCheck) Fix() error {
	// Init would be called separately; this just reports it's fixable
	return nil
}

// ConfigSchemaCheck verifies that the config file has valid schema.
type ConfigSchemaCheck struct {
	ConfigPath string
}

func (c *ConfigSchemaCheck) Name() string     { return "config_schema" }
func (c *ConfigSchemaCheck) Category() string { return "CONFIG" }

func (c *ConfigSchemaCheck) Run() CheckResult {
	path, err := config.Find(c.ConfigPath)
	if err != nil {
		// ConfigFileCheck reports the lookup error
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    "Cannot validate schema: config file not found",
			Suggestion: "Check the --config path, or run 'rr init' to create a .rr.yaml",
		}
	}
	if path == "" {
		// Nothing to validate; ConfigFileCheck already warns about it
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: "No project config to validate",
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("Failed to load config: %v", err),
			Suggestion: "Check the YAML syntax in your config file",
		}
	}

	// Run validation on project config
	err = config.Validate(cfg)
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("Schema error: %v", err),
			Suggestion: "Fix the configuration errors in your .rr.yaml",
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: "Schema valid",
	}
}

func (c *ConfigSchemaCheck) Fix() error {
	return nil // Schema issues require manual intervention
}

// ConfigHostsCheck verifies hosts are configured in global config.
type ConfigHostsCheck struct {
	ConfigPath string // Project config path (for task count)
}

func (c *ConfigHostsCheck) Name() string     { return "config_hosts" }
func (c *ConfigHostsCheck) Category() string { return "CONFIG" }

func (c *ConfigHostsCheck) Run() CheckResult {
	// Load global config to check hosts
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: "Cannot check hosts: global config load error",
		}
	}

	numHosts := len(globalCfg.Hosts)

	// Try to load project config for task count
	numTasks := 0
	var projectCfg *config.Config
	path, _ := config.Find(c.ConfigPath)
	if path != "" {
		if cfg, err := config.Load(path); err == nil {
			projectCfg = cfg
			numTasks = len(cfg.Tasks)
		}
	}

	if numHosts == 0 {
		// With local_fallback on, commands run locally when no host is
		// available, so zero hosts doesn't block a run.
		resolved := &config.ResolvedConfig{Global: globalCfg, Project: projectCfg}
		if config.ResolveLocalFallback(resolved) {
			return CheckResult{
				Name:       c.Name(),
				Status:     StatusWarn,
				Message:    "No hosts configured; commands run locally (local_fallback)",
				Suggestion: "Add a host with 'rr host add' to run remotely",
			}
		}
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    "No hosts configured",
			Suggestion: "Add at least one host with 'rr host add'",
		}
	}

	msg := fmt.Sprintf("%d host%s configured", numHosts, pluralize(numHosts))
	if numTasks > 0 {
		msg += fmt.Sprintf(", %d task%s defined", numTasks, pluralize(numTasks))
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: msg,
	}
}

func (c *ConfigHostsCheck) Fix() error {
	return nil
}

// ConfigReservedNamesCheck verifies no reserved task names are used.
type ConfigReservedNamesCheck struct {
	ConfigPath string
}

func (c *ConfigReservedNamesCheck) Name() string     { return "config_reserved_names" }
func (c *ConfigReservedNamesCheck) Category() string { return "CONFIG" }

func (c *ConfigReservedNamesCheck) Run() CheckResult {
	path, err := config.Find(c.ConfigPath)
	if err != nil || path == "" {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass, // Not applicable without config
			Message: "No config to check",
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass, // Other check catches this
			Message: "Config load error",
		}
	}

	var reserved []string
	for name := range cfg.Tasks {
		if config.IsReservedTaskName(name) {
			reserved = append(reserved, name)
		}
	}

	if len(reserved) > 0 {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("Reserved task names used: %v", reserved),
			Suggestion: "Rename these tasks to avoid conflicts with built-in commands",
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: "No reserved task names",
	}
}

func (c *ConfigReservedNamesCheck) Fix() error {
	return nil
}

// NewConfigChecks creates all config-related checks.
func NewConfigChecks(configPath string) []Check {
	return []Check{
		&ConfigFileCheck{ConfigPath: configPath},
		&ConfigSchemaCheck{ConfigPath: configPath},
		&ConfigHostsCheck{ConfigPath: configPath},
		&ConfigReservedNamesCheck{ConfigPath: configPath},
	}
}
