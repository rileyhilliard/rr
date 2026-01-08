# Troubleshooting

This guide covers common issues and their solutions.

## Running diagnostics

The `rr doctor` command checks your setup and reports issues:

```bash
rr doctor
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

**Increase probe timeout** if hosts are slow to respond:

```yaml
probe_timeout: 10s
```

Or per-command:

```bash
rr run --probe-timeout 10s "make test"
```

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
rr exec "ls -la ~/projects/"
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

**Force release a stale lock:**

```bash
# Manually remove the lock directory on remote
rr exec "rm -rf /tmp/rr-locks/myproject.lock"
```

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

Add at least one host to your `.rr.yaml`:

```yaml
hosts:
  myhost:
    ssh:
      - myserver.example.com
    dir: ~/projects/${PROJECT}
```

### "Host 'X' has no SSH aliases"

Each host needs at least one SSH connection string:

```yaml
hosts:
  myhost:
    ssh:
      - user@server.example.com  # Add this
    dir: ~/projects
```

### "Host 'X' has no dir"

Each host needs a working directory:

```yaml
hosts:
  myhost:
    ssh:
      - myserver.example.com
    dir: ~/projects/${PROJECT}  # Add this
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
# View the fingerprint being offered
ssh-keyscan myserver.example.com

# If it's correct, add to known_hosts
ssh-keyscan myserver.example.com >> ~/.ssh/known_hosts

# Or connect once manually to verify and accept
ssh user@myserver.example.com
```

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
# Show effective config (coming soon)
rr config show

# For now, check YAML syntax
cat .rr.yaml | python3 -c "import yaml, sys; yaml.safe_load(sys.stdin)"
```

### Still stuck?

1. Run `rr doctor` and share the output
2. Try the command with `--verbose`
3. Check if SSH works directly: `ssh user@host "echo ok"`
4. Open an issue at https://github.com/rileyhilliard/rr/issues
