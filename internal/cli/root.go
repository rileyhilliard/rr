package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/spf13/cobra"
)

// Global flags
var (
	cfgFile              string
	verbose              bool
	quiet                bool
	noColor              bool
	noStrictHostKeyCheck bool
	// machineMode is defined in json.go
)

// tasksRegistered tracks whether tasks have been registered to avoid double registration.
var tasksRegistered bool

// configDiscoveryState tracks the result of config discovery for error reporting.
// This allows us to provide contextual error messages when commands fail due to
// missing or invalid configuration.
type configDiscoveryState struct {
	ProjectPath    string   // Path that was searched/found (empty if not found)
	ProjectErr     error    // Error finding project config (nil if found)
	LoadErr        error    // Error loading/parsing config (nil if loaded)
	ValidateErr    error    // Error validating config (nil if valid)
	TasksAvailable []string // Available task names for suggestions
}

// discoveryState stores the result of config discovery for error reporting.
var discoveryState *configDiscoveryState

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "rr",
	Short: "Road Runner - Sync and execute on remote machines. Fast.",
	Long: `rr - Road Runner - Sync code to remote machines and execute commands with smart host fallback.

Road Runner syncs your local code to a remote machine and runs commands there,
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
	code := run()
	sshutil.CloseAgent()
	if code != 0 {
		os.Exit(code)
	}
}

// run executes the CLI and returns an exit code.
func run() int {
	// Try to register tasks before execution.
	// We need to check for --config flag manually since Cobra hasn't parsed flags yet.
	explicitConfig := findConfigFlag()
	registerTasksFromConfig(explicitConfig)

	if err := rootCmd.Execute(); err != nil {
		// Check if it's an exit code error (command ran but returned non-zero)
		if code, ok := errors.GetExitCode(err); ok {
			return code
		}

		// Check for unknown command errors - provide contextual help
		if isUnknownCommandError(err) {
			return handleUnknownCommand(err)
		}

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
		return 1
	}
	return 0
}

// registerTasksFromConfig attempts to load config and register task commands.
// This runs before command execution to make tasks available as first-class commands.
// Errors are tracked in discoveryState for later error reporting.
// The explicit parameter allows overriding the config path (e.g., from --config flag).
func registerTasksFromConfig(explicit string) {
	if tasksRegistered {
		return // Already registered, don't duplicate
	}

	// Initialize discovery state to track what happened
	discoveryState = &configDiscoveryState{}

	// Try to find config
	cfgPath, err := config.Find(explicit)
	if err != nil {
		discoveryState.ProjectErr = err
		discoveryState.ProjectPath = explicit
		return
	}
	if cfgPath == "" {
		discoveryState.ProjectErr = errors.New(errors.ErrConfig,
			"No .rr.yaml found in this directory or parent directories",
			"Run 'rr init' to create one, or check you're in the right directory.")
		return
	}

	discoveryState.ProjectPath = cfgPath

	// Load config
	cfg, err := config.Load(cfgPath)
	if err != nil {
		discoveryState.LoadErr = err
		return
	}

	// Validate - don't register tasks from invalid configs
	if err := config.Validate(cfg); err != nil {
		discoveryState.ValidateErr = err
		return
	}

	// Collect available task names for suggestions
	for name := range cfg.Tasks {
		discoveryState.TasksAvailable = append(discoveryState.TasksAvailable, name)
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

// isUnknownCommandError checks if the error is an "unknown command" error from Cobra.
// Uses HasPrefix to match Cobra's exact format: `unknown command "xyz" for "rr"`
func isUnknownCommandError(err error) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(err.Error(), "unknown command ")
}

// extractUnknownCommand extracts the command name from Cobra's error message.
// Pattern: unknown command "xyz" for "rr"
func extractUnknownCommand(err error) string {
	errStr := err.Error()
	start := strings.Index(errStr, `"`)
	if start == -1 {
		return ""
	}
	end := strings.Index(errStr[start+1:], `"`)
	if end == -1 {
		return ""
	}
	return errStr[start+1 : start+1+end]
}

// handleUnknownCommand provides contextual error messages for unknown commands.
// It uses discoveryState to provide helpful suggestions based on config status.
func handleUnknownCommand(err error) int {
	unknownCmd := extractUnknownCommand(err)

	// Check discoveryState for config-related issues
	if discoveryState != nil {
		// Case 1: Config has load error (e.g., invalid YAML) - show the actual error
		if discoveryState.LoadErr != nil {
			rrErr := errors.WrapWithCode(discoveryState.LoadErr, errors.ErrConfig,
				fmt.Sprintf("Unknown command '%s' (config failed to load)", unknownCmd),
				"Fix the config error above, then try again.")
			fmt.Fprintln(os.Stderr, rrErr.Error())
			return 1
		}

		// Case 2: Config has validation error - show the actual error
		if discoveryState.ValidateErr != nil {
			rrErr := errors.WrapWithCode(discoveryState.ValidateErr, errors.ErrConfig,
				fmt.Sprintf("Unknown command '%s' (config is invalid)", unknownCmd),
				"Fix the validation error above, then try again.")
			fmt.Fprintln(os.Stderr, rrErr.Error())
			return 1
		}

		// Case 3: No project config found - preserve the original error details
		if discoveryState.ProjectErr != nil {
			fmt.Fprintln(os.Stderr, discoveryState.ProjectErr.Error())
			return 1
		}
	}

	// Use Cobra's built-in suggestion feature (works with all registered commands including tasks)
	suggestions := rootCmd.SuggestionsFor(unknownCmd)

	var suggestion string
	if len(suggestions) > 0 {
		suggestion = fmt.Sprintf("Did you mean: %s?", strings.Join(suggestions, ", "))
	} else {
		suggestion = "Run 'rr --help' for available commands."
	}

	rrErr := errors.New(errors.ErrExec,
		fmt.Sprintf("Unknown command '%s'", unknownCmd),
		suggestion)
	fmt.Fprintln(os.Stderr, rrErr.Error())
	return 1
}

func init() {
	// Global flags available to all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .rr.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-essential output")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVar(&noStrictHostKeyCheck, "no-strict-host-key-checking", false,
		"disable SSH host key verification (insecure, for CI/automation only)")
	rootCmd.PersistentFlags().BoolVarP(&machineMode, "machine", "m", false,
		"machine-readable JSON output (for LLM/CI integration)")

	// Set up styled warning handler for sshutil package
	sshutil.WarningHandler = ui.PrintWarning

	// Set up a pre-run hook to apply global flags
	originalPreRun := rootCmd.PersistentPreRun
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Apply color setting - also disable in machine mode to suppress spinners
		if noColor || machineMode {
			ui.DisableColors()
		}
		// Apply SSH host key checking setting
		if noStrictHostKeyCheck {
			sshutil.StrictHostKeyChecking = false
		}
		// Call original pre-run if it exists
		if originalPreRun != nil {
			originalPreRun(cmd, args)
		}
	}
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
