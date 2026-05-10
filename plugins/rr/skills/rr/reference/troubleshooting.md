# Troubleshooting

## Quick Diagnostics

```bash
rr doctor           # Full diagnostic
rr host list        # See configured hosts
rr status           # Check connectivity
```

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

### "command not found" Errors

**Symptoms:** Tool exists but rr can't find it

**Causes:**
1. PATH not set in non-interactive shell
2. Tool installed in non-standard location
3. Tool requires sourcing (like nvm, pyenv)

**Fixes:**

Add `setup_commands` to global config:
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

Default excludes already include `.git/`, `.claude/`, `.cursor/`, `.aider/`, `.copilot/`, `.venv/`, `node_modules/`, `__pycache__/`, and others.

Test with dry-run:
```bash
rr sync --dry-run
```

### Stuck Lock

**Symptoms:** "Lock held by..." error

Locks have a heartbeat mechanism and auto-expire after 3 minutes without a heartbeat update. If a lock is stuck (process crashed without cleanup), it will be automatically reclaimed after the stale timeout.

**Manual fix:**
```bash
rr unlock              # Default host
rr unlock <hostname>   # Specific host
rr unlock --all        # All hosts
```

### Wrong Remote Directory

**Symptoms:** Files in wrong location, "directory not found"

**Check:**
```bash
rr exec "pwd"
cat ~/.rr/config.yaml | grep -A5 "dir:"
```

**Fix:** Update `dir` in global config:
```yaml
hosts:
  mini:
    dir: ${HOME}/projects/${PROJECT}  # Use variables
```

### Requirements Check Fails

**Symptoms:** "Missing required tools" error

**Options:**
1. Install the missing tools
2. Skip checks: `rr run --skip-requirements "..."`
3. Remove from `require` list

**Check which tools are missing:**
```bash
rr doctor --requirements
```

### Local Fallback Not Working

**Symptoms:** Command fails when no hosts available

**Check config:**
```yaml
# ~/.rr/config.yaml
defaults:
  local_fallback: true
```

Or in project config:
```yaml
# .rr.yaml
local_fallback: true
```

## Diagnostic Commands

| Command | Purpose |
|---------|---------|
| `rr doctor` | Full diagnostic |
| `rr doctor --fix` | Auto-fix fixable issues |
| `rr doctor --requirements` | Check requirement status |
| `rr status` | Host connectivity |
| `rr sync --dry-run` | Preview sync |
| `rr exec "env"` | Check remote environment |

## Debug Checklist

1. **Is SSH working?** `ssh <alias>` should connect without password
2. **Is config valid?** `rr doctor` shows no config errors
3. **Is the host reachable?** `ping <hostname>` or `rr status`
4. **Are tools available?** `rr exec "command -v <tool>"`
5. **Is PATH correct?** `rr exec "echo $PATH"`
6. **Is there a lock?** `rr unlock` if stuck

## Getting Help

```bash
rr --help           # General help
rr <command> --help # Command-specific help
```

Report issues: [GitHub Issues](https://github.com/rileyhilliard/rr/issues)
