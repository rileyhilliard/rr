# Road Runner CLI: Architecture Plan

## TL;DR

**Road Runner** (`rr`) is a CLI tool that syncs local code to remote machines and executes commands, with smart host fallback (LAN → VPN → local), atomic locking, and beautiful output formatting. Built in **Go** for single-binary distribution, zero dependencies, and fast execution. The tool fills a gap between "just rsync && ssh" scripts and heavyweight tools like Ansible - targeting solo developers and small teams with shared build machines, home labs, or Mac Mini clusters.

---

## Problem Statement

### The Pain

Developers working with remote build machines face a fragmented workflow:

1. **Manual host management**: When working from different locations (home LAN vs. coffee shop via Tailscale), you need to remember which SSH alias works. Connection failures waste time.

2. **Repeated rsync incantations**: Complex exclude patterns, preserve rules, and delete flags are buried in shell history or scattered scripts.

3. **Concurrent access conflicts**: Two developers (or the same person in two terminals) running tests simultaneously on a shared machine causes corruption, race conditions, or misleading results.

4. **Lost output context**: When a remote test fails, you get a wall of text. Scrolling to find the actual failure is tedious. Copying paths back to your local editor requires mental translation.

5. **Tool overkill**: Ansible requires inventory files, YAML playbooks, and Python dependencies. Fabric requires Python and custom code. For "sync my code, run pytest," this is excessive.

### Who This Solves For

**Primary persona: Solo developer with a home lab**

- Has a Mac Mini or Linux box for running tests, ML training, or builds
- Works from multiple locations (home LAN, remote via Tailscale/WireGuard)
- Wants "it just works" without thinking about which host to use
- Values beautiful terminal output and fast iteration

**Secondary persona: Small team sharing a build server**

- 2-5 developers sharing a powerful machine for CI-like tasks
- Need to avoid stepping on each other's toes
- Don't want to set up a full CI system for internal dev workflows

