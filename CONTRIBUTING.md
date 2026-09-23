# Contributing to Road Runner

Thanks for your interest in contributing! This doc covers how to get set up, run tests, and submit changes.

Please read our [Code of Conduct](CODE_OF_CONDUCT.md) before participating.

## Finding something to work on

- Look for issues labeled [`good first issue`](https://github.com/rileyhilliard/rr/labels/good%20first%20issue) for starter tasks
- Check [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) to understand the design before diving into code
- Not sure where to start? Open an issue and ask

## Development setup

**Requirements:**

- Go 1.26.8 or later (the `go` directive in `go.mod`)
- lefthook (for git hooks)
- golangci-lint, pinned in `.golangci-version` (`make setup` installs it)
- shellcheck (optional, for shell script linting)
- rsync (installed on both local and remote machines)
- SSH access to at least one remote host (for integration tests)

**Quick setup:**

```bash
git clone https://github.com/rileyhilliard/rr.git
cd rr
make setup    # Installs lefthook hooks, the pinned golangci-lint, goimports, and Go modules
make build
./rr --help
```

This installs lefthook git hooks. Pre-commit runs `gofmt`, `goimports`, `go vet`, `go mod tidy`, `golangci-lint --fix`, YAML/JSON syntax checks, and shellcheck (if installed). Pre-push runs the unit tests, lint, and a 50% coverage check.

**Manual dependency install (if needed):**

```bash
# macOS
brew install lefthook golangci-lint shellcheck

# Or via Go
go install github.com/evilmartians/lefthook/v2@latest
make install-linter    # golangci-lint at the version in .golangci-version
```

The Makefile runs golangci-lint from `$(go env GOBIN)` (or `$(go env GOPATH)/bin`), so a different version installed elsewhere, such as Homebrew's, is not used by `make lint`.

## Running tests

**Unit tests:**

```bash
make test          # Runs `rr test` if rr is on your PATH, otherwise `go test ./...`
make test-local    # Always runs `go test ./...` locally
```

The Makefile's `test`, `test-integration`, `test-all`, `verify`, and `verify-all` targets go through `rr` when it's installed, which syncs to the hosts in `.rr.yaml` and falls back to local execution if none are reachable. Use the `*-local` targets to skip rr.

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
RR_TEST_SSH_HOST=localhost RR_TEST_SSH_KEY=~/.ssh/id_ed25519 go test -v ./tests/integration/...
```

Tests that need a live server skip unless both `RR_TEST_SSH_HOST` and `RR_TEST_SSH_KEY` are set.

**Linting:**

```bash
make lint
```

**Full verification:**

```bash
make verify    # Runs lint + unit tests
make ci        # Format check, lint, 50% unit coverage check, build
make coverage-ci  # Unit + integration coverage against a Docker SSH server, 60% minimum (matches CI)
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
3. Ensure `make ci` passes (runs format check, lint, coverage, build)
4. Update documentation if you changed behavior
5. Open a PR with a clear description of what and why

### CI checks

Your PR must pass these automated checks:

- **Format check** - Code must be `gofmt` formatted
- **Lint** - No golangci-lint violations (version from `.golangci-version`)
- **Tests** - Unit tests pass with `-race`, on the Go version from `go.mod`
- **Integration tests** - `./tests/integration/...` and `./pkg/sshutil/...` pass against a Docker SSH server
- **Coverage** - Merged unit + integration coverage is at least 60%
- **Security** - No known vulnerabilities (govulncheck)
- **Build** - Binary compiles successfully

The workflow is `.github/workflows/ci.yml`.

### Branch protection

A repository ruleset on `main` enforces:

- Changes land through a pull request (no direct pushes)
- Squash merge is the only allowed merge method
- No force pushes or branch deletion

### Commit messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/). The lefthook commit-msg hook enforces this format:

```
<type>(<optional scope>): <description>

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

## Adding new CLI commands

To add a new command:

1. Put the implementation in a new file in `internal/cli/` (e.g., `mycommand.go`)
2. Define the Cobra command in `internal/cli/commands.go` and register it in that file's `init()`, next to the others:

```go
var myCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "One-line description",
    Long:  `Longer description if needed.`,
    RunE: func(cmd *cobra.Command, args []string) error {
        return myCommand(MyOptions{...})
    },
}

// in init():
myCmd.Flags().BoolVar(&myDryRun, "dry-run", false, "Flag description")
rootCmd.AddCommand(myCmd)
```

3. Add the command name to `ReservedTaskNames` in `internal/config/validate.go` so a task in `.rr.yaml` can't shadow it
4. Support both output modes: structured JSON is the default, and `--pretty` switches to human output. Check `PrettyMode()` and use the helpers in `internal/cli/json.go` (`WriteJSONSuccess`, `WriteJSONError`, `WritePhaseEvent`)
5. Add tests in a `*_test.go` file alongside your command
6. Regenerate shell completions with `make completions` (writes `completions/`, which release archives include)

Check existing commands like `prune.go` or `status.go` for patterns.

## Adding output formatters

Output formatters parse test runner output (pytest, jest, go test) to extract failures and show summaries.

1. Create a new file in `internal/output/formatters/` (e.g., `myrunner.go`)
2. Implement the `output.Formatter` interface (`internal/output/formatter.go`) plus `Detect` from the `formatters.Detector` interface:

```go
type MyFormatter struct {
    // State tracking fields
}

func (f *MyFormatter) Name() string { return "myrunner" }

func (f *MyFormatter) Detect(command string, output []byte) int {
    // Return 0-100 confidence this formatter handles the output
    if strings.Contains(command, "myrunner") {
        return 100
    }
    return 0
}

func (f *MyFormatter) ProcessLine(line string) string {
    // Transform/colorize the line, collect state
    return line
}

func (f *MyFormatter) Summary(exitCode int) string {
    // Return summary after command completes
    return ""
}
```

3. Add it to the list in `detectFormatter()` in `internal/output/formatters/detect.go`. The highest `Detect` score wins, and scores below 50 are ignored
4. To feed structured failures and counts into the result envelope (`details.failures`, `details.summary`), also implement `output.TestSummaryProvider`; implement `output.NoTestsReporter` if the runner can report that it collected zero tests
5. Add tests with sample output from the test runner

See `gotest.go` or `pytest.go` for real examples.

## Questions?

Open an issue if you're not sure about something. We're happy to help.

## Code of conduct

This project follows the [Contributor Covenant](https://www.contributor-covenant.org/). See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for details.
