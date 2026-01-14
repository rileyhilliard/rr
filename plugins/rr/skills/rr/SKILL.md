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
  local_fallback: false
  probe_timeout: 2s
```

#### Host Options

| Field | Purpose |
|-------|---------|
| `ssh` | List of SSH connection strings, tried in order |
| `dir` | Working directory on remote (supports variable expansion) |
| `tags` | Labels for filtering with `--tag` flag |
| `env` | Environment variables set for all commands |
| `shell` | Custom shell (default: `$SHELL` or `/bin/bash`) |
| `setup_commands` | Commands run before every task (reduces repetition) |

#### Reducing Task Verbosity with `setup_commands`

If you find yourself repeating the same setup in every task (sourcing environments, setting PATH, etc.), move it to `setup_commands` in the host config:

```yaml
# ~/.rr/config.yaml
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

These commands are automatically prepended to every task, so your `.rr.yaml` tasks stay clean:

```yaml
# .rr.yaml - no need to repeat setup in each task
tasks:
  test:
    run: uv run pytest -v
  build:
    run: bun run build
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

# Defaults applied to all tasks (reduces repetition)
defaults:
  setup:
    - source ~/.local/bin/env     # Source environment (uv, pyenv, etc.)
    - set -o pipefail             # Fail on pipe errors
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
    run: pytest -v      # defaults.setup runs first automatically
  build:
    run: make build
```

#### Project Defaults

The `defaults` section reduces repetition across tasks:

| Field | Purpose |
|-------|---------|
| `setup` | Commands run before every task (sourcing envs, shell options) |
| `env` | Environment variables applied to all tasks |

**Merge order** (lowest to highest precedence):
1. Host `env` (from global config)
2. Project `defaults.env`
3. Task-specific `env`

For setup commands:
1. Host `setup_commands` (from global config)
2. Project `defaults.setup`
3. Then the task command runs

## Commands

### Core Commands

| Command | Purpose |
|---------|---------|
| `rr run "cmd"` | Sync files, then run command |
| `rr exec "cmd"` | Run command without syncing |
| `rr sync` | Just sync files, no command |
| `rr <taskname>` | Run named task from config |
| `rr tasks` | List all available tasks |

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

Extra arguments are appended to single-command tasks:

```bash
rr test tests/test_api.py    # Runs: pytest -v tests/test_api.py
rr test -k "test_login"      # Runs: pytest -v -k "test_login"
```

Note: Args are only supported for tasks with a single `run` command, not multi-step tasks.

### Parallel Tasks

Run multiple tasks concurrently across available hosts. Useful for running tests on multiple architectures or parallelizing independent jobs:

```yaml
tasks:
  test-all:
    description: Run all tests in parallel
    parallel:
      - test
      - lint
      - vet
    fail_fast: false    # Continue even if one fails
    timeout: 10m

  quick-check:
    description: Fast verification
    parallel:
      - vet
      - lint
    fail_fast: true     # Stop on first failure
    max_parallel: 2     # Limit concurrency
```

Run with: `rr test-all`, `rr quick-check`

#### Parallel Task Flags

| Flag | Purpose |
|------|---------|
| `--stream` | Show real-time interleaved output with `[host:task]` prefixes |
| `--verbose` | Show full output per task on completion |
| `--quiet` | Summary only |
| `--fail-fast` | Stop on first failure (overrides config) |
| `--max-parallel N` | Limit concurrent tasks |
| `--dry-run` | Show plan without executing |
| `--local` | Force local execution (no remote hosts) |

#### Output Modes

- **progress** (default): Live status indicators with spinners
- **stream**: Real-time output with `[host:task]` prefixes
- **verbose**: Full output shown when each task completes
- **quiet**: Summary only at the end

Example:
```bash
rr test-all --stream    # See all output in real-time
rr test-all --dry-run   # Preview what would run
rr test-all --local     # Run locally without remote hosts
```

#### Work-Stealing Distribution

