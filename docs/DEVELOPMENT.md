# Development guide

This guide covers development setup, testing, and debugging for contributors.

## Quick setup

```bash
git clone https://github.com/rileyhilliard/rr.git
cd rr
make setup    # Installs hooks and dependencies
make build    # Creates ./rr binary
```

## Requirements

- Go 1.22+
- lefthook (git hooks)
- golangci-lint (linting)
- rsync (for integration tests)
- Docker (optional, for SSH tests)

Install on macOS:

```bash
brew install lefthook golangci-lint shellcheck rsync
```

## Running tests

### Unit tests

```bash
make test                    # All unit tests
go test ./internal/cli/...   # Specific package
go test -v -run TestName     # Single test with output
```

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
# Enable SSH in System Preferences (macOS) or sshd (Linux)
RR_TEST_SSH_HOST=localhost go test -v ./tests/integration/...
```

**Skip SSH tests:**

```bash
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...
```

### Environment variables for tests

| Variable | Description |
|----------|-------------|
| `RR_TEST_SSH_HOST` | SSH host for integration tests (default: localhost) |
| `RR_TEST_SSH_USER` | SSH username (default: current user) |
| `RR_TEST_SSH_KEY` | Path to SSH private key |
| `RR_TEST_SKIP_SSH` | Set to 1 to skip SSH-dependent tests |

## Debugging

### Debug logging

Set `RR_DEBUG=1` to enable verbose logging:

```bash
RR_DEBUG=1 rr run "make test"
```

This logs internal operations like:
- Lock acquisition/release
- Host selection attempts
- SSH connection details

### Common debugging scenarios

**SSH connection issues:**

```bash
# Test SSH directly
ssh -v your-host echo "connected"

# Check what rr sees
RR_DEBUG=1 rr status

# Run diagnostics
rr doctor
```

**Lock problems:**

```bash
# Check lock status
ssh your-host "ls -la /tmp/rr-*.lock/"

# See lock holder
ssh your-host "cat /tmp/rr-*.lock/info.json"

# Force release (careful!)
rr run --force-unlock "your command"
```

**Sync issues:**

```bash
# Test rsync directly
rsync -avz --dry-run ./ user@host:~/project/

# Check exclude patterns
RR_DEBUG=1 rr sync
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
dlv test ./internal/lock/... -- -test.run TestAcquire

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
  monitor/           # TUI dashboard
  output/            # Test output formatters
  ui/                # TUI components
  doctor/            # Diagnostic checks
  setup/             # SSH key setup
  errors/            # Structured errors
pkg/sshutil/         # Reusable SSH utilities
tests/integration/   # Integration tests
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
make build-all      # Cross-compile for all platforms
```

For release builds:

```bash
goreleaser release --snapshot --clean
```

## Release process

1. Update CHANGELOG.md
2. Create a git tag: `git tag v1.2.3`
3. Push the tag: `git push origin v1.2.3`
4. GitHub Actions runs goreleaser
5. Binaries are uploaded to GitHub releases
6. Homebrew formula is updated automatically

See [releasing.md](releasing.md) for details.

## Architecture decisions

Key design decisions are documented in [ARCHITECTURE.md](../ARCHITECTURE.md):

- Why we use mkdir for atomic locking
- Why SSH aliases are tried in order (fallback pattern)
- Why we cache connections within a session
- Why formatters use confidence scoring for detection

When making significant changes, consider documenting your reasoning in code comments or updating the architecture docs.

## Getting help

- Check existing issues and PRs
- Run `rr doctor` for diagnostics
- Open an issue with reproduction steps
