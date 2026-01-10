// Package cli implements the Road Runner command-line interface.
//
// The package is organized around Cobra commands, with each command
// delegating to workflow functions for the actual work. The general
// structure follows a clean separation between:
//
//   - Command definitions (cobra.Command instances)
//   - Workflow orchestration (SetupWorkflow, connect/sync/lock phases)
//   - Implementation details (in other internal packages)
//
// # Command Structure
//
// The root command is "rr" with subcommands for different operations:
//
//	rr run [command]    - Sync code and execute remotely
//	rr exec [command]   - Execute without syncing
//	rr sync             - Sync files only
//	rr init             - Create .rr.yaml config
//	rr setup <host>     - Configure SSH keys
//	rr host [add|remove|list] - Manage hosts
//	rr monitor          - Real-time metrics dashboard
//	rr doctor           - Diagnose issues
//
// # Workflow System
//
// The SetupWorkflow function handles the common phases shared by run/exec/sync:
//
//  1. Load and validate config
//  2. Select and connect to a host (with fallback)
//  3. Sync files (optional, skipped for exec)
//  4. Acquire lock (if enabled)
//
// Commands use WorkflowContext to carry state through these phases, then
// execute their specific logic. The context must be closed to release
// resources like locks and connections.
//
// # Flag Handling
//
// Global flags (--config, --verbose, --quiet, --no-color) are defined on
// the root command and available to all subcommands. Command-specific flags
// like --host and --tag are defined on individual commands.
//
// The CommonFlags type and AddCommonFlags function provide a standard way
// to add host selection flags (--host, --tag, --probe-timeout) to commands.
//
// # Task Registration
//
// Named tasks from the config file are registered as first-class commands
// during startup. This allows "rr mytask" to work like "rr run" with the
// task's configured command. Tasks are registered before Cobra parses
// flags, using a pre-scan of os.Args for --config.
//
// # Input Validation
//
// User input comes from three sources, each with its own validation:
//
// 1. Command-line flags (validated by Cobra and ParseProbeTimeout):
//   - --host: String, used as map key in config.Hosts (validated at selection)
//   - --tag: String, matched against host tags (validated at selection)
//   - --probe-timeout: Duration string parsed by time.ParseDuration
//   - --config: File path, validated by config.Find/Load
//
// 2. Command arguments (validated in command handlers):
//   - Commands to execute (run, exec): Joined and passed to remote shell
//     The remote shell handles parsing; no local sanitization needed
//     since the user controls both ends. Shell metacharacters work as expected.
//
// 3. Config file (.rr.yaml) (validated by config.Validate):
//   - Host SSH strings: Must not be empty
//   - Host directories: Validated for unexpanded variables; ~ allowed for remote
//   - Shell format: Must end with a flag like -c
//   - Task names: Must not conflict with built-in commands
//   - Task steps: Must have run commands, valid on_fail values
//   - Output/lock/monitor: Validated against allowed values
//
// Security notes:
//   - Commands are executed via SSH as the authenticated user - they're not
//     sanitized because the user has shell access anyway
//   - Config paths use os.Getwd() and filepath operations - path traversal
//     attempts would require modifying the config file, which already implies
//     write access to the project
//   - SSH connections use the system SSH agent or configured keys - no
//     credential storage in the tool itself
package cli
