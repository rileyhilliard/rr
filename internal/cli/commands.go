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
	runHostFlag              string
	runTagFlag               string
	runProbeTimeoutFlag      string
	runLocalFlag             bool
	runSkipRequirementsFlag  bool
	runRepeatFlag            int
	runPullFlags             []string
	runPullDestFlag          string
	execHostFlag             string
	execTagFlag              string
	execProbeTimeoutFlag     string
	execLocalFlag            bool
	execSkipRequirementsFlag bool
	execPullFlags            []string
	execPullDestFlag         string
	syncHostFlag             string
	syncTagFlag              string
	syncProbeTimeoutFlag     string
	syncDryRun               bool
	pullHostFlag             string
	pullTagFlag              string
	pullProbeTimeoutFlag     string
	pullDestFlag             string
	pullDryRun               bool
	initHostFlag             string
	initRemoteDirFlag        string
	initNameFlag             string
	initForce                bool
	initNonInteractive       bool
	initSkipProbe            bool
	monitorHostsFlag         string
	monitorIntervalFlag      string
	hostAddSkipProbe         bool
	unlockAllFlag            bool
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
		if runRepeatFlag < 0 {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("--repeat must be >= 0, got %d", runRepeatFlag),
				"Use --repeat with a positive number like --repeat 5")
		}
		return runCommand(args, runHostFlag, runTagFlag, runProbeTimeoutFlag, runLocalFlag, runSkipRequirementsFlag, runRepeatFlag, runPullFlags, runPullDestFlag)
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
		return execCommand(args, execHostFlag, execTagFlag, execProbeTimeoutFlag, execLocalFlag, execSkipRequirementsFlag, execPullFlags, execPullDestFlag)
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

// pullCmd downloads files from the remote host
var pullCmd = &cobra.Command{
	Use:   "pull <pattern> [pattern...]",
	Short: "Pull files from remote host",
	Long: `Download files from the remote host to the local machine.

Uses rsync for efficient file transfer. Supports glob patterns
which are expanded on the remote side.

Examples:
  rr pull coverage.xml
  rr pull "dist/*.whl" --dest ./artifacts/
  rr pull coverage.xml htmlcov/
  rr pull --host mini "logs/*.log"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return pullCommand(args, pullDestFlag, pullHostFlag, pullTagFlag, pullProbeTimeoutFlag, pullDryRun)
	},
}

// initCmd creates a new .rr.yaml configuration
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create .rr.yaml configuration",
	Long: `Initialize a new Road Runner configuration file.

Creates a .rr.yaml file in the current directory with sensible defaults.
Guides you through SSH host configuration with interactive prompts.

In non-interactive mode (--non-interactive or CI=true), requires --host flag.

Environment Variables:
  RR_HOST             SSH host (user@hostname or SSH config alias)
  RR_HOST_NAME        Friendly name for the host
  RR_REMOTE_DIR       Remote directory path
  RR_NON_INTERACTIVE  Set to "true" for non-interactive mode

Examples:
  rr init
  rr init --host myserver
  rr init --force
  rr init --non-interactive --host user@server --remote-dir ~/projects
  CI=true rr init --host myserver --skip-probe`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return initCommand(InitOptions{
			Host:           initHostFlag,
			Name:           initNameFlag,
			Dir:            initRemoteDirFlag,
			Overwrite:      initForce,
			NonInteractive: initNonInteractive,
			SkipProbe:      initSkipProbe,
		})
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
					fmt.Sprintf("'%s' doesn't look like a valid interval", monitorIntervalFlag),
					"Try something like 2s, 5s, or 1m.")
			}
			if parsed < 500*time.Millisecond {
				return errors.New(errors.ErrConfig,
					"That interval is too short",
					"Keep it at 500ms or above to avoid hammering the hosts.")
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
				fmt.Sprintf("Don't know that shell: %s", args[0]),
				"Supported: bash, zsh, fish, powershell")
		}
	},
}

// hostCmd is the parent command for host management
var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "Manage configured hosts",
	Long: `Add, remove, and list remote hosts in your configuration.

Similar to 'git remote', this command manages the hosts that rr can connect to.
Each host can have multiple SSH connection fallbacks (e.g., LAN, Tailscale, VPN).

Examples:
  rr host list              # List all configured hosts
  rr host add               # Add a new host interactively
  rr host remove myserver   # Remove a host`,
}

