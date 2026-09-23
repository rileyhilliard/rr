# Commands Reference

For the most current flags and options, run `rr --help` or `rr <command> --help`.

## Global Flags

These work with any command:

- `--pretty` / `-p` - Human-readable output with spinners and colors (default is structured JSON)
- `--machine` / `-m` - No-op, kept for backward compatibility (structured output is already the default)
- `--no-phases` - Suppress intermediate phase events on stderr; the final result event is still emitted
- `--config <path>` - Use a specific project config instead of discovering `.rr.yaml`
- `--no-strict-host-key-checking` - Skip SSH host key verification (CI only)
- `--no-color` - Disable colored output
- `-q` / `--quiet` - Suppress non-essential output
- `-v` / `--verbose` - Verbose output

## Core Commands

### `rr run "cmd"`

Sync files, then run command on remote host. Emits JSON phase events to stderr by default; use `--pretty` for spinners. Runs in the remote equivalent of your current subdirectory.

```bash
rr run "make test"
rr run "npm run build"
rr run --host mini "cargo test"
rr run --cwd . "make"              # Run at the project root from a subdirectory
rr run --pretty "make test"        # Human-readable output
rr run --repeat 5 "pytest tests/"  # Run 5x across hosts for flake detection
rr run --pull "coverage.xml" "pytest --cov --cov-report=xml"
```

