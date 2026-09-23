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

rr syncs code to remote machines and runs commands there. Handles host failover, file sync with rsync, distributed locking, and test output parsing.

## Quick Reference

```bash
rr run "make test"     # Sync files + run command
rr exec "git status"   # Run command without syncing
rr sync                # Just sync files
rr <taskname>          # Run named task from config
rr tasks               # List tasks
rr provision           # Install missing tools on hosts
rr doctor              # Diagnose issues
rr unlock <host>       # Release a stuck lock
rr monitor --once --json  # One-shot host metrics snapshot
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

tasks:
  test:
    run: pytest -v
```

rr ships default sync excludes (`.git`, `.venv`, `node_modules`, caches, agent dirs like `.claude/`) and applies `.gitignore`. A custom `sync.exclude` list **replaces** the defaults, so copy them in if you set one.

## Commands Overview

| Command | Purpose |
|---------|---------|
| `rr run "cmd"` | Sync files, then run command |
| `rr exec "cmd"` | Run command without syncing |
| `rr sync` | Just sync files |
| `rr pull <pattern>` | Download files from the remote |
| `rr <taskname>` | Run named task |
| `rr tasks` | List available tasks |
| `rr status` | Host connectivity and which remote dir this tree syncs to |
| `rr provision` | Install missing tools on hosts |
| `rr doctor` | Diagnose issues |
| `rr unlock [host]` / `rr unlock --all` | Release stuck locks |
| `rr prune` | Remove remote dirs left by deleted git worktrees |
| `rr host list/add/remove` | Manage hosts |

**See [commands.md](reference/commands.md) for full command reference.**

### Common Flags

- `--host <name>` - Target specific host
- `--tag <tag>` - Select host by tag
- `--local` - Force local execution
- `--cwd <dir>` - (`run`/`exec`) Directory to run in, relative to the project root
- `--tail N` - (`run`/`exec`/single-command tasks) Reprint the last N log lines after the result
- `--skip-requirements` - (`run`/`exec` only) Skip requirement checks
- `--no-phases` - Suppress intermediate phase events; the final result event is still emitted
- `--pretty` / `-p` - Human-readable output (spinners, colors). Default is structured JSON.

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

Named tasks always run from the project root, regardless of which subdirectory you invoke them from.

**See [tasks.md](reference/tasks.md) for parallel tasks, multi-step tasks, and dependencies.**

## Passing Arguments to Tasks

Tasks come in three types. The type determines whether extra args work:

| Type | Config field | Accepts args? | Example |
|------|-------------|---------------|---------|
| Single-command | `run:` | Yes | `rr test tests/test_api.py` |
| Multi-step | `steps:` | No (errors) | `rr deploy` |
| Parallel | `parallel:` | Only with `forward_args: true` | `rr test-all` |

Rules for single-command tasks:

- **Flag-like args need `--`.** rr parses flags before the task sees them, so `rr test -k foo` fails with "rr parses flags before the task sees them". Use `rr test -- -k foo`. Positional args (`rr test tests/foo.py`) work without it.
- **Args are shell-quoted.** `rr test -- -k "foo or bar"` arrives as one argument. Remote globs and `$VARS` in args are not expanded; put those in the task's `run` string.
- **Args are appended to the end** of the command, unless the command has an `{args}` placeholder. `{args:-default}` supplies a default when no args are given; `{{args}}` is a literal `{args}`.
- **Compound commands need a placeholder.** If `run` contains pipes, `&&`, `;`, redirections, `$()`, or backticks, passing args without an `{args}` placeholder is an error (otherwise the args would land on the last command in the pipeline).

```yaml
tasks:
  test:
    run: pytest {args:-tests/} -n 4 | tail -20   # rr test -- -k bond  =>  pytest -k bond -n 4 | tail -20
```