**Non-goals (who this isn't for):**

- Large teams needing fleet management → use Ansible
- Container-native workflows → use DevPod or Tilt
- Continuous deployment → use proper CI/CD

### Why Now

The rise of Tailscale and similar mesh VPNs has made remote development practical without complex networking. Developers increasingly have "build boxes" that are sometimes local, sometimes remote. Existing tools don't handle this gracefully.

---

## Product Requirements

### Core Functionality

| Feature                   | Priority | Description                                              |
| ------------------------- | -------- | -------------------------------------------------------- |
| Smart host selection      | P0       | Try hosts in order (LAN → VPN → local fallback)          |
| File sync                 | P0       | rsync with configurable excludes and preserves           |
| Command execution         | P0       | Run arbitrary commands on selected host                  |
| Atomic locking            | P0       | Prevent concurrent runs with timeout and stale detection |
| Configuration file        | P0       | YAML config for hosts, sync rules, and tasks             |
| Streaming output          | P0       | Real-time stdout/stderr with proper TTY handling         |
| Exit code propagation     | P0       | Correct exit codes for scripting/CI integration          |
| SSH key setup helper      | P0       | Guided setup for SSH key authentication                  |
| Task definitions          | P1       | Named tasks with pre-configured commands                 |
| Output formatters         | P1       | Pluggable formatters (generic, pytest, jest, go test)    |
| Shell completions         | P1       | Bash, zsh, fish completions                              |
| Multi-host load balancing | P2       | Round-robin or least-recently-used across host pool      |
| Parallel task execution   | P2       | Run multiple tasks concurrently on different hosts       |

---

## Developer Experience Design

### Design Principles

1. **Zero to working in 60 seconds**: First successful run should happen within a minute of install
2. **Progressive disclosure**: Simple things simple, complex things possible
3. **Errors that teach**: Every error message explains what went wrong AND how to fix it
4. **Minimal surprise**: Behave like tools developers already know
5. **Quiet success, loud failure**: Don't spam on success; be helpful on failure

### Command Structure

After reviewing several naming approaches, here's the final CLI design:

```
rr <command> [options] [arguments]

Primary Commands:
  run <cmd>         Sync files and execute a command on remote
  exec <cmd>        Execute command without syncing (when you just ran sync)
  sync              Sync files only, no command execution
  <task>            Run a named task (if defined in config)

Setup & Status:
  init              Create starter config with guided prompts
  setup <host>      Configure SSH keys for a host
  status            Show connectivity and selected host
  monitor           Real-time dashboard of host metrics
  doctor            Diagnose common issues

Help:
  help [command]    Show help for a command
  version           Show version info
```

**Key design decisions:**

1. **Tasks are first-class citizens**: If you define a task called `test`, you run it with `rr test`, not `rr task test`. This matches `make`, `npm run`, and muscle memory.

2. **`run` vs `exec`**: `run` always syncs first (the common case). `exec` skips sync for when you're iterating quickly and just changed one file you already synced.

3. **`setup` is prominent**: New users will need this. It's not hidden under `rr config ssh-setup`.

4. **`doctor` for debugging**: When things don't work, `rr doctor` checks everything and reports issues.

### Command Naming Rationale

| Considered         | Chosen      | Why                                         |
| ------------------ | ----------- | ------------------------------------------- |
| `rr task test`     | `rr test`   | Shorter, matches make/npm patterns          |
| `rr run --no-sync` | `rr exec`   | Common enough to deserve its own command    |
| `rr hosts`         | `rr status` | Status shows hosts AND current selection    |
| `rr ssh-setup`     | `rr setup`  | More discoverable, allows future expansion  |
| `rr check`         | `rr doctor` | "Doctor" implies diagnosis and prescription |

### Reserved Command Names

These names cannot be used as task names (with helpful error if attempted):

```
run, exec, sync, init, setup, status, monitor, doctor, help, version, completion, update, host
```

If a user has a task named `run`, we error during config load:

```
Error: Task name 'run' conflicts with built-in command.
  Rename your task or use: rr run --task run
```

---

## Terminal Output States

### State Indicators

Every operation goes through clear phases with consistent visual language:

```
PHASE INDICATORS:
  ○  Pending (not started)
  ◐  In progress (animated spinner)
  ●  Complete (success)
  ✗  Failed
  ⊘  Skipped

COLORS:
  Blue     → In progress, informational
  Green    → Success
  Yellow   → Warning, skipped
  Red      → Error, failure
  Dim/Gray → Secondary info, timing
```

### Example: Successful Run

```bash
$ rr test

◐ Connecting...
● Connected to mini via mini-local                          0.1s

◐ Syncing 234 files...
● Synced                                                    1.2s

◐ Acquiring lock...
● Lock acquired                                             0.0s

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

$ pytest -n auto
======================== test session starts =========================
...
======================== 47 passed in 3.21s ===========================

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

● Done                                                      4.8s
```

### Example: Connection Fallback

```bash
$ rr test

◐ Connecting...
  ○ mini-local                                         timeout (2s)
  ● mini (tailscale)                                        0.3s
● Connected to mini via mini (tailscale)                    2.3s

◐ Syncing...
...
```

### Example: Failed Command with Pytest Formatter

```bash
$ rr test

● Connected to mini via mini-local                          0.1s
● Synced (47 files)                                         0.8s
● Lock acquired                                             0.0s

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

$ pytest -n auto
...
FAILED tests/test_auth.py::test_login_expired - AssertionError
FAILED tests/test_users.py::test_duplicate - IntegrityError
...
======================== 2 failed, 45 passed in 3.21s =================

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✗ 2 tests failed                                            4.1s

  tests/test_auth.py:42
    test_login_expired
    AssertionError: Expected 401, got 200

  tests/test_users.py:108
    test_duplicate
    IntegrityError: duplicate key value violates unique constraint

```

### Example: Lock Contention

```bash
$ rr test

● Connected to mini via mini-local                          0.1s
● Synced (12 files changed)                                 0.3s

◐ Waiting for lock...
  Held by: alice@macbook since 2m ago

◐ Waiting for lock... (30s)
● Lock acquired                                            34.2s
...
```

### Example: SSH Setup

```bash
$ rr setup mini

Configuring SSH access for host: mini

◐ Checking for SSH keys...
● Found key: ~/.ssh/id_ed25519.pub

◐ Testing mini-local...
✗ mini-local: Permission denied (publickey)

  Your SSH key isn't authorized on this host.

? Copy your public key to mini-local? [Y/n] y

  Enter password for dev@192.168.1.50: ********

● Key copied to mini-local

◐ Testing mini-local...
● mini-local: Connected (12ms)

◐ Testing mini (fallback)...
● mini: Connected (45ms)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ Host 'mini' is ready

  Both connection methods work:
    mini-local  192.168.1.50    12ms  (LAN)
    mini        100.64.0.5      45ms  (Tailscale)

  Added to config: .rr.yaml
```

### Example: Doctor Output

```bash
$ rr doctor

Road Runner Diagnostic Report

CONFIG
  ● Config file: .rr.yaml
  ● Schema valid
  ● 1 host configured, 2 tasks defined

SSH
  ● SSH key found: ~/.ssh/id_ed25519.pub
  ● SSH agent running with 2 keys loaded

HOSTS
  ● mini
    ● mini-local: Connected (11ms)
    ● mini: Connected (52ms)

DEPENDENCIES
  ● rsync 3.2.7 (local)
  ● rsync 3.2.3 (mini)

REMOTE
  ● Working directory exists: ~/projects/myapp
  ● Write permission: OK
  ● No stale locks found

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ Everything looks good
```

### Example: Doctor with Issues

```bash
$ rr doctor

Road Runner Diagnostic Report

CONFIG
  ● Config file: .rr.yaml
  ✗ Schema error at tasks.test.on_failure
    Invalid value "skip". Expected: continue, stop

    Fix: Change to one of the valid values

SSH
  ● SSH key found: ~/.ssh/id_ed25519.pub
  ✗ SSH agent not running

    Fix: eval $(ssh-agent) && ssh-add

HOSTS
  ✗ mini
    ✗ mini-local: Connection refused
    ● mini: Connected (52ms)

    mini-local may be offline or firewalled

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✗ 3 issues found

  Run with --fix to attempt automatic fixes where possible.
```

---

## Error Message Design

Every error follows this structure:

```
✗ <What failed>

  <Why it failed — technical details>

  <How to fix it — actionable steps>
```

### Error Examples

**Host unreachable:**

```
✗ Cannot connect to any configured hosts

  mini-local: Connection timed out after 2s
  mini: Connection refused

  Possible causes:
    • Host is offline or sleeping
    • Firewall blocking SSH (port 22)
    • Tailscale not connected

  Try:
    rr doctor          Check full diagnostic
    rr status          See host configuration
```

**Lock held:**

```
✗ Lock acquisition timed out after 5m

  Lock held by: alice@macbook
  Lock age: 12m (exceeds stale threshold of 10m)

  The lock appears stale. The holder may have crashed.

  To force release:
    rr run --force-unlock "your command"

  To wait longer:
    rr run --lock-timeout=15m "your command"
```

**Config error:**

```
✗ Invalid configuration in .rr.yaml

  Line 15: Unknown field 'host' in task definition

    14 │ tasks:
    15 │   test:
    16 │     host: mini          ← Did you mean 'hosts'?
    17 │     run: pytest

  Fix: Rename 'host' to 'hosts' (plural, takes a list)
```

**rsync not found:**

```
✗ rsync not found on remote host

  Host: mini (mini-local)
  PATH searched: /usr/bin:/usr/local/bin:/opt/homebrew/bin

  Install rsync on the remote:
    macOS:  brew install rsync
    Ubuntu: sudo apt install rsync
    Fedora: sudo dnf install rsync
```

---

## Configuration Schema

### File Location

`rr` uses two configuration files:

**Global config** (`~/.rr/config.yaml`):
- Contains host definitions (SSH connections, remote directories, tags, env vars)
- Personal settings not shared with team
- Created with `rr host add` or manually

**Project config** (`.rr.yaml`):
- Contains sync rules, tasks, output settings
- Shareable with team via version control
- Created with `rr init`

Project config is loaded from (first match wins):

1. `--config` flag
2. `.rr.yaml` in current directory
3. `.rr.yaml` in parent directories (stops at git root or home)

**Design decision**: Use `.rr.yaml` not `.road-runner.yaml`. It's shorter, matches the command name, and follows the pattern of `.npmrc`, `.nvmrc`, etc.

### Complete Schema

**Global config** (`~/.rr/config.yaml`):

```yaml
# ~/.rr/config.yaml
# Personal host definitions

version: 1

# ─────────────────────────────────────────────────────────────────────────────
# HOSTS
# Define remote machines and their connection methods
# ─────────────────────────────────────────────────────────────────────────────

hosts:
  # Host name (used in commands: rr run --host=mini)
  mini:
    # SSH connection strings, tried in order until one succeeds
    # Can be: hostname, user@hostname, or SSH config alias
    ssh:
      - mini-local # Try first (LAN, usually faster)
      - mini # Fallback (Tailscale/VPN)
      - dev@192.168.1.50 # Explicit user@host also works

    # Working directory on remote (where files sync to)
    # Supports variable expansion: ${PROJECT}, ${USER}
    dir: ~/projects/${PROJECT}

    # Optional tags for filtering (used with --tag flag)
    tags: [macos, arm64, fast]

  gpu-box:
    ssh:
      - gpu.local
      - gpu-tailscale
    dir: /home/dev/projects/${PROJECT}
    tags: [linux, gpu, cuda]

    # Optional: environment variables for this host
    env:
      CUDA_VISIBLE_DEVICES: "0,1"

# ─────────────────────────────────────────────────────────────────────────────
# DEFAULTS
# Personal default settings
# ─────────────────────────────────────────────────────────────────────────────

defaults:
  # Which host to use by default (if not specified)
  host: mini

  # Fall back to local execution if all remotes fail
  # Useful for CI environments or when traveling
  local_fallback: false

  # SSH probe timeout
  probe_timeout: 2s
```

**Project config** (`.rr.yaml`):

```yaml
# .rr.yaml
# Road Runner project configuration
# Docs: https://github.com/yourorg/rr#configuration

# Schema version (for future migrations)
version: 1

# ─────────────────────────────────────────────────────────────────────────────
# HOST REFERENCES
# Reference hosts defined in ~/.rr/config.yaml
# ─────────────────────────────────────────────────────────────────────────────

# List of hosts this project can use for load balancing
# If omitted, all global hosts are available
hosts:
  - mini
  - gpu-box

# Or use a single host (mutually exclusive with hosts:)
# host: mini

# ─────────────────────────────────────────────────────────────────────────────
# SYNC
# Configure file synchronization behavior
# ─────────────────────────────────────────────────────────────────────────────

sync:
  # Patterns to exclude from sync (not sent to remote)
  # Uses rsync pattern syntax
  exclude:
    - .git/
    - .venv/
    - __pycache__/
    - "*.pyc"
    - node_modules/
    - .mypy_cache/
    - .pytest_cache/
    - .ruff_cache/
    - .DS_Store
    - "*.log"

  # Patterns to preserve on remote (not deleted even if missing locally)
  # Useful for: virtual environments, downloaded data, build caches
  preserve:
    - .venv/
    - node_modules/
    - data/
    - .cache/

  # Extra rsync flags (optional)
  # Common additions: --compress, --info=progress2
  flags: []

# ─────────────────────────────────────────────────────────────────────────────
# LOCK
# Prevent concurrent executions on shared hosts
# ─────────────────────────────────────────────────────────────────────────────

lock:
  # Enable/disable locking (default: true)
  enabled: true

  # How long to wait for a lock before giving up
  timeout: 5m

  # Consider a lock stale after this duration (holder probably crashed)
  stale: 10m

# ─────────────────────────────────────────────────────────────────────────────
# TASKS
# Named command sequences (like Makefile targets)
# Run with: rr <taskname>
# ─────────────────────────────────────────────────────────────────────────────

tasks:
  # Simple task: single command
  build:
    run: make build

  # Task with description (shown in rr --help)
  test:
    description: Run all tests
    run: pytest -n auto

  # Task restricted to specific hosts
  train:
    description: Run ML training job
    hosts: [gpu-box] # Only runs on hosts with GPU
    run: python train.py
    env:
      WANDB_MODE: offline

  # Multi-step task
  ci:
    description: Full CI pipeline
    steps:
      - name: lint
        run: ruff check .

      - name: typecheck
        run: mypy .

      - name: test
        run: pytest -n auto
        on_fail: continue # Keep going even if tests fail

      - name: build
        run: make build

# ─────────────────────────────────────────────────────────────────────────────
# OUTPUT
# Configure terminal output formatting
# ─────────────────────────────────────────────────────────────────────────────

output:
  # Color mode: auto, always, never
  # "auto" disables color when output is piped
  color: auto

  # Output formatter for command output
  # auto: detect from command (pytest, jest, go test, etc.)
  # generic: simple pass-through with error highlighting
  # pytest, jest, go, cargo: tool-specific formatters
  format: auto

  # Show timing for each phase
  timing: true

  # Verbosity: quiet, normal, verbose
  verbosity: normal
```

### Schema Design Decisions

| Decision                   | Rationale                                        |
| -------------------------- | ------------------------------------------------ |
| `exclude` not `excludes`   | Matches rsync terminology, reads naturally       |
| `preserve` not `preserves` | Consistent with `exclude`                        |
| `dir` not `workdir`        | Shorter, still clear                             |
| `steps` not `commands`     | Implies ordered sequence, matches CI terminology |
| `on_fail` not `on_failure` | Shorter, common in CI configs                    |
| `stale` not `stale_after`  | Context makes it clear                           |

### Minimal Config

For the simplest case, you need at least one host in your global config:

```yaml
# ~/.rr/config.yaml
hosts:
  mini:
    ssh: [mini-local, mini]
    dir: ~/projects/${PROJECT}
```

A project config (`.rr.yaml`) is optional. Everything else has sensible defaults. This enables:

```bash
rr run "pytest"
rr run "make build"
```

### Zero-Config Mode

If no global hosts are configured and user runs `rr run`, we offer to create one:

```bash
$ rr run "pytest"

No configuration found. Let's set one up.

? SSH host or alias to use: mini-local
? Add a fallback host? (e.g., Tailscale): mini

Testing connections...
  ● mini-local: Connected (12ms)
  ● mini: Connected (48ms)

Created .rr.yaml with host 'mini'

Proceeding with: rr run "pytest"
...
```

---

## Technical Architecture

### Language Choice: Go

**Decision**: Build in Go, not TypeScript or Python.

**Rationale:**

| Factor                     | Go                         | TypeScript                | Python                  |
| -------------------------- | -------------------------- | ------------------------- | ----------------------- |
| Single binary distribution | ✅ Yes                     | ❌ Needs Node.js          | ❌ Needs Python         |
| Cross-compilation          | ✅ Trivial                 | ⚠️ Possible but complex   | ❌ Difficult            |
| Startup time               | ✅ ~10ms                   | ❌ ~200-500ms             | ❌ ~100-300ms           |
| SSH libraries              | ✅ golang.org/x/crypto/ssh | ⚠️ ssh2 (native bindings) | ✅ Paramiko             |
| CLI frameworks             | ✅ Cobra (excellent)       | ✅ Commander/yargs        | ✅ Click/Typer          |
| Concurrency                | ✅ Goroutines              | ⚠️ Event loop limits      | ⚠️ Threading is awkward |

The killer feature is **single binary distribution**. Users run `brew install rr` or download a binary—no runtime dependencies. This matches the "just works" philosophy.

Go's SSH library means we don't shell out to `ssh`, giving us better error handling, connection pooling, and cross-platform consistency.

### System Architecture

```mermaid
flowchart TB
    subgraph cli["CLI Layer"]
        cmd[Command Parser<br/>Cobra]
        cfg[Config Loader<br/>Viper]
        comp[Completions<br/>bash/zsh/fish]
    end

    subgraph core["Core Engine"]
        host[Host Selector]
        sync[Sync Engine]
        exec[Command Executor]
        lock[Lock Manager]
    end

    subgraph output["Output Layer"]
        stream[Stream Handler]
        fmt[Formatters]
        tui[TUI Components<br/>Bubble Tea]
    end

    subgraph transport["Transport Layer"]
        ssh[SSH Client<br/>golang.org/x/crypto]
        local[Local Executor]
    end

    subgraph setup["Setup & Diagnostics"]
        keys[SSH Key Manager]
        doctor[Doctor Checks]
    end

    cmd --> host
    cfg --> cmd
    comp --> cmd
    host --> sync
    host --> exec
    exec --> lock
    sync --> ssh
    exec --> ssh
    exec --> local
    ssh --> stream
    local --> stream
    stream --> fmt
    fmt --> tui
    keys --> ssh
    doctor --> ssh
    doctor --> cfg

    style cli fill:#dbeafe,stroke:#3b82f6,stroke-width:2px
    style core fill:#dcfce7,stroke:#10b981,stroke-width:2px
    style output fill:#fef3c7,stroke:#f59e0b,stroke-width:2px
    style transport fill:#fce7f3,stroke:#ec4899,stroke-width:2px
    style setup fill:#f3e8ff,stroke:#a855f7,stroke-width:2px
```

### Component Responsibilities

**CLI Layer**

- Parse commands and flags using Cobra
- Load and merge configuration (file → env → flags) using Viper
- Generate shell completions for task names
- Validate inputs before passing to core

**Core Engine**

- **Host Selector**: Probe hosts in order, cache connectivity results, handle fallback
- **Sync Engine**: Build rsync command with configured excludes/preserves, show progress
- **Command Executor**: Execute commands via SSH or locally, handle streaming output
- **Lock Manager**: Acquire/release locks on remote, detect stale locks, handle timeout

**Output Layer**

- **Stream Handler**: Multiplex stdout/stderr, handle ANSI codes, buffer lines
- **Formatters**: Parse output for known tools (pytest, jest), extract failures
- **TUI Components**: Progress indicators, spinners, colored output using Bubble Tea

**Transport Layer**

- **SSH Client**: Connection pooling, keep-alive, exec/shell modes
- **Local Executor**: os/exec wrapper for local fallback

**Setup & Diagnostics**

- **SSH Key Manager**: Check for keys, generate if needed, run ssh-copy-id
- **Doctor Checks**: Validate config, test connectivity, check dependencies

### Package Dependencies

This diagram shows the simplified package dependency graph with key relationships:

```mermaid
flowchart TB
    subgraph entry["Entry Point"]
        cmd[cmd/rr]
    end

    subgraph cli_layer["CLI Layer"]
        cli[internal/cli]
    end

    subgraph core["Core Packages"]
        host[host]
        sync[sync]
        lock[lock]
        exec[exec]
        config[config]
    end

    subgraph infra["Infrastructure"]
        sshutil[pkg/sshutil]
    end

    cmd --> cli

    cli --> host
    cli --> sync
    cli --> lock
    cli --> exec
    cli --> config

    host --> sshutil
    sync --> host
    lock --> host

    style entry fill:#1e3a8a,stroke:#60a5fa,stroke-width:2px,color:#dbeafe
    style cli_layer fill:#14532d,stroke:#34d399,stroke-width:2px,color:#dcfce7
    style core fill:#78350f,stroke:#fbbf24,stroke-width:2px,color:#fef3c7
    style infra fill:#831843,stroke:#f472b6,stroke-width:2px,color:#fce7f3

    style cmd fill:#1e3a8a,stroke:#60a5fa,stroke-width:2px,color:#dbeafe
    style cli fill:#14532d,stroke:#34d399,stroke-width:2px,color:#dcfce7
    style host fill:#78350f,stroke:#fbbf24,stroke-width:2px,color:#fef3c7
    style sync fill:#78350f,stroke:#fbbf24,stroke-width:2px,color:#fef3c7
    style lock fill:#78350f,stroke:#fbbf24,stroke-width:2px,color:#fef3c7
    style exec fill:#78350f,stroke:#fbbf24,stroke-width:2px,color:#fef3c7
    style config fill:#78350f,stroke:#fbbf24,stroke-width:2px,color:#fef3c7
    style sshutil fill:#831843,stroke:#f472b6,stroke-width:2px,color:#fce7f3
```

**Key dependencies:**
- `cmd/rr` is the entry point, calls `internal/cli`
- `cli` orchestrates `host`, `sync`, `lock`, `exec`, and `config`
- `host` uses `pkg/sshutil` for SSH operations
- `sync` and `lock` both depend on `host` for connection management

### The `rr run` Command Flow

This sequence diagram shows what happens when you run `rr run "make test"`:

```mermaid
sequenceDiagram
    participant User
    participant CLI as cli/run.go
    participant Config as config
    participant Host as host/selector
    participant Sync as sync
    participant Lock as lock
    participant Exec as exec

    User->>CLI: rr run "make test"

    rect rgb(30, 58, 138)
        Note over CLI,Config: Phase 1: Load Config
        CLI->>Config: Find() + Load()
        Config-->>CLI: config
    end

    rect rgb(20, 83, 45)
        Note over CLI,Host: Phase 2: Select Host + Connect
        CLI->>Host: Select(preferredHost)
        loop Try each SSH alias
            Host->>Host: ProbeAndConnect(alias)
            alt Success
                Note over Host: Return connection
            else Failure
                Note over Host: Try next alias
            end
        end
        Host-->>CLI: Connection
    end

    rect rgb(120, 53, 15)
        Note over CLI,Sync: Phase 3: Sync Files
        CLI->>Sync: Sync(conn, workDir, excludes)
        Sync-->>CLI: files synced
    end

    rect rgb(88, 28, 135)
        Note over CLI,Lock: Phase 4: Acquire Lock
        CLI->>Lock: Acquire(conn, projectHash)
        alt Lock acquired
            Lock-->>CLI: Lock handle
        else Lock held by other
            Lock->>Lock: Wait and retry
        end
    end

    rect rgb(131, 24, 67)
        Note over CLI,Exec: Phase 5: Execute Command
        CLI->>Exec: ExecStream(command)
        Exec-->>User: streaming output
        Exec-->>CLI: exit code
    end

    CLI->>Lock: Release()
    CLI->>User: exit code + summary
```

**Phase summary:**
1. **Load Config** - Find and parse `.rr.yaml`
2. **Select Host** - Probe SSH aliases in order, connect to first available
3. **Sync Files** - rsync local files to remote working directory
4. **Acquire Lock** - Prevent concurrent execution on shared hosts
5. **Execute** - Run command, stream output, capture exit code
6. **Cleanup** - Release lock, return result

### Host Selection Flow

The host selector implements a probe-and-select pattern with ordered fallback:

```mermaid
flowchart TB
    start([Select Host]) --> load[Load host config]
    load --> first_alias[Try first SSH alias]

    first_alias --> probe{Probe SSH<br/>timeout 2s}

    probe -->|Success| connected[Connected]
    probe -->|Timeout/Error| next{More aliases?}

    next -->|Yes| try_next[Try next alias]
    try_next --> probe

    next -->|No| fallback{Local fallback<br/>enabled?}

    fallback -->|Yes| local[Use local execution]
    fallback -->|No| fail[Error: No hosts available]

    connected --> cache[Cache connection]
    local --> cache
    cache --> done([Return connection])

    fail --> error([Return error])

    style start fill:#1e3a8a,stroke:#60a5fa,stroke-width:2px,color:#dbeafe
    style done fill:#065f46,stroke:#10b981,stroke-width:2px,color:#d1fae5
    style error fill:#7f1d1d,stroke:#ef4444,stroke-width:2px,color:#fee2e2
    style connected fill:#065f46,stroke:#10b981,stroke-width:2px,color:#d1fae5
    style local fill:#78350f,stroke:#f59e0b,stroke-width:2px,color:#fef3c7
    style fail fill:#7f1d1d,stroke:#ef4444,stroke-width:2px,color:#fee2e2
    style probe fill:#374151,stroke:#9ca3af,stroke-width:2px,color:#e5e7eb
    style next fill:#374151,stroke:#9ca3af,stroke-width:2px,color:#e5e7eb
    style fallback fill:#374151,stroke:#9ca3af,stroke-width:2px,color:#e5e7eb
```

**Selection logic:**
1. Load configured SSH aliases for the host (e.g., `[mini-local, mini-tailscale]`)
2. Probe each alias in order with a 2-second timeout
3. Return first successful connection and cache it
4. If all fail and `local_fallback: true`, execute locally
5. Otherwise, return error with diagnostic info

### Lock Management

Locking prevents concurrent runs on shared remotes. The lock is a directory on the remote host (atomic mkdir) containing metadata about the lock holder.

```mermaid
stateDiagram-v2
    [*] --> CheckLock: Acquire requested

    CheckLock --> CheckStale: Lock exists
    CheckLock --> CreateLock: Lock doesn't exist

    CheckStale --> RemoveStaleLock: Lock age > stale threshold
    CheckStale --> WaitForLock: Lock is fresh

    RemoveStaleLock --> CreateLock: Stale lock removed

    WaitForLock --> CheckLock: Poll interval elapsed
    WaitForLock --> Timeout: timeout exceeded

    CreateLock --> LockAcquired: mkdir succeeds
    CreateLock --> CheckLock: mkdir fails (race)

    LockAcquired --> [*]: Work complete → Release
    Timeout --> [*]: Error returned

    style LockAcquired fill:#dcfce7,stroke:#10b981,stroke-width:2px
    style Timeout fill:#fee2e2,stroke:#ef4444,stroke-width:2px
```

**Lock file structure:**

```
/tmp/rr-<project-hash>.lock/
├── info.json    # {"user": "alice", "host": "macbook", "started": "..."}
└── pid          # PID on remote (for potential kill)
```

### SSH Key Setup Flow

```mermaid
sequenceDiagram
    participant User
    participant CLI
    participant KeyMgr as Key Manager
    participant SSH

    User->>CLI: rr setup mini
    CLI->>KeyMgr: CheckLocalKeys()

    alt No keys found
        KeyMgr->>User: Generate new key? [Y/n]
        User->>KeyMgr: Y
        KeyMgr->>KeyMgr: ssh-keygen -t ed25519
        KeyMgr->>User: Key generated
    end

    loop For each SSH alias in host
        CLI->>SSH: TestConnection(alias)
        alt Auth success
            SSH-->>CLI: Connected
            CLI->>User: ● alias: Connected
        else Auth failure
            SSH-->>CLI: Permission denied
            CLI->>User: Copy key to alias? [Y/n]
            User->>CLI: Y
            CLI->>KeyMgr: CopyKey(alias)
            KeyMgr->>User: Enter password:
            User->>KeyMgr: ********
            KeyMgr->>KeyMgr: ssh-copy-id
            KeyMgr-->>CLI: Key copied
            CLI->>SSH: TestConnection(alias)
            SSH-->>CLI: Connected
        end
    end

    CLI->>User: ✓ Host 'mini' is ready
```

**Security decisions:**

1. **No passwords in config**: SSH keys only. This is a security requirement, not a convenience tradeoff.

2. **ssh-copy-id for key copying**: We shell out to ssh-copy-id rather than reimplementing. It handles edge cases (authorized_keys permissions, creating .ssh directory) correctly.

3. **Key generation**: Prefer ed25519, fall back to rsa if ed25519 unavailable (old systems).

---

## Framework and Library Selection

| Component           | Library                                                                                                           | Rationale                                            |
| ------------------- | ----------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| CLI framework       | [Cobra](https://github.com/spf13/cobra)                                                                           | Industry standard, great docs, built-in completions  |
| Config management   | [Viper](https://github.com/spf13/viper)                                                                           | Pairs with Cobra, handles file + env + flags merging |
| SSH                 | [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh)                                             | Official Go SSH implementation                       |
| SSH config parsing  | [kevinburke/ssh_config](https://github.com/kevinburke/ssh_config)                                                 | Parse ~/.ssh/config for user settings                |
| TUI/Styling         | [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss) | Modern, handles terminal edge cases                  |
| Interactive prompts | [Huh](https://github.com/charmbracelet/huh)                                                                       | Part of Charm ecosystem, for setup wizard            |
| YAML                | [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3)                                                           | Standard Go YAML library                             |
| Schema validation   | [gojsonschema](https://github.com/xeipuuv/gojsonschema)                                                           | Validate config with helpful errors                  |
| Testing             | [testify](https://github.com/stretchr/testify)                                                                    | Assertions and mocking                               |

---

## Project Structure

```
rr/
├── cmd/
│   └── rr/
│       └── main.go              # Entry point
├── internal/
│   ├── cli/                     # Cobra command definitions
│   │   ├── root.go
│   │   ├── run.go
│   │   ├── exec.go
│   │   ├── sync.go
│   │   ├── setup.go
│   │   ├── status.go
│   │   ├── monitor.go
│   │   ├── doctor.go
│   │   └── init.go
│   ├── config/                  # Configuration loading
│   │   ├── config.go
│   │   ├── schema.go
│   │   ├── validate.go
│   │   └── expand.go            # Variable expansion
│   ├── host/                    # Host selection
│   │   ├── selector.go
│   │   └── probe.go
│   ├── sync/                    # rsync wrapper
│   │   ├── sync.go
│   │   └── progress.go
│   ├── exec/                    # Command execution
│   │   ├── executor.go
│   │   ├── ssh.go
│   │   └── local.go
│   ├── lock/                    # Lock management
│   │   └── lock.go
│   ├── setup/                   # SSH key setup
│   │   ├── keys.go
│   │   └── copy.go
│   ├── doctor/                  # Diagnostics
│   │   └── checks.go
│   ├── monitor/                 # Host monitoring dashboard
│   │   ├── model.go             # Bubble Tea model and state
│   │   ├── view.go              # List view, header, help overlay
│   │   ├── card.go              # Host card rendering
│   │   ├── detail.go            # Expanded single-host view
│   │   ├── collector.go         # Parallel SSH fetcher + parsers
│   │   ├── command.go           # Batched remote metric commands
│   │   ├── snapshot.go          # One-shot (--once) collection
│   │   ├── pool.go              # Persistent SSH connection pool
│   │   ├── alerts.go            # Threshold alert state machine
│   │   ├── history.go           # Ring buffers for sparklines
│   │   └── graphs.go            # Braille sparkline rendering
│   ├── output/                  # Output formatting
│   │   ├── stream.go
│   │   ├── formatter.go
│   │   ├── state.go             # Progress state management
│   │   └── formatters/
│   │       ├── generic.go
│   │       ├── pytest.go
│   │       ├── jest.go
│   │       └── gotest.go
│   └── ui/                      # TUI components
│       ├── spinner.go
│       ├── progress.go
│       └── prompt.go
├── pkg/                         # Potentially reusable packages
│   └── sshutil/
├── configs/
│   └── schema.json              # JSON Schema for validation
├── completions/                 # Generated shell completions
├── docs/
│   ├── configuration.md
│   ├── formatters.md
│   └── troubleshooting.md
├── .goreleaser.yaml
├── go.mod
└── README.md
```

---

## Output Formatter Architecture

Formatters transform raw command output into structured, readable summaries.

```mermaid
flowchart LR
    subgraph input["Raw Output"]
        stdout[stdout stream]
        stderr[stderr stream]
    end

    subgraph formatter["Formatter Pipeline"]
        detect[Auto-detect<br/>tool type]
        parse[Parse output<br/>line by line]
        extract[Extract<br/>failures/errors]
        summarize[Build<br/>summary]
    end

    subgraph output["Formatted Output"]
        live[Live output<br/>passthrough]
        summary[Failure<br/>summary]
    end

    stdout --> detect
    stderr --> detect
    detect --> parse
    parse --> live
    parse --> extract
    extract --> summarize
    summarize --> summary

    style input fill:#fef3c7,stroke:#f59e0b,stroke-width:2px
    style formatter fill:#dbeafe,stroke:#3b82f6,stroke-width:2px
    style output fill:#dcfce7,stroke:#10b981,stroke-width:2px
```

### Formatter Interface

```go
type Formatter interface {
    // Name returns the formatter identifier
    Name() string

    // Detect returns confidence (0-100) this formatter handles the output
    Detect(command string, initialOutput []byte) int

    // ProcessLine handles a single line of output
    ProcessLine(line string) (display string, data *LineData)

    // Summary generates the final summary after command completes
    Summary(exitCode int) *Summary
}

type LineData struct {
    Type     LineType  // Normal, Error, Failure, Pass, Skip
    TestName string
    FilePath string
    Line     int
    Message  string
}

type Summary struct {
    Passed   int
    Failed   int
    Skipped  int
    Failures []Failure
}
```

### Auto-Detection Logic

```go
func DetectFormatter(command string, output []byte) Formatter {
    // Check command first
    if strings.Contains(command, "pytest") {
        return &PytestFormatter{}
    }
    if strings.Contains(command, "jest") || strings.Contains(command, "vitest") {
        return &JestFormatter{}
    }
    if strings.Contains(command, "go test") {
        return &GoTestFormatter{}
    }

    // Fall back to output detection
    for _, f := range formatters {
        if f.Detect("", output) > 50 {
            return f
        }
    }

    return &GenericFormatter{}
}
```

---

## Distribution Strategy

### Release Artifacts

Each release produces:

- `rr-darwin-amd64` (macOS Intel)
- `rr-darwin-arm64` (macOS Apple Silicon)
- `rr-linux-amd64`
- `rr-linux-arm64`
- `rr-windows-amd64.exe`
- SHA256 checksums
- Homebrew formula
- Shell completions (bash, zsh, fish)

### Installation Methods

```bash
# Homebrew (macOS/Linux) - recommended
brew install yourorg/tap/rr

# Go install
go install github.com/yourorg/rr@latest

# Direct download
curl -sSL https://get.rr.dev | sh

# Manual
curl -LO https://github.com/yourorg/rr/releases/latest/download/rr-$(uname -s)-$(uname -m)
chmod +x rr-* && sudo mv rr-* /usr/local/bin/rr
```

### Shell Completions

Generated automatically and included in releases:

```bash
# After install, add to shell config:
# Bash
echo 'eval "$(rr completion bash)"' >> ~/.bashrc

# Zsh
echo 'eval "$(rr completion zsh)"' >> ~/.zshrc

# Fish
rr completion fish > ~/.config/fish/completions/rr.fish
```

Completions include:

- All commands and subcommands
- Task names from current directory's config
- Host names from config
- Flag values where applicable

---

## Success Metrics

### Adoption Metrics

| Metric            | Target (6 months)          | Measurement                |
| ----------------- | -------------------------- | -------------------------- |
| GitHub stars      | 500+                       | GitHub API                 |
| Weekly downloads  | 200+                       | GitHub releases + Homebrew |
| Active issues/PRs | >10 open, <1 week response | GitHub                     |

### Quality Metrics

| Metric            | Target                     | Measurement     |
| ----------------- | -------------------------- | --------------- |
| Test coverage     | >80%                       | Go coverage     |
| Time to first run | <60 seconds                | User testing    |
| Connection probe  | <500ms for reachable hosts | Instrumentation |
| Sync performance  | Within 10% of raw rsync    | Benchmarks      |

### User Experience Metrics

| Metric                  | Target                  | Measurement     |
| ----------------------- | ----------------------- | --------------- |
| Config creation         | <2 minutes              | User testing    |
| Setup wizard completion | >90% success rate       | Analytics       |
| Error self-resolution   | >80% via error messages | Support tickets |

---

## Host Monitoring (btop-style Dashboard)

### Overview

The `rr monitor` command opens a real-time terminal dashboard showing system metrics across all configured hosts. Think btop/htop, but for your fleet of worker machines. This gives you instant visibility into which hosts are idle, which are under load, and where to send your next job.

### Why This Matters

When you have multiple remote machines (home lab, shared build servers, GPU boxes), choosing where to run a job isn't always obvious:

- Is the GPU box already running someone's training job?
- Which machine has RAM headroom for a memory-hungry test suite?
- Is network throughput bottlenecked on the VPN connection?

Currently you'd SSH into each machine and run htop manually. `rr monitor` surfaces this info in one view.

### Command Interface

```
rr monitor [flags]

FLAGS
      --hosts string      Filter to specific hosts (comma-separated)
      --interval string   Refresh interval (default: 1s)
      --once              Print a single fleet snapshot and exit (no TUI)
      --json              Output the snapshot as JSON (requires --once)
```

**Examples:**

```bash
# Monitor all configured hosts
rr monitor

# Monitor specific hosts
rr monitor --hosts=mini,gpu-box

# Faster refresh for real-time watching
rr monitor --interval=500ms

# One-shot snapshot for scripts and agents
rr monitor --once
rr monitor --once --json
rr monitor --once --json --hosts=gpu-box
```

`--json` without `--once` is rejected: the live dashboard is a TUI with nothing to serialize. The interval floor is 500ms for both the flag and the config value, so a typo can't turn the dashboard into an SSH hammer.

### Collection Architecture

```mermaid
flowchart TB
    subgraph tui["TUI Layer (Bubble Tea)"]
        model["Model<br/>internal/monitor/model.go"]
        view["View<br/>card.go / detail.go / view.go"]
        alerts["Alert tracker<br/>alerts.go"]
    end

    subgraph collect["Collector (internal/monitor)"]
        collector["Collector<br/>collector.go"]
        pool["Pool<br/>pool.go (persistent SSH)"]
        command["Batched command<br/>command.go"]
        history["History ring buffers<br/>history.go"]
    end

    subgraph transport["Transport"]
        dial["host.DialAliases<br/>internal/host/dial.go"]
        ssh["SSH sessions<br/>2 per host per tick"]
    end

    model -->|tick| collector
    collector --> pool
    pool --> dial
    dial --> ssh
    collector --> command
    command --> ssh
    ssh -->|streamed HostResult| model
    model --> history
    model --> alerts
    history --> view
    alerts --> view
```

**Per tick, per host: two SSH sessions on one connection.**

1. **Latency probe** (`echo 1`) measures real network round-trip. Keeping it separate means the reported latency is network time, not collection time.
2. **Batched metrics command** collects everything else in a single exec: CPU, load, memory, network counters, GPU, process list, disk usage, disk I/O counters, CPU temperature, system info, and the rr lock's `info.json`. The lock check is the final section of the same command rather than a separate round trip.

**Connection pool** (`pool.go`): connections stay open between refreshes. When a host has multiple SSH aliases, the pool dials all of them in parallel through `host.DialAliases` and keeps the winner, with a short preference grace period so an earlier-listed alias (LAN) can beat a later one (VPN) that answered first. That dial path is shared with `rr run`/`rr exec` host selection, so failover behaves identically in both.

**Streaming results:** `Collector.CollectStreaming` returns a channel. The model consumes one `hostResultMsg` at a time and re-renders, so a fast host shows up immediately instead of waiting on the slowest host in the fleet.

**Backoff:** after 3 consecutive failures a host enters a 30s backoff and is skipped by the next collection passes. The card shows the countdown. A single success clears it.

**Platform detection** happens once per connection (`uname -s`) and selects the Linux or macOS variant of the batched command.

### Snapshot Mode (`--once`)

`rr monitor --once` is the mode for scripts and agents: no TUI, no alt screen, single exit.

The design problem is that CPU percent, per-core usage, disk I/O and network throughput are all *rates*, computed from the delta between two counter readings. The dashboard gets its second reading for free on the next tick. A one-shot run has no next tick, so a naive snapshot reports zeros.

`BuildSnapshotCommand` solves this without a second round trip: it emits a priming read of the delta sources, sleeps 1s on the remote, then emits exactly the sections `BuildMetricsCommand` produces. The parsers apply unchanged after dropping the prime prefix. Linux primes `/proc/stat`, `/proc/net/dev` and `/proc/diskstats`; macOS only primes `netstat -ib`, since `top -l 1` is not delta-based. Snapshot mode uses the same two-session shape as a tick (latency probe plus the batched command); the remote sleep is what buys the second sample, so the per-host timeout is extended by it.

Output is a human-readable table by default (HOST, STATUS, CPU, RAM, GPU, DISK, LATENCY, LOCK) and a snake_case JSON document with `--json`. The command exits non-zero only when *every* host failed: a partially reachable fleet is still a useful answer.

### Dashboard Layout

The list view stacks host cards in a scrollable viewport with a header (host counts, refresh age, sort order, alert badge) and a footer with key hints. Pressing `Enter` opens the detail view for the selected host: full-size CPU/GPU/RAM/latency/network graphs, a per-core heat strip, a disk section, a process table, and system info.

**Card contents (online host):** host name and connection alias, CPU with braille sparkline and load averages, GPU section when detected, latency sparkline, RAM sparkline, top processes by CPU, network rates, and a DISK line with root filesystem usage.

### Responsive Behavior

Width drives the layout mode:

| Width | Mode | Layout |
|-------|------|--------|
| <80 cols | Minimal | Single column, compact metric lines |
| 80-120 cols | Compact | Single column, single-row inline graphs |
| 120-160 cols | Standard | Full cards, multi-column grid |
| 160+ cols | Wide | Full cards, multi-column grid |

In Standard and Wide, the column count is computed rather than fixed: `width / (minCardWidth + perCardOverhead)`, where `minCardWidth` is 55 and the overhead is 3 (borders plus margin). Columns are added only while every card keeps at least 55 columns of content, and the count is capped at 4 so an ultrawide terminal doesn't degrade into a wall of unreadable slivers.

Height drives detail density:

- `<24` rows: no footer
- `24-39` rows: standard cards, 2-row braille graphs, 1 top process
- `>=40` rows: 4-row braille graphs and the top 3 processes per card

### Color Palette

The dashboard uses the shared Electric Synthwave palette from `internal/ui/colors.go`, with its own severity mapping defined in the monitor theme block there and re-exported by `internal/monitor/styles.go`.

```
Background:     #0A0A0F (deep void)
Surface:        #12121A (card backgrounds)
Border:         #2A2A4A (glass border, purple tint)

Text:
  Primary:      #FFFFFF
  Secondary:    #B4B4D0 (lavender gray)
  Muted:        #6B6B8D (purple-gray)

Metric severity (monitor-specific):
  Healthy:      #00FFFF (neon cyan)
  Warning:      #BF40FF (neon purple)
  Critical:     #FF2E97 (neon pink)

Accents:
  Primary:      #FF2E97 (neon pink, selection)
  Secondary:    #BF40FF (neon purple)
  Graphs:       #00FFFF (neon cyan)
```

The severity ramp is deliberately *not* the CLI's green/amber/red. Success/warning/error semantics belong to command output; the dashboard maps intensity instead, so a hot host reads as hot without implying something is broken.

### Metrics Collected

| Metric | Linux source | macOS source | Notes |
|--------|--------------|--------------|-------|
| CPU % | `/proc/stat` delta | `top -l 1` | Linux is delta-based, so the first sample has no baseline |
| Per-core CPU % | `/proc/stat` per-cpu lines | not collected | Drives the detail-view heat strip |
| CPU temperature | `/sys/class/hwmon/*/temp1_input` | not collected | Shown in the detail CPU header |
| Load average | `/proc/loadavg` | `top -l 1` | 1m/5m/15m |
| CPU cores | `/proc/stat` cpu lines | `sysctl -n hw.ncpu` | |
| RAM used/total | `/proc/meminfo` | `vm_stat` + `sysctl hw.memsize` | |
| GPU | `nvidia-smi --query-gpu=...` | `ioreg -r -c AGXAccelerator` | Absent GPU tooling fails silently; the section is skipped |
| Network throughput | `/proc/net/dev` delta | `netstat -ib` delta | Aggregated across non-loopback interfaces |
| Disk usage | `df -P -k /` | `df -P -k /` | Root filesystem only |
| Disk I/O rates | `/proc/diskstats` delta | not collected | |
| Processes | `ps aux --sort=-%cpu` | `ps aux -r` | Top 16 collected; cards show 1-3, detail shows 10 |
| System info | `/proc/uptime` + `uname -r` | `sysctl kern.boottime` + `uname -r` | Uptime, kernel, OS |
| Lock status | `cat <lockdir>/info.json` | same | Final section of the batched command |

**First-sample handling:** Linux CPU percent has no meaning without a previous `/proc/stat` reading. Rather than report a fake `0.0%`, the collector sets `FirstSample` and the UI renders a dim "warming up" until the second tick. The same flag surfaces in `--once --json` as `cpu.percent_unavailable`.

### Configuration

All monitor settings live in the project config (`.rr.yaml`), not the global host file.

```yaml
monitor:
  # Refresh interval. --interval overrides this. Minimum 500ms.
  interval: 1s

  # Per-host connect + collect timeout.
  timeout: 8s

  # Severity coloring for headers, bars and graphs.
  thresholds:
    cpu:
      warning: 70
      critical: 90
    ram:
      warning: 70
      critical: 90
    gpu:
      warning: 70
      critical: 90

  # Hosts to hide from the dashboard. Still usable for run/sync.
  # A host named explicitly via --hosts wins over this list.
  exclude:
    - staging-server

  # Threshold alerting.
  alerts:
    enabled: false
    bell: true
    flash: true
    cooldown: 60s
    on_alert: ""
```

**Interval precedence:** `--interval` flag > `monitor.interval` > 1s.

**Thresholds** drive both the numeric header colors and the sparkline/bar coloring. Unset values fall back to 70/90. Disk uses a fixed 80/95 pair instead, because `df` capacity sits high in normal operation and would otherwise alarm constantly.

**Exclusion** is applied after `--hosts` filtering, and `--hosts` wins: `rr monitor --hosts=staging-server` shows an excluded host on demand. If exclusion empties the list, the command errors with a pointer at the config.

### Alerting

Alerting is off by default. When `monitor.alerts.enabled` is true, each host+metric pair (CPU, RAM, GPU) runs a small state machine:

1. **Fire** when the value crosses the metric's *critical* threshold.
2. **Hold** while it stays above *warning*. A firing metric does not re-fire.
3. **Re-arm** only once it drops back below *warning*.

The hysteresis matters: a host hovering at exactly the critical line would otherwise fire on every sample. The cooldown adds a second guard, suppressing re-fires for the same host+metric within the window (default 60s) while still marking the metric as firing so the card keeps flashing.

Effects, all individually gated:

| Setting | Effect |
|---------|--------|
| `bell: true` | Writes BEL to stderr, once per batch of alerts |
| `flash: true` | Renders the alerting host's card border in the critical color |
| `on_alert: "<cmd>"` | Runs the command locally via `sh -c` |

The header always shows an alert-count badge while anything is firing, independent of `flash`.

`on_alert` runs on the machine running `rr`, not the remote host, and receives:

| Variable | Value |
|----------|-------|
| `RR_HOST` | Host name that alerted |
| `RR_METRIC` | `cpu`, `ram`, or `gpu` |
| `RR_VALUE` | The metric value, one decimal place |

Hook failures are swallowed on purpose. There is no safe place to print inside the alt screen, and a broken hook must never take down the dashboard. The bell and the hook both run as Bubble Tea commands rather than from `View`, because the framework diffs frames: a BEL embedded in rendered output would be dropped on unchanged frames and repeated on changed ones.

Alert state is cleared when a host goes unreachable, so a stale card stops flashing and recovery fires cleanly.

### Host State Indicators

| State | Display | Meaning |
|-------|---------|---------|
| Online | Filled indicator, host name in accent | Metrics flowing |
| Connecting | Spinner | First connection in flight |
| Unreachable | Dim card, error line, suggestion | Connection or collection failed |
| Backing off | "Reconnecting in Ns..." | 3+ failures, retry scheduled |

### Keyboard Controls

Bindings live in `internal/monitor/keybindings.go`.

| Key | Action |
|-----|--------|
| `q` / `Ctrl+C` | Quit |
| `r` | Force refresh now |
| `s` | Cycle sort order (default, name, CPU, RAM, GPU) |
| `↑` / `←` / `k` / `h` | Select previous host |
| `↓` / `→` / `j` / `l` | Select next host |
| `Home` / `End` | Select first / last host |
| `Enter` | Open detail view for the selected host |
| `Esc` | Back to the list (or close the help overlay) |
| `p` | Cycle the process table sort in detail view (CPU / MEM) |
| `PgUp` / `Ctrl+U` | Scroll up |
| `PgDn` / `Ctrl+D` | Scroll down |
| `?` | Toggle help overlay |

Mouse wheel scrolling works too; the program runs with `tea.WithMouseCellMotion()`.

### Rendering Performance

The dashboard re-renders on every result, so rendering cost is in the hot path.

- **Style caching:** braille graph cell styles are cached by foreground color, along with their pre-rendered ANSI prefix/suffix, so repeated cells skip Lip Gloss's style resolution.
- **Run-length merging:** consecutive graph cells sharing a color are emitted under one ANSI sequence instead of one per cell.
- **Card body caching:** the expensive part of a card (graphs and metric sections) is cached per host and invalidated on new results or a resize. Resize clears the whole cache, since graph rows and process counts depend on terminal height.

### Error Handling

**Host unreachable:** the card shows the error and a suggestion, other hosts keep updating, and the host enters backoff after 3 consecutive failures.

**Collection timeout:** bounded by `monitor.timeout` (default 8s) per host. `--once` extends its context by the snapshot's remote sleep on top of that.

**Missing GPU tooling:** the GPU section of the batched command is `|| true` guarded, so a host without `nvidia-smi` just has no GPU section. Same for hwmon, diskstats and `df`.

**No lock held:** `cat info.json` fails, which is why the lock section carries `|| true`. Without it a nonzero exit would abort the whole batched command.

### Platform Support

| Platform | CPU | Per-core | Temp | RAM | GPU | Disk usage | Disk I/O | Network | System info |
|----------|-----|----------|------|-----|-----|------------|----------|---------|-------------|
| Linux | Full | Full | hwmon | Full | NVIDIA | Full | Full | Full | Full |
| macOS | Full | No | No | Full | Apple Silicon | Full | No | Full | Full |

Anything else falls back to the Linux command path, which degrades to whatever sections the host can produce.

### Implementation Notes

**TUI framework:** [Bubble Tea](https://github.com/charmbracelet/bubbletea) for the model/update/view loop, [Lip Gloss](https://github.com/charmbracelet/lipgloss) for styling, [Bubbles](https://github.com/charmbracelet/bubbles) viewport for scrolling. Full-screen alt-screen program.

**Sparklines:** custom braille rendering (`graphs.go`), 2x4 dots per cell. History is a ring buffer of 600 samples per metric per host (10 minutes at the 1s default).

**Responsive layout:** width and height come from `tea.WindowSizeMsg`; layout mode, column count, graph rows and process counts are all derived from them.

---

## Implementation Phases

### Phase 1: Core MVP (2-3 weeks)

**Goal**: Deliver minimum viable tool that beats the script.

**Scope:**

- Single host support (first SSH alias only, no fallback chain)
- Basic config file (host, dir, exclude)
- `rr sync` and `rr run` commands
- Atomic locking with stale detection
- Streaming output with phase indicators
- Generic formatter only
- `rr setup` for SSH key configuration
- `rr init` for guided config creation

**Exit criteria:**

- Can sync code and run pytest on configured remote
- Lock prevents concurrent runs
- New user can go from install to first run in <5 minutes

### Phase 2: Smart Host Selection (1-2 weeks)

**Goal**: Add the connection magic.

**Scope:**

- Multiple SSH aliases per host with ordered fallback
- Configurable probe timeout
- Connection caching within session
- Local fallback option
- `rr status` with connectivity display
- `rr doctor` for diagnostics

**Exit criteria:**

- Tool automatically selects best available host
- Clear indication of which host was selected
- Doctor identifies and explains common issues

### Phase 3: Tasks and Formatters (1-2 weeks)

**Goal**: Power-user features for common workflows.

**Scope:**

- Task definitions with single and multi-step
- First-class task invocation (`rr test`)
- Pytest formatter with failure extraction
- Jest formatter
- Go test formatter
- Auto-detection logic
- `on_fail: continue` for multi-step tasks

**Exit criteria:**

- Named tasks work as expected
- Pytest failures show file:line and message summary
- Shell completions include task names

### Phase 4: Host Monitoring

**Goal**: Real-time visibility into your fleet.

**Scope:**

- `rr monitor` command with Bubble Tea TUI
- CPU, RAM, network metrics collection
- Parallel metric fetching across hosts
- Connection pooling for persistent SSH
- GPU detection (NVIDIA via nvidia-smi)
- Host state indicators (connected/slow/unreachable)
- Sparkline history visualization
- Keyboard navigation and sorting

**Exit criteria:**

- Can monitor all configured hosts in real-time
- Graceful handling of unreachable hosts
- Sub-second refresh feels smooth
- GPU metrics display when available

### Phase 5: Polish and Distribution

**Goal**: Make it installable and documented.

**Scope:**

- GoReleaser configuration
- Homebrew formula
- Install script (`curl | sh`)
- Shell completions for all shells
- README with examples
- Troubleshooting guide
- JSON Schema published for editor support

**Exit criteria:**

- `brew install` works
- Completions work in bash/zsh/fish
- Documentation covers common use cases

---

## Risks and Mitigations

| Risk                             | Likelihood | Impact | Mitigation                                                |
| -------------------------------- | ---------- | ------ | --------------------------------------------------------- |
| rsync not available on target    | Low        | High   | Check in `rr doctor`, clear install instructions          |
| SSH config parsing edge cases    | Medium     | Medium | Fall back gracefully, allow explicit user@host            |
| Windows SSH support              | Medium     | Low    | Windows is lower priority; document WSL as alternative    |
| Output formatter false positives | Medium     | Low    | Auto-detect has confidence threshold, `--format` override |
| Lock file permission issues      | Low        | High   | Document in troubleshooting, `rr doctor` checks           |
| Name collision (`rr`)            | Low        | Medium | Check for conflicts at install, document alternatives     |

---

## Open Questions

Resolved:

- ✅ SSH keys only, no password support — security requirement
- ✅ Config file name: `.rr.yaml`
- ✅ Task invocation: `rr <taskname>` not `rr task <name>`

Still open:

1. **Project naming**: `rr` is short but may conflict with Mozilla rr (record-replay debugger). Same namespace concerns as `fd` vs `find`. Alternatives if needed: `rem`, `rrun`, `offload`. Decision: Ship as `rr`, rename if conflicts prove problematic.

2. **Bidirectional sync**: Should we support pulling artifacts back (coverage reports, build outputs)? Recommendation: Not in v1. Add `rr pull` in v2 if requested.

3. **Watch mode**: Auto-sync on file changes? Recommendation: Not in v1. Mutagen does this well; we're solving a different problem.

4. **Multi-host parallel**: Run same command on multiple hosts? Recommendation: Not in v1. Different use case (closer to Ansible territory).

---

## Appendix: Full CLI Reference

```
rr - Road Runner

USAGE
  rr <command> [flags]

PRIMARY COMMANDS
  run <cmd>           Sync files and execute command on remote
  exec <cmd>          Execute command without syncing
  sync                Sync files only
  <task>              Run a named task from config

SETUP COMMANDS
  init                Create config file with guided prompts
  setup <host>        Configure SSH key authentication for host

STATUS COMMANDS
  status              Show selected host and connectivity
  monitor             Real-time dashboard of all host metrics
  doctor              Run diagnostic checks

HOST MANAGEMENT
  host list           List configured hosts (alias: ls)
  host add            Add a new host interactively
  host remove <name>  Remove a host (alias: rm)

MAINTENANCE
  update              Check for and install latest version

GLOBAL FLAGS
      --config string                 Config file (default is .rr.yaml)
      --no-color                      Disable colored output
      --no-strict-host-key-checking   Disable SSH host key verification (insecure, for CI/automation only)
  -q, --quiet                         Suppress non-essential output
  -v, --verbose                       Verbose output
  -h, --help                          Show help

EXAMPLES
  # Sync and run a command
  rr run "pytest tests/"

  # Run a configured task
  rr test

  # Just sync files
  rr sync

  # Run without syncing (already synced)
  rr exec "pytest tests/test_auth.py -v"

  # Override host
  rr run --host=gpu-box "python train.py"

  # Check connectivity
  rr status

  # Set up SSH keys for a new host
  rr setup mini

  # Diagnose issues
  rr doctor

  # Monitor all hosts in real-time
  rr monitor

  # Monitor specific hosts with faster refresh
  rr monitor --hosts=mini,gpu-box --interval=1s
```
