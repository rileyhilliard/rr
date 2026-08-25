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
rr provision           # Install missing tools on hosts
rr doctor              # Diagnose issues
rr monitor             # TUI dashboard for host metrics
```

## Two-Config System

rr uses two config files:

| Config | Location | Purpose |
|--------|----------|---------|
| Global | `~/.rr/config.yaml` | Personal host definitions (SSH, directories) |
| Project | `.rr.yaml` | Shareable project settings (tasks, sync rules) |

**See [config.md](reference/config.md) for complete config reference.**

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
| `rr provision` | Install missing tools on hosts |
| `rr doctor` | Diagnose issues |
| `rr host list/add/remove` | Manage hosts |

**See [commands.md](reference/commands.md) for full command reference.**

### Common Flags

- `--pretty` / `-p` - Opt into human-readable output (spinners, colors). Default is structured JSON.
- `--host <name>` - Target specific host
- `--tag <tag>` - Select host by tag
- `--local` - Force local execution
- `--skip-requirements` - Skip requirement checks

Run `rr --help` or `rr <command> --help` for complete flag reference.

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

**See [tasks.md](reference/tasks.md) for parallel tasks, multi-step tasks, and advanced configuration.**

## Task Types and Arguments

Tasks come in three types. The type determines whether extra args work:

| Type | Config field | Accepts args? | Example |
|------|-------------|---------------|---------|
| Single-command | `run:` | YES | `rr test -k "test_foo"` |
| Multi-step | `steps:` | NO (errors) | `rr deploy` |
| Parallel | `parallel:` | NO unless `forward_args: true` | `rr test-all` |

**When you need custom args on a parallel task, bypass it:**
```bash
# Preferred: use --cwd for subdirectory execution (path-traversal safe)
rr run --cwd backend "uv run pytest tests/bond/ -v"

# Fallback: manual cd pattern (avoid — quoting errors are common)
rr run "cd backend && uv run pytest tests/bond/ -v"
```

To check which type a task is: `rr <task> --help`

To forward args to all subtasks in a parallel task, set `forward_args: true` in the task config:
```yaml
tasks:
  test-backend:
    parallel: [test-backend-api, test-backend-services]
    forward_args: true   # rr test-backend -k bond works
```

## Choosing the Right Command

```
Need to run something remotely?
├── Named task exists? → rr <task>
│   ├── Single-command task → rr <task> <args>   (args forwarded)
│   ├── Parallel task (forward_args: true) → rr <task> <args>
│   └── Parallel task (default) → rr run "cd <dir> && <cmd> <args>"
├── No task, files may have changed → rr run "<command>"   (syncs first)
└── Files already synced → rr exec "<command>"   (faster, skips sync)
```

**Never nest rr inside rr exec** — rr may not be installed on the remote:
```bash
# Wrong:  rr exec "rr sync && pytest"
# Right:  rr run "pytest"
```

## Reading rr Output

rr sends phase events (connect, sync, exec) to stderr and command output to stdout.

```bash
rr test-opendata 2>/dev/null         # suppress all phase events
rr test-opendata --no-phases         # suppress intermediate events, keep final result JSON
rr test-opendata 2>&1                # capture everything (mixes phase JSON into output stream)
```

The final result is always a JSON line on stderr with `"type":"result"` containing exit code, host, and duration.

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

Run with: `rr test-all`, `rr quick-check`

#### Setup Phase (Once Per Host)

Avoid redundant setup work (dependency sync, migrations) when multiple subtasks run on the same host:

```yaml
tasks:
  test-all:
    setup: pip install -r requirements.txt   # Runs once per host
    parallel:
      - test-unit
      - test-integration
      - test-e2e
