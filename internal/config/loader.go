package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/util"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
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
	raw, err := readConfigFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.WrapWithCode(err, errors.ErrConfigNotFound,
				"Can't find the config file",
				"Looks like you haven't set up shop here yet. Run 'rr init' to get started.")
		}
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't read the config file",
			"Something's off with your .rr.yaml. Check that it's valid YAML.")
	}

	cfg, err := parseConfig(raw, path)
	if err != nil {
		return nil, err
	}

	// Apply the project's worktree-isolation preference so ${PROJECT}
	// expansion reflects it from here on. Every entry point that loads a
	// project config picks this up without extra wiring.
	if cfg.Sync.WorktreeIsolation != nil {
		SetWorktreeIsolation(*cfg.Sync.WorktreeIsolation)
	}

	return cfg, nil
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

	raw, err := readConfigFile(path)
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't read global config",
			"Check your ~/.rr/config.yaml for valid YAML syntax.")
	}

	return parseGlobalConfig(raw, path)
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

// readConfigFile reads a YAML config file into a generic map.
//
// It uses yaml.v3 directly rather than viper: viper lowercases every map key
// and splits keys on dots, which mangles env var names (FOO -> foo), task
// names, and host names, all of which are case-sensitive and may contain dots.
func readConfigFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// decodeConfig decodes a raw config map onto out, which holds the defaults.
// Keys absent from raw keep their default values. md records keys that
// matched no field so they can be reported as unknown.
func decodeConfig(raw map[string]interface{}, out interface{}, md *mapstructure.Metadata, hook mapstructure.DecodeHookFunc) error {
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:       hook,
		WeaklyTypedInput: true,
		Metadata:         md,
		Result:           out,
	})
	if err != nil {
		return err
	}
	return dec.Decode(raw)
}

// parseGlobalConfig converts a raw config map to a GlobalConfig struct.
func parseGlobalConfig(raw map[string]interface{}, path string) (*GlobalConfig, error) {
	cfg := DefaultGlobalConfig()

	var md mapstructure.Metadata
	if err := decodeConfig(raw, cfg, &md, mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		localFallbackModeDecodeHook(),
	)); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Global config has some issues",
			"Check the YAML syntax in "+path+" - something's not parsing right.")
	}

	cfg.Warnings = unknownKeyWarnings(md.Unused, path)

	// NOTE: host Dir values keep their ${PROJECT}/${HOME} variables here.
	// Expansion happens at use sites (sync, exec, doctor, display) so that
	// project-level settings loaded later - notably sync.worktree_isolation,
	// which changes what ${PROJECT} expands to - are honored.

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
				return "", errors.WrapWithCode(err, errors.ErrConfigNotFound,
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

		// Stop at git root (but only after checking for .rr.yaml in this directory)
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
	Global      *GlobalConfig
	Project     *Config
	Source      ConfigSource
	ProjectRoot string // Directory containing .rr.yaml (empty if no project config)
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
		resolved.ProjectRoot = filepath.Dir(projectPath)

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
		// Provide contextual error based on whether a project config exists
		if resolved.Source == GlobalOnly {
			return nil, nil, errors.New(errors.ErrConfig,
				"No hosts configured and no project config found",
				"Either:\n  - Run 'rr init' to create a project config\n  - Run 'rr host add' to configure hosts in ~/.rr/config.yaml")
		}
		return nil, nil, errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add hosts to ~/.rr/config.yaml or run 'rr host add'.")
	}

	var hostNames []string

	// 1. Preferred from flag - single host
	if preferred != "" {
		hostNames = []string{preferred}
	}

	// 2. Project config hosts list (plural)
	if len(hostNames) == 0 && resolved.Project != nil && len(resolved.Project.Hosts) > 0 {
		hostNames = resolved.Project.Hosts
	}

	// 3. Project config host reference (singular, backwards compat)
	if len(hostNames) == 0 && resolved.Project != nil && resolved.Project.Host != "" {
		hostNames = []string{resolved.Project.Host}
	}

	// 4. If project explicitly sets local_fallback: true and doesn't specify hosts, run locally
	// This allows users to set local_fallback: true with no hosts to force local execution
	// Only triggers when PROJECT config has local_fallback (not just global), so existing
	// setups that rely on global hosts + global local_fallback continue to work.
	if len(hostNames) == 0 && ProjectLocalMode(resolved) {
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
			return nil, nil, errors.New(errors.ErrHostNotFound,
				"Host '"+name+"' not found in global config",
				"Available hosts: "+util.JoinOrNone(available)+". Check ~/.rr/config.yaml.")
		}
		hosts[name] = host
	}

	return hostNames, hosts, nil
}

