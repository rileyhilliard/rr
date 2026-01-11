# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.7.2] - 2026-01-11

### Added

- New "default" sort order in monitor that shows online hosts first, then follows config priority order (default host, then fallbacks)
- Horizontal navigation keys (left/right/h/l) for host selection in monitor TUI
- VHS demo tapes reorganized to `tapes/` directory with new demos for doctor, init, failover, and tasks commands
- Mock rr binary for deterministic VHS demo recordings without requiring SSH
- `make demos` target for batch recording all demo GIFs

## [0.7.1] - 2026-01-11

### Added

- DNS and connection reset probe failure types for better SSH error categorization
- Host key mismatch errors now preserve original error for detailed suggestions
- Monitor cards show parsed error parts with word-wrapped suggestions for unreachable hosts

### Fixed

- Rsync progress parsing now handles carriage returns for real-time progress updates
- SSH config parsing continues reading Host blocks after Match blocks instead of stopping

### Changed

- Improved error suggestions to be more actionable and concise
- Updated troubleshooting docs with better host key verification advice

## [0.7.0] - 2026-01-11

### Added

- Multi-host project support: projects can now specify multiple hosts with `hosts:` (plural) for load balancing
- `ResolveHosts()` function with priority: CLI flag > project.Hosts > project.Host > all global hosts
- Init wizard multi-select for choosing which hosts a project can use
- Validation for hosts list (no duplicates, valid references)

### Changed

- Project config now generates `hosts:` list format instead of singular `host:`
- Empty hosts list means "use all global hosts" (backwards compatible)
- Improved test isolation across CLI test files

## [0.6.0] - 2026-01-11

### Added

- Global config separation: hosts now live in `~/.rr/config.yaml` (personal) while project config stays in `.rr.yaml` (shareable)
- `LoadGlobal()`, `SaveGlobal()`, `LoadResolved()` config functions
- `ResolveHost()` with priority: CLI flag > project.Host > global default > first alphabetically
- Migration guide in `docs/MIGRATION.md` for upgrading from v0.4.x

### Changed

- `rr host add/remove/list` now read/write global config instead of project config
- `rr init` creates project config without hosts, prompts to select from global hosts
- Project `.rr.yaml` uses `host: <name>` reference instead of inline host definitions

### Breaking Changes

- Hosts must be moved from `.rr.yaml` to `~/.rr/config.yaml`
- See `docs/MIGRATION.md` for upgrade instructions

## [0.5.0] - 2026-01-11

### Changed

- Overhauled UI to gen-z electric synthwave aesthetic

## [0.4.8] - 2026-01-11

### Changed

- Simplified monitor pool implementation for better performance

### Documentation

- Clarified usage of multiple machines in README

## [0.4.7] - 2026-01-11

### Added

- Contextual help for command failures with exit code explanations
- Troubleshooting suggestions for unexplained failures
- Tool installers for 25+ common tools (bun, uv, deno, yarn, pnpm, docker, kubectl, terraform, aws, gcloud, jq, ripgrep, fd, fzf, and more)

### Fixed

- Detection of missing tools when invoked via make/shell (e.g., `/bin/sh: line 1: uv: command not found`)

## [0.4.6] - 2026-01-11

### Added

- `rr unlock` command for manually releasing stale locks
- Signal handling improvements with proper cleanup on Ctrl+C

### Fixed

- SSH BatchMode for rsync hang issue
- Remote artifact cleanup when removing hosts
- macOS memory calculation accuracy
- Braille sparkline coloring in monitor graphs

## [0.4.5] - 2026-01-11

### Fixed

- Source `.bashrc`/`.zshrc` for proper PATH in remote commands

### Changed

- README updated with image instead of video

## [0.4.4] - 2026-01-11

### Added

- SSH setup guide as prerequisite documentation

### Fixed

- CPU usage calculation now uses delta instead of cumulative jiffies
- Progress bar timing slowed to 30 seconds for better UX
- Display host name instead of IP in status output
- Added linux_arm64 asset to TestFindAsset

