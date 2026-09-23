# Configuration reference

`rr` uses two configuration files: a **global config** for your personal host definitions and a **project config** for shareable project settings.

## Contents

- [Configuration overview](#configuration-overview)
- [Global config (~/.rr/config.yaml)](#global-config-rrconfigyaml)
- [Project config (.rr.yaml)](#project-config-rryaml)
- [Host resolution order](#host-resolution-order)
- [Sync](#sync)
- [Lock](#lock)
- [Tasks](#tasks)
- [Requirements](#requirements)
- [Output](#output)
- [Monitor](#monitor)
- [Duration syntax](#duration-syntax)
- [Validation rules](#validation-rules)
- [Minimal config](#minimal-config)

## Configuration overview

| Config | Location | Purpose | Share with team? |
|--------|----------|---------|------------------|
| Global | `~/.rr/config.yaml` | Host definitions and personal defaults | No |
| Project | `.rr.yaml` in project root | Sync rules, tasks, lock settings | Yes |

**Why the split?** Host configurations include personal SSH settings, directory paths, and machine-specific details that differ between team members. Keeping them in a global config means your `.rr.yaml` can be committed to version control without conflicts.

## Global config (~/.rr/config.yaml)

The global config stores your personal host definitions. Create it with `rr host add` or manually.

### Location

The global config is always at `~/.rr/config.yaml`. If the file doesn't exist, `rr` uses defaults.

### Complete global config example

```yaml
version: 1

hosts:
  mini:
    ssh:
      - mac-mini.local
      - mac-mini-tailscale
      - user@192.168.1.50
    dir: ${HOME}/projects/${PROJECT}
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

defaults:
  local_fallback: never
  probe_timeout: 2s

logs:
  dir: ~/.rr/logs
  keep_runs: 10
```

### Global config fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `version` | int | `1` | Config schema version. Currently must be `1`. |
| `hosts` | map | `{}` | Remote host definitions (see below). |
| `defaults.local_fallback` | string | `never` | When to run locally: `never`, `on-unreachable` (hosts down or unconfigured), or `always` (also when every host is locked). Booleans still work: `true` = `always`, `false` = `never`. |
| `defaults.probe_timeout` | duration | `2s` | How long to wait when testing SSH connectivity. |
| `defaults.rewrite_paths` | bool | `true` | Rewrite local absolute paths in commands and task args to their remote equivalents before running. |
| `logs.dir` | string | `~/.rr/logs` | Where run logs are written (single runs and parallel tasks). |
| `logs.keep_runs` | int | `10` | Keep this many recent runs per task. `0` disables count-based cleanup. |
| `logs.keep_days` | int | `0` | Delete logs older than this many days. `0` disables it. |
| `logs.max_size_mb` | int | `0` | Delete the oldest logs to stay under this total size. `0` disables it. |

Log cleanup runs after each run, in this order: `max_size_mb`, then `keep_days`, then `keep_runs`. `rr logs` lists log directories and `rr logs clean` applies the policy on demand (`--all` deletes everything, `--older 7d` deletes by age).

### Host fields

Each host entry configures a remote machine.

```yaml
hosts:
  mini:
    ssh:
      - mac-mini.local
      - mac-mini-tailscale
    dir: ${HOME}/projects/${PROJECT}
    tags:
      - fast
    env:
      DEBUG: "1"
    shell: "zsh -l -c"
    setup_commands:
      - export PATH=$HOME/.local/bin:$PATH
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ssh` | list | yes | SSH connection strings, tried in order. |
| `dir` | string | yes | Working directory on remote. Supports variable expansion. |
| `tags` | list | no | Tags for filtering with `--tag` flag. |
| `env` | map | no | Environment variables for commands on this host. |
| `shell` | string | no | Shell invocation format (e.g., `zsh -l -c`). Default uses `$SHELL -l -c`. |
| `setup_commands` | list | no | Commands to run before each command (e.g., `source ~/.nvm/nvm.sh`). |
| `require` | list | no | Tools that must exist on this host (verified before running commands). |

### SSH connection strings

Each entry in `ssh` can be:

- **Hostname**: `mac-mini.local`
- **User@host**: `deploy@server.example.com`
- **SSH config alias**: `dev-server` (from `~/.ssh/config`)
- **IP address**: `192.168.1.50`

`rr` tries each SSH alias in order until one connects. This is useful when a machine is reachable via multiple networks (e.g., local network vs. VPN).

**Passwordless SSH is required.** You must be able to run `ssh <alias>` without entering a password. See the [SSH setup guide](ssh-setup.md) if you need to configure key-based auth.

### Variable expansion

The `dir` field supports these variables:

| Variable | Expands to | Example |
|----------|------------|---------|
| `${PROJECT}` | Git repo name (from the `origin` URL, else the repo root's directory name), or the current directory name outside a repo | `myapp` |
| `${USER}` | Local username | `riley` |
| `${HOME}` | Remote user's home directory (sent as `~` for the remote shell to expand) | `/home/riley` |

```yaml
# If your local project is /Users/riley/code/myapp
dir: ~/projects/${PROJECT}
# Expands to: ~/projects/myapp
```

**Git worktrees:** in a linked worktree, `${PROJECT}` expands to
`<repo>@<worktree-name>` (e.g. `myapp@myapp-featurex`), so each worktree
syncs to its own remote directory instead of clobbering the main checkout's
mirror. The main checkout keeps its plain name. Expect a cold first sync per
worktree, and remember `preserve`d directories (like `.venv`) start empty in
the new remote dir. Disable per project with `sync.worktree_isolation: false`.

Removing a worktree locally leaves its remote directory behind, so a machine
that churns through worktrees fills the remote disk. Every sync removes the
remote directories of worktrees that no longer exist locally (disable with
`sync.prune_worktrees: false`), and `rr prune` cleans hosts that have not been
synced to since a worktree was removed:

```bash
rr prune                 # every project host
rr prune --dry-run       # list what would be removed
rr prune --host m4-mini  # one host
```

Prune only ever removes `<repo>@<worktree>` directories beside the project's
remote dir, never the main checkout's, and it skips directories whose sync
marker names a different machine.

## Project config (.rr.yaml)

The project config lives in your project root and contains settings that can be shared with your team.

### Location

`rr` searches for project configuration in this order:

1. Explicit path via `--config` flag
2. `.rr.yaml` in the current directory
3. `.rr.yaml` in parent directories (stops at git root or home directory)

### Complete project config example

```yaml
version: 1

# Reference hosts from global config
# If omitted, all global hosts are available for load balancing
hosts:
  - mini
  - server

# Or use a single host
# host: mini

sync:
  respect_gitignore: true
  exclude:
    - .git # bare pattern: .git is a file in linked worktrees
    - .venv
    - __pycache__/
    - "*.pyc"
    - node_modules
    - .mypy_cache/
    - .pytest_cache/
    - .DS_Store
  preserve:
    - .venv/
    - node_modules/
    - data/
  flags:
    - --compress
  invalidations:
    - lockfile: package-lock.json
      dirs: [node_modules/]

lock:
  enabled: true
  timeout: 5m
  wait_timeout: 1m
  stale: 90s
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

### Project config fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `version` | int | `1` | Config schema version. Currently must be `1`. |
| `host` | string | - | Single host reference (from global config). |
| `hosts` | list | all global hosts | List of host references for load balancing. |
| `local_fallback` | string | global setting | Overrides `defaults.local_fallback`: `never`, `on-unreachable`, or `always`. |
| `rewrite_paths` | bool | global setting | Overrides `defaults.rewrite_paths`. |
| `defaults` | object | - | Project defaults applied to every task (see below). |
| `require` | list | `[]` | Tools that must exist on remote hosts. |
| `sync` | object | see below | File synchronization settings. |
| `lock` | object | see below | Distributed lock settings. |
| `tasks` | map | `{}` | Named command sequences. |
| `monitor` | object | see below | Resource monitoring dashboard settings. |

**Note:** Use either `host` (singular) or `hosts` (plural), not both. If neither is specified, all hosts from your global config are available for load balancing.

### Project defaults

Settings under `defaults` apply to everything the project runs:

```yaml
defaults:
  setup:
    - source ~/rr-env.sh
  env:
    CI: "1"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `defaults.setup` | list | `[]` | Commands prepended (with `&&`) before every command, after host `setup_commands`. Applies to tasks and parallel subtasks, locally and on remote hosts, and to ad-hoc `rr run` / `rr exec` on remote hosts only. |
| `defaults.env` | map | `{}` | Environment variables for all tasks, including parallel subtasks. Overrides host env; overridden by task env. |

## Host resolution order

When you run a command, `rr` determines which host(s) to use in this order:

1. `--host` flag (explicit CLI argument)
2. `.rr.yaml` `hosts:` field (project's preferred hosts for load balancing)
3. `.rr.yaml` `host:` field (project's single preferred host)
4. All hosts from global config, alphabetically (default for load balancing)

Exception (local mode): if `.rr.yaml` sets `local_fallback` to `on-unreachable` or `always` and names no `host`/`hosts`, or `local_fallback` is on and no hosts are configured at all, commands run locally without trying any remote host. `--host` or `--tag` overrides local mode. Local-mode runs report `details.reason: "local_mode"` on their connect event, and `--local` runs report `local_flag`; neither needs any hosts configured.

**Important:** The order of hosts in your `hosts:` list determines priority. The first host is tried first. If it's busy or unreachable, `rr` moves to the next host in the list. This gives you explicit control over which machines are preferred.

```yaml
# .rr.yaml
hosts:
  - gpu-box      # Tried first (highest priority)
  - mini-server  # Tried second
  - backup-host  # Tried last (lowest priority)
```

## Working directory

`rr run` and `rr exec` preserve the subdirectory you invoked them from, so relative paths keep the meaning they have locally:

```bash
cd backend
rr run "pytest tests/test_api.py"   # runs in <remote dir>/backend
```

The offset is reported as `remote_cwd` in the result envelope. It applies to local execution too, so a command means the same thing whether or not a remote host answered. If the directory wasn't synced (it matches a `sync.exclude` pattern, for instance), the command falls back to the project root rather than failing.

`--cwd` overrides the offset:

```bash
rr run --cwd backend "pytest tests/"   # explicit, ignores where you are
```

**Named tasks do not get an offset.** `rr test` means the same thing from every directory, since task commands are project-scoped and often embed their own `cd` (see [Tasks](#tasks)).

This changed in the release noted in the CHANGELOG: ad-hoc commands used to run at the project root no matter where you were. If you relied on that, `--cwd .` restores it.

When a relative path fails because it resolved differently than you expected, `rr` says so in the result `hint`, naming the directory it ran in and the fix. The hint needs the failing path to appear in stderr, so tools that report a missing file without naming it (`make` says only "no makefile found") fail without one.

## Exit codes and pipes

`rr` reports the exit code the remote command returned, unchanged. If your command pipes its output, the shell reports only the **last** stage's status, so a failing test runner can still exit 0:

```bash
rr run "pytest tests/ | tail -20"    # exit code comes from tail, not pytest
```

`rr` won't rewrite your command's semantics, since `cmd | grep -q pattern` tolerates upstream failure on purpose. Two things help:

- When a piped run collects zero tests, `rr` warns and sets `details.piped_exit_code` so the misleading exit code is visible.
- To propagate the failure, enable `pipefail` through the host's `shell` field:

  ```yaml
  hosts:
    mini:
      shell: "bash -o pipefail -c"
  ```

  Note `bash`, not `sh`: `dash` (Debian/Ubuntu's `/bin/sh`) doesn't support `pipefail`.

The same trap applies one level up, where no host config can reach it. Piping `rr` itself discards `rr`'s exit code:

```bash
rr test | tail -8          # exit code comes from tail; a failed suite reports 0
set -o pipefail            # in your shell or script, before the pipe
rr test | tail -8          # exit code comes from rr
```

Use `--tail N` instead of an external pager when you only want the end of the output; it prints after the result envelope and keeps the exit code intact:

```bash
rr test --tail 20
```

## Sync

Controls file synchronization behavior using rsync.

```yaml
sync:
  exclude:
    - .git
    - node_modules
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
| `respect_gitignore` | bool | `true` | Apply the repository-root `.gitignore` as extra exclude rules. Nested `.gitignore` files are not read. Explicit `exclude`/`preserve` patterns take precedence. |
| `flags` | list | `[]` | Extra flags passed to rsync. |
| `invalidations` | list | see below | Lockfiles that, when changed, delete remote install directories so they get reinstalled. |
| `worktree_isolation` | bool | `true` | Give each linked git worktree its own remote directory (`${PROJECT}` becomes `repo@worktree`). |
| `prune_worktrees` | bool | `true` | After a sync, remove remote `repo@worktree` directories whose worktree no longer exists locally. |

### Default excludes

If you don't specify `exclude`, these patterns are used:

```yaml
exclude:
  - .git # bare patterns for .git/.venv/node_modules: in linked
  - .venv # worktrees .git is a file, and node_modules may be a
  - __pycache__/ # symlink - dir-only patterns miss those (rsync exit 23)
  - "*.pyc"
  - node_modules
  - .mypy_cache/
  - .pytest_cache/
  - .ruff_cache/
  - .DS_Store
  - "*.log"
  - .claude/
  - .cursor/
  - .aider/
  - .copilot/
```

**Your `exclude` list replaces the defaults** - it isn't merged. If you
maintain your own list, prefer the bare `.git` / `.venv` / `node_modules`
patterns so linked worktrees sync cleanly. Note a bare pattern also matches
files with that name at any depth (e.g. a vendored fixture named `.git`).

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

### Lockfile invalidations

Preserved install directories go stale when a lockfile changes locally: rsync syncs the new lockfile, but the old `node_modules/` stays put and the package manager may not notice. Before each sync, `rr` compares each local lockfile's mtime with the remote directory's mtime and deletes the remote directory when the lockfile is newer. A remote directory that doesn't exist is left alone, since there's nothing to invalidate. This runs inside every sync, including `rr sync` and parallel runs. Your next install command then starts clean. `rr` only deletes; it doesn't run the install for you, so pair this with a task `setup` or an install task.

If you don't specify `invalidations`, these are used:

```yaml
invalidations:
  - lockfile: bun.lock
    dirs: [node_modules/]
  - lockfile: package-lock.json
    dirs: [node_modules/]
  - lockfile: yarn.lock
    dirs: [node_modules/]
  - lockfile: pnpm-lock.yaml
    dirs: [node_modules/]
  - lockfile: poetry.lock
    dirs: [.venv/]
  - lockfile: Pipfile.lock
    dirs: [.venv/]
```

`lockfile` is relative to the project root, and `dirs` are relative to the remote project directory. Like `exclude`, your list replaces the defaults. Set `invalidations: []` to turn the behavior off.

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
  stale: 90s
  dir: /tmp/rr-locks
```

### Lock fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Whether to use distributed locking. |
| `timeout` | duration | `5m` | How long to wait for a lock on a single host. |
| `wait_timeout` | duration | `1m` | How long to round-robin when all hosts are locked. |
| `stale` | duration | `90s` | When to consider a lock abandoned, measured from the holder's last heartbeat. |
| `dir` | string | `/tmp/rr-locks` | Directory for lock files on remote. |

### How locking works

1. Before running a command, `rr` atomically creates a `rr.lock/` directory inside the lock `dir` on the remote, with an `info.json` describing the holder. There is one lock per host and lock `dir`: projects share a lock only when they use the same host and the same `lock.dir`.
2. While it holds the lock, `rr` touches `info.json` every 30s as a heartbeat
3. If another instance holds the lock, `rr` waits up to `timeout`
4. If the holder's heartbeat is older than `stale`, the lock is considered abandoned and taken over with a warning
5. If the holder is an `rr` process on this machine that is no longer running, the lock is taken immediately without waiting for `stale`
6. The lock is released when the command finishes

`rr unlock [host]` (or `rr unlock --all`) force-releases a stuck lock.

### Load balancing with multiple hosts

When multiple hosts are configured, `rr` distributes work automatically:

1. Tries each host with a non-blocking lock check
2. If a host is locked, immediately tries the next host
3. Locks held by dead processes on this machine are reclaimed automatically
4. If all hosts are locked, what happens depends on `local_fallback`:
   - `always`: runs locally right away with a loud warning (and `details.fallback` in structured output). If any lock holder is on this same machine (likely your own other run), it first waits up to `wait_timeout` for a host to free up.
   - `never` / `on-unreachable`: waits up to `wait_timeout`, cycling through the hosts, then fails with the lock holders listed

```yaml
lock:
  enabled: true
  timeout: 5m        # Per-host lock wait time
  wait_timeout: 2m   # Total time to round-robin when all hosts locked
  stale: 90s
```

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

### Passing extra arguments

Extra CLI args flow into single-command tasks. Use an `{args}` placeholder
to control where they land - essential for pipelines, where appended args
would bind to the last command instead of the one you mean:

```yaml
tasks:
  test:
    run: pytest {args:-tests/} -n 4 | tail -20
```

- `rr test -- tests/foo.py -k bond` runs `pytest 'tests/foo.py' '-k' 'bond' -n 4 | tail -20`
- `rr test` with no args uses the `{args:-default}` default: `pytest tests/ -n 4 | tail -20`
- Args are shell-quoted before substitution
- Without a placeholder, args are appended (quoted) to simple commands;
  compound commands (pipes, `&&`, redirections, `$()`) reject extra args
  and tell you where to add `{args}`
- Write `{{args}}` for a literal `{args}` in the command
- Multi-step (`steps`) tasks don't accept extra args
- Put flag-style args after `--` (`rr test -- -k bond -x`). Without it, `rr` parses them as its own flags and fails with `CONFIG_INVALID` and a hint to use `--`. That includes `-v`, which is no longer an `rr` flag. `-q`, `-p`, and `-m` are still `rr` flags, so they need `--` too.

Parallel tasks reject extra args unless they set `forward_args: true` (see [Forwarding args to subtasks](#forwarding-args-to-subtasks)).

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
| `description` | string | no | Shown in `rr --help` and `rr tasks`. |
| `run` | string | if no steps/parallel/depends | Command to execute (simple tasks). |
| `steps` | list | if no run/parallel | Steps for multi-step tasks. |
| `parallel` | list | if no run/steps | Subtask names to run concurrently across hosts. |
| `setup` | string | no | Command to run once per host before parallel subtasks. |
| `depends` | list | no | Task dependencies to run before this task. |
| `hosts` | list | no | Restrict this task to specific hosts. |
| `env` | map | no | Environment variables for this task. |
| `require` | list | no | Tools that must exist for this task. |
| `pull` | list | no | Files to download from the remote after the task runs (see [Pulling files back](#pulling-files-back)). On a parallel task itself it has no effect and warns; set it on the subtasks. |
| `fail_fast` | bool | no | Stop all tasks on first failure (parallel/depends tasks). |
| `max_parallel` | int | no | Limit concurrent tasks (parallel tasks only). |
| `timeout` | duration | no | Per-subtask timeout (parallel tasks) or total timeout (depends tasks). Ignored for plain `run`/`steps` tasks. |
| `output` | string | no | Default output mode for parallel tasks: `progress` (default), `stream`, `verbose`, or `quiet`. CLI flags override it. Validated on every task; on a non-parallel task it has no effect and warns. |
| `forward_args` | bool | no | Parallel tasks only: forward extra CLI args to each subtask. |

**CLI flags for non-parallel tasks:** `--host`, `--tag`, `--local`, `--probe-timeout`, `--repeat N` (run the task N times in parallel across hosts, for flake detection), and `--tail N` (print the last N lines of the run log after the result). Tasks with `depends` also get `--skip-deps` and `--from`.

### Parallel task

Run multiple subtasks simultaneously across your host pool:

```yaml
tasks:
  test-backend:
    run: cd backend && pytest
  test-frontend:
    run: cd frontend && npm test
  test-opendata:
    run: cd opendata && python -m pytest

  # Runs all three subtasks in parallel across hosts
  test:
    description: Run all test suites
    parallel: [test-backend, test-frontend, test-opendata]
    fail_fast: false    # Continue even if one fails
    max_parallel: 3     # Limit concurrency (optional)
    timeout: 10m        # Per-subtask timeout (optional)
```

Run with: `rr test`

**How it works (work-stealing queue):**

1. All subtasks are placed in a shared queue
2. One worker per host pulls tasks from the queue (a subtask with `hosts:` only goes to a worker for one of its allowed hosts)
3. Files are synced and locks acquired once per host (not per task)
4. After first tasks complete, rr tracks host performance
5. Slow hosts wait before grabbing additional tasks, giving fast hosts priority
6. Output is captured and shown in a summary when complete
7. Logs are saved to `<logs.dir>/<task>-<timestamp>/` (default `~/.rr/logs`)

This performance-based work-stealing ensures efficient distribution across heterogeneous hosts. If you have 6 tasks across 3 hosts where one host is slower, the fast hosts grab more tasks (e.g., 3-2-1 distribution) instead of round-robin (2-2-2).

#### Setup phase (once per host)

When subtasks need shared setup (dependency installation, database migrations, etc.), use `setup` to avoid redundant work:

```yaml
tasks:
  test-all:
    setup: pip install -r requirements.txt   # Runs once per host
    parallel:
      - test-unit
      - test-integration
      - test-e2e

  test-unit:
    run: pytest tests/unit -v
  test-integration:
    run: pytest tests/integration -v
  test-e2e:
    run: pytest tests/e2e -v
```

Setup behavior:
- Runs exactly once per host, after file sync but before any subtasks
- If a host runs 3 subtasks, setup runs once (not 3 times)
- Setup failure aborts all subtasks on that host
- Works with both remote and local execution

#### Subtask environment and working directory

Each subtask runs with the same environment a single task gets: host `env`, then `defaults.env`, then the subtask's own `env`, with later entries winning. Host `setup_commands` and `defaults.setup` run before the subtask's command, after the `cd` into the project directory.

Subtasks that land on the same host run in the same remote directory. If two of them write the same file (a junit report, a coverage file), the later one overwrites the earlier one, so give each subtask its own output path.

#### Forwarding args to subtasks

By default a parallel task rejects extra CLI args: `rr test-backend -k bond` fails with `CONFIG_INVALID` and a hint to set `forward_args: true` and put task flags after `--`. Set `forward_args: true` to pass them to every subtask:

```yaml
tasks:
  test-api:
    run: pytest {args:-tests/api} -q
  test-models:
    run: pytest {args:-tests/models} -q

  test-backend:
    parallel: [test-api, test-models]
    forward_args: true
```

`rr test-backend -- -k bond` runs both subtasks with `-k bond`. Each subtask follows the same rules as a single task: args replace its `{args}` placeholder, or are appended (quoted) to a simple command, and a compound command without a placeholder is an error. Subtasks that use `steps` can't receive forwarded args. `{args:-default}` defaults apply even without `forward_args`. Absolute local paths in forwarded args are rewritten to project-relative paths (unless `rewrite_paths` is off).

A forwarded test filter can leave some subtasks with nothing to run, which pytest reports as a failure (exit 5).

#### Nested parallel tasks

Parallel tasks can reference other parallel tasks. When `rr` encounters a nested reference, it flattens the task tree before execution:

```yaml
tasks:
  # Split test suites for parallelization
  opendata-1:
    run: pytest opendata --test-group-count 3 --test-group 1
  opendata-2:
    run: pytest opendata --test-group-count 3 --test-group 2
  opendata-3:
    run: pytest opendata --test-group-count 3 --test-group 3

  backend-1:
    run: pytest backend --test-group-count 3 --test-group 1
  backend-2:
    run: pytest backend --test-group-count 3 --test-group 2
  backend-3:
    run: pytest backend --test-group-count 3 --test-group 3

  frontend:
    run: npm test

  # Group related subtasks
  test-opendata:
    parallel: [opendata-1, opendata-2, opendata-3]

  test-backend:
    parallel: [backend-1, backend-2, backend-3]

  # Reference parallel tasks - automatically flattened
  test:
    parallel: [test-opendata, test-backend, frontend]
```

Running `rr test` expands to 7 parallel tasks: `opendata-1`, `opendata-2`, `opendata-3`, `backend-1`, `backend-2`, `backend-3`, `frontend`.

Benefits:
- Run `rr test-opendata` to run just the 3 opendata splits
- Run `rr test` to run everything
- Add a 4th split to `test-opendata` and `test` automatically includes it
- No manual flattening or repetition required
- Diamond dependencies are deduplicated (if the same task is reachable through multiple paths, it runs once)

Use `--dry-run` to see the expanded task list:

```bash
rr test --dry-run
# Shows: test-opendata -> [opendata-1, opendata-2, opendata-3]
#        test-backend -> [backend-1, backend-2, backend-3]
#        Tasks to execute (7 total): ...
```

Circular references are detected during config validation.

**CLI flags for parallel tasks:**

| Flag | Description |
|------|-------------|
| `--stream` | Real-time interleaved output with `[host:task]` prefixes |
| `--verbose` | Full output shown when each task completes |
| `--quiet` | Summary only, no per-task output |
| `--fail-fast` | Stop all tasks on first failure (overrides config) |
| `--max-parallel N` | Limit concurrent tasks (overrides config) |
| `--dry-run` | Show execution plan without running |
| `--local` | Force local execution (ignore remote hosts) |
| `--no-logs` | Don't save output to log files |
| `--host` / `--tag` | Limit the host pool |

### Step fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | no | Identifier shown in output. |
| `run` | string | yes | Command to execute. |
| `on_fail` | string | no | Behavior on failure: `stop` (default) or `continue`. |

### Task dependencies

Define task execution order with the `depends` field. Tasks run their dependencies first, then execute their own command.

```yaml
tasks:
  lint:
    run: golangci-lint run
  test:
    run: go test ./...
  build:
    run: go build ./...

  # Linear dependency chain
  ci:
    description: Run full CI pipeline
    depends:
      - lint
      - test
      - build
```

Run with: `rr ci`

This executes: `lint` -> `test` -> `build` in order.

#### Parallel groups in dependencies

Run multiple dependencies simultaneously using the `parallel` syntax:

```yaml
tasks:
  lint:
    run: golangci-lint run
  typecheck:
    run: mypy .
  test:
    run: pytest
  deploy:
    run: ./deploy.sh

  ci:
    description: Run CI pipeline
    depends:
      - parallel: [lint, typecheck]  # Run simultaneously
      - test                          # Run after parallel group completes
      - deploy
```

This executes: `[lint, typecheck]` (parallel) -> `test` -> `deploy`

#### Orchestrator tasks (no command)

Tasks with only `depends` orchestrate their dependencies without running their own command:

```yaml
tasks:
  lint:
    run: golangci-lint run
  test:
    run: go test ./...

  # Just orchestrates - no 'run' command needed
  verify:
    description: Run all verification tasks
    depends:
      - lint
      - test
```

#### Dependency deduplication

Tasks are deduplicated automatically. In a diamond dependency pattern, shared dependencies run once:

```yaml
tasks:
  base:
    run: echo "base"
  left:
    depends: [base]
    run: echo "left"
  right:
    depends: [base]
    run: echo "right"
  top:
    depends: [left, right]
```

Running `rr top` executes: `base` -> `left` -> `right` (base only runs once)

#### Dependency CLI flags

| Flag | Description |
|------|-------------|
| `--skip-deps` | Skip dependencies, run only the target task |
| `--from <task>` | Start from a specific task in the chain |

```bash
rr ci                  # Run full dependency chain
rr ci --skip-deps      # Run only ci task (skip lint, test, build)
rr ci --from test      # Start from test, skip lint
```

#### Dependency validation

Dependencies are validated at config load time:

| Validation | Error |
|------------|-------|
| Missing reference | `task 'X' depends on non-existent task 'Y'` |
| Self-reference | `task 'X' can't depend on itself` |
| Circular dependency | `circular dependency detected: A -> B -> C -> A` |

#### Combining dependencies with other task features

Dependencies work with other task fields:

```yaml
tasks:
  setup:
    run: ./setup.sh

  deploy:
    description: Deploy with prerequisites
    depends: [setup]
    run: ./deploy.sh
    hosts:
      - production
    env:
      DEPLOY_ENV: staging
    timeout: 10m
    fail_fast: true
```

### Pulling files back

`pull` downloads files from the remote after the task finishes, whether it passed or failed (test reports are often most useful on failure):

```yaml
tasks:
  coverage:
    run: pytest --cov --cov-report=xml --cov-report=html
    pull:
      - coverage.xml            # to the current directory
      - src: htmlcov/
        dest: ./reports/        # to a specific local directory
```

Sources are paths or globs relative to the host's `dir`. `dest` defaults to the current directory and is created if missing. A failed pull is reported but doesn't change the task's exit code. Pulling is skipped for local runs. For ad-hoc commands, use `rr run --pull <pattern> [--pull-dest <dir>]`, or `rr pull <pattern>` on its own.

**Subtasks of a parallel task** pull too, with three differences:

- Pulls run after every subtask has finished, pass or fail, one at a time, each from the host its subtask ran on.
- Each subtask's files land in `<dest>/<subtask>/` (`./<subtask>/` when `dest` is unset), so shards with the same output paths don't overwrite each other locally.
- Nothing is pulled after Ctrl+C.

Subtasks that run on the same host share one remote directory, so they overwrite each other there unless each writes to its own path, such as a junit file named after the subtask. `pull:` on the parallel task itself does nothing and produces a config warning.

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

The restriction holds inside parallel groups too: a restricted subtask is
scheduled only on a host it allows, and the run fails up front if `--host` or
`--tag` leaves it with no host to run on.

### Reserved task names

You cannot name a task after a built-in command. These names are reserved:

- `run`, `exec`, `sync`, `prune`, `pull`
- `init`, `setup`, `status`, `provision`
- `monitor`, `doctor`, `completion`, `logs`
- `help`, `version`, `update`, `host`
- `unlock`, `tasks`

## Requirements

The `require` field declares tools that must exist on remote hosts before commands run. rr verifies requirements after SSH connect but before file sync.

### Configuration levels

Requirements can be specified at three levels, which are merged together:

**Project level** (applied to all hosts and tasks):

```yaml
# .rr.yaml
require:
  - go
  - node
  - golangci-lint
```

**Host level** (applied to a specific host):

```yaml
# ~/.rr/config.yaml
hosts:
  gpu-box:
    ssh: [gpu.local]
    dir: ~/projects/${PROJECT}
    require:
      - nvidia-smi
      - python3
      - cuda
```

**Task level** (applied when running a specific task):

```yaml
# .rr.yaml
tasks:
  build:
    run: cargo build --release
    require:
      - cargo
      - rustc
```

### How it works

1. SSH connection established
2. Requirements merged from project + host + task (deduplicated)
3. Each tool verified with `command -v <tool>`
4. If any missing, error with actionable suggestions

### Built-in installers

A missing tool fails the run with a `DEPENDENCY_MISSING` error listing what's missing. `rr provision` installs missing tools on your hosts, for tools that have a built-in installer (`--check` reports without installing, `--yes` skips prompts, `--host` targets one host).

**Built-in installers:** `go`, `node`, `npm`, `yarn`, `pnpm`, `bun`, `deno`, `python`/`python3`, `pip`, `uv`/`uvx`, `rust`/`rustc`/`cargo`, `ruby`, `gem`, `java`/`javac`, `make`, `git`, `docker`, `kubectl`, `terraform`, `aws`, `gcloud`, `jq`, `curl`, `wget`, `rsync`, `ripgrep`/`rg`, `fd`, `fzf`, `tree`, `htop`, `tmux`, `vim`, `nvim`/`neovim`, `chromium`. Other tools can still be listed in `require`; `rr` checks for them but can't install them.

### CLI flags

Skip requirement checks:

```bash
rr run --skip-requirements "make test"
rr exec --skip-requirements "echo hello"
```

Check requirement status with doctor:

```bash
rr doctor --requirements
```

`rr doctor --requirements` also checks that rsync is installed on each host. A missing requirement is a doctor failure (exit 1), since `rr run` fails on it too.

## Output

Output is controlled by CLI flags. The old `output:` config section was never read and has been removed; if your `.rr.yaml` still has one, rr prints a config warning and ignores it. Delete the block.

| Flag | Effect |
|------|--------|
| (default) | Structured JSON: phase events on stderr, then a final result event. |
| `--pretty`, `-p` | Human-readable output with spinners and colors. |
| `--no-color` | Disable colors in pretty mode. |
| `--no-phases` | Suppress intermediate phase events (connect, sync, exec). The final result event is still emitted. |
| `--machine`, `-m` | No-op, kept for compatibility (JSON is already the default). |

The result event's `details` can include `summary` (test counts), `failures` (name, `file:line`, message), `no_tests`, `piped_exit_code`, `fallback`, `log_file`, `hint`, `remote_cwd`, and `path_rewrites`.

### Test output parsing

`rr` detects the test framework from the command and its output, and parses pytest, Jest, and `go test` results into `summary` and `failures`. Other runners (including `cargo test`) are passed through without parsing.

## Monitor

Controls the resource monitoring dashboard (`rr monitor`). Monitor settings live in the project config (`.rr.yaml`), not in `~/.rr/config.yaml`.

```yaml
monitor:
  interval: 1s
  timeout: 8s
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
  exclude:
    - slow-host
  alerts:
    enabled: false
    bell: true
    flash: true
    cooldown: 60s
    on_alert: ""
```

### Monitor fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `interval` | duration | `1s` | Time between metric updates. Minimum `500ms`. |
| `timeout` | duration | `8s` | Per-host connect and collect timeout. |
| `thresholds` | object | see below | Warning and critical percentages for metric coloring. |
| `exclude` | list | `[]` | Host names to hide from the dashboard. |
| `alerts` | object | see below | Threshold alerting (bell, card flash, hook). |

**Interval precedence:** the `--interval` flag wins over `monitor.interval`, which wins over the `1s` default. An invalid or sub-500ms value in config is an error, not a silent fallback.

```bash
rr monitor                    # uses monitor.interval, or 1s if unset
rr monitor --interval=2s      # flag wins regardless of config
```

### Thresholds

Each metric type (CPU, RAM, GPU) has warning and critical percentages. They drive both the numeric readouts and the sparkline/bar coloring in the dashboard, plus the colored cells in `rr monitor --once`:

- Below warning: healthy
- Warning to critical: warning
- At or above critical: critical

| Threshold | Default | Description |
|-----------|---------|-------------|
| `cpu.warning` | `70` | CPU percentage that turns the value warning-colored. |
| `cpu.critical` | `90` | CPU percentage that turns the value critical-colored. |
| `ram.warning` | `70` | RAM percentage for warning color. |
| `ram.critical` | `90` | RAM percentage for critical color. |
| `gpu.warning` | `70` | GPU percentage for warning color. |
| `gpu.critical` | `90` | GPU percentage for critical color. |

Unset (or zero) values fall back to 70/90. Disk usage is not configurable: it uses a fixed 80/95 pair, since `df` capacity normally sits high and the shared defaults would flag every healthy host.

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

Excluded hosts stay fully usable for `rr run`, `rr exec` and `rr sync`. Exclusion applies after `--hosts` filtering, and `--hosts` wins, so you can still pull up an excluded host on demand:

```bash
rr monitor                          # staging-server hidden
rr monitor --hosts=staging-server   # staging-server shown
```

If the exclude list empties the host set, the command errors instead of opening an empty dashboard.

### Alerts

Alerting is off by default. Turn it on to get notified when a host crosses its critical threshold.

```yaml
monitor:
  alerts:
    enabled: true
    bell: true
    flash: true
    cooldown: 5m
    on_alert: 'terminal-notifier -title "rr" -message "$RR_HOST $RR_METRIC at $RR_VALUE%"'
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Turns threshold alerting on. |
| `bell` | bool | `true` | Rings the terminal bell when an alert fires. |
| `flash` | bool | `true` | Draws the alerting host's card border in the critical color. |
| `cooldown` | duration | `60s` | Minimum time between re-fires for the same host and metric. Use `0s` to fire on every crossing. |
| `on_alert` | string | `""` | Shell command run locally when an alert fires. |

**When an alert fires:** a metric fires once when it crosses its `critical` threshold, and does not fire again until it drops back below `warning`. That hysteresis keeps a host hovering at the critical line from alerting on every sample. The cooldown is a second guard on top of it.

The header shows a count of active alerts whenever anything is firing, regardless of the `bell` and `flash` settings.

**`on_alert` hook:** runs on your machine (the one running `rr`), not the remote host, via `sh -c`. `rr` sets these variables in the hook's environment (note that `RR_HOST` here is the alerting host, unrelated to the `RR_HOST` input `rr init` reads):

| Variable | Value |
|----------|-------|
| `RR_HOST` | Name of the host that alerted |
| `RR_METRIC` | `cpu`, `ram`, or `gpu` |
| `RR_VALUE` | The metric value, to one decimal place |

Hook failures are ignored. There's nowhere safe to print an error inside a full-screen TUI, and a broken hook shouldn't take down the dashboard, so test your command outside `rr monitor` first.

## Environment variables

These environment variables affect `rr` behavior:

| Variable | Description |
|----------|-------------|
| `RR_HOST` | SSH host for `rr init` (non-interactive mode). |
| `RR_HOST_NAME` | Friendly name for the host in `rr init`. |
| `RR_REMOTE_DIR` | Remote directory path for `rr init`. |
| `RR_NON_INTERACTIVE` | Set to `true` to skip prompts in `rr init`. A non-empty `CI` variable does the same. |
| `RR_NO_UPDATE_CHECK` | Set to `1` to disable automatic update checks. |
| `RR_DEBUG` | Set to any value to print debug logs (lock acquisition and similar internals). |

**Example: non-interactive setup in CI**

```bash
export RR_HOST=user@build-server
export RR_HOST_NAME=ci-runner
export RR_REMOTE_DIR=/home/ci/projects/\${PROJECT}
export RR_NON_INTERACTIVE=true
rr init
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
| "No hosts configured" | Add at least one host to `~/.rr/config.yaml` or run `rr host add` |
| "Project references host 'X' which doesn't exist in global config" | The host referenced in `.rr.yaml` doesn't exist in `~/.rr/config.yaml` |
| "host 'X' needs at least one SSH connection" | Add `ssh:` list to the host in global config |
| "host 'X' needs a 'dir'" | Add `dir:` to the host in global config |
| "Can't use 'X' as a task name - that's a built-in command" | Rename the task to avoid built-in command names |
| "task 'X' has both 'run' and 'steps'" | Use either `run` or `steps`, not both |
| "task 'X' depends on non-existent task 'Y'" | Add the missing task or fix the dependency reference |
| "task 'X' can't depend on itself" | Remove self-reference from depends list |
| "circular dependency detected: A -> B -> A" | Break the cycle by removing one of the dependencies |
| "task 'X' has both 'parallel' and 'depends'" | Parallel tasks can't have dependencies; use depends inside subtasks instead |
| "parallel task 'X' references non-existent task 'Y'" | Add the missing subtask or fix the reference |
| "task 'X' has output 'Y' but it needs to be one of: ..." | Use `progress`, `stream`, `verbose`, or `quiet` |
| "Config file has some issues" | A value has the wrong shape, e.g. an unknown `local_fallback` mode. Use `never`, `on-unreachable`, `always`, or a boolean there. |

Unknown keys don't fail validation, but each one produces a config warning naming the key and the file, so a misspelled key doesn't silently do nothing. Removed keys (`output:`, the global `defaults.host`) and settings that have no effect where they're placed (`pull:` on a parallel task, `output:` on a non-parallel task) warn the same way. Warnings are emitted once per command: a `config` phase event with `status: "warn"` in structured mode, or a styled warning with `--pretty`. The config still loads.

## Minimal config

The smallest working setup requires a global config with at least one host:

```yaml
# ~/.rr/config.yaml
version: 1

hosts:
  myhost:
    ssh:
      - myserver.example.com
    dir: ${HOME}/projects/${PROJECT}
```

A project config (`.rr.yaml`) is optional. If not present, `rr` uses all hosts from your global config with default sync/lock settings.

```yaml
# .rr.yaml (optional, for project-specific settings)
version: 1

sync:
  exclude:
    - .git
    - node_modules
```

Everything else uses sensible defaults.
