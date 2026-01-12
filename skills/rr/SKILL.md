---
name: rr
description: Use the rr CLI to sync code and run commands on remote machines. Invoke when the user wants to run tests, builds, or commands remotely, sync files to a remote host, set up remote development, or troubleshoot rr configuration.
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Edit
  - Grep
  - Glob
---

# rr (Road Runner) CLI

rr syncs code to remote machines and runs commands there. It handles host failover, file sync with rsync, distributed locking, and test output formatting.

## Quick Reference

```bash
rr run "make test"     # Sync files + run command
rr exec "git status"   # Run command without syncing
rr sync                # Just sync files
rr doctor              # Diagnose issues
rr monitor             # TUI dashboard for host metrics
```

## Two-Config System

rr uses two config files with different purposes:

### Global Config (`~/.rr/config.yaml`)

Personal host definitions. Not shared with team. Contains SSH connections, directories, and machine-specific settings.

```yaml
version: 1

hosts:
  mini:
    ssh:
      - mac-mini.local      # LAN hostname - try first
      - mac-mini-tailscale  # SSH config alias - fallback
    dir: ${HOME}/projects/${PROJECT}
    tags:
      - fast
    env:
      DEBUG: "1"

  server:
    ssh:
      - dev-server          # SSH config alias from ~/.ssh/config
    dir: /var/projects/${PROJECT}

defaults:
  host: mini
  local_fallback: false
  probe_timeout: 2s
```

**SSH entries can be:**
- Hostnames: `mac-mini.local`, `192.168.1.50`
- User@host: `deploy@server.example.com`
- SSH config aliases: Names defined in `~/.ssh/config` (e.g., `dev-server`, `mac-mini-tailscale`)

**Passwordless SSH is required.** The user must be able to run `ssh <alias>` without entering a password. This typically means configuring key-based auth in `~/.ssh/config`.

### Project Config (`.rr.yaml`)

Shareable project settings. Can be committed to version control. References hosts by name from global config.

```yaml
version: 1

# Reference hosts from global config (optional)
hosts:
  - mini
  - server

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
```

## Commands

### Core Commands

| Command | Purpose |
|---------|---------|
| `rr run "cmd"` | Sync files, then run command |
| `rr exec "cmd"` | Run command without syncing |
| `rr sync` | Just sync files, no command |
| `rr <taskname>` | Run named task from config |

### Host Management

| Command | Purpose |
|---------|---------|
| `rr host list` | List configured hosts |
| `rr host add` | Add new host interactively |
| `rr host remove <name>` | Remove a host |

### Diagnostics & Monitoring

| Command | Purpose |
|---------|---------|
| `rr doctor` | Diagnose SSH, config, dependency issues |
| `rr monitor` | TUI dashboard showing CPU/RAM/GPU |
| `rr status` | Show connection and sync status |

### Setup & Utilities

| Command | Purpose |
|---------|---------|
| `rr init` | Create .rr.yaml in current project |
| `rr setup <host>` | Configure SSH keys for a host |
| `rr unlock` | Release stuck lock on default host |
| `rr unlock --all` | Release locks on all hosts |
| `rr update` | Update rr to latest version |
| `rr completion <shell>` | Generate shell completions |

## Common Flags

Most commands accept:

- `--host <name>` - Target specific host
- `--tag <tag>` - Select host by tag
- `--probe-timeout <duration>` - SSH probe timeout (e.g., `5s`)

Sync-specific:
- `--dry-run` - Show what would sync without syncing

## Variable Expansion

The `dir` field in host config supports:

| Variable | Expands to |
|----------|------------|
| `${PROJECT}` | Current directory name |
| `${USER}` | Local username |
| `${HOME}` | Remote user's home directory |

## Tasks

Define reusable commands in `.rr.yaml`:

```yaml
tasks:
  test:
    description: Run all tests
    run: pytest -v

  deploy:
    description: Build and deploy
    steps:
      - name: Build
        run: make build
      - name: Deploy
        run: ./deploy.sh
        on_fail: stop
```

Run with: `rr test`, `rr deploy`

## How It Works

1. **Host Selection**: Tries SSH aliases in order until one connects and is not busy
2. **File Sync**: Uses rsync with exclude/preserve patterns
3. **Locking**: Creates lock on remote before running; if host locked, tries next host
4. **Load Balancing**: When all hosts locked, round-robins until one frees up
5. **Output Formatting**: Auto-detects pytest/jest/go test and formats failures

## Troubleshooting

### Check Configuration

```bash
rr doctor           # Full diagnostic
rr host list        # See configured hosts
```

### Connection Issues

If SSH fails:
1. Verify `ssh <alias>` works manually
2. Check `~/.rr/config.yaml` has correct SSH aliases
3. Run `rr doctor` for detailed diagnostics

### Sync Slow

Add exclusions to `.rr.yaml`:

```yaml
sync:
  exclude:
    - .git/
    - node_modules/
    - .venv/
    - __pycache__/
```

### Stuck Lock

```bash
rr unlock              # Default host
rr unlock <hostname>   # Specific host
rr unlock --all        # All hosts
```

## When to Use Each Command

| Situation | Command |
|-----------|---------|
| Run tests with latest code | `rr run "make test"` |
| Quick check on remote | `rr exec "git log -1"` |
| Prep remote before multiple runs | `rr sync` |
| Debug connection issues | `rr doctor` |
| Watch resource usage | `rr monitor` |
| First time setup in project | `rr init` |
| Add new remote machine | `rr host add` |

## Example Workflows

### Initial Setup

```bash
# 1. Add a host to global config
rr host add

# 2. Initialize project config
cd your-project
rr init

# 3. Verify everything works
rr doctor

# 4. Run something
rr run "make test"
```

### Daily Development

```bash
# Run tests
rr run "pytest -v"

# Or use a named task
rr test

# Quick command without sync
rr exec "git status"

# Monitor hosts while working
rr monitor
```

### Multiple Hosts (Load Balancing)

```yaml
# ~/.rr/config.yaml
hosts:
  gpu-1:
    ssh: [gpu1.local, gpu1-tailscale]
    dir: ~/projects/${PROJECT}
  gpu-2:
    ssh: [gpu2.local, gpu2-tailscale]
    dir: ~/projects/${PROJECT}
```

```yaml
# .rr.yaml
hosts:
  - gpu-1
  - gpu-2
```

Now `rr run` automatically uses whichever GPU box is free.
