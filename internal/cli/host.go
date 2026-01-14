package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/internal/util"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// Host command flags
var (
	hostListJSON bool
	// Non-interactive host add flags
	hostAddName string
	hostAddSSH  string
	hostAddDir  string
	hostAddTags []string
	hostAddEnv  []string // KEY=VALUE pairs
)

// HostListOutput represents the JSON output for host list command.
type HostListOutput struct {
	Hosts       []HostConfigInfo `json:"hosts"`
	DefaultHost string           `json:"default_host,omitempty"`
}

// HostConfigInfo represents a single host in JSON output.
type HostConfigInfo struct {
	Name       string            `json:"name"`
	SSHAliases []string          `json:"ssh_aliases"`
	Dir        string            `json:"dir"`
	Tags       []string          `json:"tags,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	IsDefault  bool              `json:"is_default"`
}

// HostAddOptions holds options for the host add command.
type HostAddOptions struct {
	Host      string // Pre-specified SSH host/alias
	Name      string // Friendly name for the host
	Dir       string // Pre-specified remote directory
	SkipProbe bool   // Skip connection testing
}

// hostAdd adds a new host to the global configuration.
func hostAdd(opts HostAddOptions) error {
	cfg, _, err := loadGlobalConfig()
	if err != nil {
		return err
	}

	// Check if non-interactive mode is requested via flags
	if hostAddName != "" && hostAddSSH != "" {
		return hostAddNonInteractive(cfg, opts.SkipProbe)
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
			remoteDir = "~/rr/${PROJECT}"
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

	// Save config
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("%s Added host '%s'\n", ui.SymbolSuccess, machine.name)
	return nil
}

// hostAddNonInteractive adds a host using command-line flags (for CI/LLM usage).
// Uses package-level flag variables (hostAddName, hostAddSSH, etc.) for input.
func hostAddNonInteractive(cfg *config.GlobalConfig, skipProbe bool) error {
	// Parse SSH aliases from comma-separated string
	sshAliases := strings.Split(hostAddSSH, ",")
	for i := range sshAliases {
		sshAliases[i] = strings.TrimSpace(sshAliases[i])
	}

	// Validate inputs
	if hostAddName == "" {
		return errors.New(errors.ErrConfig,
			"Host name is required",
			"Use --name to specify a friendly name for the host")
	}
	if len(sshAliases) == 0 || sshAliases[0] == "" {
		return errors.New(errors.ErrConfig,
			"SSH connection is required",
			"Use --ssh to specify SSH hostname or alias (comma-separated for multiple)")
	}

	// Check for name conflict
	if _, exists := cfg.Hosts[hostAddName]; exists {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' already exists", hostAddName),
			"Choose a different name, or use 'rr host remove' first.")
	}

	// Determine remote directory
	remoteDir := hostAddDir
	if remoteDir == "" {
		// Use same dir as existing hosts, or default
		for _, h := range cfg.Hosts {
			if h.Dir != "" {
				remoteDir = h.Dir
				break
			}
		}
		if remoteDir == "" {
			remoteDir = "~/rr/${PROJECT}"
		}
	}

	// Test connection (unless --skip-probe)
	if !skipProbe {
		_, err := host.Probe(sshAliases[0], 10*time.Second)
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrSSH,
				fmt.Sprintf("Can't reach %s", sshAliases[0]),
				"Make sure the host is up and SSH is working: ssh "+sshAliases[0])
		}
	}

	// Parse environment variables from KEY=VALUE pairs
	var envMap map[string]string
	if len(hostAddEnv) > 0 {
		envMap = make(map[string]string)
		for _, pair := range hostAddEnv {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
		}
	}

	// Build host config
	hostConfig := config.Host{
		SSH:  sshAliases,
		Dir:  remoteDir,
		Tags: hostAddTags,
		Env:  envMap,
	}

	// Add to config
	cfg.Hosts[hostAddName] = hostConfig

	// Save config
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}

	// Output result
	if machineMode {
		return WriteJSONSuccess(os.Stdout, map[string]interface{}{
			"name":        hostAddName,
			"ssh_aliases": sshAliases,
			"dir":         remoteDir,
			"tags":        hostAddTags,
			"env":         envMap,
		})
	}

	fmt.Printf("%s Added host '%s'\n", ui.SymbolSuccess, hostAddName)
	return nil
}

// hostRemove removes a host from the global configuration.
func hostRemove(name string) error {
	cfg, _, err := loadGlobalConfig()
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
			"Try again or edit ~/.rr/config.yaml manually.")
	}
	if !confirm {
		fmt.Println("Cancelled.")
		return nil
	}

	// Try to clean up remote artifacts before removing from config
	cleanupRemoteArtifacts(name, hostConfig)

	// Remove the host
	delete(cfg.Hosts, name)

	// Save config
	if err := saveGlobalConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("%s Removed host '%s'\n", ui.SymbolSuccess, name)
	return nil
}

// hostList lists all configured hosts from global config.
func hostList() error {
	cfg, globalPath, err := loadGlobalConfig()
	if err != nil {
		if hostListJSON || machineMode {
			return WriteJSONFromError(os.Stdout, err)
		}
		return err
	}

	// Try to load project config to get host order for determining default
	var hostOrder []string
	if projectPath, findErr := config.Find(""); findErr == nil && projectPath != "" {
		if projectCfg, loadErr := config.Load(projectPath); loadErr == nil {
			// Use project's host order: Hosts (plural) takes precedence over Host (singular)
			if len(projectCfg.Hosts) > 0 {
				hostOrder = projectCfg.Hosts
			} else if projectCfg.Host != "" {
				hostOrder = []string{projectCfg.Host}
			}
		}
	}

	// JSON/machine mode output
	if hostListJSON || machineMode {
		return outputHostListJSON(cfg, hostOrder)
	}

	// Human-readable output
	return outputHostListText(cfg, globalPath)
}

// outputHostListJSON outputs hosts in JSON format with envelope.
// hostOrder specifies the priority order from project config (if available).
// The default host is the first valid host from hostOrder, falling back to alphabetical.
func outputHostListJSON(cfg *config.GlobalConfig, hostOrder []string) error {
	output := HostListOutput{
		Hosts: make([]HostConfigInfo, 0, len(cfg.Hosts)),
	}

	// Sort host names for consistent output
	var names []string
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)

	// Determine default host: first valid host from hostOrder, else first alphabetically
	if len(hostOrder) > 0 {
		// Find first host from order that exists in global config
		for _, h := range hostOrder {
			if _, exists := cfg.Hosts[h]; exists {
				output.DefaultHost = h
				break
			}
		}
	}
	// Fall back to alphabetical if no host order or no match found
	if output.DefaultHost == "" && len(names) > 0 {
		output.DefaultHost = names[0]
	}

	for _, name := range names {
		h := cfg.Hosts[name]
		info := HostConfigInfo{
			Name:       name,
			SSHAliases: h.SSH,
			Dir:        h.Dir,
			Tags:       h.Tags,
			Env:        h.Env,
			IsDefault:  name == output.DefaultHost,
		}
		output.Hosts = append(output.Hosts, info)
	}

	// Use envelope wrapper in machine mode, plain JSON for --json
	if machineMode {
		return WriteJSONSuccess(os.Stdout, output)
	}

	// Legacy --json behavior (no envelope)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputHostListText outputs hosts in human-readable format.
func outputHostListText(cfg *config.GlobalConfig, globalPath string) error {
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

	// Show config location
	fmt.Printf("%s\n\n", dimStyle.Render("Config: "+globalPath))

	for _, name := range names {
		h := cfg.Hosts[name]

		// Name
		fmt.Println(nameStyle.Render(name))

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

// loadGlobalConfig loads the global config from ~/.rr/config.yaml.
// Returns the config, the path to the config file, and any error.
func loadGlobalConfig() (*config.GlobalConfig, string, error) {
	globalPath, err := config.GlobalConfigPath()
	if err != nil {
		return nil, "", err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return nil, "", err
	}

	return cfg, globalPath, nil
}

// saveGlobalConfig saves the global config to ~/.rr/config.yaml.
func saveGlobalConfig(cfg *config.GlobalConfig) error {
	return config.SaveGlobal(cfg)
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
	rmCmd := fmt.Sprintf("rm -rf %s", util.ShellQuotePreserveTilde(remoteDir))

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
