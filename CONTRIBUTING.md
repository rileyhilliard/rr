# Contributing to Road Runner

Thanks for your interest in contributing! This doc covers how to get set up, run tests, and submit changes.

Please read our [Code of Conduct](CODE_OF_CONDUCT.md) before participating.

## Finding something to work on

- Look for issues labeled [`good first issue`](https://github.com/rileyhilliard/rr/labels/good%20first%20issue) for starter tasks
- Check the [ARCHITECTURE.md](ARCHITECTURE.md) to understand the design before diving into code
- Not sure where to start? Open an issue and ask

## Development setup

**Requirements:**

- Go 1.22 or later
- lefthook (for git hooks)
- golangci-lint (for linting)
- shellcheck (optional, for shell script linting)
- rsync (installed on both local and remote machines)
- SSH access to at least one remote host (for integration tests)

**Quick setup:**

```bash
git clone https://github.com/rileyhilliard/rr.git
cd rr
make setup    # Installs lefthook hooks and dependencies
make build
./rr --help
```

This installs lefthook git hooks that run formatting and linting automatically before each commit.

**Manual dependency install (if needed):**

```bash
# macOS
brew install lefthook golangci-lint shellcheck

# Or via Go (lefthook and golangci-lint only)
go install github.com/evilmartians/lefthook@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Running tests

**Unit tests:**

```bash
make test
```

**Integration tests:**

Integration tests require SSH access. See [tests/integration/README.md](tests/integration/README.md) for full setup.

```bash
# Option 1: Docker SSH server (recommended)
./scripts/ci-ssh-server.sh start
eval $(./scripts/ci-ssh-server.sh env)
go test -v ./tests/integration/... ./pkg/sshutil/...
./scripts/ci-ssh-server.sh stop

# Option 2: Skip SSH tests (when working on non-SSH features)
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...

# Option 3: Local SSH (requires SSH enabled on your machine)
RR_TEST_SSH_HOST=localhost make test-integration
```

**Linting:**

```bash
make lint
```

**Full verification:**

```bash
make verify    # Runs lint + test
make ci        # Full CI suite (format, lint, coverage, build)
```

## Code style guidelines

Follow standard Go conventions. A few specifics:

### Error handling

Always use the structured error types from `internal/errors`. This ensures consistent, helpful error messages.

```go
// Good: structured error with context and suggestion
return errors.New(errors.ErrConfig, "config file not found", "Run 'rr init' to create one")

// Good: wrap underlying errors
return errors.WrapWithCode(err, errors.ErrSSH, "connection failed", "Check if the host is reachable")

// Bad: plain error without context
return fmt.Errorf("something went wrong")
```

### General style

- Formatting is auto-applied by pre-commit hooks (`gofmt`, `goimports`)
- Keep functions focused and small
- Prefer table-driven tests
- Add comments for exported functions
- Use meaningful variable names over abbreviations

### Package organization

- `internal/` - Core implementation (not importable by external packages)
- `pkg/` - Potentially reusable utilities
- `cmd/rr/` - Main entry point only

## Pull request process

1. Fork the repo and create a branch from `main`
2. Make your changes with clear, focused commits
3. Ensure `make ci` passes (runs format, lint, coverage, build)
4. Update documentation if you changed behavior
5. Open a PR with a clear description of what and why

### CI checks

Your PR must pass these automated checks:

- **Format check** - Code must be `gofmt` formatted
- **Lint** - No golangci-lint violations
- **Tests** - All tests pass on Go 1.22, 1.23, and 1.24
- **Coverage** - Minimum 50% test coverage
- **Security** - No known vulnerabilities (govulncheck)
- **Build** - Binary compiles successfully

### Branch protection

The `main` branch has these protections enabled:

- Require PR reviews before merging
- Require all CI status checks to pass
- Require branches to be up to date before merging
- Dismiss stale reviews when new commits are pushed

### Commit messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/). The lefthook commit-msg hook enforces this format:

```
<type>: <description>

[optional body]
```

**Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`

**Examples:**

```
feat: add host fallback timeout configuration
fix: handle SSH connection timeout gracefully
docs: update configuration reference
refactor: extract host probing into separate module
```

Focus on the "why" over the "what" in the body when helpful.

## Questions?

Open an issue if you're not sure about something. We're happy to help.

## Code of conduct

This project follows the [Contributor Covenant](https://www.contributor-covenant.org/). See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for details.
