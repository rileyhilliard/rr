# Remote Runner (`rr`) Implementation Plan

> **Status:** APPROVED - Ready for execution

## Overview

**What we're building:** A Go CLI tool that syncs local code to remote machines and executes commands, with smart host fallback (LAN -> VPN -> local), atomic locking, and beautiful terminal output.

**Why:** Fill the gap between "just rsync && ssh" scripts and heavyweight tools like Ansible. Target solo developers and small teams with shared build machines, home labs, or Mac Mini clusters.

**Proof of concept:** See [proof-of-concept.sh](../../proof-of-concept.sh) - a 579-line bash script that demonstrates the core workflow `rr` will replace. It handles:
- Host fallback (mini-local -> mini -> local)
- Atomic locking with mkdir
- rsync with excludes/preserves
- pytest output formatting with failure summaries
- Timing for each phase

The bash script works but is hard to maintain, extend, and distribute. `rr` will provide the same functionality in a single Go binary with a cleaner interface.

**Architecture reference:** See [ARCHITECTURE.md](../../ARCHITECTURE.md) for full design details.

## Success Criteria

- [ ] Single binary distribution (no runtime deps)
- [ ] Zero to working in <60 seconds
- [ ] Test coverage >80%
- [ ] Connection probe <500ms for reachable hosts
- [ ] Sync performance within 10% of raw rsync

## Phase Overview

| Phase | Name | Status | Tasks | Description |
|-------|------|--------|-------|-------------|
| 0 | [Project Setup](./phase-0-setup.md) | NOT_STARTED | 8 | Go module, directory structure, CI, dependencies, error types |
| 1 | [Core MVP](./phase-1-core-mvp.md) | NOT_STARTED | 7 | Single host, sync, run, lock, basic output |
| 2 | [Smart Host Selection](./phase-2-host-selection.md) | NOT_STARTED | 5 | Multi-alias fallback, status, doctor (with --json) |
| 3 | [Tasks & Formatters](./phase-3-tasks-formatters.md) | NOT_STARTED | 6 | Named tasks, pytest/jest/go formatters |
| 4 | [Distribution](./phase-4-distribution.md) | NOT_STARTED | 7 | GoReleaser, Homebrew, docs, v0.1.0 release |
| 5 | [Host Monitoring](./phase-5-monitoring.md) | NOT_STARTED | 8 | Real-time TUI dashboard |

**Total: 41 tasks across 6 phases**

## Execution Order

Phases must be executed in order. Within each phase, task groups can run in parallel where noted.

```
Phase 0 (Setup)           - Foundation: module, CI, error types, test infra
     │
     v
Phase 1 (Core MVP)        - Usable tool: sync + run + lock
     │
     v
Phase 2 (Host Selection)  - Smart: fallback, status, doctor
     │
     v
Phase 3 (Tasks)           - Power user: named tasks, formatters
     │
     v
Phase 4 (Distribution)    - Installable: brew, go install, docs, v0.1.0
     │
     v
Phase 5 (Monitoring)      - Complete: TUI dashboard for host metrics
```

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Config version | Enforced `version: 1` | Enables future migrations, clear errors |
| Lock location | Configurable `lock.dir` | Supports `/tmp` alternatives |
| Reserved names | 12 commands | Prevents task/command conflicts |
| Error types | Structured with suggestions | Better DX than raw errors |
| Phase order | Distribution before Monitor | Users need install before nice-to-haves |

## Reserved Command Names

These cannot be used as task names:
- `run`, `exec`, `sync`, `init`, `setup`, `status`
- `monitor`, `doctor`, `help`, `version`, `completion`, `update`

## Key Dependencies

| Dependency | Purpose | Version |
|------------|---------|---------|
| github.com/spf13/cobra | CLI framework | v1.8+ |
| github.com/spf13/viper | Config management | v1.18+ |
| golang.org/x/crypto/ssh | SSH client | latest |
| github.com/kevinburke/ssh_config | Parse ~/.ssh/config | latest |
| github.com/charmbracelet/bubbletea | TUI framework | v0.25+ |
| github.com/charmbracelet/lipgloss | Terminal styling | v0.10+ |
| github.com/charmbracelet/huh | Interactive prompts | v0.3+ |
| gopkg.in/yaml.v3 | YAML parsing | v3 |
| github.com/stretchr/testify | Testing assertions | v1.8+ |
| github.com/google/go-cmp | Struct comparison (diffs) | latest |

## Directory Structure (Target)

```
rr/
├── cmd/rr/main.go
├── internal/
│   ├── cli/           # Cobra commands
│   ├── config/        # Config loading & validation
│   ├── errors/        # Structured error types
│   ├── host/          # Host selection & probing
│   ├── sync/          # rsync wrapper
│   ├── exec/          # Command execution
│   ├── lock/          # Lock management
│   ├── setup/         # SSH key setup
│   ├── doctor/        # Diagnostics
│   ├── monitor/       # TUI dashboard
│   ├── output/        # Formatters & streaming
│   └── ui/            # Shared TUI components
├── pkg/sshutil/       # Reusable SSH utilities
├── tests/integration/ # Integration tests
├── configs/schema.json
├── completions/       # Shell completion files
├── scripts/           # Install & test scripts
├── docs/              # Documentation
├── .goreleaser.yaml
├── go.mod
└── README.md
```

## Progress Log

| Date | Phase | Notes |
|------|-------|-------|
| - | - | - |

## Testing Philosophy

This project follows **Test-Driven Development (TDD)** using the Testing Trophy model.

### Testing Trophy Priority

| Priority | Type        | When                                            |
|----------|-------------|-------------------------------------------------|
| 1st      | Integration | Default - multiple units with real dependencies |
| 2nd      | E2E         | Complete user workflows                         |
| 3rd      | Unit        | Pure functions only (no dependencies)           |

### Mocking Guidelines

**Default: Don't mock. Use real dependencies.**

**Only mock:**
- External HTTP/API calls
- Time/randomness
- Third-party services (payments, email)

**Never mock:**
- Internal modules
- Database queries (use test DB)
- Business logic
- Your own code calling your own code

### Go-Specific Patterns

- **Use `testify/require`** for setup and errors (stop test immediately)
- **Use `testify/assert`** for simple value checks
- **Use `google/go-cmp`** for complex struct comparisons (superior diff output)
- **Table-driven tests** with `t.Parallel()` for speed
- **Testcontainers** for real dependencies (SSH server, etc.)
- **`httptest`** for HTTP handler testing

### Anti-Patterns to Avoid

| Pattern                         | Fix                         |
|---------------------------------|-----------------------------|
| Testing mock calls              | Test actual outcome         |
| Test-only methods in production | Move to test utilities      |
| `time.Sleep(500ms)`             | Use condition-based waiting |
| Asserting on internal state     | Assert on observable output |

### TDD Workflow

1. **Red**: Write a failing test that defines the expected behavior
2. **Green**: Write minimal code to make the test pass
3. **Refactor**: Clean up while keeping tests green

Every feature starts with a test. No exceptions.

## Rollback Strategy

If a phase reveals fundamental issues with a previous phase:

1. Stop work on current phase
2. Document the issue and proposed fix
3. Create a patch task in the affected phase
4. Complete the patch before resuming

## Notes

- All code should have tests. Run `go test ./...` after each task.
- Use `golangci-lint` for linting.
- Follow Go conventions: short variable names, error handling, etc.
- Reference `proof-of-concept.sh` for working patterns when implementing.
- Each phase has explicit exit criteria - don't proceed until met.
