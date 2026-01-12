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
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// InitOptions holds options for the init command.
type InitOptions struct {
	Host           string // Pre-specified SSH host/alias (for non-interactive)
	Name           string // Friendly name for the host (for non-interactive)
	Dir            string // Pre-specified remote directory (for non-interactive)
	Overwrite      bool   // Overwrite existing config without asking
	NonInteractive bool   // Skip prompts, use defaults
	SkipProbe      bool   // Skip connection testing
}

// getInitDefaults returns InitOptions populated from environment variables.
func getInitDefaults() InitOptions {
	nonInteractive := os.Getenv("RR_NON_INTERACTIVE") == "true" || os.Getenv("CI") != ""
	return InitOptions{
		Host:           os.Getenv("RR_HOST"),
		Name:           os.Getenv("RR_HOST_NAME"),
		Dir:            os.Getenv("RR_REMOTE_DIR"),
		NonInteractive: nonInteractive,
	}
}

// mergeInitOptions merges command-line options with environment defaults.
// Command-line flags take precedence over environment variables.
func mergeInitOptions(opts InitOptions) InitOptions {
	defaults := getInitDefaults()

	// Command-line flags override env vars
	if opts.Host == "" {
		opts.Host = defaults.Host
	}
	if opts.Name == "" {
		opts.Name = defaults.Name
	}
	if opts.Dir == "" {
		opts.Dir = defaults.Dir
	}
	// NonInteractive is true if either flag or env var is set
	if defaults.NonInteractive {
		opts.NonInteractive = true
	}

	return opts
}

// extractHostname extracts the hostname from an SSH connection string.
// user@hostname -> hostname
// hostname -> hostname
func extractHostname(sshHost string) string {
	if idx := strings.LastIndex(sshHost, "@"); idx != -1 {
		return sshHost[idx+1:]
	}
	return sshHost
}

// machineConfig holds configuration for a single machine.
type machineConfig struct {
	name          string   // Friendly name (config key)
	sshHosts      []string // Connection fallbacks for this machine
	setupCommands []string // Setup commands (e.g., PATH exports)
	remoteDir     string   // Remote directory for this host
}

// projectConfigValues holds the collected project configuration values.
type projectConfigValues struct {
	hostRefs []string // References to hosts in global config (empty = use all)
}

// checkExistingConfig checks for existing config and prompts for overwrite.
// Returns true if we should proceed, false if cancelled.
func checkExistingConfig(configPath string, opts InitOptions) (bool, error) {
	if _, err := os.Stat(configPath); err != nil || opts.Overwrite {
		return true, nil
	}

	if opts.NonInteractive {
		return false, errors.New(errors.ErrConfig,
			fmt.Sprintf("There's already a config file at %s", configPath),
			"Use --force to overwrite it.")
	}

	var overwrite bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Config file '%s' already exists. Overwrite?", config.ConfigFileName)).
				Value(&overwrite),
		),
	)

	if err := form.Run(); err != nil {
		return false, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Try running with --force to overwrite.")
	}

	if !overwrite {
		fmt.Println("Cancelled.")
	}
	return overwrite, nil
}

// getSSHHostsForPicker returns SSH hosts formatted for the picker, optionally excluding some.
func getSSHHostsForPicker(exclude ...string) []ui.SSHHostInfo {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}

	sshHosts, err := sshutil.ParseSSHConfig()
	if err != nil || len(sshHosts) == 0 {
		return nil
	}

	// Build exclusion set
	excludeSet := make(map[string]bool)
	for _, e := range exclude {
		excludeSet[e] = true
	}

	// Convert to UI format, filtering out excluded hosts
	var uiHosts []ui.SSHHostInfo
	for _, h := range sshHosts {
		if excludeSet[h.Alias] {
			continue
		}
		uiHosts = append(uiHosts, ui.SSHHostInfo{
			Alias:       h.Alias,
			Hostname:    h.Hostname,
			User:        h.User,
			Port:        h.Port,
			Description: h.Description(),
		})
	}
	return uiHosts
}

