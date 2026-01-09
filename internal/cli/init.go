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
	sshHost      string
	remoteDir    string
	hostName     string
	fallbackHost string
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

	vals := &initConfigValues{sshHost: opts.Host}
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

// trySSHHostPicker shows the SSH config host picker if available.
// Returns selected host info, cancelled flag, and any error.
func trySSHHostPicker() (sshHost, hostName string, cancelled bool) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", "", false
	}

	sshHosts, err := sshutil.ParseSSHConfig()
	if err != nil || len(sshHosts) == 0 {
		return "", "", false
	}

	// Convert to UI format
	uiHosts := make([]ui.SSHHostInfo, len(sshHosts))
	for i, h := range sshHosts {
		uiHosts[i] = ui.SSHHostInfo{
			Alias:       h.Alias,
			Hostname:    h.Hostname,
			User:        h.User,
			Port:        h.Port,
			Description: h.Description(),
		}
	}

	fmt.Println("Found hosts in your SSH config:")
	selected, wasCancelled, pickerErr := ui.PickSSHHost(uiHosts)
	if pickerErr != nil {
		fmt.Printf("Picker error: %v, falling back to manual entry\n", pickerErr)
		return "", "", false
	}
	if wasCancelled {
		fmt.Println("Cancelled.")
		return "", "", true
	}
	if selected != nil {
		return selected.Alias, selected.Alias, false
	}
	return "", "", false
}

// collectInteractiveValues collects config values interactively.
func collectInteractiveValues() (*initConfigValues, error) {
	vals := &initConfigValues{}

	// Try SSH host picker first
	var cancelled bool
	vals.sshHost, vals.hostName, cancelled = trySSHHostPicker()
	if cancelled {
		return nil, nil // User cancelled
	}

	// Prompt for SSH host if not selected from picker
	if vals.sshHost == "" {
		if err := promptSSHHost(&vals.sshHost); err != nil {
			return nil, err
		}
	}

	// Get remaining config values
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

// promptRemainingValues prompts for host name, fallback, and remote dir.
func promptRemainingValues(vals *initConfigValues) error {
	form := huh.NewForm(
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
		huh.NewGroup(
			huh.NewInput().
				Title("Fallback SSH host (optional)").
				Description("Alternative connection for when primary is unavailable").
				Placeholder("user@backup-server (leave empty to skip)").
				Value(&vals.fallbackHost),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Remote directory").
				Description("Where files sync to on remote (supports ${PROJECT}, ${USER}, ${HOME})").
				Placeholder("~/projects/${PROJECT}").
				Value(&vals.remoteDir).
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

	sshList := []string{vals.sshHost}
	if vals.fallbackHost != "" {
		sshList = append(sshList, vals.fallbackHost)
	}

	cfg.Hosts[vals.hostName] = config.Host{
		SSH: sshList,
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
	if !opts.SkipProbe {
		if err := testConnection(vals.sshHost, opts); err != nil {
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
