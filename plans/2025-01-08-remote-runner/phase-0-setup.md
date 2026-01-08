# Phase 0: Project Setup

> **Status:** NOT_STARTED

## Goal

Initialize the Go project with proper structure, dependencies, CI, development tooling, and test infrastructure. After this phase, we have a working skeleton that compiles, runs tests, and has patterns for error handling.

**This project follows Test-Driven Development (TDD).** Every feature starts with a test. See the main [README.md](./README.md#testing-philosophy) for the full testing philosophy.

## Success Criteria

- [ ] `go build ./...` succeeds
- [ ] `go test ./...` succeeds (even if no tests yet)
- [ ] `./rr --help` shows basic usage
- [ ] CI runs on push (lint + test)
- [ ] Test environment documented and working
- [ ] Structured error types defined

## Phase Exit Criteria

- [ ] All 8 tasks completed
- [ ] `make lint && make test && make build` passes
- [ ] Manual verification of `./rr --help` output matches expected
- [ ] CONTRIBUTING.md includes test environment setup

## Context Loading

```bash
# This is a new project - reference these for patterns:
read ../../proof-of-concept.sh          # Lines 32-45 for color palette
read ../../ARCHITECTURE.md              # Lines 365-440 for error message design
```

---

## Execution Order

Tasks can be grouped for parallel execution:

```
Group 1 (Foundation):     Task 1, Task 2
Group 2 (Core Tooling):   Task 3, Task 4, Task 5  (after Group 1)
Group 3 (Infrastructure): Task 6, Task 7, Task 8  (after Group 2)
```

---

## Tasks

### Task 1: Initialize Go Module and Directory Structure

**Context:**
- Empty project directory
- Reference: `../../ARCHITECTURE.md` lines 917-990 for target structure

**Steps:**

1. [ ] Run `go mod init github.com/rileyhilliard/rr`
2. [ ] Create directory structure:
   ```
   cmd/rr/main.go
   internal/cli/root.go
   internal/cli/version.go
   internal/config/config.go
   internal/errors/errors.go
   internal/host/selector.go
   internal/sync/sync.go
   internal/exec/executor.go
   internal/lock/lock.go
   internal/setup/keys.go
   internal/doctor/checks.go
   internal/monitor/monitor.go
   internal/output/stream.go
   internal/output/formatter.go
   internal/ui/spinner.go
   pkg/sshutil/client.go
   tests/integration/.gitkeep
   ```
3. [ ] Add placeholder files with package declarations
4. [ ] Create `.gitignore` with Go defaults + IDE files + `dist/`

**Verify:** `go build ./...`

---

### Task 2: Add Core Dependencies

**Context:**
- `go.mod` (created in Task 1)

**Steps:**

1. [ ] Add CLI dependencies:
   - `go get github.com/spf13/cobra@latest`
   - `go get github.com/spf13/viper@latest`
2. [ ] Add SSH dependencies:
   - `go get golang.org/x/crypto/ssh@latest`
   - `go get github.com/kevinburke/ssh_config@latest`
3. [ ] Add TUI dependencies:
   - `go get github.com/charmbracelet/bubbletea@latest`
   - `go get github.com/charmbracelet/lipgloss@latest`
   - `go get github.com/charmbracelet/huh@latest`
4. [ ] Add utility dependencies:
   - `go get gopkg.in/yaml.v3@latest`
   - `go get github.com/stretchr/testify@latest`
   - `go get github.com/google/go-cmp/cmp@latest`  # Superior diff output for complex structs
5. [ ] Run `go mod tidy`

**Verify:** `go build ./...`

---

### Task 3: Implement Root Command and Structured Errors (TDD)

**Context:**
- Create: `cmd/rr/main.go`, `internal/cli/root.go`, `internal/cli/version.go`, `internal/errors/errors.go`
- Reference: `../../ARCHITECTURE.md` lines 365-440 for error message design
- Reference: `../../proof-of-concept.sh` lines 32-45 for color palette

**TDD Approach:** Write `internal/errors/errors_test.go` FIRST, then implement to make tests pass.

**Steps:**

1. [ ] **Write tests first** in `internal/errors/errors_test.go`:
   - Test error message formatting
   - Test error wrapping preserves cause
   - Test error codes are correct
   - Use table-driven tests
2. [ ] Create `internal/errors/errors.go` to make tests pass:
   - Define `Error` struct: `Code`, `Message`, `Suggestion`, `Cause`
   - Implement `error` interface with formatted output
   - Add constructor `New(code, message, suggestion string) *Error`
   - Add `Wrap(err error, message string) *Error` for wrapping
   - Define error codes: `ErrConfig`, `ErrSSH`, `ErrSync`, `ErrLock`, `ErrExec`
3. [ ] Create `internal/cli/root.go`:
   - Define `rootCmd` with app description
   - Add global flags: `--config`, `--verbose`, `--quiet`, `--no-color`
   - Add `Execute()` function with error handling using structured errors
4. [ ] Create `internal/cli/version.go`:
   - Add `versionCmd` that prints version info
   - Use ldflags pattern for version injection: `version`, `commit`, `date`
5. [ ] Create `cmd/rr/main.go`:
   - Call `cli.Execute()`
   - Set version variables
6. [ ] Add stub commands for: `run`, `exec`, `sync`, `init`, `setup`, `status`, `monitor`, `doctor`, `completion`
   - Each returns structured error "not implemented yet"

**Verify:**
```bash
go build -o rr ./cmd/rr && ./rr --help
./rr version
./rr run "echo test"  # Should print structured "not implemented" error
go test ./internal/errors/...
```

---

### Task 4: Add Linting and Formatting

**Context:**
- Project root (after Task 1-2 complete)

**Steps:**

1. [ ] Create `.golangci.yml`:
   ```yaml
   linters:
     enable:
       - gofmt
       - govet
       - errcheck
       - staticcheck
       - unused
       - gosimple
       - ineffassign
   linters-settings:
     errcheck:
       check-blank: true
   ```
2. [ ] Create `Makefile` with targets:
   ```makefile
   .PHONY: build test lint fmt clean test-integration

   build:
   	go build -o rr ./cmd/rr

   test:
   	go test ./...

   test-integration:
   	RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v

   lint:
   	golangci-lint run

   fmt:
   	go fmt ./...

   clean:
   	rm -f rr
   	rm -rf dist/
   ```
3. [ ] Run `make lint` and fix any issues

**Verify:** `make lint && make build && make test`

---

### Task 5: Add GitHub Actions CI

**Context:**
- Create: `.github/workflows/ci.yml`, `.github/workflows/release.yml`

**Steps:**

1. [ ] Create `.github/workflows/ci.yml`:
   ```yaml
   name: CI
   on:
     push:
       branches: [main]
     pull_request:
       branches: [main]

   jobs:
     build:
       runs-on: ubuntu-latest
       steps:
         - uses: actions/checkout@v4
         - uses: actions/setup-go@v5
           with:
             go-version: '1.22'
         - name: Lint
           uses: golangci/golangci-lint-action@v4
         - name: Test
           run: go test ./...
         - name: Build
           run: go build ./cmd/rr
   ```
2. [ ] Create `.github/workflows/release.yml` (placeholder):
   ```yaml
   name: Release
   on:
     push:
       tags: ['v*']
   jobs:
     placeholder:
       runs-on: ubuntu-latest
       steps:
         - run: echo "Release workflow - configure in Phase 4"
   ```

**Verify:** Validate YAML syntax, push to GitHub and check Actions tab

---

### Task 6: Add Test Environment Setup

**Context:**
- Create: `tests/integration/README.md`, `tests/integration/setup_test.go`
- Create: `scripts/test-ssh-server.sh` (optional Docker-based SSH server)

**Steps:**

1. [ ] Create `tests/integration/README.md`:
   - Document how to set up local SSH for testing
   - Document Docker-based SSH server option
   - Document environment variables: `RR_TEST_SSH_HOST`, `RR_TEST_SKIP_SSH`
2. [ ] Create `tests/integration/setup_test.go`:
   - Skip tests if `RR_TEST_SKIP_SSH=1`
   - Helper to get test SSH host from env
   - Helper to create temp directories for sync tests
3. [ ] Create `scripts/test-ssh-server.sh`:
   ```bash
   #!/bin/bash
   # Runs a Docker SSH server for integration testing
   docker run -d --name rr-test-ssh \
     -p 2222:22 \
     linuxserver/openssh-server
   echo "SSH test server running on localhost:2222"
   echo "Set RR_TEST_SSH_HOST=localhost:2222 to use"
   ```
4. [ ] Update `CONTRIBUTING.md` with test setup instructions

**Verify:**
```bash
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...
# Or with real SSH:
RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v
```

---

### Task 7: Add Development Documentation

**Context:**
- Create: `CONTRIBUTING.md`, `README.md`, `LICENSE`

**Steps:**

1. [ ] Create `CONTRIBUTING.md`:
   - Development setup instructions (Go 1.22+, golangci-lint)
   - How to run tests (`make test`, `make test-integration`)
   - Test environment setup (reference `tests/integration/README.md`)
   - Code style guidelines (use structured errors, follow Go conventions)
   - Error handling guidelines (always use `internal/errors` types)
   - PR process
2. [ ] Create basic `README.md`:
   - Project name and one-line description
   - Installation (placeholder: "Coming soon")
   - Quick start (placeholder)
   - Link to ARCHITECTURE.md
   - Link to CONTRIBUTING.md
3. [ ] Add `LICENSE` file (MIT)

**Verify:** Files exist, links work, markdown renders correctly

---

### Task 8: Define Color Palette and UI Constants (TDD)

**Context:**
- Create: `internal/ui/colors.go`, `internal/ui/symbols.go`
- Reference: `../../proof-of-concept.sh` lines 32-53 for color palette and symbols

**TDD Approach:** Write tests FIRST that verify the expected color values and symbol constants exist.

**Steps:**

1. [ ] **Write tests first** in `internal/ui/ui_test.go`:
   - Test that semantic colors are defined (Success, Error, Warning, Info)
   - Test that text colors are defined (Primary, Secondary, Muted)
   - Test symbols are correct Unicode characters
   - Use table-driven tests
2. [ ] Create `internal/ui/colors.go` to make tests pass:
   - Define color constants using Lip Gloss
   - Semantic colors: `ColorSuccess`, `ColorError`, `ColorWarning`, `ColorInfo`
   - Text colors: `ColorPrimary`, `ColorSecondary`, `ColorMuted`
   - Match palette from proof-of-concept.sh
3. [ ] Create `internal/ui/symbols.go` to make tests pass:
   - Define symbols: `SymbolSuccess = "✓"`, `SymbolFail = "✗"`, `SymbolPending = "○"`, `SymbolProgress = "◐"`, `SymbolComplete = "●"`
   - Match symbols from proof-of-concept.sh lines 47-52

**Verify:** `go test ./internal/ui/...`

---

## Verification

After all tasks complete:

```bash
# Full verification
make lint
make test
make build
./rr --help
./rr version

# Verify error formatting
./rr run "test" 2>&1 | grep -q "not implemented"

# Verify test infrastructure
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...
```

Expected output from `./rr --help`:
```
rr - Remote Runner

Sync code to remote machines and execute commands with smart host fallback.

Usage:
  rr [command]

Available Commands:
  run         Sync files and execute command on remote
  exec        Execute command without syncing
  sync        Sync files only
  init        Create config file with guided prompts
  setup       Configure SSH keys for a host
  status      Show connectivity and selected host
  monitor     Real-time dashboard of host metrics
  doctor      Run diagnostic checks
  completion  Generate shell completions
  version     Show version information
  help        Help about any command

Flags:
  -c, --config string   Path to config file (default ".rr.yaml")
  -h, --help            help for rr
      --no-color        Disable colored output
  -q, --quiet           Minimal output
  -v, --verbose         Verbose output

Use "rr [command] --help" for more information about a command.
```