// trySSHHostPicker shows the SSH config host picker if available.
// Returns the selected SSH host string and cancelled flag.
// exclude optionally filters out hosts (e.g., the already-selected primary).
func trySSHHostPicker(exclude ...string) (sshHost string, cancelled bool) {
	uiHosts := getSSHHostsForPicker(exclude...)
	if len(uiHosts) == 0 {
		return "", false
	}

	fmt.Println("Found hosts in your SSH config:")
	selected, wasCancelled, pickerErr := ui.PickSSHHost(uiHosts)
	if pickerErr != nil {
		fmt.Printf("Picker error: %v, falling back to manual entry\n", pickerErr)
		return "", false
	}
	if wasCancelled {
		fmt.Println("Cancelled.")
		return "", true
	}
	if selected != nil {
		return selected.Alias, false
	}
	return "", false
}

// collectMachineConfig collects configuration for a single machine.
// Returns the machine config, cancelled flag, and any error.
func collectMachineConfig(excludeSSHHosts []string, skipProbe bool) (*machineConfig, bool, error) {
	machine := &machineConfig{}

	// Get primary SSH connection for this machine
	primaryHost, cancelled := trySSHHostPicker(excludeSSHHosts...)
	if cancelled {
		return nil, true, nil
	}

	// Prompt for SSH host if not selected from picker
	if primaryHost == "" {
		if err := promptSSHHost(&primaryHost); err != nil {
			return nil, false, err
		}
	}
	machine.sshHosts = []string{primaryHost}

	// Prompt for friendly machine name
	hostname := extractHostname(primaryHost)
	if !isIPAddress(hostname) {
		machine.name = hostname
	}

	if err := promptMachineName(&machine.name); err != nil {
		return nil, false, err
	}

	// Prompt for additional connections to THIS machine (inner loop)
	allExcluded := make([]string, 0, len(excludeSSHHosts)+len(machine.sshHosts))
	allExcluded = append(allExcluded, excludeSSHHosts...)
	allExcluded = append(allExcluded, machine.sshHosts...)
	if err := promptAdditionalConnections(machine, allExcluded); err != nil {
		return nil, false, err
	}

	// Prompt for remote directory
	machine.remoteDir = "${HOME}/rr/${PROJECT}"
	if err := promptRemoteDir(&machine.remoteDir); err != nil {
		return nil, false, err
	}

	// Test connection and detect PATH setup commands (unless --skip-probe)
	if !skipProbe && len(machine.sshHosts) > 0 {
		setupCommands, err := testConnectionInteractive(machine.sshHosts[0])
		if err != nil {
			return nil, false, err
		}
		machine.setupCommands = setupCommands
	}

	return machine, false, nil
}

// promptMachineName prompts for a friendly name for the machine.
func promptMachineName(name *string) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Name for this machine").
				Description("A friendly name to identify this machine in your config").
				Placeholder("gpu-box").
				Value(name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("machine name is required")
					}
					if strings.ContainsAny(s, " \t\n") {
						return fmt.Errorf("machine name cannot contain whitespace")
					}
					return nil
				}),
		),
	)
	if err := form.Run(); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Your terminal might not support the prompts. Try --non-interactive mode instead.")
	}
	return nil
}

// promptAdditionalConnections prompts for additional SSH connections to the same machine.
func promptAdditionalConnections(machine *machineConfig, excludeSSHHosts []string) error {
	for {
		var addMore bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Add another connection to %s?", machine.name)).
					Description("Same machine via Tailscale, VPN, etc.").
					Value(&addMore),
			),
		)
		if err := confirmForm.Run(); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				"Couldn't get your input",
				"Your terminal might not support the prompts. Try --non-interactive mode instead.")
		}

		if !addMore {
			return nil
		}

		// Try SSH host picker (excluding all already-selected hosts)
		allExcluded := make([]string, 0, len(excludeSSHHosts)+len(machine.sshHosts))
		allExcluded = append(allExcluded, excludeSSHHosts...)
		allExcluded = append(allExcluded, machine.sshHosts...)
		newHost, cancelled := trySSHHostPicker(allExcluded...)
		if cancelled {
			continue // User cancelled picker, ask again
		}

		if newHost == "" {
			// No picker available or user chose manual entry
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Next SSH connection").
						Description("Another way to reach this same machine").
						Placeholder("tailscale-hostname or user@vpn-ip").
						Value(&newHost),
				),
			)
			if err := form.Run(); err != nil {
				return errors.WrapWithCode(err, errors.ErrConfig,
					"Couldn't get your input",
					"Your terminal might not support the prompts. Try --non-interactive mode instead.")
			}
		}

		if newHost != "" {
			machine.sshHosts = append(machine.sshHosts, newHost)
		}
	}
}

