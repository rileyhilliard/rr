package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"gopkg.in/yaml.v3"
)

// HostAddOptions holds options for the host add command.
type HostAddOptions struct {
	Host      string // Pre-specified SSH host/alias
	Name      string // Friendly name for the host
	Dir       string // Pre-specified remote directory
	SkipProbe bool   // Skip connection testing
}

// hostAdd adds a new host to the configuration.
func hostAdd(opts HostAddOptions) error {
	cfg, configPath, err := loadExistingConfig()
	if err != nil {
		return err
	}

	// Get list of existing SSH hosts to exclude from picker
	var existingSSHHosts []string
	for _, h := range cfg.Hosts {
		existingSSHHosts = append(existingSSHHosts, h.SSH...)
	}

	// Collect machine config interactively (don't skip probe)
	machine, cancelled, err := collectMachineConfig(existingSSHHosts, false)
	if err != nil {
		return err
	}
	if cancelled {
		fmt.Println("Cancelled.")
		return nil
	}

	// Check for name conflict
	if _, exists := cfg.Hosts[machine.name]; exists {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' already exists", machine.name),
			"Choose a different name, or use 'rr host remove' first.")
	}

	// Determine remote directory
	remoteDir := opts.Dir
	if remoteDir == "" {
		// Use same dir as existing hosts, or default
		for _, h := range cfg.Hosts {
			if h.Dir != "" {
				remoteDir = h.Dir
				break
			}
		}
		if remoteDir == "" {
			remoteDir = "${HOME}/rr/${PROJECT}"
		}

		// Prompt to confirm or change
		if err := promptRemoteDir(&remoteDir); err != nil {
			return err
		}
	}

	// Test connection (unless --skip-probe)
	if !opts.SkipProbe && len(machine.sshHosts) > 0 {
		if err := testConnectionForAdd(machine.sshHosts[0]); err != nil {
			return err
		}
	}

	// Add to config
	cfg.Hosts[machine.name] = config.Host{
		SSH: machine.sshHosts,
		Dir: remoteDir,
	}

	// If this is the first host, make it the default
	if cfg.Default == "" {
		cfg.Default = machine.name
	}

	// Save config
	if err := saveConfig(configPath, cfg); err != nil {
		return err
	}

	fmt.Printf("%s Added host '%s'\n", ui.SymbolSuccess, machine.name)
	return nil
}

// hostRemove removes a host from the configuration.
func hostRemove(name string) error {
	cfg, configPath, err := loadExistingConfig()
	if err != nil {
		return err
	}

	// If no name provided, show picker
	if name == "" {
		if len(cfg.Hosts) == 0 {
			return errors.New(errors.ErrConfig,
				"No hosts configured",
				"Nothing to remove.")
		}

		// Build sorted list of host names
		var hostNames []string
		for k := range cfg.Hosts {
			hostNames = append(hostNames, k)
		}
		sort.Strings(hostNames)

		// Build options with SSH info
		options := make([]huh.Option[string], len(hostNames))
		for i, h := range hostNames {
			label := h
			if h == cfg.Default {
				label += " (default)"
			}
			// Add first SSH connection as hint
			if host, ok := cfg.Hosts[h]; ok && len(host.SSH) > 0 {
				label += " - " + host.SSH[0]
			}
			options[i] = huh.NewOption(label, h)
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select host to remove").
					Options(options...).
					Value(&name),
			),
		)
		if err := form.Run(); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				"Couldn't get your selection",
				"Try again or use: rr host remove <name>")
		}
	}

	// Check if host exists
	hostConfig, exists := cfg.Hosts[name]
	if !exists {
		// List available hosts in error message
		var available []string
		for k := range cfg.Hosts {
			available = append(available, k)
		}
		sort.Strings(available)
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' not found", name),
			fmt.Sprintf("Available hosts: %s", strings.Join(available, ", ")))
	}

	// Confirm removal
	var confirm bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Remove host '%s'?", name)).
				Description("This cannot be undone").
				Value(&confirm),
		),
	)
	if err := form.Run(); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Try again or edit .rr.yaml manually.")
	}
	if !confirm {
		fmt.Println("Cancelled.")
		return nil
	}

	// Try to clean up remote artifacts before removing from config
	cleanupRemoteArtifacts(name, hostConfig)

	// Remove the host
	delete(cfg.Hosts, name)

	// Handle default host
	if cfg.Default == name {
		cfg.Default = ""
		// Pick a new default if there are other hosts
		if len(cfg.Hosts) > 0 {
			// Pick first alphabetically for consistency
			var remaining []string
			for k := range cfg.Hosts {
				remaining = append(remaining, k)
			}
			sort.Strings(remaining)
			cfg.Default = remaining[0]
			fmt.Printf("  Default host changed to '%s'\n", cfg.Default)
		}
	}

	// Save config
	if err := saveConfig(configPath, cfg); err != nil {
		return err
	}

	fmt.Printf("%s Removed host '%s'\n", ui.SymbolSuccess, name)
	return nil
}

