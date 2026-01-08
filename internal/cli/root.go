package cli

import (
	"fmt"
	"os"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/spf13/cobra"
)

// Global flags
var (
	cfgFile string
	verbose bool
	quiet   bool
	noColor bool
)

// tasksRegistered tracks whether tasks have been registered to avoid double registration.
var tasksRegistered bool

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "rr",
	Short: "Remote Runner - Sync and execute on remote machines",
	Long: `rr - Remote Runner - Sync code to remote machines and execute commands with smart host fallback.

Remote Runner syncs your local code to a remote machine and runs commands there,
with automatic host failover, smart caching, and collaborative locking.

Common workflows:
  rr run "make test"        Sync and run tests remotely
  rr exec "ls -la"          Run command without syncing
  rr sync                   Just sync files, don't run anything
  rr status                 Show connection and sync status

Get started:
  rr init                   Create .rr.yaml configuration
  rr setup                  Configure SSH keys and test connection
  rr doctor                 Diagnose connection issues`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and handles errors with structured output.
func Execute() {
	// Try to register tasks before execution.
	// We need to check for --config flag manually since Cobra hasn't parsed flags yet.
	explicitConfig := findConfigFlag()
	registerTasksFromConfig(explicitConfig)

	if err := rootCmd.Execute(); err != nil {
		// Check if it's a structured error
		var rrErr *errors.Error
		if ok := errors.IsCode(err, ""); !ok {
			// Try to cast to our Error type
			if e, ok := err.(*errors.Error); ok {
				rrErr = e
			}
		}

		if rrErr != nil {
			// Print structured error format
			fmt.Fprintln(os.Stderr, err.Error())
		} else {
			// Wrap unknown errors in our format
			wrapped := errors.Wrap(err, err.Error())
			fmt.Fprintln(os.Stderr, wrapped.Error())
		}
		os.Exit(1)
	}
}

// registerTasksFromConfig attempts to load config and register task commands.
// This runs before command execution to make tasks available as first-class commands.
// Errors are silently ignored since config may not exist or be valid yet.
// The explicit parameter allows overriding the config path (e.g., from --config flag).
func registerTasksFromConfig(explicit string) {
	if tasksRegistered {
		return // Already registered, don't duplicate
	}

	// Try to find config
	cfgPath, err := config.Find(explicit)
	if err != nil || cfgPath == "" {
		return // No config found, skip task registration
	}

	// Load config
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return // Config invalid, skip task registration
	}

	// Validate (silently) - don't register tasks from invalid configs
	if err := config.Validate(cfg, config.AllowNoHosts()); err != nil {
		return
	}

	// Register tasks as commands
	RegisterTaskCommands(cfg)
	tasksRegistered = true
}

// findConfigFlag manually looks for --config flag in os.Args.
// This is needed because we want to register task commands before Cobra parses flags.
func findConfigFlag() string {
	for i, arg := range os.Args {
		// Check for --config=value format
		if len(arg) > 9 && arg[:9] == "--config=" {
			return arg[9:]
		}
		// Check for --config value format
		if arg == "--config" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func init() {
	// Global flags available to all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .rr.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-essential output")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
}

// GetRootCmd returns the root command for testing and subcommand registration.
func GetRootCmd() *cobra.Command {
	return rootCmd
}

// Config returns the config file path flag value.
func Config() string {
	return cfgFile
}

// Verbose returns the verbose flag value.
func Verbose() bool {
	return verbose
}

// Quiet returns the quiet flag value.
func Quiet() bool {
	return quiet
}

// NoColor returns the no-color flag value.
func NoColor() bool {
	return noColor
}