// promptRemoteDir prompts for the remote directory.
func promptRemoteDir(remoteDir *string) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Remote directory").
				Description("Where files sync to (supports ${PROJECT}, ${USER}, ${HOME}, ~)").
				Placeholder("${HOME}/rr/${PROJECT}").
				Value(remoteDir).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("remote directory is required")
					}
					return nil
				}),
		),
	)
	if err := form.Run(); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Your terminal might not support the prompts. Try --non-interactive mode instead.")
	}
	return nil
}

// promptSSHHost prompts for SSH host input.
func promptSSHHost(sshHost *string) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("SSH host or alias").
				Description("Enter hostname, user@host, or SSH config alias").
				Placeholder("myserver or user@192.168.1.100").
				Value(sshHost).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("SSH host is required")
					}
					return nil
				}),
		),
	)
	if err := form.Run(); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Your terminal might not support the prompts. Try --non-interactive mode instead.")
	}
	return nil
}

// isIPAddress returns true if the string looks like an IP address.
func isIPAddress(s string) bool {
	// Simple check: if it's all digits and dots, it's likely an IPv4
	// Also handle IPv6 which contains colons
	for _, c := range s {
		if c != '.' && c != ':' && (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	// Must have at least one separator to be an IP
	return strings.Contains(s, ".") || strings.Contains(s, ":")
}

// testConnectionInteractive tests SSH connection during interactive setup.
// Prompts user to continue on failure. Returns setup commands if PATH differences detected.
func testConnectionInteractive(sshHost string) ([]string, error) {
	fmt.Println()
	spinner := ui.NewSpinner("Testing connection to " + sshHost)
	spinner.Start()

	_, err := host.Probe(sshHost, 10*time.Second)
	if err == nil {
		spinner.Success()

		// Check for PATH differences and get setup commands
		setupCommands := detectPATHSetupCommands(sshHost)

		fmt.Println()
		return setupCommands, nil
	}

	spinner.Fail()

	// Offer to continue anyway
	fmt.Printf("\n%s Connection to '%s' failed: %v\n\n", ui.SymbolFail, sshHost, err)
	var continueAnyway bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Continue anyway? (You can fix the connection later)").
				Value(&continueAnyway),
		),
	)

	if formErr := form.Run(); formErr != nil || !continueAnyway {
		return nil, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Can't reach %s", sshHost),
			"Make sure the host is up and SSH is working: ssh "+sshHost)
	}
	return nil, nil
}

// testConnectionNonInteractive tests SSH connection in non-interactive mode.
// Returns setup commands if PATH differences detected, or error on failure.
func testConnectionNonInteractive(sshHost string) ([]string, error) {
	_, err := host.Probe(sshHost, 10*time.Second)
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Can't reach %s", sshHost),
			"Make sure the host is up, or use --skip-probe to skip the connection test: ssh "+sshHost)
	}

	// Check for PATH differences and get setup commands
	return detectPATHSetupCommands(sshHost), nil
}

