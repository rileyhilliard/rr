# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
