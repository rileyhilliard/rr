package config

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/util"
	"github.com/spf13/viper"
)

const (
	// ConfigFileName is the default config file name.
	ConfigFileName = ".rr.yaml"
	// GlobalConfigDir is the directory for global config (~/.rr/).
	GlobalConfigDir = ".rr"
	// GlobalConfigFile is the global config file name.
	GlobalConfigFile = "config.yaml"
)

// Load reads project config from the specified path.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return nil, errors.WrapWithCode(err, errors.ErrConfig,
				"Can't find the config file",
				"Looks like you haven't set up shop here yet. Run 'rr init' to get started.")
		}
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't read the config file",
			"Something's off with your .rr.yaml. Check that it's valid YAML.")
	}

	return parseConfig(v, path)
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.WrapWithCode(err, errors.ErrConfig,
			"Can't find your home directory",
			"This is unusual - check your environment.")
	}
	return filepath.Join(home, GlobalConfigDir, GlobalConfigFile), nil
}

// EnsureGlobalConfigDir creates ~/.rr/ if it doesn't exist.
func EnsureGlobalConfigDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Can't find your home directory",
			"This is unusual - check your environment.")
	}

	dir := filepath.Join(home, GlobalConfigDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Can't create global config directory "+dir,
			"Check your permissions.")
	}
	return nil
}

// LoadGlobal reads global config from ~/.rr/config.yaml.
// Returns default global config if file doesn't exist.
func LoadGlobal() (*GlobalConfig, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return nil, err
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Return defaults if no global config exists yet
		return DefaultGlobalConfig(), nil
	}

	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't read global config",
			"Check your ~/.rr/config.yaml for valid YAML syntax.")
	}

	return parseGlobalConfig(v, path)
}

// SaveGlobal writes global config to ~/.rr/config.yaml.
func SaveGlobal(cfg *GlobalConfig) error {
	if err := EnsureGlobalConfigDir(); err != nil {
		return err
	}

	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}

	v := viper.New()
	v.Set("version", cfg.Version)
	v.Set("hosts", cfg.Hosts)
	v.Set("defaults", cfg.Defaults)

	if err := v.WriteConfigAs(path); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Can't save global config to "+path,
			"Check your permissions.")
	}

	return nil
}

// parseGlobalConfig converts viper config to GlobalConfig struct.
func parseGlobalConfig(v *viper.Viper, path string) (*GlobalConfig, error) {
	cfg := DefaultGlobalConfig()

	// Set duration defaults for global config
	v.SetDefault("defaults.probe_timeout", "2s")
	v.SetDefault("defaults.local_fallback", false)

	if err := v.Unmarshal(cfg); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Global config has some issues",
			"Check the YAML syntax in "+path+" - something's not parsing right.")
	}

	// Expand variables in host directories
	for name, host := range cfg.Hosts {
		host.Dir = ExpandRemote(host.Dir)
		cfg.Hosts[name] = host
	}

	return cfg, nil
}

// Find locates the project config file using the search order:
// 1. Explicit path (from --config flag)
// 2. .rr.yaml in current directory
// 3. .rr.yaml in parent directories (stops at git root or home)
//
// Returns the path to the config file, or empty string if not found.
// Note: Global config (~/.rr/config.yaml) is loaded separately via LoadGlobal().
func Find(explicit string) (string, error) {
	// 1. Explicit path takes precedence
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			if os.IsNotExist(err) {
				return "", errors.WrapWithCode(err, errors.ErrConfig,
					"Can't find config file at "+explicit,
					"Double-check that path - it doesn't seem to exist.")
			}
			return "", errors.WrapWithCode(err, errors.ErrConfig,
				"Can't access config file at "+explicit,
				"Looks like a permissions issue. Check you have read access.")
		}
		return explicit, nil
	}

	// 2. Current directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.WrapWithCode(err, errors.ErrConfig,
			"Can't figure out what directory you're in",
			"This is unusual - check your directory permissions.")
	}

	localConfig := filepath.Join(cwd, ConfigFileName)
	if _, err := os.Stat(localConfig); err == nil {
		return localConfig, nil
	}

	// 3. Walk up to parent directories
	home, _ := os.UserHomeDir()
	dir := cwd
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		if home != "" && parent == home {
			// Don't go above home directory
			break
		}
		dir = parent

		// Check for .rr.yaml
		configPath := filepath.Join(dir, ConfigFileName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		// Stop at git root
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			break
		}
	}

	return "", nil
}

// LoadOrDefault loads config from the found path, or returns defaults if not found.
// This is useful for commands like 'rr init' that should work without existing config.
func LoadOrDefault() (*Config, error) {
	path, err := Find("")
	if err != nil {
		return nil, err
	}

	if path == "" {
		return DefaultConfig(), nil
	}

	return Load(path)
}

// ConfigSource indicates where configuration was loaded from.
type ConfigSource int

const (
	// GlobalOnly means only global config was found.
	GlobalOnly ConfigSource = iota
	// ProjectOnly means only project config was found (global defaults used).
	ProjectOnly
	// Both means both global and project configs were found.
	Both
)

// ResolvedConfig contains both global and project configuration.
type ResolvedConfig struct {
	Global  *GlobalConfig
	Project *Config
	Source  ConfigSource
}

// LoadResolved loads both global and project configuration.
// Global config is always loaded (or defaults used).
// Project config is loaded if found (explicit path or search).
func LoadResolved(explicitPath string) (*ResolvedConfig, error) {
	resolved := &ResolvedConfig{}

	// Always load global config
	global, err := LoadGlobal()
	if err != nil {
		return nil, err
	}
	resolved.Global = global

	// Find project config
	projectPath, err := Find(explicitPath)
	if err != nil {
		return nil, err
	}

	// Determine source and load project config
	if projectPath == "" {
		// No project config found
		resolved.Project = DefaultConfig()
		resolved.Source = GlobalOnly
	} else {
		// Load project config
		project, err := Load(projectPath)
		if err != nil {
			return nil, err
		}
		resolved.Project = project

		// Check if global config has hosts (file existed and was non-empty)
		if len(global.Hosts) > 0 {
			resolved.Source = Both
		} else {
			resolved.Source = ProjectOnly
		}
	}

	return resolved, nil
}