// detectPATHSetupCommands checks if login and interactive shell PATH differ.
// Returns setup commands to add to config, or nil if no differences detected.
func detectPATHSetupCommands(sshHost string) []string {
	// Connect for PATH check
	client, err := sshutil.Dial(sshHost, 10*time.Second)
	if err != nil {
		return nil // Silent fail - connection already verified
	}
	defer client.Close()

	diff, err := exec.GetPATHDifference(client)
	if err != nil || len(diff.InterOnly) == 0 {
		return nil // No differences or error - nothing to add
	}

	// Generate setup command with PATH exports (up to 3 paths for readability)
	exportCount := len(diff.InterOnly)
	if exportCount > 3 {
		exportCount = 3
	}
	pathParts := make([]string, exportCount)
	for i := 0; i < exportCount; i++ {
		pathParts[i] = toHomeRelativePath(diff.InterOnly[i])
	}

	setupCmd := fmt.Sprintf("export PATH=%s:$PATH", strings.Join(pathParts, ":"))
	return []string{setupCmd}
}

// toHomeRelativePath converts absolute paths to $HOME-relative for portability.
func toHomeRelativePath(path string) string {
	prefixes := []string{"/Users/", "/home/", "/root"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			rest := path[len(prefix):]
			if prefix == "/root" {
				return "$HOME" + rest
			}
			if idx := strings.Index(rest, "/"); idx != -1 {
				return "$HOME" + rest[idx:]
			}
			return "$HOME"
		}
	}
	return path
}

// yamlValue safely formats a string for YAML output.
// Quotes the value if it contains special characters.
func yamlValue(s string) string {
	// Characters that require quoting in YAML
	needsQuoting := strings.ContainsAny(s, ":{}[]!#&*?|>'\"%@`") ||
		strings.HasPrefix(s, "-") ||
		strings.HasPrefix(s, " ") ||
		strings.HasSuffix(s, " ")

	if needsQuoting {
		// Use yaml.Marshal to get proper escaping
		b, err := yaml.Marshal(s)
		if err != nil {
			// Fallback to double-quoting
			return fmt.Sprintf("%q", s)
		}
		return strings.TrimSpace(string(b))
	}
	return s
}