## [0.4.3] - 2026-01-10

### Fixed

- Expand `${HOME}` to `~` for remote paths instead of local home directory

## [0.4.2] - 2026-01-10

### Added

- Interactive tool provisioning when commands fail on remote

### Fixed

- Improved command-not-found detection and guidance

## [0.4.1] - 2026-01-10

### Fixed

- Respect default host in load balancing mode

## [0.4.0] - 2026-01-10

### Added

- Load-balanced host selection: when multiple hosts are configured, work is distributed across available hosts instead of using subsequent hosts only as failover
- Non-blocking lock acquisition with `TryAcquire` for immediate lock status checks
- Round-robin waiting when all hosts are locked (configurable via `lock.wait_timeout`, default 1 minute)
- `local_fallback` now takes precedence: if true and all hosts locked, immediately runs locally without waiting
- Auto-detection of PATH differences between local and remote, with automatic `setup_commands` suggestions during `rr init`

### Changed

- Workflow phase order changed to Connect -> Lock -> Sync (avoids syncing to hosts that can't be locked)
- Shared golangci-lint version between CI and local via `.golangci-version` file

### Developer Experience

- Test coverage improved to 70.2%
- New integration tests for load balancing functionality

## [0.3.3] - 2026-01-09

### Security

- Bumped Go to 1.24.11 for security fixes

### Fixed

- CI release workflow now reads Go version from go.mod

## [0.3.2] - 2026-01-09

### Added

- PATH diagnostics for "command not found" errors with actionable suggestions

### Security

- Bumped Go to 1.24.4 for security fixes

### Fixed

- Use `${HOME}` instead of `~` for default remote directory (shell compatibility)
- Improved SSH server startup reliability in CI

### Changed

- CI dependencies updated (actions/setup-go v6, golangci-lint-action v9, upload-artifact v6)

## [0.3.1] - 2026-01-09

### Fixed

- Use `${HOME}` instead of `~` for default remote directory path expansion

## [0.3.0] - 2026-01-09

### Added

- `rr hosts` command for managing configured hosts
- `rr update` command for self-updating the CLI
- CI integration tests with SSH server in Docker
- Expanded test coverage across packages

### Changed

- Improved UX for host selection and error messages

## [0.2.2] - 2026-01-09

### Changed

- Use login shell by default for proper PATH setup on remote hosts
- Set coverage threshold to 50% with CI enforcement

## [0.2.1] - 2026-01-09

### Added

- `rr update` command for self-updating to the latest release

## [0.2.0] - 2025-01-09

### Added

- SSH mock testing infrastructure with virtual filesystem simulation
- `SSHClient` interface for dependency injection and testability
- Comprehensive lock system tests using mock infrastructure
- CI integration tests with GitHub Actions
- Test coverage reporting and enforcement

### Changed

- `host.Connection.Client` now uses `SSHClient` interface (enables mock injection)
- Lock helper functions accept interface instead of concrete type
- Upgraded Go dependencies

### Developer Experience

- Test coverage improved from ~50% to 55.6%
- Lock package coverage: 34% to 86.8%
- New `pkg/sshutil/testing` package for SSH simulation in tests

## [0.1.0] - 2025-01-08

### Added

- Initial release of Road Runner CLI
- `rr run` - Sync files and execute commands remotely
- `rr exec` - Execute commands without syncing
- `rr sync` - Sync files only
- `rr init` - Create configuration file interactively
- `rr setup` - Configure SSH keys for a host
- `rr doctor` - Diagnose configuration and connectivity issues
- `rr status` - Show connection and sync status
- `rr monitor` - Watch files and sync on changes
- Multi-host fallback with latency-based selection
- Local fallback when all remote hosts fail
- Atomic locking to prevent concurrent execution
- Task definitions with multi-step support
- Output formatters for pytest, Jest, Go test, and Cargo
- Shell completions for bash, zsh, fish, and PowerShell
- Comprehensive documentation (README, configuration guide, troubleshooting)
