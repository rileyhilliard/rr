---
name: rr
description: Sync code and run commands on remote machines. Use when running tests, builds, or commands remotely, syncing files to hosts, setting up remote development, or troubleshooting rr configuration.
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Edit
  - Grep
  - Glob
---

# rr (Road Runner) CLI

rr syncs code to remote machines and runs commands there. Handles host failover, file sync with rsync, distributed locking, and test output formatting.

## Quick Reference

```bash
rr run "make test"     # Sync files + run command
rr exec "git status"   # Run command without syncing
rr sync                # Just sync files
rr <taskname>          # Run named task from config
rr doctor              # Diagnose issues
rr monitor             # TUI dashboard for host metrics
```

## Two-Config System

rr uses two config files:

| Config | Location | Purpose |
|--------|----------|---------|
| Global | `~/.rr/config.yaml` | Personal host definitions (SSH, directories) |
| Project | `.rr.yaml` | Shareable project settings (tasks, sync rules) |

**See [config.md](config.md) for complete config reference.**

### Minimal Global Config

```yaml
version: 1
hosts:
  mini:
    ssh: [mac-mini.local, mac-mini-tailscale]
    dir: ${HOME}/projects/${PROJECT}
```

### Minimal Project Config

```yaml
version: 1
hosts: [mini]

sync:
  exclude: [.git/, node_modules/, .venv/]

tasks:
  test:
    run: pytest -v
```

## Commands Overview

| Command | Purpose |
|---------|---------|
| `rr run "cmd"` | Sync files, then run command |
| `rr exec "cmd"` | Run command without syncing |
| `rr sync` | Just sync files |
| `rr <taskname>` | Run named task |
| `rr tasks` | List available tasks |
| `rr doctor` | Diagnose issues |
| `rr host list/add/remove` | Manage hosts |

**See [commands.md](commands.md) for full command reference.**

### Common Flags

- `--host <name>` - Target specific host
- `--tag <tag>` - Select host by tag
- `--local` - Force local execution
- `--skip-requirements` - Skip requirement checks

## Tasks

Define reusable commands in `.rr.yaml`:

```yaml
tasks:
  test:
    description: Run tests
    run: pytest -v

  deploy:
    steps:
      - name: Build
        run: make build
      - name: Deploy
        run: ./deploy.sh
```

Run with: `rr test`, `rr deploy`

Extra arguments append to single-command tasks: `rr test -k "test_login"`

**See [tasks.md](tasks.md) for parallel tasks, multi-step tasks, and advanced configuration.**

## Remote Environment Bootstrap

Declare required tools with `require:` - rr verifies they exist before running commands:

```yaml
# .rr.yaml
require:
  - go
  - node

tasks:
  build:
    run: make build
    require: [cargo]  # Task-specific requirement
```

```yaml
# ~/.rr/config.yaml
hosts:
  gpu-box:
    ssh: [gpu.local]
    require: [nvidia-smi, python3]  # Host-specific requirements
```

Missing tools trigger actionable error messages. Tools with built-in installers (40+) can be auto-installed.

**See [requirements.md](requirements.md) for complete requirements reference.**

## How It Works

1. **Host Selection**: Tries SSH aliases in order until one connects
2. **Requirements**: Verifies required tools exist (if configured)
3. **File Sync**: Uses rsync with exclude/preserve patterns
4. **Locking**: Creates lock on remote; if locked, tries next host
5. **Execution**: Runs command with configured environment

## Troubleshooting

| Problem | Fix |
|---------|-----|
| SSH fails | Check `ssh <alias>` manually, verify `~/.ssh/config` |
| "command not found" | Add `setup_commands` or check `require` config |
| Sync slow | Add large dirs to `sync.exclude` |
| Lock stuck | `rr unlock` |

**See [troubleshooting.md](troubleshooting.md) for detailed diagnostics.**

## Machine Interface (LLM/CI)

Use `--machine` or `-m` for structured JSON output:

```bash
rr doctor --machine
rr status --machine
rr tasks --machine
```

**See [machine-interface.md](machine-interface.md) for JSON envelope format and error codes.**

## Quick Setup

```bash
# 1. Add a host
rr host add

# 2. Initialize project
cd your-project && rr init

# 3. Verify
rr doctor

# 4. Run
rr run "make test"
```

## When to Use Each Command

| Situation | Command |
|-----------|---------|
| Run tests with latest code | `rr run "make test"` |
| Quick check on remote | `rr exec "git log -1"` |
| Prep remote before multiple runs | `rr sync` |
| Debug connection issues | `rr doctor` |
| Watch resource usage | `rr monitor` |
| First time setup | `rr init` |
| Add new machine | `rr host add` |

## Reference Files

- **[config.md](config.md)** - Complete config reference (global + project)
- **[commands.md](commands.md)** - All commands and flags
- **[tasks.md](tasks.md)** - Task definitions, parallel execution, multi-step
- **[requirements.md](requirements.md)** - Remote environment bootstrap
- **[machine-interface.md](machine-interface.md)** - JSON output and error codes
- **[troubleshooting.md](troubleshooting.md)** - Diagnostics and common fixes
