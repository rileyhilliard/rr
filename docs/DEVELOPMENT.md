# Development guide

This guide covers development setup, testing, and debugging for contributors.

## Quick setup

```bash
git clone https://github.com/rileyhilliard/rr.git
cd rr
make setup    # Installs hooks, pinned golangci-lint, goimports, Go modules
make build    # Creates ./rr binary
```

## Requirements

- Go 1.26.8+ (the `go` directive in `go.mod`)
- lefthook (git hooks)
- golangci-lint at the version in `.golangci-version` (`make setup` or `make install-linter` installs it into your Go bin dir, and `make lint` runs that binary)
- rsync (for integration tests)
- Docker (optional, for SSH tests)
- shellcheck (optional, used by the pre-commit hook when present)

Install on macOS:

```bash
brew install lefthook shellcheck rsync
make install-linter
```

## Running tests

### Unit tests

```bash
make test                    # All unit tests (through rr if installed, else local)
make test-local              # All unit tests, always local
go test ./internal/cli/...   # Specific package
go test -v -run TestName ./internal/lock/...   # Single test with output
```

`make test` calls `rr test`, which syncs to the hosts in `.rr.yaml` and falls back to local when none are reachable (`local_fallback: true`).

### Integration tests

Integration tests need SSH access. Options:

**Docker SSH server (recommended for CI):**

```bash
./scripts/ci-ssh-server.sh start
eval $(./scripts/ci-ssh-server.sh env)
go test -v ./tests/integration/... ./pkg/sshutil/...
./scripts/ci-ssh-server.sh stop
```

**Local SSH (your machine):**

```bash
# Enable SSH in System Settings (macOS) or sshd (Linux)
RR_TEST_SSH_HOST=localhost RR_TEST_SSH_KEY=~/.ssh/id_ed25519 go test -v ./tests/integration/...
```

**Skip SSH tests:**

```bash
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...
```

### Environment variables for tests

| Variable | Description |
|----------|-------------|
| `RR_TEST_SSH_HOST` | SSH host for integration tests, `host` or `host:port`. Tests that need a live server skip when unset |
| `RR_TEST_SSH_USER` | SSH username (default: current user) |
| `RR_TEST_SSH_KEY` | Path to SSH private key. Live-server tests also skip when this is unset |
| `RR_TEST_SKIP_SSH` | Set to 1 to skip SSH-dependent tests |

## Debugging

### Output modes

Commands emit structured JSON by default: phase events on stderr, command output passed through. Add `--pretty` (`-p`) for spinners and human-readable summaries while debugging. Every `rr run`/`rr exec`/task run also writes its raw output to `~/.rr/logs/<name>-<timestamp>/output.log`, reported as `details.log_file`.

### Debug logging

Set `RR_DEBUG=1` to print debug logs from the lock package (acquire, release, stale detection):

```bash
RR_DEBUG=1 rr run "make test"
```

No other package reads `RR_DEBUG`. For host selection and SSH problems, use `rr doctor` and `rr status --pretty`.

### Common debugging scenarios

**SSH connection issues:**

```bash
# Test SSH directly
ssh -v your-host echo "connected"

# Check what rr sees
rr status --pretty

# Run diagnostics
rr doctor
```

**Lock problems:**

```bash
# Check lock status (default lock.dir is /tmp/rr-locks)
ssh your-host "ls -la /tmp/rr-locks/rr.lock/"

# See lock holder
ssh your-host "cat /tmp/rr-locks/rr.lock/info.json"

# Force release (careful!)
rr unlock your-host
rr unlock --all
```

**Sync issues:**

```bash
# Test rsync directly
rsync -avz --dry-run ./ user@host:~/project/

# See what rr would sync with the configured excludes
rr sync --dry-run --pretty
```

### VS Code debugging

Create `.vscode/launch.json`:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug rr",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}/cmd/rr",
      "args": ["run", "echo hello"],
      "cwd": "${workspaceFolder}"
    }
  ]
}
```

### Delve debugging

```bash
# Debug a specific test
dlv test ./internal/lock -- -test.run TestAcquire

# Debug the CLI
dlv debug ./cmd/rr -- run "echo hello"
```

## Project structure

```
cmd/rr/              # Entry point
internal/
  cli/               # Cobra commands
  config/            # YAML config handling
  host/              # Host selection, SSH probing
  sync/              # rsync wrapper
  exec/              # Command execution
  lock/              # Distributed locking
  parallel/          # Parallel task orchestration and per-task logs
  deps/              # Task dependency resolution
  require/           # `require:` tool checks on remote hosts
  monitor/           # TUI dashboard
  output/            # Stream handling, test output formatters
  ui/                # TUI components
  doctor/            # Diagnostic checks
  setup/             # SSH key setup
  errors/            # Structured errors
  logger/            # Debug logger (RR_DEBUG)
  util/              # Shell quoting and string helpers
pkg/sshutil/         # Reusable SSH utilities
tests/integration/   # Integration tests
scripts/             # CI SSH server, e2e tests, completions, install, git hook fallbacks
```

## Linting

```bash
make lint           # Run golangci-lint
make lint-fix       # Auto-fix issues
```

Pre-commit hooks run formatting automatically. If a commit is rejected, fix the issues and try again.

## Building

```bash
make build          # Build ./rr
make completions    # Rebuild and regenerate completions/ (bash, zsh, fish, PowerShell)
```

For cross-platform release builds (all targets in `.goreleaser.yaml`, written to `dist/`):

```bash
goreleaser release --snapshot --clean
```

## Release process

1. Merge the release changes to `main`
2. Create a git tag: `git tag v1.2.3`
3. Push the tag: `git push origin v1.2.3`
4. `.github/workflows/release.yml` runs GoReleaser
5. Binaries are uploaded to GitHub releases
6. The Homebrew cask in `rileyhilliard/homebrew-tap` is updated automatically
7. Add the version's entry to CHANGELOG.md

See [releasing.md](releasing.md) for details.

## Architecture decisions

The design is documented in [ARCHITECTURE.md](ARCHITECTURE.md), including:

- The config schema and the global/project config split
- The `rr run` flow (connect, lock, sync, execute)
- Host selection and fallback
- Lock management (atomic mkdir, stale detection)
- Output formatter detection
- The `rr monitor` collection architecture

When making significant changes, consider documenting your reasoning in code comments or updating the architecture docs.

## Getting help

- Check existing issues and PRs
- Run `rr doctor` for diagnostics
- Open an issue with reproduction steps