```

Setup runs exactly once per host before any subtasks execute. If a host runs 3 subtasks, setup runs once (not 3 times).

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

### Task Dependencies

Define task execution order with `depends`. Tasks run their dependencies first, then execute their own command:

```yaml
tasks:
  lint:
    run: golangci-lint run
  test:
    run: go test ./...
  build:
    run: go build ./...

  # Linear chain: lint -> test -> build
  ci:
    description: Full CI pipeline
    depends:
      - lint
      - test
      - build
```

Run with: `rr ci`

#### Parallel Groups in Dependencies

Run multiple dependencies simultaneously:

```yaml
tasks:
  lint:
    run: golangci-lint run
  typecheck:
    run: mypy .
  test:
    run: pytest

  ci:
    depends:
      - parallel: [lint, typecheck]  # Run simultaneously
      - test                          # Run after parallel completes
```

Executes: `[lint, typecheck]` (parallel) -> `test`

#### Orchestrator Tasks

Tasks with only `depends` orchestrate without running their own command:

```yaml
tasks:
  lint:
    run: golangci-lint run
  test:
    run: go test ./...

  verify:
    description: Run all checks
    depends: [lint, test]
    # No 'run' - just orchestrates
```

#### Dependency Flags

| Flag | Purpose |
|------|---------|
| `--skip-deps` | Skip dependencies, run only the target task |
| `--from <task>` | Start from a specific task in the chain |

```bash
rr ci                  # Full dependency chain
rr ci --skip-deps      # Only run ci task itself
rr ci --from test      # Start from test, skip lint
```

#### Dependency Features

- **Deduplication**: Tasks run once even if referenced multiple times (diamond deps)
- **Validation**: Circular dependencies detected at config load
- **Fail-fast**: Stops on first failure when `fail_fast: true`
- **Timeout**: Honor `timeout` field for entire dependency chain

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

Missing tools trigger actionable error messages. Tools with built-in installers (40+) can be auto-installed.

**See [requirements.md](reference/requirements.md) for complete requirements reference.**

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
| "handshake failed" but ssh works | Key not in agent: `ssh-add ~/.ssh/id_rsa`, add `AddKeysToAgent yes` to SSH config |
| "command not found" | Add `setup_commands` or check `require` config |
| Sync slow | Add large dirs to `sync.exclude` |
| Lock stuck | `rr unlock` |
| Lock timeout after crash or cancel | `rr unlock --all` — stale locks from sessions that didn't exit cleanly |

## Self-Documentation

When you're unsure about a task's type, subtasks, or flags, run `rr <task> --help` before executing it. The help output shows whether the task is parallel (and lists subtasks), what flags it accepts, and whether it takes extra arguments. This is faster than reading `.rr.yaml` and avoids the silent-arg-drop problem.

**See [troubleshooting.md](reference/troubleshooting.md) for detailed diagnostics.**

## Structured Output (Default)

rr defaults to structured output (agent-first). No flags needed. Phase events are emitted as JSON lines to stderr, command stdout/stderr passes through undecorated.

```bash
# Default behavior - structured JSON events on stderr, raw output on stdout
rr run "make test"
rr test

# Opt into human-readable spinners/colors
rr run --pretty "make test"
```

The `--machine` / `-m` flag still works but is a no-op (structured is already the default).

**See [machine-interface.md](reference/machine-interface.md) for JSON event format and error codes.**

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
| Install missing tools on hosts | `rr provision` |
| Debug connection issues | `rr doctor` |
| Watch resource usage | `rr monitor` |
| First time setup | `rr init` |
| Add new machine | `rr host add` |

## Reference Files

- **[config.md](reference/config.md)** - Complete config reference (global + project)
- **[commands.md](reference/commands.md)** - All commands and flags
- **[tasks.md](reference/tasks.md)** - Task definitions, parallel execution, multi-step
- **[requirements.md](reference/requirements.md)** - Remote environment bootstrap
- **[machine-interface.md](reference/machine-interface.md)** - JSON output and error codes
- **[troubleshooting.md](reference/troubleshooting.md)** - Diagnostics and common fixes
