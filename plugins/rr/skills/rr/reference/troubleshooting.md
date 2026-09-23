# Troubleshooting

## Quick Diagnostics

```bash
rr doctor           # Full diagnostic (JSON; add --pretty for a readable report)
rr host list        # See configured hosts
rr status           # Check connectivity per SSH alias
```

`rr doctor` exits 1 when a check fails and 0 when there are only warnings. Details are in `data.categories[].results[]`; `data.summary.all_clear` is true only with no failures or warnings. Inside a project it checks only the project's hosts.

## Common Issues

### SSH Connection Fails

**Symptoms:** Timeout, connection refused, auth failed

**Diagnose:**
```bash
# Test manual SSH
ssh <host-alias>

# Check SSH config
grep -A5 "<host-alias>" ~/.ssh/config

# Verbose SSH for details
ssh -vv <host-alias>
```

**Common fixes:**
- Password prompt → needs key-based auth
- Host not found → add to `~/.ssh/config`
- Timeout → host unreachable, try alternative address
- Permission denied → run `ssh-copy-id <host-alias>`
- `SSH_AUTH_FAILED` but `ssh` works → passphrase-protected key not in the agent: `ssh-add ~/.ssh/id_ed25519`
- `SSH_HOST_KEY` → rr checks `~/.ssh/known_hosts` and never prompts; accept the key once with `ssh -o StrictHostKeyChecking=accept-new <host-alias> exit`
- `ProxyJump` isn't supported; rr warns and suggests an equivalent `ProxyCommand`

### "command not found" Errors

**Symptoms:** Tool exists but rr can't find it

**Causes:**
1. PATH not set in non-interactive shell
2. Tool installed in non-standard location
3. Tool requires sourcing (like nvm, pyenv)

**Fixes:**