// generateProjectConfigContent creates a project-focused .rr.yaml config file.
// This config references hosts from global config rather than defining them.
func generateProjectConfigContent(vals *projectConfigValues) string {
	var sb strings.Builder

	// Header
	sb.WriteString(`# Road Runner project configuration
# Run 'rr run <command>' to sync and execute remotely
# See: https://github.com/rileyhilliard/rr for documentation

`)

	// Version
	sb.WriteString("version: 1\n\n")

	// Host references (if user selected any)
	if len(vals.hostRefs) > 0 {
		sb.WriteString("# Hosts this project can use for load balancing (from ~/.rr/config.yaml)\n")
		sb.WriteString("hosts:\n")
		for _, h := range vals.hostRefs {
			sb.WriteString(fmt.Sprintf("  - %s\n", yamlValue(h)))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("# Hosts this project can use (from ~/.rr/config.yaml)\n")
		sb.WriteString("# If omitted, all global hosts are used for load balancing\n")
		sb.WriteString("# hosts:\n")
		sb.WriteString("#   - my-host\n")
		sb.WriteString("#   - other-host\n\n")
	}

	// Sync section
	sb.WriteString("# File sync settings (uses rsync under the hood)\n")
	sb.WriteString("sync:\n")
	sb.WriteString("  # Files/directories to exclude from sync (rsync patterns)\n")
	sb.WriteString("  exclude:\n")
	sb.WriteString("    - .git/\n")
	sb.WriteString("    - .venv/\n")
	sb.WriteString("    - __pycache__/\n")
	sb.WriteString("    - \"*.pyc\"\n")
	sb.WriteString("    - node_modules/\n")
	sb.WriteString("    - .mypy_cache/\n")
	sb.WriteString("    - .pytest_cache/\n")
	sb.WriteString("    - .ruff_cache/\n")
	sb.WriteString("    - .DS_Store\n")
	sb.WriteString("    - \"*.log\"\n")
	sb.WriteString("\n")
	sb.WriteString("  # Files to keep on remote even if deleted locally\n")
	sb.WriteString("  preserve:\n")
	sb.WriteString("    - .venv/\n")
	sb.WriteString("    - node_modules/\n")
	sb.WriteString("    - data/\n")
	sb.WriteString("    - .cache/\n")
	sb.WriteString("\n")
	sb.WriteString("  # Extra rsync flags\n")
	sb.WriteString("  # flags:\n")
	sb.WriteString("  #   - --compress\n")
	sb.WriteString("  #   - --bwlimit=1000\n\n")

	// Lock section
	sb.WriteString("# Distributed locking prevents concurrent runs on the same host\n")
	sb.WriteString("lock:\n")
	sb.WriteString("  enabled: true\n")
	sb.WriteString("\n")
	sb.WriteString("  # How long to wait for a lock before giving up\n")
	sb.WriteString("  timeout: 5m\n")
	sb.WriteString("\n")
	sb.WriteString("  # How long to retry across hosts when all are locked\n")
	sb.WriteString("  wait_timeout: 1m\n")
	sb.WriteString("\n")
	sb.WriteString("  # When to consider a lock stale (holder probably crashed)\n")
	sb.WriteString("  stale: 10m\n")
	sb.WriteString("\n")
	sb.WriteString("  # Where lock files are stored on remote\n")
	sb.WriteString("  # dir: /tmp/rr-locks\n\n")

	// Tasks section (commented out example)
	sb.WriteString("# Named tasks for common commands\n")
	sb.WriteString("# tasks:\n")
	sb.WriteString("#   test:\n")
	sb.WriteString("#     description: Run the test suite\n")
	sb.WriteString("#     run: pytest\n")
	sb.WriteString("#\n")
	sb.WriteString("#   build:\n")
	sb.WriteString("#     description: Build the project\n")
	sb.WriteString("#     steps:\n")
	sb.WriteString("#       - name: Install dependencies\n")
	sb.WriteString("#         run: pip install -e .\n")
	sb.WriteString("#       - name: Run build\n")
	sb.WriteString("#         run: python setup.py build\n\n")

	// Output section (commented out)
	sb.WriteString("# Output formatting\n")
	sb.WriteString("# output:\n")
	sb.WriteString("#   color: auto      # auto, always, or never\n")
	sb.WriteString("#   format: auto     # auto, generic, pytest, jest, go, cargo\n")
	sb.WriteString("#   timing: true     # show timing for each phase\n")
	sb.WriteString("#   verbosity: normal  # quiet, normal, or verbose\n\n")

	// Monitor section (commented out)
	sb.WriteString("# Resource monitoring dashboard settings (rr monitor)\n")
	sb.WriteString("# monitor:\n")
	sb.WriteString("#   interval: 2s     # how often to refresh metrics\n")
	sb.WriteString("#   exclude: []      # hosts to hide from dashboard\n")
	sb.WriteString("#   thresholds:\n")
	sb.WriteString("#     cpu:\n")
	sb.WriteString("#       warning: 70\n")
	sb.WriteString("#       critical: 90\n")
	sb.WriteString("#     ram:\n")
	sb.WriteString("#       warning: 70\n")
	sb.WriteString("#       critical: 90\n")
	sb.WriteString("#     gpu:\n")
	sb.WriteString("#       warning: 70\n")
	sb.WriteString("#       critical: 90\n")

	return sb.String()
}

// writeProjectConfig writes the project configuration file.
func writeProjectConfig(configPath string, vals *projectConfigValues) error {
	content := generateProjectConfigContent(vals)

	// Validate the generated YAML is parseable
	var testConfig config.Config
	if err := yaml.Unmarshal([]byte(content), &testConfig); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Generated config file has invalid YAML",
			"This is unexpected - please report this bug!")
	}

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			fmt.Sprintf("Couldn't write the config file to %s", configPath),
			"Check that you have write permissions in this directory.")
	}

	fmt.Printf("%s Created %s\n\n", ui.SymbolSuccess, configPath)

	fmt.Println("Next steps:")
	if len(vals.hostRefs) == 0 {
		fmt.Println("  rr host add   - Add a host to your global config")
	}
	fmt.Println("  rr sync       - Sync files to remote")
	fmt.Println("  rr run <cmd>  - Sync and run a command")
	fmt.Println("  rr doctor     - Check configuration")

	return nil
}

