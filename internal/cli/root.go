package cli

import (
	// Dependencies for CLI framework
	_ "github.com/spf13/cobra"
	_ "github.com/spf13/viper"

	// Dependencies for SSH functionality
	_ "golang.org/x/crypto/ssh"
	_ "github.com/kevinburke/ssh_config"

	// Dependencies for TUI/terminal UI
	_ "github.com/charmbracelet/bubbletea"
	_ "github.com/charmbracelet/lipgloss"
	_ "github.com/charmbracelet/huh"

	// Utility dependencies
	_ "gopkg.in/yaml.v3"
)
