package cli

import (
	"fmt"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/setup"
	"github.com/rileyhilliard/rr/internal/ui"
)

// SetupOptions holds options for the setup command.
type SetupOptions struct {
	Host           string // Host to set up
	NonInteractive bool   // Skip prompts
}

// Setup configures SSH keys and tests connection to a host.
// This is a guided wizard that helps users get passwordless SSH working.
func Setup(opts SetupOptions) error {
	if opts.Host == "" {
		return errors.New(errors.ErrConfig,
			"No host specified",
			"Usage: rr setup <host>")
	}

	fmt.Printf("Setting up SSH for '%s'\n\n", opts.Host)

	// Step 1: Check for local SSH keys
	keys := setup.FindLocalKeys()
	var selectedKey *setup.KeyInfo

	if len(keys) == 0 {
		fmt.Printf("%s No SSH keys found\n\n", ui.SymbolPending)

		if opts.NonInteractive {
			return errors.New(errors.ErrSSH,
				"No SSH keys found",
				"Generate a key first: ssh-keygen -t ed25519")
		}

		// Offer to generate a key
		var generateKey bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("No SSH keys found. Generate one now?").
					Value(&generateKey),
			),
		)

		if err := form.Run(); err != nil {
			return errors.WrapWithCode(err, errors.ErrSSH,
				"Failed to get user input",
				"")
		}

		if !generateKey {
			fmt.Println("Cannot proceed without SSH keys.")
			return nil
		}

		// Generate key
		keyPath := setup.DefaultKeyPath()
		spinner := ui.NewSpinner("Generating SSH key")
		spinner.Start()

		if err := setup.GenerateKey(keyPath, "ed25519"); err != nil {
			spinner.Fail()
			return err
		}

		spinner.Success()
		fmt.Printf("%s Generated key at %s\n\n", ui.SymbolSuccess, keyPath)

		// Refresh key list
		keys = setup.FindLocalKeys()
	}

	// Select the best key
	selectedKey = setup.GetPreferredKey()
	if selectedKey == nil {
		return errors.New(errors.ErrSSH,
			"No usable SSH keys found",
			"Generate a key: ssh-keygen -t ed25519")
	}

	fmt.Printf("%s Using SSH key: %s (%s)\n", ui.SymbolSuccess, selectedKey.Path, selectedKey.Type)

	// Step 2: Test connection
	fmt.Println()
	spinner := ui.NewSpinner("Testing SSH connection")
	spinner.Start()

	latency, err := host.Probe(opts.Host, 10*time.Second)
	if err != nil {
		spinner.Fail()

		// Check if it's an auth failure
		probeErr, ok := err.(*host.ProbeError)
		if ok && probeErr.Reason == host.ProbeFailAuth {
			fmt.Printf("\n%s Connection works but authentication failed\n\n", ui.SymbolPending)

			// Offer to copy key
			if !opts.NonInteractive {
				var copyKey bool
				form := huh.NewForm(
					huh.NewGroup(
						huh.NewConfirm().
							Title("Copy SSH key to remote host?").
							Description("This will enable passwordless login").
							Value(&copyKey),
					),
				)

				if formErr := form.Run(); formErr != nil {
					return errors.WrapWithCode(formErr, errors.ErrSSH,
						"Failed to get user input",
						"")
				}

				if copyKey {
					return copyKeyAndTest(opts.Host, selectedKey)
				}
			}

			// Show manual instructions
			fmt.Println("To copy your key manually:")
			fmt.Println(setup.CopyKeyManual(opts.Host, selectedKey.PublicPath))
			return nil
		}

		return errors.WrapWithCode(err, errors.ErrSSH,
			fmt.Sprintf("Cannot connect to '%s'", opts.Host),
			"Check that the host is reachable and SSH is running")
	}

	spinner.Success()
	fmt.Printf("%s Connected in %dms\n", ui.SymbolSuccess, latency.Milliseconds())

	// Step 3: Test passwordless auth
	fmt.Println()
	spinner = ui.NewSpinner("Testing passwordless authentication")
	spinner.Start()

	authOk, err := setup.TestPasswordlessAuth(opts.Host)
	if err != nil {
		spinner.Fail()
		return err
	}

	if !authOk {
		spinner.Fail()
		fmt.Printf("\n%s Passwordless auth not working\n\n", ui.SymbolPending)

		if !opts.NonInteractive {
			var copyKey bool
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title("Copy SSH key to enable passwordless login?").
						Value(&copyKey),
				),
			)

			if formErr := form.Run(); formErr != nil {
				return errors.WrapWithCode(formErr, errors.ErrSSH,
					"Failed to get user input",
					"")
			}

			if copyKey {
				return copyKeyAndTest(opts.Host, selectedKey)
			}
		}

		// Show manual instructions
		fmt.Println("To enable passwordless login manually:")
		fmt.Println(setup.CopyKeyManual(opts.Host, selectedKey.PublicPath))
		return nil
	}

	spinner.Success()

	// Success summary
	fmt.Println()
	fmt.Printf("%s Setup complete for '%s'\n\n", ui.SymbolSuccess, opts.Host)
	fmt.Println("You can now:")
	fmt.Println("  rr init        - Create a config file using this host")
	fmt.Println("  rr sync        - Sync files to remote")
	fmt.Println("  rr run <cmd>   - Run commands remotely")

	return nil
}

// copyKeyAndTest copies the SSH key and verifies it works.
func copyKeyAndTest(host string, key *setup.KeyInfo) error {
	fmt.Println()
	spinner := ui.NewSpinner("Copying SSH key")
	spinner.Start()

	if err := setup.CopyKey(host, key.Path); err != nil {
		spinner.Fail()

		// Show manual instructions as fallback
		fmt.Println()
		fmt.Println("Automatic key copy failed. Try manually:")
		fmt.Println(setup.CopyKeyManual(host, key.PublicPath))
		return nil
	}

	spinner.Success()

	// Verify it worked
	spinner = ui.NewSpinner("Verifying passwordless login")
	spinner.Start()

	authOk, err := setup.TestPasswordlessAuth(host)
	if err != nil || !authOk {
		spinner.Fail()
		fmt.Printf("\n%s Key copied but passwordless login still not working\n", ui.SymbolPending)
		fmt.Println("This may be a server configuration issue (e.g., PubkeyAuthentication disabled)")
		return nil
	}

	spinner.Success()

	// Success
	fmt.Println()
	fmt.Printf("%s Setup complete! Passwordless login to '%s' is working.\n\n", ui.SymbolSuccess, host)
	fmt.Println("Next steps:")
	fmt.Println("  rr init        - Create a config file using this host")
	fmt.Println("  rr sync        - Sync files to remote")
	fmt.Println("  rr run <cmd>   - Run commands remotely")

	return nil
}

// setupCommand is the implementation called by the cobra command.
func setupCommand(host string) error {
	return Setup(SetupOptions{
		Host: host,
	})
}
