# Configuration Reference

rr uses two config files with different purposes.

## Global Config (`~/.rr/config.yaml`)

Personal host definitions. Not shared with team. Contains SSH connections, directories, and machine-specific settings.

```yaml
version: 1

hosts:
  mini:
    ssh:
      - mac-mini.local      # LAN hostname - try first
      - mac-mini-tailscale  # SSH config alias - fallback
    dir: ${HOME}/projects/${PROJECT}
    tags: [fast]
    env:
      DEBUG: "1"
    require: [go, node]     # Tools that must exist on this host

  server:
    ssh: [dev-server]       # SSH config alias from ~/.ssh/config
    dir: /var/projects/${PROJECT}

defaults:
  local_fallback: false
  probe_timeout: 2s
```

### Host Options

| Field | Purpose |
|-------|---------|
| `ssh` | List of SSH connection strings, tried in order |
| `dir` | Working directory on remote (supports variable expansion) |
| `tags` | Labels for filtering with `--tag` flag |
| `env` | Environment variables set for all commands |
| `shell` | Custom shell (default: `$SHELL` or `/bin/bash`) |
| `setup_commands` | Commands run before every task |
| `require` | Tools that must exist on this host |

### SSH Entries

SSH entries can be:
- Hostnames: `mac-mini.local`, `192.168.1.50`
- User@host: `deploy@server.example.com`
- SSH config aliases: Names defined in `~/.ssh/config`

**Passwordless SSH is required.** Configure key-based auth in `~/.ssh/config`.

### Setup Commands

If you repeat the same setup in every task, move it to `setup_commands`:

```yaml
hosts:
  dev-box:
    ssh: [dev.local, dev-tailscale]
    dir: ~/projects/${PROJECT}
    setup_commands:
      - source ~/.local/bin/env     # Load uv, pyenv, etc.
      - export PATH="$HOME/.bun/bin:$PATH"
    env:
      PYTHONDONTWRITEBYTECODE: "1"
```

These commands are automatically prepended to every task.

## Project Config (`.rr.yaml`)

Shareable project settings. Can be committed to version control.

```yaml
version: 1

# Reference hosts from global config
hosts:
  - mini
  - server

# Project-level requirements
require:
  - go
  - golangci-lint

# Defaults applied to all tasks
defaults:
  setup:
    - source ~/.local/bin/env
    - set -o pipefail
  env:
    PYTHONDONTWRITEBYTECODE: "1"

sync:
  exclude:
    - .git/
    - node_modules/
    - .venv/
  preserve:
    - .venv/
    - node_modules/

lock:
  enabled: true
  timeout: 5m

tasks:
  test:
    run: pytest -v
  build:
    run: make build
    require: [cargo]  # Task-specific requirement
```

### Project Defaults

| Field | Purpose |
|-------|---------|
| `setup` | Commands run before every task |
| `env` | Environment variables applied to all tasks |

### Merge Order (lowest to highest precedence)

**Environment variables:**
1. Host `env` (from global config)
2. Project `defaults.env`
3. Task-specific `env`

**Setup commands:**
1. Host `setup_commands` (from global config)
2. Project `defaults.setup`
3. Then the task command runs

### Sync Configuration

| Field | Default | Purpose |
|-------|---------|---------|
| `exclude` | see below | Patterns to skip during sync (rsync exclude) |
| `preserve` | `[]` | Patterns to preserve on remote (don't delete) |
| `respect_gitignore` | `true` | Apply `.gitignore` patterns as rsync excludes |

Default excludes include `.git/`, `.claude/`, `.cursor/`, `.aider/`, `.copilot/`, `.venv/`, `node_modules/`, `__pycache__/`, and others.

When `respect_gitignore` is true, rsync reads `.gitignore` files in each directory and applies those patterns as excludes. Explicit `.rr.yaml` excludes take precedence (first-match-wins).

### Lock Configuration

| Field | Default | Purpose |
|-------|---------|---------|
| `enabled` | `true` | Enable distributed locking |
| `timeout` | `5m` | Lock acquisition timeout |
| `stale` | `3m` | Time without heartbeat before lock is considered dead |

Locks are refreshed every 30 seconds via heartbeat. A lock without a heartbeat update for the `stale` duration is automatically reclaimed.

## Variable Expansion

The `dir` field supports:

| Variable | Expands to |
|----------|------------|
| `${PROJECT}` | Current directory name |
| `${USER}` | Local username |
| `${HOME}` | Remote user's home directory |
