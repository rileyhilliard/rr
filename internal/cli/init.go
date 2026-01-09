package cli

import (
	"fmt"
	"os"
	"path/filepath"
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
	Host           string // Pre-specified SSH host/alias
	Name           string // Friendly name for the host
	Dir            string // Pre-specified remote directory
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
	name     string   // Friendly name (config key)
	sshHosts []string // Connection fallbacks for this machine
}

// initConfigValues holds the collected configuration values.
type initConfigValues struct {
	machines  []machineConfig // All machines configured
	remoteDir string          // Shared across all machines
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

// collectNonInteractiveValues collects config values in non-interactive mode.
func collectNonInteractiveValues(opts InitOptions) (*initConfigValues, error) {
	if opts.Host == "" {
		return nil, errors.New(errors.ErrConfig,
			"Need an SSH host in non-interactive mode",
			"Pass --host, set RR_HOST env var, or drop the --non-interactive flag to use the prompts.")
	}

	machineName := opts.Name
	if machineName == "" {
		machineName = extractHostname(opts.Host)
	}

	vals := &initConfigValues{
		machines: []machineConfig{
			{
				name:     machineName,
				sshHosts: []string{opts.Host},
			},
		},
	}
	vals.remoteDir = opts.Dir
	if vals.remoteDir == "" {
		vals.remoteDir = "${HOME}/rr/${PROJECT}"
	}
	return vals, nil
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

// getAllSelectedSSHHosts returns all SSH hosts selected across all machines.
func getAllSelectedSSHHosts(machines []machineConfig) []string {
	var all []string
	for _, m := range machines {
		all = append(all, m.sshHosts...)
	}
	return all
}

// collectInteractiveValues collects config values interactively.
func collectInteractiveValues() (*initConfigValues, error) {
	vals := &initConfigValues{
		remoteDir: "${HOME}/rr/${PROJECT}", // Default, will prompt at end
	}

	// Machine loop - collect one or more machines
	for {
		allSelected := getAllSelectedSSHHosts(vals.machines)

		machine, cancelled, err := collectMachineConfig(allSelected)
		if err != nil {
			return nil, err
		}
		if cancelled {
			if len(vals.machines) == 0 {
				return nil, nil // User cancelled on first machine
			}
			break // User cancelled adding more, but we have at least one
		}

		vals.machines = append(vals.machines, *machine)

		// Ask if they want to add another machine
		if !promptAddAnotherMachine() {
			break
		}
	}

	// Prompt for remote directory (shared across all machines)
	if err := promptRemoteDir(&vals.remoteDir); err != nil {
		return nil, err
	}

	return vals, nil
}

// collectMachineConfig collects configuration for a single machine.
// Returns the machine config, cancelled flag, and any error.
func collectMachineConfig(excludeSSHHosts []string) (*machineConfig, bool, error) {
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

// promptAddAnotherMachine asks if the user wants to add another machine.
func promptAddAnotherMachine() bool {
	var addMore bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Add another machine?").
				Description("A different computer that can run your jobs").
				Value(&addMore),
		),
	)
	if err := form.Run(); err != nil {
		return false
	}
	return addMore
}

// promptRemoteDir prompts for the remote directory.
func promptRemoteDir(remoteDir *string) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Remote directory").
				Description("Where files sync to on all machines (supports ${PROJECT}, ${USER}, ${HOME})").
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

// testConnection tests the SSH connection and handles failure.
func testConnection(sshHost string, opts InitOptions) error {
	fmt.Println()
	spinner := ui.NewSpinner("Testing connection to " + sshHost)
	spinner.Start()

	_, err := host.Probe(sshHost, 10*time.Second)
	if err == nil {
		spinner.Success()

		// Check for PATH differences (non-blocking warning)
		checkPATHDifference(sshHost)

		fmt.Println()
		return nil
	}

	spinner.Fail()

	if opts.NonInteractive {
		return errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Can't reach %s", sshHost),
			"Make sure the host is up, or use --skip-probe to skip the connection test: ssh "+sshHost)
	}

	// Offer to save anyway
	fmt.Printf("\n%s Connection to '%s' failed: %v\n\n", ui.SymbolFail, sshHost, err)
	var saveAnyway bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Save config anyway? (You can fix the connection later)").
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

// checkPATHDifference checks if login and interactive shell PATH differ.
// This is a non-blocking warning to help users understand potential issues.
func checkPATHDifference(sshHost string) {
	// Connect for PATH check
	client, err := sshutil.Dial(sshHost, 10*time.Second)
	if err != nil {
		return // Silent fail - connection already verified
	}
	defer client.Close()

	diff, err := exec.GetPATHDifference(client)
	if err != nil || len(diff.InterOnly) == 0 {
		return // No differences or error - nothing to report
	}

	// Warn about PATH differences
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	fmt.Println()
	fmt.Printf("%s PATH Difference Detected\n", warnStyle.Render(ui.SymbolWarning))
	fmt.Println()
	fmt.Println(mutedStyle.Render("  Your interactive shell has additional PATH directories that won't"))
	fmt.Println(mutedStyle.Render("  be available when rr runs commands (login shell mode):"))
	fmt.Println()

	// Show up to 5 directories
	displayCount := len(diff.InterOnly)
	if displayCount > 5 {
		displayCount = 5
	}
	for _, dir := range diff.InterOnly[:displayCount] {
		fmt.Printf("    %s\n", mutedStyle.Render(dir))
	}
	if len(diff.InterOnly) > 5 {
		fmt.Printf("    %s\n", mutedStyle.Render(fmt.Sprintf("... and %d more", len(diff.InterOnly)-5)))
	}

	fmt.Println()
	fmt.Println(mutedStyle.Render("  You may want to add setup_commands to your config later:"))
	fmt.Println(mutedStyle.Render("    setup_commands:"))

	// Generate suggestion with up to 3 paths
	exportCount := len(diff.InterOnly)
	if exportCount > 3 {
		exportCount = 3
	}
	pathParts := make([]string, exportCount)
	for i := 0; i < exportCount; i++ {
		pathParts[i] = toHomeRelativePath(diff.InterOnly[i])
	}
	fmt.Printf("      %s\n", mutedStyle.Render(fmt.Sprintf("- export PATH=%s:$PATH", strings.Join(pathParts, ":"))))
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

// writeConfig writes the configuration file.
func writeConfig(configPath string, vals *initConfigValues) error {
	cfg := config.DefaultConfig()

	// Add all machines to config
	for _, machine := range vals.machines {
		cfg.Hosts[machine.name] = config.Host{
			SSH: machine.sshHosts,
			Dir: vals.remoteDir,
		}
	}

	// First machine added is the default
	if len(vals.machines) > 0 {
		cfg.Default = vals.machines[0].name
	}

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
			fmt.Sprintf("Couldn't write the config file to %s", configPath),
			"Check that you have write permissions in this directory.")
	}

	fmt.Printf("%s Created %s\n\n", ui.SymbolSuccess, configPath)
	fmt.Println("Next steps:")
	fmt.Println("  rr sync       - Sync files to remote")
	fmt.Println("  rr run <cmd>  - Sync and run a command")
	fmt.Println("  rr doctor     - Check configuration")

	return nil
}

// Init creates a new .rr.yaml configuration file.
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

	// Collect configuration values
	var vals *initConfigValues
	if opts.NonInteractive {
		vals, err = collectNonInteractiveValues(opts)
	} else {
		vals, err = collectInteractiveValues()
	}
	if err != nil {
		return err
	}
	if vals == nil {
		return nil // User cancelled
	}

	// Test connection to first machine before saving (unless --skip-probe)
	if !opts.SkipProbe && len(vals.machines) > 0 && len(vals.machines[0].sshHosts) > 0 {
		if err := testConnection(vals.machines[0].sshHosts[0], opts); err != nil {
			return err
		}
	}

	return writeConfig(configPath, vals)
}

// initCommand is the implementation called by the cobra command.
func initCommand(opts InitOptions) error {
	// Merge with environment variable defaults
	opts = mergeInitOptions(opts)
	return Init(opts)
}