// addHostToGlobal adds a new host to the global config.
// Returns the host name and any error.
func addHostToGlobal(globalCfg *config.GlobalConfig, machine *machineConfig) (string, error) {
	// Add host to global config
	globalCfg.Hosts[machine.name] = config.Host{
		SSH:           machine.sshHosts,
		Dir:           machine.remoteDir,
		SetupCommands: machine.setupCommands,
	}

	// Set as default if first host
	if globalCfg.Defaults.Host == "" {
		globalCfg.Defaults.Host = machine.name
	}

	// Save global config
	if err := config.SaveGlobal(globalCfg); err != nil {
		return "", err
	}

	globalPath, _ := config.GlobalConfigPath()
	fmt.Printf("%s Added host '%s' to %s\n\n", ui.SymbolSuccess, machine.name, globalPath)

	// Inform user about auto-added setup commands
	if len(machine.setupCommands) > 0 {
		mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
		fmt.Printf("%s Added setup_commands to extend PATH\n", ui.SymbolSuccess)
		fmt.Println(mutedStyle.Render("  Some tools (like those in ~/.local/bin or /opt/homebrew/bin) are only"))
		fmt.Println(mutedStyle.Render("  available in interactive shells. rr runs commands in login shells, so"))
		fmt.Println(mutedStyle.Render("  setup_commands ensures these paths are available when rr executes."))
		fmt.Println()
	}

	return machine.name, nil
}

// promptHostsSelection shows a multi-select to choose which hosts this project can use.
// Returns selected host names (empty = use all global hosts).
func promptHostsSelection(globalCfg *config.GlobalConfig) ([]string, error) {
	// Build sorted list of host names
	var hostNames []string
	for name := range globalCfg.Hosts {
		hostNames = append(hostNames, name)
	}
	sort.Strings(hostNames)

	// Build options
	options := make([]huh.Option[string], 0, len(hostNames))

	for _, name := range hostNames {
		label := name
		if name == globalCfg.Defaults.Host {
			label += " (default)"
		}
		// Add first SSH connection as hint
		if h, ok := globalCfg.Hosts[name]; ok && len(h.SSH) > 0 {
			label += " - " + h.SSH[0]
		}
		options = append(options, huh.NewOption(label, name))
	}

	// Default to all hosts selected
	selected := make([]string, len(hostNames))
	copy(selected, hostNames)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Which hosts can this project use?").
				Description("Select hosts for load balancing. Uncheck hosts this project shouldn't use.").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your selection",
			"Try --host flag to specify a host.")
	}

	return selected, nil
}

// promptAddHost asks if the user wants to add a host now.
func promptAddHost() (bool, error) {
	var addHost bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("No hosts configured yet. Add one now?").
				Description("Hosts are stored globally in ~/.rr/config.yaml").
				Value(&addHost),
		),
	)

	if err := form.Run(); err != nil {
		return false, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Your terminal might not support the prompts. Try --non-interactive mode instead.")
	}

	return addHost, nil
}

// promptAddMoreHosts asks if the user wants to add another host to global config.
func promptAddMoreHosts() (bool, error) {
	var addMore bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Add another host to global config?").
				Description("You can always add more later with 'rr host add'").
				Value(&addMore),
		),
	)

	if err := form.Run(); err != nil {
		return false, errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Your terminal might not support the prompts. Try --non-interactive mode instead.")
	}

	return addMore, nil
}

// getExistingGlobalHostSSHAliases returns all SSH aliases already configured in global hosts.
func getExistingGlobalHostSSHAliases(globalCfg *config.GlobalConfig) []string {
	var aliases []string
	for _, host := range globalCfg.Hosts {
		aliases = append(aliases, host.SSH...)
	}
	return aliases
}

// hasUnaddedSSHHosts checks if there are SSH config hosts not yet added to global config.
func hasUnaddedSSHHosts(globalCfg *config.GlobalConfig) bool {
	existingAliases := getExistingGlobalHostSSHAliases(globalCfg)
	unadded := getSSHHostsForPicker(existingAliases...)
	return len(unadded) > 0
}

