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
		// Use provided values or sensible defaults
		sshHost = opts.Host
		if sshHost == "" {
			return errors.New(errors.ErrConfig,
				"SSH host is required in non-interactive mode",
				"Provide --host flag, set RR_HOST env var, or run interactively")
		}
		remoteDir = opts.Dir
		if remoteDir == "" {
			remoteDir = "~/projects/${PROJECT}"
		}
		hostName = opts.Name
		if hostName == "" {
			hostName = extractHostname(sshHost)
		}
	} else {
		// Interactive prompts using huh
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("SSH host or alias").
					Description("Enter hostname, user@host, or SSH config alias").
					Placeholder("myserver or user@192.168.1.100").
					Value(&sshHost).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("SSH host is required")
						}
						return nil
					}),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Host name").
					Description("A friendly name for this host in your config").
					Placeholder("gpu-box").
					Value(&hostName).
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
					Value(&fallbackHost),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Remote directory").
					Description("Where files sync to on remote (supports ${PROJECT}, ${USER}, ${HOME})").
					Placeholder("~/projects/${PROJECT}").
					Value(&remoteDir).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("remote directory is required")
						}
						return nil
					}),
			),
		)

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

	// Test connection before saving (unless --skip-probe)
	if !opts.SkipProbe {
		fmt.Println()
		spinner := ui.NewSpinner("Testing connection to " + sshHost)
		spinner.Start()

		_, err := host.Probe(sshHost, 10*time.Second)
		if err != nil {
			spinner.Fail()

			// Connection failed, but still offer to save config
			var saveAnyway bool
			if !opts.NonInteractive {
				fmt.Printf("\n%s Connection to '%s' failed: %v\n\n", ui.SymbolFail, sshHost, err)

				form := huh.NewForm(
					huh.NewGroup(
						huh.NewConfirm().
							Title("Save config anyway? (You can fix the connection later)").
							Value(&saveAnyway),
					),
				)

				if formErr := form.Run(); formErr != nil {
					return errors.WrapWithCode(err, errors.ErrSSH,
						fmt.Sprintf("Connection to '%s' failed", sshHost),
						"Check that the host is reachable: ssh "+sshHost)
				}

				if !saveAnyway {
					return errors.WrapWithCode(err, errors.ErrSSH,
						fmt.Sprintf("Connection to '%s' failed", sshHost),
						"Check that the host is reachable: ssh "+sshHost)
				}
			} else {
				return errors.WrapWithCode(err, errors.ErrSSH,
					fmt.Sprintf("Connection to '%s' failed", sshHost),
					"Check that the host is reachable, or use --skip-probe: ssh "+sshHost)
			}
		} else {
			spinner.Success()
			fmt.Println()
		}
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

// initCommand is the implementation called by the cobra command.
func initCommand(opts InitOptions) error {
	// Merge with environment variable defaults
	opts = mergeInitOptions(opts)
	return Init(opts)
}
