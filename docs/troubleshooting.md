# Troubleshooting

This guide covers common issues and their solutions.

## Contents

- [Running diagnostics](#running-diagnostics)
- [SSH connection failures](#ssh-connection-failures)
- [rsync issues](#rsync-issues)
- [Lock contention](#lock-contention)
- [Config validation errors](#config-validation-errors)
- [Platform-specific issues](#platform-specific-issues)
- [Debug tips](#debug-tips)

## Running diagnostics

The `rr doctor` command checks your setup and reports issues:

```bash
rr doctor           # Run all diagnostic checks
rr doctor --fix     # Attempt automatic fixes where possible
rr doctor --json    # Output diagnostics in JSON format (for scripts)
```

Example output:

```
CONFIG
  [PASS] config_file: Config file: .rr.yaml
  [PASS] config_schema: Schema valid
  [PASS] config_hosts: 2 hosts configured

SSH
  [PASS] ssh_key: SSH key found: ~/.ssh/id_ed25519.pub
  [PASS] ssh_agent: SSH agent running with 1 key loaded
  [PASS] ssh_key_permissions: SSH key permissions OK

HOSTS
  [PASS] host_mini: mini
  [FAIL] host_server: all aliases failed
         Suggestion: Host may be offline or blocked by firewall

DEPENDENCIES
  [PASS] rsync_local: rsync 3.2.7 (local)

1 issue found
```

## SSH connection failures

### "Connection refused"

**Symptom:** `rr doctor` shows "all aliases failed" with "connection refused"

**Causes and fixes:**

1. **SSH server not running on remote host**
   ```bash
   # Check if SSH is running (on the remote machine)
   sudo systemctl status sshd

   # Start it if needed
   sudo systemctl start sshd
   ```

2. **Wrong port**
   ```bash
   # If SSH runs on a non-standard port, specify it in ~/.ssh/config:
   Host myserver
       HostName server.example.com
       Port 2222
   ```

3. **Firewall blocking port 22**
   ```bash
   # Check if you can reach the port
   nc -zv myserver.example.com 22
   ```

### "Permission denied (publickey)"

**Symptom:** SSH connects but auth fails

**Fixes:**

1. **Check your SSH key is loaded**
   ```bash
   ssh-add -l
   # If empty, add your key:
   ssh-add ~/.ssh/id_ed25519
   ```

2. **Ensure your public key is on the remote**
   ```bash
   # Copy your key to the server
   ssh-copy-id user@myserver.example.com

   # Or use rr setup
   rr setup user@myserver.example.com
   ```

3. **Check key permissions**
   ```bash
   chmod 600 ~/.ssh/id_ed25519
   chmod 644 ~/.ssh/id_ed25519.pub
   ```

### "Connection timed out"

**Symptom:** SSH hangs then times out

**Causes:**

1. **Host is offline** - Wake or start the machine
2. **Network issue** - Check your connection (VPN, firewall, etc.)
3. **Wrong hostname/IP** - Verify the address in your config

**Increase probe timeout** if hosts are slow to respond (in `~/.rr/config.yaml`):

```yaml
defaults:
  probe_timeout: 10s
```

Or per-command:

```bash
rr run --probe-timeout 10s "make test"
```

### "handshake failed" but ssh command works

**Symptom:** `rr monitor` or `rr doctor` shows "handshake failed", but `ssh user@host` works fine from the terminal.

**Cause:** Your SSH key is passphrase-protected and not loaded in the agent. The `ssh` command can prompt for the passphrase or use macOS Keychain automatically, but rr's Go SSH library cannot prompt interactively.

**Fix:**

1. Add your key to the SSH agent:
   ```bash
   ssh-add ~/.ssh/id_rsa  # or your key file
   ```

2. Verify the key is loaded:
   ```bash
   ssh-add -l
   ```

3. To persist across reboots (macOS), add to your `~/.ssh/config`:
   ```
   Host your-host
       AddKeysToAgent yes
       UseKeychain yes
   ```

This ensures the key is automatically added to the agent and the passphrase is stored in Keychain.

### "SSH agent not running"

**Symptom:** `rr doctor` shows "SSH_AUTH_SOCK not set"

**Fix:**

```bash
# Start the agent and add your key
eval $(ssh-agent)
ssh-add
```

For persistent agent across terminal sessions, add to your shell profile (`~/.bashrc`, `~/.zshrc`):

```bash
# Start SSH agent if not running
if [ -z "$SSH_AUTH_SOCK" ]; then
    eval $(ssh-agent -s)
    ssh-add ~/.ssh/id_ed25519 2>/dev/null
fi
```

### "SSH config contains Match directive" warning

**Symptom:** Warning appears when connecting about unsupported `Match` directives

**Cause:** The SSH config parser doesn't support OpenSSH's `Match` directives. Host entries after the first `Match` line in your `~/.ssh/config` may not be recognized.

**Fixes:**

1. **Move important Host entries before the Match block** in `~/.ssh/config`:
   ```
   # These will be parsed
   Host myserver
       HostName 192.168.1.100
       User deploy

   # Everything after Match may not be recognized by rr
   Match host *.internal
       ProxyJump bastion
   ```

2. **Use explicit `user@hostname` format** in your global config instead of SSH aliases:
   ```yaml
   # ~/.rr/config.yaml
   hosts:
     myserver:
       ssh:
         - deploy@192.168.1.100  # Explicit format, doesn't need SSH config
       dir: ~/projects/${PROJECT}
   ```

### "command not found" on remote

**Symptom:** Commands like `go test` or `npm run` fail with "command not found" even though they work when you SSH manually.

**Cause:** SSH sessions don't source shell config files (`.zshrc`, `.bashrc`) by default, so tools installed via Homebrew or nvm aren't in PATH.

**Fixes:**

1. **Add shell config to your global config** (recommended):
   ```yaml
   # ~/.rr/config.yaml
   hosts:
     myserver:
       ssh:
         - user@server
       dir: ${HOME}/projects/${PROJECT}
       shell: "zsh -l -c"  # Use login shell for full PATH
   ```

2. **Or use setup_commands** for specific initialization:
   ```yaml
   # ~/.rr/config.yaml
   hosts:
     myserver:
       setup_commands:
         - source ~/.nvm/nvm.sh  # Load nvm
   ```

3. **Or source manually** in the command:
   ```bash
   rr run "source ~/.zshrc && go test ./..."
   ```

## rsync issues

### "rsync not found locally"

**Fix (macOS):**
```bash
brew install rsync
```

**Fix (Ubuntu/Debian):**
```bash
sudo apt install rsync
```

**Fix (Fedora/RHEL):**
```bash
sudo dnf install rsync
```

### "rsync not found on remote"

Install rsync on the remote host using the same commands above.

### "rsync: connection unexpectedly closed"

**Causes:**

1. **SSH connection dropped** - Check network stability
2. **Remote disk full** - Check disk space on remote
3. **Permission denied** - Check directory permissions

```bash
# Check remote disk space
rr exec "df -h"

# Check directory permissions
rr exec "ls -la \${HOME}/projects/"
```

### Sync is slow

1. **Use compression** for slow networks:
   ```yaml
   sync:
     flags:
       - --compress
   ```

2. **Check what's being synced**:
   ```bash
   rr sync --dry-run
   ```

3. **Exclude large directories** you don't need:
   ```yaml
   sync:
     exclude:
       - .git/
       - node_modules/
       - "*.zip"
       - build/
   ```

## Lock contention

### "Lock held by another process"

**Symptom:** Command waits or fails with lock timeout

**Causes:**

1. **Another `rr` instance is running** - Wait for it to finish
2. **Previous run crashed** - Lock is stale

**Check lock status:**

```bash
rr doctor
# Look for "stale locks" in the output
```

**Release a stuck lock:**

```bash
rr unlock              # Release lock (shows picker if multiple hosts)
rr unlock gpu-box      # Release lock on specific host
rr unlock --all        # Release locks on all configured hosts
```

The lock is project-specific (based on your current directory), so this only affects locks for your project.

**When do locks get stuck?**

- Process crashed or was killed (Ctrl+C during lock phase)
- Network disconnected during a run
- SSH connection dropped unexpectedly

Stale locks are automatically cleaned up after the configured `stale` duration (default: 1 hour), but you can use `rr unlock` if you need to release one immediately.

**Increase lock timeout** for long-running commands:

```yaml
lock:
  timeout: 30m
```

**Disable locking** if you're the only user:

```yaml
lock:
  enabled: false
```

## Config validation errors

### "No config file found"

**Fix:**

```bash
rr init
```

### "No hosts configured"

Add at least one host to your global config (`~/.rr/config.yaml`):

```yaml
# ~/.rr/config.yaml
hosts:
  myhost:
    ssh:
      - myserver.example.com
    dir: ${HOME}/projects/${PROJECT}
```

Or use the interactive command:

```bash
rr host add
```

### "Host 'X' not found in global config"

The host referenced in your project's `.rr.yaml` doesn't exist in `~/.rr/config.yaml`. Either:

1. Add the host to your global config with `rr host add`
2. Remove the reference from `.rr.yaml`
3. Check for typos in the host name

### "Host 'X' has no SSH aliases"

Each host needs at least one SSH connection string in the global config:

```yaml
# ~/.rr/config.yaml
hosts:
  myhost:
    ssh:
      - user@server.example.com  # Add this
    dir: ${HOME}/projects
```

### "Host 'X' has no dir"

Each host needs a working directory in the global config:

```yaml
# ~/.rr/config.yaml
hosts:
  myhost:
    ssh:
      - myserver.example.com
    dir: ${HOME}/projects/${PROJECT}  # Add this
```

### "Reserved task name"

You can't name a task after a built-in command. Rename your task:

```yaml
tasks:
  # Bad: "run" is reserved
  run:
    run: make run

  # Good: use a different name
  start:
    run: make run
```

## Platform-specific issues

### macOS

**"rsync version too old"**

macOS ships with an old rsync (2.x). Install a newer version:

```bash
brew install rsync
```

Then ensure `/opt/homebrew/bin` (Apple Silicon) or `/usr/local/bin` (Intel) is before `/usr/bin` in your PATH.

**SSH key not in keychain**

```bash
# Add key to macOS keychain
ssh-add --apple-use-keychain ~/.ssh/id_ed25519
```

Add to `~/.ssh/config` to auto-load from keychain:

```
Host *
    UseKeychain yes
    AddKeysToAgent yes
```

### Linux

**"Host key verification failed"**

The remote host's key changed or you're connecting for the first time:

```bash
# Accept the new host key (works with SSH config aliases)
ssh -o StrictHostKeyChecking=accept-new myserver exit

# Or connect manually to verify and accept interactively
ssh myserver
```

Note: `ssh-keyscan` won't work with SSH config aliases since it doesn't read `~/.ssh/config`. Use the `ssh` command instead.

**SELinux blocking SSH**

On systems with SELinux, check for denials:

```bash
sudo ausearch -m avc -ts recent
```

### Windows (WSL)

**SSH key permissions too open**

WSL doesn't enforce Unix permissions on Windows filesystems:

```bash
# Move keys to WSL filesystem
cp /mnt/c/Users/you/.ssh/* ~/.ssh/
chmod 600 ~/.ssh/id_*
chmod 644 ~/.ssh/*.pub
```

## Debug tips

### Verbose output

```bash
# See more detail about what rr is doing
rr run --verbose "make test"
```

### Test SSH directly

```bash
# Test if SSH works outside of rr
ssh -v user@myserver.example.com "echo connected"
```

### Check rsync command

```bash
# See what rsync would do
rr sync --dry-run
```

### Verify config parsing

```bash
# Check YAML syntax
cat .rr.yaml | python3 -c "import yaml, sys; yaml.safe_load(sys.stdin)"

# Or use yq if installed
yq . .rr.yaml
```

### Still stuck?

1. Run `rr doctor` and share the output
2. Try the command with `--verbose`
3. Check if SSH works directly: `ssh user@host "echo ok"`
4. Open an issue at https://github.com/rileyhilliard/rr/issues