// hostAddCmd adds a new host
var hostAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new host",
	Long: `Add a new remote host to your configuration.

Launches an interactive wizard to configure the host's SSH connections,
friendly name, and remote directory.

Examples:
  rr host add
  rr host add --skip-probe`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return hostAdd(HostAddOptions{
			SkipProbe: hostAddSkipProbe,
		})
	},
}

// hostRemoveCmd removes a host
var hostRemoveCmd = &cobra.Command{
	Use:     "remove [name]",
	Aliases: []string{"rm"},
	Short:   "Remove a host",
	Long: `Remove a host from your configuration.

If no name is provided, shows a picker to select the host to remove.
If you remove the default host, another host will be selected as the new default.

Examples:
  rr host remove           # Interactive selection
  rr host remove myserver
  rr host rm old-machine`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return hostRemove(name)
	},
}

// hostListCmd lists all hosts
var hostListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List configured hosts",
	Long: `List all hosts configured in your .rr.yaml file.

Shows each host's name, SSH connections, and whether it's the default.

Examples:
  rr host list
  rr host ls`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return hostList()
	},
}

// unlockCmd releases the lock on a remote host
var unlockCmd = &cobra.Command{
	Use:   "unlock [host]",
	Short: "Release lock on remote host",
	Long: `Force-release the project lock on a remote host.

Use this when a lock is stuck due to a crashed process or lost connection.
The lock is project-specific, so this only releases the lock for the current
directory's project.

If no host is specified, uses the default host. With --all, releases locks
on all configured hosts.

Examples:
  rr unlock              # Unlock default host
  rr unlock dev-box      # Unlock specific host
  rr unlock --all        # Unlock all configured hosts`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		hostArg := ""
		if len(args) > 0 {
			hostArg = args[0]
		}
		return unlockCommand(UnlockOptions{
			Host: hostArg,
			All:  unlockAllFlag,
		})
	},
}

// tasksCmd lists available tasks
var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "List available tasks",
	Long: `List all tasks defined in your .rr.yaml configuration.

Shows task names, descriptions, commands, and any host restrictions.
Tasks can be run directly as top-level commands (e.g., 'rr test').

Examples:
  rr tasks`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return ListTasks()
	},
}