// collectInteractiveValues collects project config values interactively.
func collectInteractiveValues(globalCfg *config.GlobalConfig, skipProbe bool) (*projectConfigValues, error) {
	vals := &projectConfigValues{}

	// First, if we have existing hosts, let user select which ones to use
	if len(globalCfg.Hosts) > 0 {
		selected, err := promptHostsSelection(globalCfg)
		if err != nil {
			return nil, err
		}
		vals.hostRefs = selected
	}

	// If no hosts exist yet, prompt to add at least one
	if len(globalCfg.Hosts) == 0 {
		addHost, err := promptAddHost()
		if err != nil {
			return nil, err
		}

		if addHost {
			machine, cancelled, err := collectMachineConfig(nil, skipProbe)
			if err != nil {
				return nil, err
			}
			if cancelled {
				return vals, nil
			}

			hostName, err := addHostToGlobal(globalCfg, machine)
			if err != nil {
				return nil, err
			}
			vals.hostRefs = []string{hostName}
		}
	}

	// Only offer to add more hosts if there are SSH hosts not yet added
	for hasUnaddedSSHHosts(globalCfg) {
		addMore, err := promptAddMoreHosts()
		if err != nil {
			return nil, err
		}
		if !addMore {
			break
		}

		// Get existing SSH aliases to exclude from picker
		existingAliases := getExistingGlobalHostSSHAliases(globalCfg)

		machine, cancelled, err := collectMachineConfig(existingAliases, skipProbe)
		if err != nil {
			return nil, err
		}
		if cancelled {
			break
		}

		hostName, err := addHostToGlobal(globalCfg, machine)
		if err != nil {
			return nil, err
		}
		vals.hostRefs = append(vals.hostRefs, hostName)
	}

	return vals, nil
}

// collectNonInteractiveValues collects project config values in non-interactive mode.
func collectNonInteractiveValues(opts InitOptions, globalCfg *config.GlobalConfig) (*projectConfigValues, error) {
	vals := &projectConfigValues{}

	// If RR_HOST is set, check if it exists in global config
	if opts.Host != "" {
		machineName := opts.Name
		if machineName == "" {
			machineName = extractHostname(opts.Host)
		}

		// Check if host already exists
		if _, exists := globalCfg.Hosts[machineName]; exists {
			// Host exists, just reference it
			vals.hostRefs = []string{machineName}
		} else {
			// Host doesn't exist, add it to global config
			machine := &machineConfig{
				name:      machineName,
				sshHosts:  []string{opts.Host},
				remoteDir: opts.Dir,
			}
			if machine.remoteDir == "" {
				machine.remoteDir = "${HOME}/rr/${PROJECT}"
			}

			// Test connection if not skipping probe
			if !opts.SkipProbe {
				setupCommands, err := testConnectionNonInteractive(opts.Host)
				if err != nil {
					return nil, err
				}
				machine.setupCommands = setupCommands
			}

			// Add to global config
			hostName, err := addHostToGlobal(globalCfg, machine)
			if err != nil {
				return nil, err
			}
			vals.hostRefs = []string{hostName}
		}
	}
	// If no hosts specified, hostRefs stays empty = use all global hosts

	return vals, nil
}

// Init creates a new .rr.yaml project configuration file.
func Init(opts InitOptions) error {
	configPath := filepath.Join(".", config.ConfigFileName)

	// Check for existing config
	proceed, err := checkExistingConfig(configPath, opts)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	// Load global config to check for existing hosts
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	// Collect configuration values
	var vals *projectConfigValues
	if opts.NonInteractive {
		vals, err = collectNonInteractiveValues(opts, globalCfg)
	} else {
		vals, err = collectInteractiveValues(globalCfg, opts.SkipProbe)
	}
	if err != nil {
		return err
	}
	if vals == nil {
		return nil // User cancelled
	}

	return writeProjectConfig(configPath, vals)
}

// initCommand is the implementation called by the cobra command.
func initCommand(opts InitOptions) error {
	// Merge with environment variable defaults
	opts = mergeInitOptions(opts)
	return Init(opts)
}