Tasks are distributed across hosts using a work-stealing queue. Fast hosts automatically grab more work, providing natural load balancing without pre-assignment.

#### Log Storage

Task output is saved to `~/.rr/logs/<task>-<timestamp>/`:
- One file per subtask
- Summary file with timing and results

Use `rr logs` to view recent logs or `rr logs clean` to remove old ones.

#### Multi-Step Task Progress

Multi-step tasks show progress as each step runs:

```
━━━ Step 1/3: Build ━━━
$ make build
[output...]
● Step 1/3: Build (2.3s)

━━━ Step 2/3: Test ━━━
$ pytest -v
[output...]
● Step 2/3: Test (45.1s)

━━━ Step 3/3: Deploy ━━━
$ ./deploy.sh
[output...]
● Step 3/3: Deploy (5.2s)
```

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

## Machine Interface (LLM/CI Mode)

When running rr from an LLM or CI context, use `--machine` (or `-m`) for structured JSON output with a consistent envelope format.

### Global Flag

```bash
rr --machine <command>   # JSON output with success/error envelope
rr -m doctor             # Short form
```

### Read Commands with JSON Output

| Command | Purpose |
|---------|---------|
| `rr doctor --machine` | Full diagnostic with structured results |
| `rr status --machine` | Host connectivity check |
| `rr tasks --machine` | List available tasks |
| `rr host list --machine` | List configured hosts |

### JSON Envelope Format

All `--machine` output follows this structure:

```json
{
  "success": true,
  "data": { /* command-specific output */ },
  "error": null
}
```

On failure:
```json
{
  "success": false,
  "data": null,
  "error": {
    "code": "SSH_AUTH_FAILED",
    "message": "Authentication failed for host m1-mini",
    "suggestion": "Run: ssh-copy-id m1-mini"
  }
}
```

### Error Codes

| Code | Meaning | Action |
|------|---------|--------|
| `CONFIG_NOT_FOUND` | No .rr.yaml | Run `rr init` |
| `CONFIG_INVALID` | Schema error | Fix config syntax |
| `HOST_NOT_FOUND` | Unknown host name | Check `rr host list` |
| `SSH_TIMEOUT` | Connection timed out | Check network/VPN |
| `SSH_AUTH_FAILED` | Key rejected | Run `rr setup <host>` |
| `SSH_HOST_KEY` | Host key mismatch | Verify fingerprint |
| `LOCK_HELD` | Another process has lock | Run `rr unlock` |
| `COMMAND_FAILED` | Remote command failed | Check command output |

### Non-Interactive Commands

For CI/automation, use flag-based commands instead of interactive prompts:

```bash
# Add host without prompts
rr host add --name dev-box \
  --ssh "dev.local,dev-tailscale" \
  --dir '~/projects/${PROJECT}' \
  --tag fast --skip-probe

# Initialize project without prompts
rr init --non-interactive --host dev-box
```

### Troubleshooting Decision Tree (LLM)

```
1. Run: rr doctor --machine
2. Parse: response.success
   - true  -> Setup OK
   - false -> Check response.error.code

3. Based on error.code:

   CONFIG_NOT_FOUND:
     -> Run: rr init --non-interactive --host <host>

   SSH_TIMEOUT:
     -> Check network: ping <hostname>
     -> Try alternate SSH alias

   SSH_AUTH_FAILED:
     -> Run: rr setup <hostname>
     -> Or: ssh-copy-id <hostname>

   SSH_HOST_KEY:
     -> Inform user about host key verification
     -> Run: ssh -o StrictHostKeyChecking=accept-new <hostname> exit

   LOCK_HELD:
     -> Run: rr unlock
     -> Retry original command
```

### When to Use rr vs Local Execution

```
IF .rr.yaml exists AND rr status --machine shows healthy hosts:
  -> Use rr for tests, builds, remote commands

IF no .rr.yaml OR all hosts unhealthy:
  -> Check if local_fallback is enabled in config
  -> If yes: rr will run locally automatically
  -> If no: run commands locally with Bash tool
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
