# Configuration reference

This document covers all configuration options for `.rr.yaml`.

## Contents

- [File location](#file-location)
- [Complete example](#complete-example)
- [Top-level fields](#top-level-fields)
- [Hosts](#hosts)
- [Sync](#sync)
- [Lock](#lock)
- [Tasks](#tasks)
- [Output](#output)
- [Monitor](#monitor)
- [Duration syntax](#duration-syntax)
- [Validation rules](#validation-rules)
- [Minimal config](#minimal-config)

## File location

`rr` searches for configuration in this order:

1. `.rr.yaml` in the current directory
2. `.rr.yaml` in parent directories (up to filesystem root)

## Complete example

```yaml
version: 1

hosts:
  mini:
    ssh:
      - mac-mini.local
      - mac-mini-tailscale
      - user@192.168.1.50
    dir: ~/projects/${PROJECT}
    tags:
      - fast
      - local
    env:
      GOPATH: /home/user/go
      DEBUG: "1"

  server:
    ssh:
      - dev-server
    dir: /var/projects/${PROJECT}
    tags:
      - linux

default: mini
local_fallback: false
probe_timeout: 2s

sync:
  exclude:
    - .git/
    - .venv/
    - __pycache__/
    - "*.pyc"
    - node_modules/
    - .mypy_cache/
    - .pytest_cache/
    - .DS_Store
  preserve:
    - .venv/
    - node_modules/
    - data/
  flags:
    - --compress

lock:
  enabled: true
  timeout: 5m
  stale: 10m
  dir: /tmp/rr-locks

tasks:
  test:
    description: Run all tests
    run: make test

  deploy:
    description: Build and deploy to staging
    hosts:
      - server
    steps:
      - name: Build
        run: make build
      - name: Deploy
        run: ./scripts/deploy.sh
        on_fail: stop

output:
  color: auto
  format: auto
  timing: true
  verbosity: normal

monitor:
  interval: 2s
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
  exclude: []
```

## Top-level fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `version` | int | `1` | Config schema version. Currently must be `1`. |
| `hosts` | map | required | Remote host definitions. |
| `default` | string | first host | Default host when `--host` is not specified. |
| `local_fallback` | bool | `false` | Run locally if no hosts are reachable. |
| `probe_timeout` | duration | `2s` | How long to wait when testing SSH connectivity. |
| `sync` | object | see below | File synchronization settings. |
| `lock` | object | see below | Distributed lock settings. |
| `tasks` | map | `{}` | Named command sequences. |
| `output` | object | see below | Terminal output formatting. |
| `monitor` | object | see below | Resource monitoring dashboard settings. |

## Hosts

Each host entry configures a remote machine.

```yaml
hosts:
  mini:
    ssh:
      - mac-mini.local
      - mac-mini-tailscale
    dir: ~/projects/${PROJECT}
    tags:
      - fast
    env:
      DEBUG: "1"
```

### Host fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ssh` | list | yes | SSH connection strings, tried in order. |
| `dir` | string | yes | Working directory on remote. Supports variable expansion. |
| `tags` | list | no | Tags for filtering with `--tag` flag. |
| `env` | map | no | Environment variables for commands on this host. |

### SSH connection strings

Each entry in `ssh` can be:

- **Hostname**: `mac-mini.local`
- **User@host**: `deploy@server.example.com`
- **SSH config alias**: `dev-server` (from `~/.ssh/config`)
- **IP address**: `192.168.1.50`

`rr` tries each SSH alias in order until one connects. This is useful when a machine is reachable via multiple networks (e.g., local network vs. VPN).

### Variable expansion

The `dir` field supports these variables:

| Variable | Expands to | Example |
|----------|------------|---------|
| `${PROJECT}` | Current directory name | `myapp` |
| `${USER}` | Local username | `riley` |
| `${HOME}` | Remote user's home directory | `/home/riley` |

```yaml
# If your local project is /Users/riley/code/myapp
dir: ~/projects/${PROJECT}
# Expands to: ~/projects/myapp
```

## Sync

Controls file synchronization behavior using rsync.

```yaml
sync:
  exclude:
    - .git/
    - node_modules/
    - "*.pyc"
  preserve:
    - node_modules/
    - .venv/
  flags:
    - --compress
```

### Sync fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `exclude` | list | see below | Patterns for files not sent to remote. |
| `preserve` | list | see below | Patterns for files not deleted on remote. |
| `flags` | list | `[]` | Extra flags passed to rsync. |

### Default excludes

If you don't specify `exclude`, these patterns are used:

```yaml
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
```

### Default preserves

If you don't specify `preserve`, these patterns are used:

```yaml
preserve:
  - .venv/
  - node_modules/
  - data/
  - .cache/
```

**Note:** Preserved files are not deleted on the remote even if they don't exist locally. This is useful for dependencies that should be installed once on the remote.

### Pattern syntax

Patterns use rsync filter syntax:

- `*.pyc` - Match files ending in `.pyc`
- `node_modules/` - Match directory named `node_modules`
- `/build/` - Match `build/` at the root only
- `**/*.log` - Match `.log` files in any subdirectory

## Lock

Distributed locking prevents multiple `rr` instances from running on the same host simultaneously.

```yaml
lock:
  enabled: true
  timeout: 5m
  stale: 10m
  dir: /tmp/rr-locks
```

### Lock fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Whether to use distributed locking. |
| `timeout` | duration | `5m` | How long to wait for a lock. |
| `stale` | duration | `10m` | When to consider a lock abandoned. |
| `dir` | string | `/tmp/rr-locks` | Directory for lock files on remote. |

### How locking works

1. Before running a command, `rr` creates a lock file on the remote
2. If another instance holds the lock, `rr` waits up to `timeout`
3. If the lock is older than `stale`, it's considered abandoned and can be taken
4. The lock is released when the command finishes

Disable locking if you're the only user of a remote host:

```yaml
lock:
  enabled: false
```

## Tasks

Named tasks let you define reusable command sequences.

### Simple task (single command)

```yaml
tasks:
  test:
    description: Run all tests
    run: pytest tests/ -v
```

Run with: `rr test`

### Multi-step task

```yaml
tasks:
  deploy:
    description: Build and deploy
    steps:
      - name: Build
        run: make build
      - name: Deploy
        run: ./scripts/deploy.sh
        on_fail: stop
```

### Task fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | no | Shown in `rr --help`. |
| `run` | string | if no steps | Command to execute (simple tasks). |
| `steps` | list | if no run | Steps for multi-step tasks. |
| `hosts` | list | no | Restrict this task to specific hosts. |
| `env` | map | no | Environment variables for this task. |

### Step fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | no | Identifier shown in output. |
| `run` | string | yes | Command to execute. |
| `on_fail` | string | no | Behavior on failure: `stop` (default) or `continue`. |

### Host-restricted tasks

```yaml
tasks:
  deploy:
    description: Deploy to production
    hosts:
      - server
    run: ./deploy.sh
```

This task only runs on the `server` host, regardless of the default.

### Reserved task names

You cannot name a task after a built-in command. These names are reserved:

- `run`, `exec`, `sync`
- `init`, `setup`, `status`
- `monitor`, `doctor`, `completion`
- `help`, `version`

## Output

Controls terminal output formatting.

```yaml
output:
  color: auto
  format: auto
  timing: true
  verbosity: normal
```

### Output fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `color` | string | `auto` | Color mode: `auto`, `always`, or `never`. |
| `format` | string | `auto` | Output formatter: `auto`, `generic`, `pytest`, `jest`, `go`, `cargo`. |
| `timing` | bool | `true` | Show timing for each phase. |
| `verbosity` | string | `normal` | Output level: `quiet`, `normal`, or `verbose`. |

### Color modes

- `auto` - Enable color when output is a terminal, disable when piped
- `always` - Always use color (even when piped)
- `never` - Never use color

### Output formatters

- `auto` - Detect test framework from command and apply appropriate formatting
- `generic` - No special formatting
- `pytest` - Format pytest output
- `jest` - Format Jest output
- `go` - Format `go test` output
- `cargo` - Format `cargo test` output

## Monitor

Controls the resource monitoring dashboard (`rr monitor`).

```yaml
monitor:
  interval: 2s
  thresholds:
    cpu:
      warning: 70
      critical: 90
    ram:
      warning: 80
      critical: 95
    gpu:
      warning: 70
      critical: 90
  exclude:
    - slow-host
```

### Monitor fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `interval` | duration | `2s` | Time between metric updates. |
| `thresholds` | object | see below | Threshold settings for metric coloring. |
| `exclude` | list | `[]` | Host names to exclude from the monitor. |

### Thresholds

Each metric type (CPU, RAM, GPU) has warning and critical thresholds that control the color coding in the dashboard:

- Below warning: Green (healthy)
- Warning to critical: Yellow (warning)
- Above critical: Red (critical)

| Threshold | Default | Description |
|-----------|---------|-------------|
| `cpu.warning` | `70` | CPU percentage for yellow color. |
| `cpu.critical` | `90` | CPU percentage for red color. |
| `ram.warning` | `70` | RAM percentage for yellow color. |
| `ram.critical` | `90` | RAM percentage for red color. |
| `gpu.warning` | `70` | GPU percentage for yellow color. |
| `gpu.critical` | `90` | GPU percentage for red color. |

### Excluding hosts

Use `exclude` to hide specific hosts from the monitor dashboard. This is useful for hosts that are:

- Slow to respond (causing dashboard delays)
- Temporarily offline for maintenance
- Not relevant for monitoring

```yaml
monitor:
  exclude:
    - dev-machine
    - staging-server
```

## Duration syntax

Fields that accept durations use Go's duration format:

| Format | Meaning |
|--------|---------|
| `500ms` | 500 milliseconds |
| `5s` | 5 seconds |
| `2m` | 2 minutes |
| `1h` | 1 hour |
| `1h30m` | 1 hour 30 minutes |

## Validation rules

`rr` validates your config on load. Common validation errors:

| Error | Fix |
|-------|-----|
| "no hosts configured" | Add at least one host under `hosts:` |
| "host 'X' has no SSH aliases" | Add `ssh:` list to the host |
| "host 'X' has no dir" | Add `dir:` to the host |
| "default host 'X' not found" | Set `default:` to a configured host name |
| "reserved task name 'X'" | Rename the task to avoid built-in command names |
| "task 'X' has both run and steps" | Use either `run` or `steps`, not both |

## Minimal config

The smallest valid config:

```yaml
version: 1

hosts:
  myhost:
    ssh:
      - myserver.example.com
    dir: ~/projects/${PROJECT}
```

Everything else uses sensible defaults.
