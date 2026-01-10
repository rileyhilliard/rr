package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/ui"
	"golang.org/x/term"
)

// FixOption represents a choice in the fix menu.
type FixOption string

const (
	FixOptionInstall FixOption = "install"
	FixOptionPATH    FixOption = "path"
	FixOptionSkip    FixOption = "skip"
)

// FixResult contains the result of attempting to fix a missing tool error.
type FixResult struct {
	Fixed       bool   // Whether the fix was applied
	ShouldRetry bool   // Whether the user wants to retry the command
	Message     string // Human-readable message about what happened
}

// HandleMissingTool presents the user with options to fix a missing tool error.
// It returns a FixResult indicating what action was taken.
func HandleMissingTool(
	missingTool *exec.MissingToolError,
	sshClient exec.SSHStreamer,
	configPath string,
) (*FixResult, error) {
	// Can't show interactive prompts without a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return &FixResult{Fixed: false, ShouldRetry: false}, nil
	}

	// Build fix options based on what's available
	options := buildFixOptions(missingTool)
	if len(options) == 0 {
		return &FixResult{Fixed: false, ShouldRetry: false}, nil
	}

	// Show fix menu
	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Would you like rr to fix this?").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		// User cancelled
		return &FixResult{Fixed: false, ShouldRetry: false}, nil
	}

	// Execute the selected fix
	switch FixOption(selected) {
	case FixOptionInstall:
		return executeInstallFix(missingTool, sshClient, configPath)
	case FixOptionPATH:
		return executePATHFix(missingTool, configPath)
	case FixOptionSkip:
		return &FixResult{Fixed: false, ShouldRetry: false}, nil
	}

	return &FixResult{Fixed: false, ShouldRetry: false}, nil
}

// buildFixOptions creates the menu options based on the missing tool error.
func buildFixOptions(missingTool *exec.MissingToolError) []huh.Option[string] {
	var options []huh.Option[string]

	// Option 1: Install (if we have an installer)
	if missingTool.CanInstall {
		options = append(options, huh.NewOption(
			fmt.Sprintf("Install '%s' on the remote machine", missingTool.ToolName),
			string(FixOptionInstall),
		))
	}

	// Option 2: Add PATH export (if tool was found but not in PATH)
	if missingTool.FoundButNotInPATH() {
		pathDir := missingTool.GetPATHToAdd()
		if pathDir != "" {
			options = append(options, huh.NewOption(
				fmt.Sprintf("Add PATH export to .rr.yaml (found in %s)", pathDir),
				string(FixOptionPATH),
			))
		}
	}

	// Option 3: Skip
	options = append(options, huh.NewOption("Skip", string(FixOptionSkip)))

	return options
}

// executeInstallFix attempts to install the tool on the remote machine.
func executeInstallFix(
	missingTool *exec.MissingToolError,
	sshClient exec.SSHStreamer,
	configPath string,
) (*FixResult, error) {
	if sshClient == nil {
		return &FixResult{
			Fixed:   false,
			Message: "No SSH connection available for installation",
		}, nil
	}

	// Detect remote OS for display
	osName, err := exec.DetectRemoteOS(sshClient)
	if err != nil {
		osName = "unknown"
	}

	// Show what we're about to do
	installDesc := exec.GetInstallCommandDescription(missingTool.ToolName, osName)
	fmt.Printf("\nInstalling '%s' on %s...\n", missingTool.ToolName, missingTool.HostName)
	if installDesc != "" {
		fmt.Printf("  Running: %s\n\n", installDesc)
	}

	// Run the installation with streaming output
	result, err := exec.InstallTool(sshClient, missingTool.ToolName, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Printf("\n%s Installation failed: %v\n", ui.SymbolFail, err)
		return &FixResult{Fixed: false, Message: err.Error()}, nil
	}

	if !result.Success {
		fmt.Printf("\n%s Installation failed: %s\n", ui.SymbolFail, result.Output)
		return &FixResult{Fixed: false, Message: result.Output}, nil
	}

	fmt.Printf("\n%s Installed '%s' successfully\n", ui.SymbolSuccess, missingTool.ToolName)

	// If there are PATH additions needed, offer to add them to config
	if len(result.PathAdditions) > 0 {
		exportCmd := config.GeneratePATHExportCommand(result.PathAdditions)
		if exportCmd != "" {
			var addToConfig bool
			pathForm := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Add PATH export to .rr.yaml?\n  %s", exportCmd)).
						Value(&addToConfig),
				),
			)
			if pathForm.Run() == nil && addToConfig {
				if err := config.AddSetupCommand(configPath, missingTool.HostName, exportCmd); err != nil {
					fmt.Printf("%s Failed to update config: %v\n", ui.SymbolFail, err)
				} else {
					fmt.Printf("%s Added setup_command to .rr.yaml\n", ui.SymbolSuccess)
				}
			}
		}
	}

	// Ask if user wants to retry
	shouldRetry := promptRetry()

	return &FixResult{
		Fixed:       true,
		ShouldRetry: shouldRetry,
		Message:     fmt.Sprintf("Installed '%s'", missingTool.ToolName),
	}, nil
}

// executePATHFix adds a PATH export to the config file.
func executePATHFix(missingTool *exec.MissingToolError, configPath string) (*FixResult, error) {
	pathDir := missingTool.GetPATHToAdd()
	if pathDir == "" {
		return &FixResult{Fixed: false, Message: "Could not determine PATH to add"}, nil
	}

	exportCmd := config.GeneratePATHExportCommand([]string{pathDir})

	if err := config.AddSetupCommand(configPath, missingTool.HostName, exportCmd); err != nil {
		fmt.Printf("%s Failed to update config: %v\n", ui.SymbolFail, err)
		return &FixResult{Fixed: false, Message: err.Error()}, nil
	}

	fmt.Printf("\n%s Added setup_command to .rr.yaml:\n    %s\n", ui.SymbolSuccess, exportCmd)

	// Ask if user wants to retry
	shouldRetry := promptRetry()

	return &FixResult{
		Fixed:       true,
		ShouldRetry: shouldRetry,
		Message:     fmt.Sprintf("Added PATH export: %s", exportCmd),
	}, nil
}

// promptRetry asks the user if they want to retry the command.
func promptRetry() bool {
	var retry bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Retry the command?").
				Value(&retry),
		),
	)
	if form.Run() != nil {
		return false
	}
	return retry
}