// hostList lists all configured hosts.
func hostList() error {
	cfg, _, err := loadExistingConfig()
	if err != nil {
		return err
	}

	if len(cfg.Hosts) == 0 {
		fmt.Println("No hosts configured.")
		fmt.Println("\nAdd one with: rr host add")
		return nil
	}

	// Sort host names for consistent output
	var names []string
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	// Styles
	nameStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	defaultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("green"))

	for _, name := range names {
		h := cfg.Hosts[name]

		// Name with default indicator
		line := nameStyle.Render(name)
		if name == cfg.Default {
			line += defaultStyle.Render(" (default)")
		}
		fmt.Println(line)

		// SSH connections
		for i, ssh := range h.SSH {
			prefix := "  └─ "
			if i < len(h.SSH)-1 {
				prefix = "  ├─ "
			}
			fmt.Printf("%s%s\n", dimStyle.Render(prefix), ssh)
		}

		// Directory
		if h.Dir != "" {
			fmt.Printf("  %s\n", dimStyle.Render("dir: "+h.Dir))
		}
		fmt.Println()
	}

	return nil
}

// loadExistingConfig loads the existing config or returns an error if it doesn't exist.
func loadExistingConfig() (*config.Config, string, error) {
	configPath := filepath.Join(".", config.ConfigFileName)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, "", errors.New(errors.ErrConfig,
			"No config file found",
			"Run 'rr init' first to create one.")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, "", errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't load config file",
			"Check that .rr.yaml is valid YAML.")
	}

	return cfg, configPath, nil
}

// saveConfig saves the config to the specified path.
func saveConfig(configPath string, cfg *config.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't generate the config file",
			"This is unexpected - please report this bug!")
	}

	header := `# Road Runner configuration
# Run 'rr run <command>' to sync and execute remotely
# See: https://github.com/rileyhilliard/rr for documentation

`
	content := header + string(data)

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			fmt.Sprintf("Couldn't write config file to %s", configPath),
			"Check that you have write permissions.")
	}

	return nil
}

// testConnectionForAdd tests the SSH connection when adding a host.
func testConnectionForAdd(sshHost string) error {
	fmt.Println()
	spinner := ui.NewSpinner("Testing connection to " + sshHost)
	spinner.Start()

	_, err := host.Probe(sshHost, 10*time.Second)
	if err == nil {
		spinner.Success()
		fmt.Println()
		return nil
	}

	spinner.Fail()

	// Offer to save anyway
	fmt.Printf("\n%s Connection to '%s' failed: %v\n\n", ui.SymbolFail, sshHost, err)
	var saveAnyway bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Add host anyway? (You can fix the connection later)").
				Value(&saveAnyway),
		),
	)

	if formErr := form.Run(); formErr != nil || !saveAnyway {
		return errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Can't reach %s", sshHost),
			"Make sure the host is up and SSH is working: ssh "+sshHost)
	}
	return nil
}

// cleanupRemoteArtifacts attempts to remove synced files from the remote host.
// If the host is unreachable, it logs a warning but doesn't fail.
func cleanupRemoteArtifacts(hostName string, hostConfig config.Host) {
	if len(hostConfig.SSH) == 0 || hostConfig.Dir == "" {
		return
	}

	// Try each SSH alias until one works
	var client *sshutil.Client
	var connErr error
	for _, sshAlias := range hostConfig.SSH {
		client, _, connErr = host.ProbeAndConnect(sshAlias, 10*time.Second)
		if connErr == nil {
			break
		}
	}

	if connErr != nil {
		remoteDir := config.ExpandRemote(hostConfig.Dir)
		fmt.Printf("  %s Host '%s' is unreachable, skipping remote cleanup\n", ui.SymbolWarning, hostName)
		fmt.Printf("    Synced files remain at: %s\n", remoteDir)
		fmt.Printf("    To remove manually: ssh %s 'rm -rf %s'\n", hostConfig.SSH[0], remoteDir)
		return
	}
	defer client.Close()

	// Expand the remote directory path
	remoteDir := config.ExpandRemote(hostConfig.Dir)

	// Build rm command with proper quoting for tilde expansion
	rmCmd := fmt.Sprintf("rm -rf %s", shellQuotePreserveTilde(remoteDir))

	spinner := ui.NewSpinner("Cleaning up remote files")
	spinner.Start()

	_, stderr, exitCode, err := client.Exec(rmCmd)
	if err != nil || exitCode != 0 {
		spinner.Fail()
		errMsg := strings.TrimSpace(string(stderr))
		if err != nil {
			errMsg = err.Error()
		}
		fmt.Printf("  %s Remote cleanup failed: %s\n", ui.SymbolWarning, errMsg)
		fmt.Printf("    Synced files remain at: %s\n", remoteDir)
		fmt.Printf("    To remove manually: ssh %s 'rm -rf %s'\n", hostConfig.SSH[0], remoteDir)
		return
	}

	spinner.Success()
}

// shellQuotePreserveTilde quotes a path for shell execution while preserving tilde expansion.
func shellQuotePreserveTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		return "~/" + shellQuote(path[2:])
	}
	if path == "~" {
		return "~"
	}
	return shellQuote(path)
}

// shellQuote wraps a string in single quotes, escaping any existing single quotes.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}