// ProjectLocalMode reports whether the project runs locally by design: the
// project config enables local_fallback and lists no hosts. Only the project
// setting counts, so setups relying on global hosts plus a global
// local_fallback keep using their hosts. ResolveHosts returns no hosts in
// this mode (unless --host names one).
func ProjectLocalMode(resolved *ResolvedConfig) bool {
	if resolved == nil || resolved.Project == nil {
		return false
	}
	p := resolved.Project
	return p.LocalFallback != nil && p.LocalFallback.Enabled() && len(p.Hosts) == 0 && p.Host == ""
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

// ResolveLocalFallbackMode determines the local fallback mode.
// Project config overrides global config when explicitly set.
func ResolveLocalFallbackMode(resolved *ResolvedConfig) LocalFallbackMode {
	// Project config takes precedence when explicitly set
	if resolved.Project != nil && resolved.Project.LocalFallback != nil && resolved.Project.LocalFallback.Valid() {
		return *resolved.Project.LocalFallback
	}
	// Fall back to global config
	if resolved.Global != nil && resolved.Global.Defaults.LocalFallback.Valid() {
		return resolved.Global.Defaults.LocalFallback
	}
	return LocalFallbackNever
}

// ResolveLocalFallback determines whether any local fallback is enabled.
// Kept for call sites that only need the boolean (e.g. the host selector's
// unreachable-hosts fallback, which applies in both non-never modes).
func ResolveLocalFallback(resolved *ResolvedConfig) bool {
	return ResolveLocalFallbackMode(resolved).Enabled()
}

// ResolveRewritePaths reports whether local-to-remote path rewriting is
// enabled. Project config overrides global config; the default is on.
func ResolveRewritePaths(resolved *ResolvedConfig) bool {
	if resolved.Project != nil && resolved.Project.RewritePaths != nil {
		return *resolved.Project.RewritePaths
	}
	if resolved.Global != nil && resolved.Global.Defaults.RewritePaths != nil {
		return *resolved.Global.Defaults.RewritePaths
	}
	return true
}

// parseConfig converts a raw config map to our Config struct with defaults merged in.
func parseConfig(raw map[string]interface{}, path string) (*Config, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Decode with custom decoders for DependencyItem and PullItem
	var md mapstructure.Metadata
	if err := decodeConfig(raw, cfg, &md, mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		dependencyItemDecodeHook(),
		pullItemDecodeHook(),
		localFallbackModeDecodeHook(),
	)); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Config file has some issues",
			"Check the YAML syntax in "+path+" - something's not parsing right.")
	}

	cfg.Warnings = projectWarnings(raw, cfg, md.Unused, path)

	return cfg, nil
}

// dependencyItemDecodeHook returns a decode hook that handles DependencyItem
// from both string and map formats.
func dependencyItemDecodeHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		// Only handle conversion to DependencyItem
		if to != reflect.TypeOf(DependencyItem{}) {
			return data, nil
		}

		// Delegate to the shared conversion function for supported types
		if from.Kind() == reflect.String || from.Kind() == reflect.Map {
			return DependencyItemFromInterface(data)
		}

		return data, nil
	}
}

// localFallbackModeDecodeHook returns a decode hook that converts booleans
// and boolean-looking strings to LocalFallbackMode for backwards
// compatibility (true -> always, false -> never) and validates mode strings.
func localFallbackModeDecodeHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		if to != reflect.TypeOf(LocalFallbackMode("")) {
			return data, nil
		}

		switch v := data.(type) {
		case bool:
			if v {
				return LocalFallbackAlways, nil
			}
			return LocalFallbackNever, nil
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			switch s {
			case "true", "yes", "on":
				return LocalFallbackAlways, nil
			case "false", "no", "off":
				return LocalFallbackNever, nil
			}
			mode := LocalFallbackMode(s)
			if !mode.Valid() {
				return nil, fmt.Errorf("invalid local_fallback value %q: use never, on-unreachable, always, or a boolean", v)
			}
			return mode, nil
		}

		return data, nil
	}
}

// pullItemDecodeHook returns a decode hook that handles PullItem
// from both string and map formats.
func pullItemDecodeHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
		// Only handle conversion to PullItem
		if to != reflect.TypeOf(PullItem{}) {
			return data, nil
		}

		// Delegate to the shared conversion function for supported types
		if from.Kind() == reflect.String || from.Kind() == reflect.Map {
			return PullItemFromInterface(data)
		}

		return data, nil
	}
}
