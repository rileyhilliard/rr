package sshutil

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// SSHHostEntry represents a parsed host entry from SSH config.
type SSHHostEntry struct {
	Alias        string // The Host pattern (alias)
	Hostname     string // The HostName value (actual host to connect to)
	User         string // The User value
	Port         string // The Port value
	IdentityFile string // The IdentityFile value
}

// Description returns a user-friendly description of the host.
func (h SSHHostEntry) Description() string {
	parts := []string{}

	if h.Hostname != "" && h.Hostname != h.Alias {
		parts = append(parts, h.Hostname)
	}

	if h.User != "" {
		parts = append(parts, "user: "+h.User)
	}

	if h.Port != "" && h.Port != "22" {
		parts = append(parts, "port: "+h.Port)
	}

	if len(parts) == 0 {
		return h.Alias
	}

	return strings.Join(parts, ", ")
}

// ParseSSHConfig parses ~/.ssh/config and returns all host entries.
// It filters out wildcards and includes hosts, returning only concrete host aliases.
func ParseSSHConfig() ([]SSHHostEntry, error) {
	configPath := filepath.Join(homeDir(), ".ssh", "config")
	return ParseSSHConfigFile(configPath)
}

// ParseSSHConfigFile parses the specified SSH config file.
func ParseSSHConfigFile(configPath string) ([]SSHHostEntry, error) {
	content, _, err := preprocessSSHConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No SSH config is fine
		}
		return nil, err
	}

	cfg, err := ssh_config.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}

	var hosts []SSHHostEntry
	seen := make(map[string]bool)

	for _, host := range cfg.Hosts {
		for _, pattern := range host.Patterns {
			alias := pattern.String()

			// Skip wildcards and special patterns
			if strings.Contains(alias, "*") || strings.Contains(alias, "?") {
				continue
			}

			// Skip if we've already seen this alias
			if seen[alias] {
				continue
			}
			seen[alias] = true

			entry := SSHHostEntry{
				Alias: alias,
			}

			// Get values from config
			if hostname, _ := cfg.Get(alias, "HostName"); hostname != "" {
				entry.Hostname = hostname
			}

			if user, _ := cfg.Get(alias, "User"); user != "" {
				entry.User = user
			}

			if port, _ := cfg.Get(alias, "Port"); port != "" {
				entry.Port = port
			}

			if identity, _ := cfg.Get(alias, "IdentityFile"); identity != "" {
				entry.IdentityFile = expandPath(identity)
			}

			hosts = append(hosts, entry)
		}
	}

	// Sort by alias for consistent ordering
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Alias < hosts[j].Alias
	})

	return hosts, nil
}

// HasIdentityFile returns true if the host has an IdentityFile configured
// or if default keys exist in ~/.ssh/
func (h SSHHostEntry) HasIdentityFile() bool {
	if h.IdentityFile != "" {
		// Check if the configured file exists
		if _, err := os.Stat(h.IdentityFile); err == nil {
			return true
		}
	}

	// Check for default key files
	defaultKeys := []string{
		filepath.Join(homeDir(), ".ssh", "id_ed25519"),
		filepath.Join(homeDir(), ".ssh", "id_rsa"),
		filepath.Join(homeDir(), ".ssh", "id_ecdsa"),
	}

	for _, key := range defaultKeys {
		if _, err := os.Stat(key); err == nil {
			return true
		}
	}

	return false
}

// FilterHostsWithKeys returns only hosts that have identity files configured
// or where default SSH keys exist.
func FilterHostsWithKeys(hosts []SSHHostEntry) []SSHHostEntry {
	var filtered []SSHHostEntry
	for _, h := range hosts {
		if h.HasIdentityFile() {
			filtered = append(filtered, h)
		}
	}
	return filtered
}
