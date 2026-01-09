package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
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

// initConfigValues holds the collected configuration values.
type initConfigValues struct {
	sshHosts  []string // All SSH connections in order
	remoteDir string
	hostName  string
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

	vals := &initConfigValues{sshHosts: []string{opts.Host}}
	vals.remoteDir = opts.Dir
	if vals.remoteDir == "" {
		vals.remoteDir = "~/projects/${PROJECT}"
	}
	vals.hostName = opts.Name
	if vals.hostName == "" {
		vals.hostName = extractHostname(opts.Host)
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

// collectInteractiveValues collects config values interactively.
func collectInteractiveValues() (*initConfigValues, error) {
	vals := &initConfigValues{}

	// Get primary SSH host
	primaryHost, cancelled := trySSHHostPicker()
	if cancelled {
		return nil, nil // User cancelled
	}

	// Prompt for SSH host if not selected from picker
	if primaryHost == "" {
		if err := promptSSHHost(&primaryHost); err != nil {
			return nil, err
		}
	}
	vals.sshHosts = []string{primaryHost}

	// Get remaining config values (host name, additional hosts, remote dir)
	if err := promptRemainingValues(vals); err != nil {
		return nil, err
	}

	return vals, nil
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

// promptRemainingValues prompts for host name, additional hosts, and remote dir.
func promptRemainingValues(vals *initConfigValues) error {
	// Use the first SSH host as a starting suggestion for friendly name,
	// but only if it's a readable hostname (not an IP address)
	if vals.hostName == "" && len(vals.sshHosts) > 0 {
		hostname := extractHostname(vals.sshHosts[0])
		if !isIPAddress(hostname) {
			vals.hostName = hostname
		}
	}

	// Prefill remote directory with sensible default
	if vals.remoteDir == "" {
		vals.remoteDir = "~/rr/${PROJECT}"
	}

	// Prompt for friendly host name
	hostNameForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Host name").
				Description("A friendly name for this host in your config").
				Placeholder("gpu-box").
				Value(&vals.hostName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("host name is required")
					}
					if strings.ContainsAny(s, " \t\n") {
						return fmt.Errorf("host name cannot contain whitespace")
					}
					return nil
				}),
		),
	)
	if err := hostNameForm.Run(); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Your terminal might not support the prompts. Try --non-interactive mode instead.")
	}

	// Prompt for additional hosts (loop until user says no)
	if err := promptAdditionalHosts(vals); err != nil {
		return err
	}

	// Prompt for remote directory
	remoteDirForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Remote directory").
				Description("Where files sync to on remote (supports ${PROJECT}, ${USER}, ${HOME})").
				Placeholder("~/rr/${PROJECT}").
				Value(&vals.remoteDir).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("remote directory is required")
					}
					return nil
				}),
		),
	)
	if err := remoteDirForm.Run(); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Couldn't get your input",
			"Your terminal might not support the prompts. Try --non-interactive mode instead.")
	}

	return nil
}

// promptAdditionalHosts prompts for additional SSH hosts in a loop.
func promptAdditionalHosts(vals *initConfigValues) error {
	for {
		// Ask if they want to add another host
		var addMore bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Add another SSH connection?").
					Description("e.g., Tailscale for remote access, or backup machines when busy").
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

		// Try SSH host picker (excluding already-selected hosts)
		newHost, cancelled := trySSHHostPicker(vals.sshHosts...)
		if cancelled {
			continue // User cancelled picker, ask again
		}

		if newHost == "" {
			// No picker available or user chose manual entry - show input
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Next SSH connection").
						Description("Tried when earlier connections fail or are busy").
						Placeholder("tailscale-hostname or user@backup-server").
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
			vals.sshHosts = append(vals.sshHosts, newHost)
		}
	}
}

// testConnection tests the SSH connection and handles failure.
func testConnection(sshHost string, opts InitOptions) error {
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

// writeConfig writes the configuration file.
func writeConfig(configPath string, vals *initConfigValues) error {
	cfg := config.DefaultConfig()

	cfg.Hosts[vals.hostName] = config.Host{
		SSH: vals.sshHosts,
		Dir: vals.remoteDir,
	}
	cfg.Default = vals.hostName

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

	// Test connection before saving (unless --skip-probe)
	if !opts.SkipProbe && len(vals.sshHosts) > 0 {
		if err := testConnection(vals.sshHosts[0], opts); err != nil {
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