**Flags:**
- `--host <name>` - Target specific host
- `--tag <tag>` - Select host by tag
- `--probe-timeout <duration>` - SSH probe timeout (e.g., `5s`)
- `--local` - Force local execution
- `--cwd <dir>` - Directory to run in, relative to the project root (can't escape it)
- `--skip-requirements` - Skip requirement checks
- `--repeat <N>` - Run command N times in parallel across available hosts (flake detection)
- `--pull <pattern>` - Pull files back after the command, even if it failed (repeatable)
- `--pull-dest <dir>` - Local destination for `--pull`
- `--tail <N>` - Reprint the last N lines of the run log after the result

### `rr exec "cmd"`

Run command on remote host without syncing files first.

```bash
rr exec "ls -la"
rr exec "git status"
rr exec --host server "cat /var/log/app.log"
```

**Flags:** Same as `run`, except `--repeat`

### `rr sync`

Sync files to remote host without running a command.

```bash
rr sync
rr sync --dry-run
rr sync --host mini
```

**Flags:**
- `--host <name>` - Target specific host
- `--tag <tag>` - Select host by tag
- `--probe-timeout <duration>` - SSH probe timeout
- `--dry-run` - Show what would be synced

### `rr <taskname>`

Run a named task from `.rr.yaml`.

```bash
rr test
rr build
rr test tests/test_api.py  # Extra args appended to single-command tasks
rr test -- -k login -x     # Flag-like args go after --
```

**Single-command and multi-step task flags:** `--host`, `--tag`, `--probe-timeout`, `--local`, `--repeat`, `--tail`, plus `--skip-deps` and `--from <task>` when the task has `depends`. Tasks don't take `--cwd`, `--pull`, or `--skip-requirements`; they always run from the project root.

**Parallel task flags:** `--host`, `--tag`, `--local`, `--stream`, `--verbose`, `--quiet`, `--fail-fast`, `--max-parallel N`, `--no-logs`, `--dry-run`.

Run `rr <task> --help` to see a task's type, command or subtasks, and flags.

### `rr tasks`

List all available tasks.

```bash
rr tasks
rr tasks --json
```

## Host Management

### `rr host list`

List configured hosts.

```bash
rr host list
rr host list --json
```

### `rr host add`

Add a new host interactively.

```bash
rr host add
rr host add --skip-probe
```

**Non-interactive mode:**
```bash
rr host add --name dev-box \
  --ssh "dev.local,dev-tailscale" \
  --dir '~/projects/${PROJECT}' \
  --tag fast \
  --env "DEBUG=1"
```

### `rr host remove`

Remove a host from config.

```bash
rr host remove myserver
rr host rm old-machine
```

## Diagnostics & Monitoring

### `rr doctor`

Run diagnostic checks.

```bash
rr doctor
rr doctor --fix           # Auto-fix fixable issues
rr doctor --requirements  # Check requirement status
rr doctor --path          # Compare login vs interactive shell PATH on hosts
rr doctor --pretty        # Human-readable report (default is a JSON envelope)
```

Doctor exits 0 even when checks fail. Read `data.summary.all_clear` in the JSON output.

### `rr monitor`

TUI dashboard showing CPU/RAM/GPU metrics.

```bash
rr monitor
rr monitor --hosts mini,workstation
rr monitor --interval 5s
rr monitor --once          # One snapshot as a table, no TUI
rr monitor --once --json   # One snapshot as JSON (for scripts and agents)
```

**Keyboard shortcuts:**
- `q` / `Ctrl+C` - Quit
- `r` - Force refresh
- `s` - Cycle sort order
- Arrow keys / `hjkl` - Move selection
- `Enter` / `Esc` - Open / close host detail view
- `p` - Cycle process sort (detail view)
- `?` - Show help

### `rr status`

Show connection and sync status.

```bash
rr status
rr status --json
```

Shows each host's alias probe results, which host would be selected, and which remote directory the current tree syncs to. Output is a JSON envelope by default.

## Setup & Utilities

### `rr init`

Create `.rr.yaml` configuration.

```bash
rr init
rr init --host myserver
rr init --force
rr init --non-interactive --host user@server
```

**Flags:**
- `--host <host>` - SSH host
- `--remote-dir <path>` - Remote directory
- `--name <name>` - Friendly host name
- `--force` - Overwrite existing config
- `--non-interactive` - Skip prompts
- `--skip-probe` - Skip SSH testing

### `rr pull`

Pull files from remote host to local machine.

```bash
rr pull "build/output.bin"
rr pull "logs/*.log" --dest ./local-logs
rr pull --dry-run "dist/"
```

**Flags:** `--host`, `--tag`, `--probe-timeout`, `--dest <dir>`, `--dry-run`

### `rr setup <host>`

Configure SSH keys and test connection.

```bash
rr setup myserver
rr setup user@192.168.1.100
```

### `rr unlock`

Force-release the lock on a remote host. The holder refreshes its lock every 30 seconds; a lock that stops being refreshed goes stale after `lock.stale` and is reclaimed automatically. Locks held by a dead rr process on your own machine are reclaimed right away.

```bash
rr unlock              # Only host (with several hosts: picker in --pretty mode, error otherwise)
rr unlock dev-box      # Specific host
rr unlock --all        # The project's hosts (all global hosts outside a project)
```

### `rr prune`

Remove remote `<repo>@<worktree>` directories whose git worktree no longer exists locally. Syncs already do this for the host they sync to (`sync.prune_worktrees`, default `true`).

```bash
rr prune                 # Every project host
rr prune --dry-run       # List what would be removed
rr prune --host m4-mini  # One host
```

### `rr provision`

Install tools listed in `require` that are missing on hosts.

```bash
rr provision              # Check and install on all project hosts
rr provision --check      # Report only
rr provision --host mini  # One host
rr provision --yes        # Skip confirmation prompts
```

### `rr logs`

Run logs live in `~/.rr/logs/<name>-<timestamp>/` (single runs write `output.log`; parallel runs write one file per subtask plus `summary.json`).

```bash
rr logs                    # List recent log directories
rr logs clean              # Apply the retention policy
rr logs clean --older 7d   # Delete logs older than a duration
rr logs clean --all        # Delete all logs
```

### `rr update`

Update rr to latest version.

```bash
rr update
rr update --check   # Only check for a newer version
rr version
```

### `rr completion <shell>`

Generate shell completion script.

```bash
rr completion bash > /etc/bash_completion.d/rr
rr completion zsh > "${fpath[1]}/_rr"
rr completion fish > ~/.config/fish/completions/rr.fish
```