// ResolveHosts determines which hosts to use based on resolution order:
// 1. preferred (from --host flag) - single host
// 2. project.Hosts (from .rr.yaml hosts field) - multiple hosts
// 3. project.Host (from .rr.yaml host field) - single host (backwards compat)
// 4. If local_fallback is enabled and no hosts specified in project, return empty (local mode)
// 5. All global hosts (default behavior for load balancing)
//
// Returns list of host names and map of host configs.
// Empty hosts list with nil error indicates local-only mode.
func ResolveHosts(resolved *ResolvedConfig, preferred string) ([]string, map[string]Host, error) {
	if resolved.Global == nil {
		return nil, nil, errors.New(errors.ErrConfig,
			"Global config not loaded",
			"This is unexpected - try running the command again.")
	}

	localFallback := ResolveLocalFallback(resolved)

	// Check for hosts early, but allow empty if local_fallback is enabled
	if len(resolved.Global.Hosts) == 0 && !localFallback {
		return nil, nil, errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add hosts to ~/.rr/config.yaml or run 'rr host add'.")
	}

	var hostNames []string
	projectSpecifiesHosts := false

	// 1. Preferred from flag - single host
	if preferred != "" {
		hostNames = []string{preferred}
	}

	// 2. Project config hosts list (plural)
	if len(hostNames) == 0 && resolved.Project != nil && len(resolved.Project.Hosts) > 0 {
		hostNames = resolved.Project.Hosts
		projectSpecifiesHosts = true
	}

	// 3. Project config host reference (singular, backwards compat)
	if len(hostNames) == 0 && resolved.Project != nil && resolved.Project.Host != "" {
		hostNames = []string{resolved.Project.Host}
		projectSpecifiesHosts = true
	}

	// 4. If project explicitly sets local_fallback: true and doesn't specify hosts, run locally
	// This allows users to set local_fallback: true with no hosts to force local execution
	// Only triggers when PROJECT config has local_fallback (not just global), so existing
	// setups that rely on global hosts + global local_fallback continue to work.
	projectSetsLocalFallback := resolved.Project != nil && resolved.Project.LocalFallback != nil && *resolved.Project.LocalFallback
	if len(hostNames) == 0 && projectSetsLocalFallback && !projectSpecifiesHosts {
		return []string{}, make(map[string]Host), nil
	}

	// 5. All global hosts (default - enables load balancing across everything)
	// Uses alphabetical order for deterministic behavior
	if len(hostNames) == 0 {
		for name := range resolved.Global.Hosts {
			hostNames = append(hostNames, name)
		}
		sort.Strings(hostNames)
	}

	// Validate all hosts exist and build config map
	hosts := make(map[string]Host)
	var available []string
	for name := range resolved.Global.Hosts {
		available = append(available, name)
	}

	for _, name := range hostNames {
		host, ok := resolved.Global.Hosts[name]
		if !ok {
			return nil, nil, errors.New(errors.ErrConfig,
				"Host '"+name+"' not found in global config",
				"Available hosts: "+util.JoinOrNone(available)+". Check ~/.rr/config.yaml.")
		}
		hosts[name] = host
	}

	return hostNames, hosts, nil
}

// ResolveHost determines which host to use based on resolution order.
// This is a convenience wrapper around ResolveHosts that returns only the first host.
// Used when a single host is needed (e.g., for display purposes).
func ResolveHost(resolved *ResolvedConfig, preferred string) (string, *Host, error) {
	names, hosts, err := ResolveHosts(resolved, preferred)
	if err != nil {
		return "", nil, err
	}
	if len(names) == 0 {
		return "", nil, errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add hosts to ~/.rr/config.yaml or run 'rr host add'.")
	}
	host := hosts[names[0]]
	return names[0], &host, nil
}

// ResolveLocalFallback determines whether local fallback is enabled.
// Project config overrides global config when explicitly set.
func ResolveLocalFallback(resolved *ResolvedConfig) bool {
	// Project config takes precedence when explicitly set
	if resolved.Project != nil && resolved.Project.LocalFallback != nil {
		return *resolved.Project.LocalFallback
	}
	// Fall back to global config
	if resolved.Global != nil {
		return resolved.Global.Defaults.LocalFallback
	}
	return false
}

// parseConfig converts viper config to our Config struct with defaults merged in.
func parseConfig(v *viper.Viper, path string) (*Config, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Set up duration parsing for lock timeouts
	setDurationDefaults(v)

	// Unmarshal into config
	if err := v.Unmarshal(cfg); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Config file has some issues",
			"Check the YAML syntax in "+path+" - something's not parsing right.")
	}

	return cfg, nil
}

// setDurationDefaults configures viper to handle duration strings for project config.
func setDurationDefaults(v *viper.Viper) {
	// Viper handles duration parsing automatically for time.Duration fields
	// but we need to help with nested structs using DecodeHook

	// Set defaults that will be merged
	v.SetDefault("lock.enabled", true)
	v.SetDefault("lock.timeout", "5m")
	v.SetDefault("lock.stale", "10m")
	v.SetDefault("lock.dir", "/tmp/rr-locks")
	v.SetDefault("output.color", "auto")
	v.SetDefault("output.format", "auto")
	v.SetDefault("output.timing", true)
	v.SetDefault("output.verbosity", "normal")
}
