# Troubleshooting

This guide covers common issues and their solutions.

## Contents

- [Running diagnostics](#running-diagnostics)
- [SSH connection failures](#ssh-connection-failures)
- [rsync issues](#rsync-issues)
- [Lock contention](#lock-contention)
- [Config validation errors](#config-validation-errors)
- [Task and output surprises](#task-and-output-surprises)
- [Platform-specific issues](#platform-specific-issues)
- [Debug tips](#debug-tips)

## Running diagnostics

The `rr doctor` command checks your setup and reports issues:

```bash
rr doctor                 # Run all diagnostic checks (JSON envelope on stdout)
rr doctor --pretty        # Human-readable report
rr doctor --fix           # Attempt automatic fixes where possible
rr doctor --requirements  # Also check required tools on each host
rr doctor --path          # Compare login vs interactive shell PATH on each host
```

Like every rr command, doctor prints structured JSON by default. It exits 0 even when checks fail, so scripts should read `data.summary.all_clear` (and `data.summary.fail`/`warn`) instead of the exit code.

Example `--pretty` output:

```
Road Runner Diagnostic Report

CONFIG
  ● Config file: .rr.yaml
  ● Schema valid
  ● 2 hosts configured, 5 tasks defined
  ● No reserved task names

SSH
  ● SSH key found: ~/.ssh/id_ed25519
  ● SSH agent running with 1 key loaded
  ● SSH key permissions OK

HOSTS
  ● mini
    ● mini-lan: Connected (12ms)
  ✕ server
    ✕ server.example.com: Connection refused
      ...

DEPENDENCIES
  ● rsync 3.2.7 (local)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✕ 1 issue found
```

## SSH connection failures

### "Connection refused"

**Symptom:** `rr doctor` shows "Connection refused" for an alias, or `rr run` fails with `SSH_CONNECTION_FAILED`

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

**Symptom:** SSH connects but auth fails (`SSH_AUTH_FAILED`, "authentication failed")

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

**Symptom:** SSH hangs then times out (`SSH_TIMEOUT`, "connection timed out")

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

**Symptom:** `rr monitor` shows "handshake failed", or `rr doctor`/`rr run` report "authentication failed" (`SSH_AUTH_FAILED`), but `ssh user@host` works fine from the terminal.

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

**Symptom:** `rr doctor` shows "SSH agent not running" (`SSH_AUTH_SOCK` isn't set).

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

**Symptom:** Warning like "Host 'myserver' not found in SSH config (config has a Match block at line 12 that may hide later entries)"

**Cause:** rr's SSH config parser doesn't support OpenSSH's `Match` directives. Host entries after the first `Match` line in your `~/.ssh/config` aren't recognized.

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

### "uses ProxyJump which is not yet supported"

rr reads `HostName`, `Port`, `User`, `IdentityFile`, `IdentityAgent`, and `ProxyCommand` from `~/.ssh/config`, but not `ProxyJump`. Replace it with the equivalent `ProxyCommand`:

```
Host myserver
    HostName 10.0.0.5
    ProxyCommand ssh -W %h:%p bastion
```

### "command not found" on remote

**Symptom:** Commands like `go test` or `npm run` fail with "command not found" even though they work when you SSH manually.

**Cause:** rr runs commands with `${SHELL:-/bin/bash} -c` after sourcing `~/.bashrc` and `~/.zshrc`. That's a non-login shell, so anything set up in `~/.zprofile`, `~/.bash_profile`, or `~/.profile` (Homebrew's `shellenv` usually lives there) is missing, and many `.bashrc` files return early for non-interactive shells. `rr doctor --path` shows the PATH difference between login and interactive shells on each host.

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

### rsync missing on the remote

`rr doctor` only checks for rsync locally. Check the remote yourself and install it with the same commands above:

```bash
rr exec "rsync --version"
```

### "rsync version too old"

The `--info=progress2` flag rr uses needs rsync 3.1.0 or newer on your machine. See [macOS](#macos) below for replacing the system rsync.

### "Partial transfer due to error" (rsync exit 23)

Usually a permission problem on the remote, or a local path that is a file where the remote has a directory (or the reverse). If you set a custom `sync.exclude` from before v0.23, change `.git/`, `.venv/`, and `node_modules/` to the bare patterns `.git`, `.venv`, and `node_modules`: in a linked git worktree `.git` is a file, and the trailing-slash pattern doesn't match it.

### "remote ... was last synced from ...; now syncing from ..."

Each sync writes a `.rr-source` marker on the remote. This warning means the remote directory was last synced from a different checkout or machine, so two trees are sharing one remote copy. Give them different `dir` values, or leave `sync.worktree_isolation` on (the default) so each git worktree gets its own `<repo>@<worktree>` directory.

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

3. **Exclude large directories** you don't need. A custom list replaces the default excludes, so keep the ones you still want:
   ```yaml
   sync:
     exclude:
       - .git
       - node_modules
       - .venv
       - "*.zip"
       - build/
   ```

4. **First sync from a git worktree is slow.** Each linked worktree syncs to its own remote directory, so the first sync copies everything (including a fresh `node_modules`/`.venv` install). Later syncs are incremental. `rr prune` removes directories for worktrees you've deleted.

## Lock contention

### "Lock timeout" or "All hosts are locked"

**Symptom:** The command waits, then fails with `LOCK_HELD` and one of:

- `Lock timeout after 5m0s - someone else is using this remote` (single host, `lock.timeout`)
- `All hosts are locked - timed out after 1m0s` (several hosts, `lock.wait_timeout`)

The error names the holder: user, hostname, pid, command, and how long it has held the lock.

**How locking works:** there's one lock per host, a directory at `<lock.dir>/rr.lock` (default `/tmp/rr-locks/rr.lock`). It isn't per project, so a run from another project on the same host blocks you too. The holder refreshes the lock every 30 seconds. A lock that hasn't been refreshed for `lock.stale` (default 90s) is taken over automatically, and a lock left by a dead rr process on your own machine is cleared right away with a warning.

**Causes:**

1. **Another `rr` run is using the host** - Wait for it, or add more hosts so rr can pick a free one
2. **A holder hung without exiting** - Its heartbeat keeps the lock fresh; release it by hand

**Release a stuck lock:**

```bash
rr unlock gpu-box      # Release lock on specific host
rr unlock --all        # Release locks on the project's hosts
rr unlock              # With one host configured; with several, --pretty shows a picker
```

**Wait longer** before giving up:

```yaml
lock:
  timeout: 30m        # single host
  wait_timeout: 5m    # all hosts locked
```

**Disable locking** if you're the only user:

```yaml
lock:
  enabled: false
```

## Config validation errors

### "No config file found" / "Can't find the config file"

**Fix:**

```bash
rr init
```

`rr doctor` reports "No config file found" when there's no `.rr.yaml` in the current directory or any parent. If you passed `--config`, check that path.

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

### "host 'X' needs at least one SSH connection"

Each host needs at least one SSH connection string in the global config:

```yaml
# ~/.rr/config.yaml
hosts:
  myhost:
    ssh:
      - user@server.example.com  # Add this
    dir: ${HOME}/projects
```

### "host 'X' needs a 'dir'"

Each host needs a working directory in the global config:

```yaml
# ~/.rr/config.yaml
hosts:
  myhost:
    ssh:
      - myserver.example.com
    dir: ${HOME}/projects/${PROJECT}  # Add this
```

### "Can't use 'X' as a task name - that's a built-in command"

You can't name a task after a built-in command (`run`, `exec`, `sync`, `prune`, and so on). Rename your task:

```yaml
tasks:
  # Bad: "run" is reserved
  run:
    run: make run

  # Good: use a different name
  start:
    run: make run
```

## Task and output surprises

### "rr parses flags before the task sees them"

`rr test -k foo` fails because rr tries to parse `-k` as its own flag. Put task arguments after `--`:

```bash
rr test -- -k foo
```

### "parallel task 'X' doesn't accept extra arguments"

Parallel tasks drop args unless the task sets `forward_args: true`. Subtasks that are compound commands also need an `{args}` placeholder. For a one-off, run the command directly: `rr run "pytest tests/api -k foo"`.

### "This task is a compound command ..."

The task's `run` has a pipe, `&&`, `;`, redirection, or `$()`, so appended args would land on the last command in the chain. Add an `{args}` placeholder where they belong:

```yaml
tasks:
  test:
    run: pytest {args:-tests/} -n 4 | tail -20
```

### Relative path works locally but not through rr

`rr run` and `rr exec` run in the remote equivalent of your current subdirectory. From `backend/`, `rr run "make"` uses `backend/Makefile`. Use `rr run --cwd . "..."` to run at the project root. When this is why a path failed, the result event's `details.hint` names both directories.

### Tests "pass" but nothing ran

Check the result event for `details.no_tests`. A filter that matches nothing can exit 0. If `details.piped_exit_code` is also set, the command pipes into something like `tail`, and without `pipefail` the shell reports the last stage's exit code. Turn it on per host:

```yaml
# ~/.rr/config.yaml
hosts:
  myhost:
    shell: "bash -o pipefail -c"
```

### No JSON, or JSON mixed into my output

Structured output is the default: JSON phase events and a final `{"type":"result",...}` event go to stderr, and the command's own stdout/stderr pass through. Use `--pretty` for spinners and colors, `--no-phases` to keep only the final result event, or `2>/dev/null` to drop rr's events entirely. The full raw output of every run is in the file named by `details.log_file`.

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

### See what happened

```bash
# Human-readable phases
rr run --pretty "make test"

# Structured events: each phase, then the result with details
rr run "make test" 2>events.jsonl
grep '"type":"result"' events.jsonl

# Lock acquisition debug logging
RR_DEBUG=1 rr run "make test"
```

The global `-v`/`--verbose` flag is accepted but doesn't add output today. Every run's raw output is saved to `~/.rr/logs/`; the result event's `details.log_file` has the exact path.

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

1. Run `rr doctor --pretty` and share the output
2. Try the command with `--pretty` and check the log file from `details.log_file`
3. Check if SSH works directly: `ssh user@host "echo ok"`
4. Open an issue at https://github.com/rileyhilliard/rr/issues
