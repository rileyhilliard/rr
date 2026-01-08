# Contributing to Remote Runner

Thanks for your interest in contributing! This doc covers how to get set up, run tests, and submit changes.

## Development setup

**Requirements:**

- Go 1.22 or later
- golangci-lint (for linting)
- rsync (installed on both local and remote machines)
- SSH access to at least one remote host (for integration tests)

**Install dependencies:**

```bash
# Install golangci-lint (macOS)
brew install golangci-lint

# Or via Go
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

**Clone and build:**

```bash
git clone https://github.com/rileyhilliard/rr.git
cd rr
make build
./rr --help
```

## Running tests

**Unit tests:**

```bash
make test
```

**Integration tests:**

Integration tests require SSH access to a remote host. See [tests/integration/README.md](tests/integration/README.md) for full setup details.

```bash
# Option 1: Skip SSH-dependent tests (when working on non-SSH features)
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...

# Option 2: Use Docker SSH server
./scripts/test-ssh-server.sh
RR_TEST_SSH_HOST=localhost:2222 go test ./tests/integration/... -v

# Option 3: Use local SSH (requires SSH enabled on your machine)
RR_TEST_SSH_HOST=localhost make test-integration
```

**Linting:**

```bash
make lint
```

**Full verification:**

```bash
make lint && make test && make build
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

- Use `gofmt` (enforced by CI)
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
3. Ensure `make lint && make test` passes
4. Update documentation if you changed behavior
5. Open a PR with a clear description of what and why

### Commit messages

Keep them concise and descriptive. Focus on the "why" over the "what" when possible:

```
Add host fallback timeout configuration

Users need to control how long to wait before trying the next host.
Default 2s was too short for high-latency VPN connections.
```

## Questions?

Open an issue if you're not sure about something. We're happy to help.
