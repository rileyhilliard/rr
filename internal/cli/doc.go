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
package cli
