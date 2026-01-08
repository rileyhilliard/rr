package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/spf13/cobra"
)

// Command-specific flags
var (
	runHostFlag          string
	runTagFlag           string
	runProbeTimeoutFlag  string
	execHostFlag         string
	execTagFlag          string
	execProbeTimeoutFlag string
	syncHostFlag         string
	syncTagFlag          string
	syncProbeTimeoutFlag string
	syncDryRun           bool
	initHostFlag         string
	initForce            bool
	monitorHostsFlag     string
	monitorIntervalFlag  string
)

// runCmd syncs code and executes a command on the remote host
var runCmd = &cobra.Command{
	Use:   "run [command]",
	Short: "Sync code and run command on remote host",
	Long: `Sync local code to the remote host and execute the specified command.

This is the primary command for running builds, tests, or any command remotely.
Files are synced using rsync, then the command runs in the remote project directory.

Examples:
  rr run "make test"
  rr run "npm run build"
  rr run --host mini "cargo test"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCommand(args, runHostFlag, runTagFlag, runProbeTimeoutFlag)
	},
}

// execCmd executes a command on the remote host without syncing
var execCmd = &cobra.Command{
	Use:   "exec [command]",
	Short: "Run command on remote host without syncing",
	Long: `Execute a command on the remote host without syncing files first.

Useful for quick commands, checking status, or when files are already synced.

Examples:
  rr exec "ls -la"
  rr exec "git status"
  rr exec "cat /var/log/app.log"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return execCommand(args, execHostFlag, execTagFlag, execProbeTimeoutFlag)
	},
}

// syncCmd syncs code to the remote host without executing
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync code to remote host",
	Long: `Sync local code to the remote host without running any command.

Uses rsync for efficient incremental file transfer.

Examples:
  rr sync
  rr sync --dry-run
  rr sync --host mini`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return syncCommand(syncHostFlag, syncTagFlag, syncProbeTimeoutFlag, syncDryRun)
	},
}

// initCmd creates a new .rr.yaml configuration
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create .rr.yaml configuration",
	Long: `Initialize a new Road Runner configuration file.

Creates a .rr.yaml file in the current directory with sensible defaults.
Guides you through SSH host configuration with interactive prompts.

Examples:
  rr init
  rr init --host myserver
  rr init --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return initCommand(initHostFlag, initForce)
	},
}

// setupCmd configures SSH keys and tests connection
var setupCmd = &cobra.Command{
	Use:   "setup <host>",
	Short: "Configure SSH keys and test connection",
	Long: `Set up SSH authentication and verify connectivity to a remote host.

Guides you through:
  - SSH key generation (if needed)
  - Key deployment to remote hosts
  - Connection testing

Examples:
  rr setup myserver
  rr setup user@192.168.1.100`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return setupCommand(args[0])
	},
}

// statusCmd shows connection and sync status
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show connection and sync status",
	Long: `Display the current status of remote hosts and sync state.

Shows:
  - Host connectivity
  - Last sync time
  - Lock status
  - Configuration summary

Examples:
  rr status
  rr status --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.NewNotImplemented("status")
	},
}

// monitorCmd starts the TUI monitoring dashboard
var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Real-time system metrics dashboard for remote hosts",
	Long: `Start an interactive TUI dashboard showing real-time system metrics
for all configured remote hosts.

Displays CPU, RAM, GPU (if available), and network metrics with
color-coded status indicators and responsive layout.

Keyboard shortcuts:
  q / Ctrl+C  Quit
  r           Force refresh
  s           Cycle sort order (name/CPU/RAM/GPU)
  up/k        Select previous host
  down/j      Select next host
  Enter       Expand selected host details
  Esc         Collapse / go back
  ?           Show help

Examples:
  rr monitor
  rr monitor --hosts mini,workstation
  rr monitor --interval 5s`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse interval
		interval := 2 * time.Second
		if monitorIntervalFlag != "" {
			parsed, err := time.ParseDuration(monitorIntervalFlag)
			if err != nil {
				return errors.WrapWithCode(err, errors.ErrConfig,
					fmt.Sprintf("Invalid interval: %s", monitorIntervalFlag),
					"Use a valid duration like 2s, 5s, or 1m")
			}
			if parsed < 500*time.Millisecond {
				return errors.New(errors.ErrConfig,
					"Interval too short",
					"Minimum interval is 500ms to avoid overwhelming hosts")
			}
			interval = parsed
		}

		return monitorCommand(monitorHostsFlag, interval)
	},
}

// doctorCmd diagnoses connection and configuration issues
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose connection and config issues",
	Long: `Run diagnostic checks to identify and fix common issues.

Checks:
  - SSH connectivity to all hosts
  - rsync availability
  - Configuration validity
  - Lock file status
  - Network latency

Examples:
  rr doctor
  rr doctor --fix`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return errors.NewNotImplemented("doctor")
	},
}

// completionCmd generates shell completion scripts
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate shell completion scripts for rr.

Examples:
  # Bash
  rr completion bash > /etc/bash_completion.d/rr

  # Zsh
  rr completion zsh > "${fpath[1]}/_rr"

  # Fish
  rr completion fish > ~/.config/fish/completions/rr.fish`,
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletion(os.Stdout)
		default:
			return errors.New(errors.ErrExec,
				"Unknown shell: "+args[0],
				"Supported shells: bash, zsh, fish, powershell")
		}
	},
}

func init() {
	// run command flags
	runCmd.Flags().StringVar(&runHostFlag, "host", "", "target host name")
	runCmd.Flags().StringVar(&runTagFlag, "tag", "", "select host by tag")
	runCmd.Flags().StringVar(&runProbeTimeoutFlag, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")

	// exec command flags
	execCmd.Flags().StringVar(&execHostFlag, "host", "", "target host name")
	execCmd.Flags().StringVar(&execTagFlag, "tag", "", "select host by tag")
	execCmd.Flags().StringVar(&execProbeTimeoutFlag, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")

	// sync command flags
	syncCmd.Flags().StringVar(&syncHostFlag, "host", "", "target host name")
	syncCmd.Flags().StringVar(&syncTagFlag, "tag", "", "select host by tag")
	syncCmd.Flags().StringVar(&syncProbeTimeoutFlag, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "show what would be synced without syncing")

	// init command flags
	initCmd.Flags().StringVar(&initHostFlag, "host", "", "pre-specify SSH host")
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "overwrite existing config")

	// monitor command flags
	monitorCmd.Flags().StringVar(&monitorHostsFlag, "hosts", "", "filter to specific hosts (comma-separated)")
	monitorCmd.Flags().StringVar(&monitorIntervalFlag, "interval", "2s", "refresh interval (e.g., 2s, 5s, 1m)")

	// Register all commands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(monitorCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(completionCmd)
}