rr runs commands with `${SHELL:-/bin/bash} -c` after sourcing `~/.bashrc` and `~/.zshrc`. Tools set up in login files (`~/.zprofile`, `~/.bash_profile`, e.g. Homebrew's `shellenv`) or behind an "interactive only" guard in `.bashrc` won't be on PATH. `rr doctor --path` compares login and interactive PATH on each host.

Use a login shell for the host:
```yaml
# ~/.rr/config.yaml
hosts:
  dev-box:
    shell: "zsh -l -c"
```

Or add `setup_commands` to global config:
```yaml
# ~/.rr/config.yaml
hosts:
  dev-box:
    setup_commands:
      - source ~/.local/bin/env     # uv
      - export PATH="$HOME/.bun/bin:$PATH"
      - source ~/.nvm/nvm.sh        # nvm
```

Or use `require` field to verify tools exist:
```yaml
# .rr.yaml
require: [go, node, python3]
```

### Sync is Slow

**Cause:** Syncing large directories

**Fix:** rr now respects `.gitignore` by default (`respect_gitignore: true`), so most generated directories are already excluded. For additional exclusions, add to `.rr.yaml`:
```yaml
sync:
  exclude:
    - target/          # Rust
    - build/           # Various
    - dist/
    - .next/           # Next.js
    - .turbo/          # Turborepo
```

Default excludes already include `.git`, `.venv`, `node_modules`, `__pycache__/`, `.claude/`, `.cursor/`, `.aider/`, `.copilot/`, and others. A custom `exclude` list replaces the defaults, so include them in yours.

In a linked git worktree the first sync is a full copy, because each worktree gets its own remote dir (`<repo>@<worktree>`).

Test with dry-run:
```bash
rr sync --dry-run
```

### Stuck Lock

**Symptoms:** `LOCK_HELD`, "Lock timeout after 5m0s - someone else is using this remote", or "All hosts are locked - timed out after 1m0s". The message names the holder (user, pid, command, age).

There's one lock per host, shared across projects, so another project's run on the same host blocks you. The holder refreshes the lock every 30 seconds. A lock that stops being refreshed goes stale after `lock.stale` (default 90s) and is reclaimed automatically. A lock left by a dead rr process on your own machine is reclaimed immediately.

**Manual fix** (only when the holder is gone):
```bash
rr unlock <hostname>   # Specific host
rr unlock --all        # All project hosts
rr unlock              # Works without a name only if one host is configured
```

### Wrong Remote Directory

**Symptoms:** Files in wrong location, "No such file or directory"

**Check:**
```bash
rr status                        # Shows which remote dir this tree syncs to
rr exec --cwd . "pwd"
cat ~/.rr/config.yaml | grep -A5 "dir:"
```

`rr run`/`rr exec` run in the remote equivalent of your current subdirectory. From `backend/`, `rr run "cat README.md"` reads `backend/README.md`. Use `--cwd .` for the project root. When a relative path fails for this reason, the result event's `details.hint` says so.

**Fix:** Update `dir` in global config:
```yaml
hosts:
  mini:
    dir: ${HOME}/projects/${PROJECT}  # Use variables
```

### Requirements Check Fails

**Symptoms:** "Missing required tools: ..." error (code `DEPENDENCY_MISSING`; `COMMAND_FAILED` on rr binaries before this release)

**Options:**
1. Install them: `rr provision` (or `rr provision --yes`)
2. Skip checks: `rr run --skip-requirements "..."` (only `run` and `exec` have this flag)
3. Remove from `require` list

**Check which tools are missing:**
```bash
rr doctor --requirements
```

### Local Fallback Not Working

**Symptoms:** Command fails when no hosts available

`local_fallback` takes `never` (default), `on-unreachable`, or `always` (`true` means `always`, `false` means `never`). `on-unreachable` runs locally only when no host can be reached. `always` also falls back when every host stays locked past `lock.wait_timeout`.

**Check config:**
```yaml
# ~/.rr/config.yaml
defaults:
  local_fallback: on-unreachable
```

Or in project config (overrides global):
```yaml
# .rr.yaml
local_fallback: on-unreachable
```

A local fallback shows up as a `connect` phase event with `"status":"warn"` and `details.reason` (`hosts_unreachable` or `all_hosts_locked`), plus `details.fallback` on the result. A deliberate local run is a normal `connect` `complete` event with `host: local` and `details.reason` of `local_flag` (`--local`) or `local_mode` (`local_fallback` on with no hosts listed). Neither needs any host configured.

### Task Args Rejected

| Error | Fix |
|-------|-----|
| "rr parses flags before the task sees them" | Put task args after `--`: `rr test -- -k foo` |
| "parallel task '...' doesn't accept extra arguments" (`CONFIG_INVALID`) | Set `forward_args: true` on the task and pass args after `--`, or use `rr run "<cmd> <args>"` |
| "This task is a compound command ..." | Add an `{args}` placeholder to the task's `run` |
| "Can't pass arguments to multi-step tasks" | Use `rr run` for a one-off command |

### Tests Pass but Nothing Ran

Check `details.no_tests` on the result event. A `-k`/`-run`/path filter that matches nothing makes some runners exit 0. If `details.piped_exit_code` is also set, the command pipes into something like `tail`, and the exit code came from the last stage; set `shell: "bash -o pipefail -c"` on the host.

## Diagnostic Commands

| Command | Purpose |
|---------|---------|
| `rr doctor` | Full diagnostic |
| `rr doctor --fix` | Auto-fix fixable issues |
| `rr doctor --requirements` | Check requirement status |
| `rr doctor --path` | Compare login vs interactive shell PATH |
| `rr status` | Host connectivity |
| `rr sync --dry-run` | Preview sync |
| `rr exec "env"` | Check remote environment |

## Debug Checklist

1. **Is SSH working?** `ssh <alias>` should connect without password
2. **Is config valid?** `rr doctor` shows no config errors
3. **Is the host reachable?** `ping <hostname>` or `rr status`
4. **Are tools available?** `rr exec "command -v <tool>"`
5. **Is PATH correct?** `rr exec 'echo $PATH'` (single quotes, so your local shell doesn't expand it)
6. **Is there a lock?** The lock error names the holder; `rr unlock <host>` if it's gone
7. **What did the run print?** Read `details.log_file` from the result event

## Getting Help

```bash
rr --help           # General help
rr <command> --help # Command-specific help
```

Report issues: [GitHub Issues](https://github.com/rileyhilliard/rr/issues)