func init() {
	// run command flags
	runCmd.Flags().StringVar(&runHostFlag, "host", "", "target host name")
	runCmd.Flags().StringVar(&runTagFlag, "tag", "", "select host by tag")
	runCmd.Flags().StringVar(&runProbeTimeoutFlag, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")
	runCmd.Flags().BoolVar(&runLocalFlag, "local", false, "force local execution (skip remote hosts)")
	runCmd.Flags().BoolVar(&runSkipRequirementsFlag, "skip-requirements", false, "skip requirement checks")
	runCmd.Flags().IntVar(&runRepeatFlag, "repeat", 0, "run command N times in parallel across available hosts (for flake detection)")
	runCmd.Flags().StringArrayVar(&runPullFlags, "pull", nil, "pull files from remote after command (can be repeated)")
	runCmd.Flags().StringVar(&runPullDestFlag, "pull-dest", "", "destination directory for pulled files (default: current directory)")

	// exec command flags
	execCmd.Flags().StringVar(&execHostFlag, "host", "", "target host name")
	execCmd.Flags().StringVar(&execTagFlag, "tag", "", "select host by tag")
	execCmd.Flags().StringVar(&execProbeTimeoutFlag, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")
	execCmd.Flags().BoolVar(&execSkipRequirementsFlag, "skip-requirements", false, "skip requirement checks")
	execCmd.Flags().BoolVar(&execLocalFlag, "local", false, "force local execution (skip remote hosts)")
	execCmd.Flags().StringArrayVar(&execPullFlags, "pull", nil, "pull files from remote after command (can be repeated)")
	execCmd.Flags().StringVar(&execPullDestFlag, "pull-dest", "", "destination directory for pulled files (default: current directory)")

	// sync command flags
	syncCmd.Flags().StringVar(&syncHostFlag, "host", "", "target host name")
	syncCmd.Flags().StringVar(&syncTagFlag, "tag", "", "select host by tag")
	syncCmd.Flags().StringVar(&syncProbeTimeoutFlag, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "show what would be synced without syncing")

	// pull command flags
	pullCmd.Flags().StringVar(&pullHostFlag, "host", "", "target host name")
	pullCmd.Flags().StringVar(&pullTagFlag, "tag", "", "select host by tag")
	pullCmd.Flags().StringVar(&pullProbeTimeoutFlag, "probe-timeout", "", "SSH probe timeout (e.g., 5s, 2m)")
	pullCmd.Flags().StringVar(&pullDestFlag, "dest", "", "local destination directory (default: current directory)")
	pullCmd.Flags().BoolVar(&pullDryRun, "dry-run", false, "show what would be pulled without pulling")

	// init command flags
	initCmd.Flags().StringVar(&initHostFlag, "host", "", "SSH host (user@hostname or SSH config alias)")
	initCmd.Flags().StringVar(&initRemoteDirFlag, "remote-dir", "", "remote directory path (default: ~/rr/${PROJECT})")
	initCmd.Flags().StringVar(&initNameFlag, "name", "", "friendly name for the host (default: extracted from host)")
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "overwrite existing config without prompting")
	initCmd.Flags().BoolVar(&initNonInteractive, "non-interactive", false, "skip interactive prompts, use flags and defaults")
	initCmd.Flags().BoolVar(&initSkipProbe, "skip-probe", false, "skip SSH connection testing")

	// monitor command flags
	monitorCmd.Flags().StringVar(&monitorHostsFlag, "hosts", "", "filter to specific hosts (comma-separated)")
	monitorCmd.Flags().StringVar(&monitorIntervalFlag, "interval", "1s", "refresh interval (e.g., 1s, 2s, 5s)")

	// host command flags
	hostAddCmd.Flags().BoolVar(&hostAddSkipProbe, "skip-probe", false, "skip SSH connection testing")
	hostAddCmd.Flags().StringVar(&hostAddName, "name", "", "friendly name for the host (for non-interactive mode)")
	hostAddCmd.Flags().StringVar(&hostAddSSH, "ssh", "", "SSH hostname/alias, comma-separated for multiple (for non-interactive mode)")
	hostAddCmd.Flags().StringVar(&hostAddDir, "dir", "", "remote directory path (default: ~/rr/${PROJECT})")
	hostAddCmd.Flags().StringSliceVar(&hostAddTags, "tag", nil, "host tags (can be repeated)")
	hostAddCmd.Flags().StringSliceVar(&hostAddEnv, "env", nil, "environment variables as KEY=VALUE (can be repeated)")

	// host list flags
	hostListCmd.Flags().BoolVar(&hostListJSON, "json", false, "output in JSON format")

	// unlock command flags
	unlockCmd.Flags().BoolVarP(&unlockAllFlag, "all", "a", false, "unlock all configured hosts")

	// tasks command flags
	tasksCmd.Flags().BoolVar(&tasksJSON, "json", false, "output in JSON format")

	// Register host subcommands
	hostCmd.AddCommand(hostAddCmd)
	hostCmd.AddCommand(hostRemoveCmd)
	hostCmd.AddCommand(hostListCmd)

	// Register all commands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(pullCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(monitorCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(completionCmd)
	rootCmd.AddCommand(hostCmd)
	rootCmd.AddCommand(unlockCmd)
	rootCmd.AddCommand(tasksCmd)
}
