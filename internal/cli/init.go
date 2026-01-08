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
	"gopkg.in/yaml.v3"
)

// InitOptions holds options for the init command.
type InitOptions struct {
	Host           string // Pre-specified SSH host/alias
	Dir            string // Pre-specified remote directory
	Overwrite      bool   // Overwrite existing config without asking
	NonInteractive bool   // Skip prompts, use defaults
}

// Init creates a new .rr.yaml configuration file.
func Init(opts InitOptions) error {
	configPath := filepath.Join(".", config.ConfigFileName)

	// Check for existing config
	if _, err := os.Stat(configPath); err == nil && !opts.Overwrite {
		var overwrite bool

		if opts.NonInteractive {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("Config file already exists: %s", configPath),
				"Use --force to overwrite")
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Config file '%s' already exists. Overwrite?", config.ConfigFileName)).
					Value(&overwrite),
			),
		)

		if err := form.Run(); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				"Failed to get user input",
				"Try running with --force to overwrite")
		}

		if !overwrite {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Collect configuration values
	var sshHost, remoteDir, hostName string
	var fallbackHost string

	if opts.NonInteractive {
		// Use provided values or sensible defaults
		sshHost = opts.Host
		if sshHost == "" {
			return errors.New(errors.ErrConfig,
				"SSH host is required in non-interactive mode",
				"Provide --host flag or run interactively")
		}
		remoteDir = opts.Dir
		if remoteDir == "" {
			remoteDir = "~/projects/${PROJECT}"
		}
		hostName = "default"
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

		if err := form.Run(); err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				"Failed to get user input",
				"Check terminal compatibility or use --non-interactive flag")
		}
	}

	// Test connection before saving
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
				"Check that the host is reachable: ssh "+sshHost)
		}
	} else {
		spinner.Success()
		fmt.Println()
	}

	// Build config
	cfg := config.DefaultConfig()

	sshList := []string{sshHost}
	if fallbackHost != "" {
		sshList = append(sshList, fallbackHost)
	}

	cfg.Hosts[hostName] = config.Host{
		SSH: sshList,
		Dir: remoteDir,
	}
	cfg.Default = hostName

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			"Failed to generate config",
			"This shouldn't happen - please report this bug")
	}

	// Add a header comment
	header := `# Remote Runner configuration
# Run 'rr run <command>' to sync and execute remotely
# See: https://github.com/rileyhilliard/rr for documentation

`
	content := header + string(data)

	// Write config file
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return errors.WrapWithCode(err, errors.ErrConfig,
			fmt.Sprintf("Failed to write config file: %s", configPath),
			"Check directory permissions")
	}

	fmt.Printf("%s Created %s\n\n", ui.SymbolSuccess, configPath)
	fmt.Println("Next steps:")
	fmt.Println("  rr sync       - Sync files to remote")
	fmt.Println("  rr run <cmd>  - Sync and run a command")
	fmt.Println("  rr doctor     - Check configuration")

	return nil
}

// initCommand is the implementation called by the cobra command.
func initCommand(hostFlag string, force bool) error {
	return Init(InitOptions{
		Host:      hostFlag,
		Overwrite: force,
	})
}
