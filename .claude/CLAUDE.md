## Project Overview

`rr` (Road Runner) is a Go CLI tool that syncs code to remote machines and executes commands. It handles the tedious parts of remote development: host fallback (LAN to VPN), file sync with rsync, distributed locking, and formatted output for test runners.

## Build & Test Commands

This project uses `rr` itself for remote execution. Tasks are defined in `.rr.yaml`. The config has `local_fallback: true`, so if no remote hosts are available, commands run locally.

```bash
# Setup (run once after cloning)
make setup              # Installs lefthook hooks + dependencies

# Build
rr build                # Build ./rr binary on remote

# Testing (use rr for remote execution with local fallback)
rr test                 # Run all unit tests
rr test-integration     # Run integration tests
rr test-all             # Run unit + integration tests in parallel
rr test-v               # Tests with verbose output
rr test-race            # Tests with race detector

# Verification
rr lint                 # Run golangci-lint
rr verify               # lint + unit tests
rr verify-all           # lint + unit + integration tests in parallel
rr ci                   # Complete CI suite

# Other useful tasks
rr coverage             # Generate coverage report
rr bench                # Run benchmarks
rr fmt                  # Format code
rr deps                 # Download/tidy dependencies

# Run single test
rr run "go test ./internal/lock/... -run TestLockAcquisition -v"

# E2E CLI validation (manual testing)
./scripts/e2e-test.sh           # Full test suite (requires working hosts)
./scripts/e2e-test.sh --quick   # Fast mode (skips sync/exec/run tests)
./scripts/e2e-test.sh --verbose # Show command output for debugging

# Make targets (auto-use rr with local fallback)
make test               # Uses rr test (falls back to local if rr unavailable)
make test-all           # Uses rr test-all (parallel unit + integration)
make verify             # Uses rr verify
make verify-all         # Uses rr verify-all (parallel lint + all tests)
make test-local         # Force local execution (skip rr)
make lint-local         # Force local lint (skip rr)
```

## Git Hooks (Lefthook)

Pre-commit hooks auto-run on commit: `gofmt`, `goimports`, `go vet`, `golangci-lint --fix`. Commit messages must follow Conventional Commits format (`feat:`, `fix:`, `docs:`, etc.).

## Architecture

### Package Layout

```
cmd/rr/main.go       # Entry point, sets version info, calls cli.Execute()
internal/
  cli/               # Cobra commands (run, exec, sync, setup, doctor, monitor, etc.)
  config/            # YAML config loading, validation, variable expansion
  host/              # Host selection with ordered SSH fallback, probing, caching
  sync/              # Rsync wrapper with progress parsing
  exec/              # Command execution (SSH and local), task runner
  lock/              # Distributed locking via atomic mkdir on remote
  doctor/            # Diagnostic checks (SSH, rsync, config, hosts)
  monitor/           # Real-time TUI dashboard (Bubble Tea)
  output/            # Stream handling, test formatters (pytest, jest, go test)
  ui/                # TUI components (spinner, phase indicators, progress)
  setup/             # SSH key generation and deployment
  errors/            # Structured error types with suggestions
pkg/sshutil/         # Reusable SSH client utilities
```

### Key Flows

1. **Host Selection** (`internal/host/selector.go`, `dial.go`): Dials a host's SSH aliases in parallel. Earlier aliases are preferred: if a later one connects first, it waits a short grace window for an earlier one. Caches connections within a session. With several hosts, `internal/cli/loadbalance.go` walks them in project `hosts:` order (alphabetical global order otherwise) and takes the first unlocked one.

2. **Run Command** (`internal/cli/workflow.go`, `run.go`): Decide the execution target (`internal/cli/target.go`: `--local`, local mode, or remote) -> Connect -> Acquire lock -> Check requirements -> Sync files -> Execute command -> Release lock. Locking before sync avoids syncing to a host that's busy. Each phase has spinner/progress UI in `--pretty` and emits phase events in structured mode.

3. **Lock System** (`internal/lock/`): Creates a `<lock.dir>/rr.lock/` directory on the remote (default `/tmp/rr-locks/rr.lock/`) with holder info in `info.json`. The lock is per host, not per project. A heartbeat touches `info.json` every 30s, and a lock is stale when that file's mtime is older than `lock.stale`.

4. **Output Formatters** (`internal/output/formatters/`): `ParseRunOutcome` detects the test framework from the command and run log, and extracts counts, failures, and the no-tests signal once. The JSON result details and the `--pretty` failure block both render from that outcome.

## Error Handling Pattern

Always use structured errors from `internal/errors`:

```go
// Good: includes code, message, and actionable suggestion
return errors.New(errors.ErrConfigNotFound, "config file not found", "Run 'rr init' to create one")

// Good: wrap with context
return errors.WrapWithCode(err, errors.ErrSSH, "connection failed", "Check if host is reachable")
```

The code is the contract: `mapErrorCode` (`internal/cli/json.go`) turns it into the public code agents branch on (`ErrConfigNotFound` -> `CONFIG_NOT_FOUND`, `ErrHostNotFound` -> `HOST_NOT_FOUND`, `ErrDependency` -> `DEPENDENCY_MISSING`, and so on) by table lookup, never by reading the message. Pick the code that says what failed where the error is created.

## Testing Conventions

- Use `testify/assert` and `testify/require`
- Table-driven tests preferred
- Integration tests use env vars: `RR_TEST_SSH_HOST`, `RR_TEST_SSH_KEY`, `RR_TEST_SSH_USER`
- Use `./scripts/ci-ssh-server.sh` for Docker-based SSH testing
- Run `./scripts/e2e-test.sh` before PRs that touch CLI commands, flags, or output formatting

## Config Files

The tool uses two config files:

- **Global config** (`~/.rr/config.yaml`): Host definitions (SSH connections, directories, tags, env vars). Personal settings not shared with team.
- **Project config** (`.rr.yaml` in project root): Shareable settings including host references, sync rules, lock settings, tasks.

Key sections in project config: `host`/`hosts` (references to global hosts), `sync` (exclude/preserve patterns), `lock` (timeout settings), `tasks` (named commands).

## Dependencies

- **CLI**: Cobra + Viper
- **TUI**: Bubble Tea + Lip Gloss + Huh
- **SSH**: golang.org/x/crypto/ssh + kevinburke/ssh_config
- **Testing**: testify
