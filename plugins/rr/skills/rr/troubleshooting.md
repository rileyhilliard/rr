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

### SSH Works Manually but rr Fails (macOS)

**Symptoms:** `ssh <host>` works, but rr shows "encrypted keys" or auth errors

**Cause:** rr uses Go's SSH library which can't access macOS Keychain. The `UseKeychain yes` setting in `~/.ssh/config` only works for the native ssh command.

**Fix:** Configure SSH to automatically load keys into the agent:

```bash
# Add to ~/.ssh/config (under Host *)
Host *
  AddKeysToAgent yes
  UseKeychain yes
```

Then load your keys once:
```bash
# Add keys to agent with Keychain storage
ssh-add --apple-use-keychain ~/.ssh/id_ed25519
ssh-add --apple-use-keychain ~/.ssh/id_rsa
# Add any host-specific keys too
ssh-add --apple-use-keychain ~/.ssh/id_rsa_myhost
```

**Why this works:** `AddKeysToAgent yes` tells ssh to add keys to the agent after unlocking them. Once in the agent, rr (and any Go-based SSH tool) can use them. The Keychain stores the passphrase, so keys persist across restarts.

**Security notes:**
- This is Apple's recommended approach - the Keychain is encrypted with your login credentials
- Only the passphrase is stored, not the private key
- Keys remain in agent memory while logged in - standard trade-off for convenience
- For higher security needs:
  - Use hardware keys (YubiKey) instead of file-based keys
  - Set agent timeout: `ssh-add -t 3600 <key>` (expires after 1 hour)
  - Use `AddKeysToAgent confirm` to prompt before each use
  - Skip Keychain and enter passphrase manually each session

**Verify:**
```bash
ssh-add -l  # Should list your keys
rr doctor   # Should show SSH OK
```

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

**Fix:** Add exclusions to `.rr.yaml`:
```yaml
sync:
  exclude:
    - .git/
    - node_modules/
    - .venv/
    - __pycache__/
    - target/          # Rust
    - build/           # Various
    - dist/
    - .next/           # Next.js
    - .turbo/          # Turborepo
```

Test with dry-run:
```bash
rr sync --dry-run
```

### Stuck Lock

**Symptoms:** "Lock held by..." error

**Fix:**
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
