package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/spf13/viper"
)

const (
	// ConfigFileName is the default config file name.
	ConfigFileName = ".rr.yaml"
	// GlobalConfigDir is the directory for global config.
	GlobalConfigDir = ".config/rr"
	// GlobalConfigFile is the global config file name.
	GlobalConfigFile = "config.yaml"
)

// Load reads config from the specified path.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return nil, errors.WrapWithCode(err, errors.ErrConfig,
				"Config file not found",
				"Run 'rr init' to create a config file, or specify one with --config")
		}
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Failed to read config file",
			"Check the file exists and is valid YAML")
	}

	return parseConfig(v, path)
}

// Find locates the config file using the search order:
// 1. Explicit path (from --config flag)
// 2. .rr.yaml in current directory
// 3. .rr.yaml in parent directories (stops at git root or home)
// 4. ~/.config/rr/config.yaml (global defaults)
//
// Returns the path to the config file, or empty string if not found.
func Find(explicit string) (string, error) {
	// 1. Explicit path takes precedence
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			if os.IsNotExist(err) {
				return "", errors.WrapWithCode(err, errors.ErrConfig,
					"Specified config file not found: "+explicit,
					"Check the path is correct")
			}
			return "", errors.WrapWithCode(err, errors.ErrConfig,
				"Cannot access config file: "+explicit,
				"Check file permissions")
		}
		return explicit, nil
	}

	// 2. Current directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.WrapWithCode(err, errors.ErrConfig,
			"Cannot determine current directory",
			"Check directory permissions")
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

	// 4. Global config
	if home != "" {
		globalConfig := filepath.Join(home, GlobalConfigDir, GlobalConfigFile)
		if _, err := os.Stat(globalConfig); err == nil {
			return globalConfig, nil
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

// parseConfig converts viper config to our Config struct with defaults merged in.
func parseConfig(v *viper.Viper, path string) (*Config, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Set up duration parsing for lock timeouts
	setDurationDefaults(v)

	// Unmarshal into config
	if err := v.Unmarshal(cfg); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Invalid config format",
			"Check the YAML syntax in "+path)
	}

	// Expand variables in host directories
	for name, host := range cfg.Hosts {
		host.Dir = Expand(host.Dir)
		cfg.Hosts[name] = host
	}

	return cfg, nil
}

// setDurationDefaults configures viper to handle duration strings.
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
	v.SetDefault("local_fallback", false)
}

// parseDuration parses a duration string, returning the default if parsing fails.
func parseDuration(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

// getProjectName returns the project name for variable expansion.
// It tries git repo name first, then falls back to directory name.
func getProjectName() string {
	// Try git remote URL first
	// This is handled in expand.go for proper separation

	// Fallback to current directory name
	cwd, err := os.Getwd()
	if err != nil {
		return "project"
	}
	return filepath.Base(cwd)
}

// configDir returns the directory containing the config file.
func configDir(configPath string) string {
	if configPath == "" {
		cwd, _ := os.Getwd()
		return cwd
	}
	return filepath.Dir(configPath)
}

// isGitRoot checks if a directory is a git repository root.
func isGitRoot(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// findGitRoot walks up from dir looking for a .git directory.
func findGitRoot(dir string) string {
	for {
		if isGitRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// parseGitRemoteURL extracts the repo name from a git remote URL.
func parseGitRemoteURL(url string) string {
	// Handle SSH URLs: git@github.com:user/repo.git
	if strings.HasPrefix(url, "git@") {
		parts := strings.Split(url, ":")
		if len(parts) == 2 {
			return strings.TrimSuffix(filepath.Base(parts[1]), ".git")
		}
	}

	// Handle HTTPS URLs: https://github.com/user/repo.git
	return strings.TrimSuffix(filepath.Base(url), ".git")
}
