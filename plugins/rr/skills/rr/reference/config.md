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
  local_fallback: never   # never | on-unreachable | always (true = always, false = never)
  probe_timeout: 2s
  rewrite_paths: true     # Rewrite local absolute paths in commands to remote paths
```

### Host Options

| Field | Purpose |
|-------|---------|
| `ssh` | List of SSH connection strings, tried in order |
| `dir` | Working directory on remote (supports variable expansion) |
| `tags` | Labels for filtering with `--tag` flag |
| `env` | Environment variables set for all commands |
| `shell` | Shell invocation the command is appended to (default: `${SHELL:-/bin/bash} -c`, after sourcing `~/.bashrc` and `~/.zshrc` if present). Use `"zsh -l -c"` for a login shell or `"bash -o pipefail -c"` to catch failures inside pipes |
| `setup_commands` | Commands run before every remote command (`run`, `exec`, tasks) |
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

These commands are prepended (joined with `&&`) to every remote command: `rr run`, `rr exec`, and tasks.

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

# Defaults applied to all tasks and ad-hoc run/exec commands
defaults:
  setup:
    - source ~/.local/bin/env
  env:
    PYTHONDONTWRITEBYTECODE: "1"

sync:
  exclude:            # Replaces the default list, so repeat the defaults you want
    - .git
    - node_modules
    - .venv
    - build/
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
| `setup` | Commands run before every task and every remote `rr run`/`rr exec` |
| `env` | Environment variables applied to all tasks |

Project-level `local_fallback` and `rewrite_paths` override the global `defaults` values.

### Merge Order (lowest to highest precedence)

Single tasks and parallel subtasks use the same merge.

**Environment variables:**
1. Host `env` (from global config)
2. Project `defaults.env`
3. Task-specific `env`

**Setup commands:**
1. Host `setup_commands` (from global config)
2. Project `defaults.setup`
3. Then the task command runs

Setup commands run after the `cd` into the remote project dir, so relative paths in them resolve there.

### Sync Configuration

| Field | Default | Purpose |
|-------|---------|---------|
| `exclude` | see below | Patterns to skip during sync (rsync exclude). Setting it replaces the defaults |
| `preserve` | `.venv/`, `node_modules/`, `data/`, `.cache/` | Patterns kept on the remote even when missing locally |
| `respect_gitignore` | `true` | Apply `.gitignore` patterns as rsync excludes |
| `flags` | `[]` | Extra rsync flags (e.g. `--compress`) |
| `invalidations` | JS and Python lockfiles | Delete a preserved remote dir (e.g. `node_modules/`) when its lockfile changes locally |
| `worktree_isolation` | `true` | Give each linked git worktree its own remote dir (`<repo>@<worktree>`) |
| `prune_worktrees` | `true` | After a sync, remove remote dirs for worktrees that no longer exist locally |

Default excludes: `.git`, `.venv`, `node_modules` (bare patterns, so they match files and symlinks too), `__pycache__/`, `*.pyc`, `.mypy_cache/`, `.pytest_cache/`, `.ruff_cache/`, `.DS_Store`, `*.log`, `.claude/`, `.cursor/`, `.aider/`, `.copilot/`.

When `respect_gitignore` is true, rr reads the repo-root `.gitignore` (nested `.gitignore` files are not read) and translates it into rsync filter rules, including `!` negations. Explicit `.rr.yaml` excludes take precedence.

### Lock Configuration

| Field | Default | Purpose |
|-------|---------|---------|
| `enabled` | `true` | Enable distributed locking |
| `timeout` | `5m` | How long to wait for a lock on a single host |
| `wait_timeout` | `1m` | With several hosts all locked, how long to keep cycling before failing (or falling back locally with `local_fallback: always`) |
| `stale` | `90s` | Time without a heartbeat before a lock is considered dead |
| `dir` | `/tmp/rr-locks` | Remote directory holding the lock (`<dir>/rr.lock/`) |

There is one lock per host, shared by every project that uses the same `lock.dir`. The holder refreshes it every 30 seconds. A lock without a heartbeat for the `stale` duration is reclaimed automatically, and a lock held by a dead rr process on your own machine is reclaimed immediately.

## Config Warnings and Reserved Names

rr reports config it accepts but ignores, once per invocation, as a `config` phase event with `status: warn` (a styled warning with `--pretty`). That covers unknown keys (usually typos), the removed `output:` section, the removed `defaults.host` global key, `pull:` on a parallel task, and `output:` on a non-parallel task. The config still loads; remove what the warning names.

Task names can't shadow built-in commands. `run`, `exec`, `sync`, `pull`, `logs`, `provision`, `prune`, `doctor`, and the rest are reserved; a task with a reserved name fails validation with `CONFIG_INVALID` and a suggestion to rename it.

## Variable Expansion

The `dir` field supports:

| Variable | Expands to |
|----------|------------|
| `${PROJECT}` | Git repo name, or the project directory name. In a linked worktree: `<repo>@<worktree>` |
| `${USER}` | Local username |
| `${HOME}` | Remote user's home directory (passed to the remote as `~`) |
