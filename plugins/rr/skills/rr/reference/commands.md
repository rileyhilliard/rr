# Commands Reference

## Core Commands

### `rr run "cmd"`

Sync files, then run command on remote host.

```bash
rr run "make test"
rr run "npm run build"
rr run --host mini "cargo test"
rr run --repeat 5 "pytest tests/"  # Run 5x across hosts for flake detection
```

**Flags:**
- `--host <name>` - Target specific host
- `--tag <tag>` - Select host by tag
- `--probe-timeout <duration>` - SSH probe timeout (e.g., `5s`)
- `--local` - Force local execution
- `--skip-requirements` - Skip requirement checks
- `--repeat <N>` - Run command N times in parallel across available hosts (flake detection)

### `rr exec "cmd"`

Run command on remote host without syncing files first.

```bash
rr exec "ls -la"
rr exec "git status"
rr exec --host server "cat /var/log/app.log"
```

**Flags:** Same as `run`, plus `--skip-requirements`

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
- `--dry-run` - Show what would be synced

### `rr <taskname>`

Run a named task from `.rr.yaml`.

```bash
rr test
rr build
rr test -v  # Extra args appended to single-command tasks
```

**Flags:** Same as `run`

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
rr doctor --machine       # JSON output
```

### `rr monitor`

TUI dashboard showing CPU/RAM/GPU metrics.

```bash
rr monitor
rr monitor --hosts mini,workstation
rr monitor --interval 5s
```

**Keyboard shortcuts:**
- `q` / `Ctrl+C` - Quit
- `r` - Force refresh
- `s` - Cycle sort order
- `?` - Show help

### `rr status`

Show connection and sync status.

```bash
rr status
rr status --machine
```

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

### `rr setup <host>`

Configure SSH keys and test connection.

```bash
rr setup myserver
rr setup user@192.168.1.100
```

### `rr clean`

Remove stale per-branch directories from remote hosts.

When using `${BRANCH}` in a host's `dir` template (e.g., `~/rr/${PROJECT}-${BRANCH}`), directories accumulate as branches are merged or deleted. This command finds directories on remotes that no longer correspond to a local branch and offers to remove them.

```bash
rr clean              # Interactive: discover and prompt for removal
rr clean --dry-run    # Show what would be removed without deleting
rr clean --host mini  # Clean only a specific host
```

**Flags:**
- `--dry-run` - Show what would be removed without deleting
- `--host <name>` - Clean only a specific host
- `--probe-timeout <duration>` - SSH probe timeout (e.g., `5s`)

Hosts without `${BRANCH}` in their `dir` template are automatically skipped.

### `rr unlock`

Release stuck lock on remote host.

```bash
rr unlock              # Default host
rr unlock dev-box      # Specific host
rr unlock --all        # All configured hosts
```

### `rr update`

Update rr to latest version.

```bash
rr update
```

### `rr completion <shell>`

Generate shell completion script.

```bash
rr completion bash > /etc/bash_completion.d/rr
rr completion zsh > "${fpath[1]}/_rr"
rr completion fish > ~/.config/fish/completions/rr.fish
```