Parallel tasks reject args unless `forward_args: true` is set. With it, args are forwarded to every subtask (appended, or substituted into each subtask's `{args}`). Subtasks that are compound commands need an `{args}` placeholder, and multi-step subtasks can't take forwarded args. A forwarded test filter can leave some subtasks with zero matching tests (pytest exits 5).

```yaml
tasks:
  test-backend:
    parallel: [test-backend-api, test-backend-services]
    forward_args: true   # rr test-backend -- -k bond
```

**When a parallel task doesn't forward args, bypass it:**
```bash
rr run --cwd backend "uv run pytest tests/bond/ -v"
```

To check a task's type, subtasks, and flags: `rr <task> --help`. This is faster than reading `.rr.yaml`.

## Choosing the Right Command

```
Need to run something remotely?
├── Named task exists? → rr <task>
│   ├── Single-command task → rr <task> -- <args>
│   ├── Parallel task (forward_args: true) → rr <task> -- <args>
│   └── Parallel task (default) → rr run --cwd <dir> "<cmd> <args>"
├── No task, files may have changed → rr run "<command>"   (syncs first)
└── Files already synced → rr exec "<command>"   (faster, skips sync)
```

Quote the whole command: `rr run "make test"`. rr rejects `rr run <host> make test` and `rr run <task> -k foo` with an error that shows the correct form (`rr run --host <host> "..."` or `rr <task> -- ...`).

**Never nest rr inside rr exec.** rr may not be installed on the remote:
```bash
# Wrong:  rr exec "rr sync && pytest"
# Right:  rr run "pytest"
```

## Where Commands Run

- `rr run`/`rr exec` run in the remote equivalent of your **current subdirectory**. From `backend/`, `rr run "make"` uses `backend/Makefile`. Use `--cwd .` to run at the project root, or `--cwd <dir>` for another subdirectory. `details.remote_cwd` reports the subdirectory used.
- Absolute local paths under the project are rewritten to the remote project dir (`details.path_rewrites` counts them). Absolute paths outside the project draw a warning. Disable with `rewrite_paths: false`.
- In a linked git worktree, `${PROJECT}` expands to `<repo>@<worktree>`, so each worktree gets its own remote copy (and its own `node_modules`/`.venv`, which means a cold first sync).

## Reading rr Output

Output is structured by default. No flags needed.

- **stderr**: JSON phase events, one per line (`connect`, `lock`, `sync`, `exec`), then a final `{"type":"result",...}` line.
- **stdout/stderr**: the command's own output, passed through raw.
- **Exit code**: the remote command's exit code. If rr itself fails before the command runs (config, SSH, lock, sync, missing tools), it exits 1 and writes a JSON error envelope (`{"success":false,"error":{"code":...,"message":...,"suggestion":...}}`) to stderr instead of a result event.

```json
{"type":"result","status":"failed","exit_code":1,"host":"mini","duration_s":14.2,"details":{"exec_duration_s":11.8,"log_file":"/home/me/.rr/logs/test-20260101-120000/output.log","summary":{"passed":41,"failed":1,"skipped":0,"errors":0},"failures":[{"name":"test_login","file":"tests/test_auth.py:42","message":"AssertionError: ..."}]}}
```

Useful `details` keys on the result event:

| Key | Meaning |
|-----|---------|
| `summary` | Test counts (pytest, jest/vitest, go test only) |
| `failures` | Failed tests with `name`, `file` (file:line), `message` |
| `no_tests` | The runner reported collecting zero tests. Exit code may still be 0. |
| `piped_exit_code` | Zero tests plus a pipe: the exit code is the last pipeline stage's, not the runner's |
| `log_file` | Full raw output (`~/.rr/logs/...`). Read it instead of rerunning. |
| `hint` | Explanation of a likely local-vs-remote path mistake |
| `fallback` | Ran locally because all hosts were locked (reason, wait time, lock holders) |
| `path_rewrites` | Number of local paths rewritten to remote paths |
| `remote_cwd` | Subdirectory the command ran in |

Parallel tasks print nothing on stdout by default (only with `--stream` or `--verbose`). The result event has `total`, `passed`, `failed`, `log_dir`, and `failures` (per subtask: `task`, `host`, `exit_code`, `log_file`, parsed test failures or an `output_tail`). Use `--stream` to see live output prefixed with `[host:task]`.

```bash
rr test 2>/dev/null          # command output only
rr test --no-phases          # keep only the final result JSON on stderr
rr test --tail 50            # reprint the last 50 log lines after the result
```

**See [machine-interface.md](reference/machine-interface.md) for the event schema and error codes.**

## Pitfalls

- **Pipes hide failures.** Without `pipefail`, `pytest | tail` exits with `tail`'s status. Set `shell: "bash -o pipefail -c"` on the host, or check `details.summary`/`failures`.
- **Zero tests isn't success.** Check `details.no_tests` after narrowing with `-k`, `-run`, or paths.
- **Locks are per host, shared across projects.** A run from another project on the same host blocks you. With several hosts, rr tries the next free one. If all are locked it waits up to `lock.wait_timeout` (1m) for one to free up, then fails, or runs locally when `local_fallback: always`. With one host it waits up to `lock.timeout` (5m).
- **`rr unlock` with no host only works when one host is configured.** With several, the host picker only appears in `--pretty` mode; otherwise pass a name (`rr unlock mini`) or `--all`.
- **Custom `sync.exclude` replaces the defaults.** Include `.git`, `node_modules`, `.venv` yourself.
- **Relative paths follow your cwd** for `run`/`exec` (see Where Commands Run). If a path fails, read `details.hint`.

## Remote Environment Bootstrap

Declare required tools with `require:`. rr checks they exist before syncing and fails with "Missing required tools: ..." if not:

```yaml
# .rr.yaml
require: [go, node]

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

`rr provision` installs missing tools that have built-in installers (40+). `rr run/exec --skip-requirements` skips the check.

**See [requirements.md](reference/requirements.md) for complete requirements reference.**

## Parallel Tasks and Dependencies

```yaml
tasks:
  test-all:
    setup: pip install -r requirements.txt   # Runs once per host before subtasks
    parallel: [test-unit, test-integration, test-e2e]
    fail_fast: false

  ci:
    depends:
      - parallel: [lint, typecheck]  # Run simultaneously
      - test                         # Then this
```

Parallel task flags: `--stream`, `--verbose`, `--quiet`, `--fail-fast`, `--max-parallel N`, `--no-logs`, `--dry-run`, `--local`, `--host`, `--tag`. `--dry-run` shows the flattened subtask list and commands.

Dependency flags (tasks with `depends`): `--skip-deps` runs only the target task, `--from <task>` starts partway through the chain.

`--repeat N` on `rr run` or a single-command task runs it N times in parallel across hosts for flake detection.

**See [tasks.md](reference/tasks.md) for details.**

## How It Works

1. **Host selection**: Tries hosts in order; for each host, races its SSH aliases (earlier aliases preferred)
2. **Locking**: Takes a lock on the host; if it's locked, tries the next host
3. **Requirements**: Verifies required tools exist (if configured)
4. **File sync**: rsync with exclude/preserve patterns
5. **Execution**: Runs the command with configured env and setup commands, then releases the lock

## Troubleshooting

| Problem | Fix |
|---------|-----|
| SSH fails | Check `ssh <alias>` manually, verify `~/.ssh/config` |
| `SSH_AUTH_FAILED` but `ssh` works | Key not in agent: `ssh-add ~/.ssh/id_ed25519`, add `AddKeysToAgent yes` to SSH config |
| `SSH_HOST_KEY` | Host not in `~/.ssh/known_hosts` yet: `ssh -o StrictHostKeyChecking=accept-new <alias> exit` |
| "command not found" | Add `setup_commands` or `shell: "zsh -l -c"` to the host; check `require` |
| Sync slow | Add large dirs to `sync.exclude` |
| `LOCK_HELD` / lock timeout | Wait, or `rr unlock <host>` / `rr unlock --all` if the holder is gone |
| Unknown task flag error | Put task args after `--` |

**See [troubleshooting.md](reference/troubleshooting.md) for detailed diagnostics.**

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

## Reference Files

- **[config.md](reference/config.md)** - Complete config reference (global + project)
- **[commands.md](reference/commands.md)** - All commands and flags
- **[tasks.md](reference/tasks.md)** - Task definitions, parallel execution, multi-step
- **[requirements.md](reference/requirements.md)** - Remote environment bootstrap
- **[machine-interface.md](reference/machine-interface.md)** - JSON output and error codes
- **[troubleshooting.md](reference/troubleshooting.md)** - Diagnostics and common fixes
