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

Like every rr command, doctor prints structured JSON by default. It exits 1 when any check fails and 0 when there are only warnings. The envelope's `success` is `true` whenever doctor ran; `data.summary.all_clear` is `false` if there's any failure or warning, and `data.summary.fail`/`warn` have the counts.

Checks are graded by whether a run would fail:

- Inside a project, host checks cover only the project's hosts. Outside one, they cover every global host.
- An unreachable host is a warning while another host in scope is reachable, or when `local_fallback` would run the command locally. It's a failure only when nothing can take the run.
- A missing SSH agent, a missing default key file, or no `.rr.yaml` is a warning, since agentless setups, custom keys, and global-only use all work. When every host in scope is a local host, the SSH agent and key checks are skipped.
- With `--requirements`, a missing required tool or a missing remote rsync is a failure, because `rr run` fails on it too. The suggestion points to `rr provision`.
- `--path` and `--requirements` connect the way `rr run` does, racing every SSH alias. A host that can't be reached is reported once, on its host check, and its remote checks are skipped.

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
  ● server
    ✕ server.example.com: Connection refused
      ...

    Other hosts are reachable, so runs use the other reachable hosts.

DEPENDENCIES
  ● rsync 3.2.7 (local)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠ 1 warning found
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

### "Couldn't reach '...' through jump host '...'"

The host's `ProxyJump` failed: rr connects through the jump host as OpenSSH does, by running `ssh -W` to it, and that leg didn't get through. The line under the message is what `ssh` printed, such as `Could not resolve hostname bastion` or `connect failed: No route to host`. `rr status` shows the same failure as, for example, "hostname not found via jump host 'bastion'".

Check each leg in turn:

1. The jump host on its own: `ssh bastion`. If that fails, fix it first. It's resolved through `~/.ssh/config` like any other host.
2. The target from the jump host: the `HostName` and `Port` in the error are where the jump host was asked to connect. If they're a LAN address, the jump host has to be on that network.

"Timed out reaching '...' through jump host '...'" means nothing answered within the probe timeout (`probe_timeout`, 2s by default). That can be either leg: a jump host that's slow to connect, or a target it can't reach. Two SSH handshakes, one per leg, can take longer than 2s over a VPN or a chain of jump hosts. If `ssh <target>` works but is slow, raise the timeout in `~/.rr/config.yaml`:

```yaml
defaults:
  probe_timeout: 5s
```

rr runs the jump `ssh` in batch mode and detached from the terminal, so no jump host can stop to ask for a password, a passphrase, or whether to trust a new host key. Anything that would prompt fails right away, with `ssh`'s reason. Connect with `ssh <jump-host>` once to answer those prompts.

"Couldn't run ssh to reach '...' through jump host '...'" means the `ssh` client isn't on `PATH`. rr needs it for `ProxyJump`.

When the jump host works but the target refuses the handshake, such as an unknown host key or a rejected key, the error is the target's ("SSH handshake with '...' didn't go through"), not the jump host's. So is "'...' closed the connection before the SSH handshake": the jump host reached the target's SSH server, which hung up. The target's key is checked under its `HostName`, which you may never have connected to directly, so connect once with `ssh <target>` to record it.

A failing `ProxyCommand` gives the same errors, naming the `ProxyCommand` instead of a jump host. `none` turns either off. When a host gets both, rr uses the `ProxyCommand`; OpenSSH uses whichever appears first in the file, so keep only one per host.

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

The error code is `DEPENDENCY_MISSING`.

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

Sync fails with `DEPENDENCY_MISSING` ("rsync isn't installed on <host>"). `rr doctor --requirements` checks each host for rsync; install it with the same commands above, or add `rsync` to `require:` and run `rr provision`, which has a built-in installer for it.

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

- `Lock timeout after 5m0s - another run is using m4-mini` (single host, `lock.timeout`)
- `All hosts are locked - timed out after 1m0s` (several hosts, `lock.wait_timeout`)

The error names the holder: user, hostname, pid, command, and how long it has held the lock. You don't have to wait for the timeout to find out: as soon as rr starts waiting it emits a `lock` `waiting` event (one host) or a `connect` `waiting` event (several hosts) with the same holder details, and `--pretty` shows them in the spinner.

**How locking works:** there's one lock per host, a directory at `<lock.dir>/rr.lock` (default `/tmp/rr-locks/rr.lock`). It isn't per project, so a run from another project on the same host blocks you too. The holder refreshes the lock every 30 seconds. A lock that hasn't been refreshed for `lock.stale` (default 90s) is taken over automatically, and a lock left by a dead rr process on your own machine is cleared right away with a warning.

**Causes:**

1. **Another `rr` run is using the host** - Wait for it, or add more hosts so rr can pick a free one
2. **A holder hung without exiting** - Its heartbeat keeps the lock fresh; release it by hand
3. **A killed rr's job is still running on a local host** - If rr was SIGKILLed during a run on a `local: true` host, the job it started keeps running, and its lock stays held until the job exits, even though rr's pid is dead and the heartbeat stopped. The group is `job_pgid` in `<lock.dir>/rr.lock/info.json`: list it with `pgrep -l -g <job_pgid>` and stop it with `kill -- -<job_pgid>`, or wait for it

### "Stopped waiting for the lock on X"

**Symptom:** rr exits 130 with `INTERRUPTED` and "Stopped waiting for the lock on dev" (or a list of hosts).

You, or whatever ran rr, stopped it with Ctrl+C or SIGTERM while it waited for a lock. Nothing ran on any host. Run the command again when you want it. An agent should treat this as the user's decision and not retry on its own.

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

### "No .rr.yaml found" / "Can't find the config file"

**Fix:**

```bash
rr init
```

The error code is `CONFIG_NOT_FOUND`. `rr doctor` reports a `config_file` warning, "No project config (.rr.yaml) found; using global hosts only", when there's no `.rr.yaml` in the current directory or any parent up to the git top level. If you passed `--config`, check that path.

### "No .rr.yaml in this checkout ... Found ... above it, but didn't use it"

**Symptom:** `CONFIG_NOT_FOUND` from inside a git worktree or checkout that sits inside another one, typically a worktree under the main checkout (`.claude/worktrees/<name>`).

rr looks for `.rr.yaml` only up to the top of the checkout you're in. The file it found sits inside an outer git checkout and belongs to it, and using it would make that checkout the project root: a local host would run the outer checkout's code, and a remote host would sync it. Commit `.rr.yaml` so the worktree's branch has it, or copy it in with the `cp` command from the suggestion. `rr doctor` fails its `config_file` check with the same message. `rr monitor`, `rr status`, `rr host list` and `rr unlock` still work there with your global hosts.

A `.rr.yaml` above the checkout that isn't inside another checkout (say `~/code/.rr.yaml` over the repo `~/code/foo`) doesn't trigger this: rr ignores it and treats the repo as having no project config.

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

The full message is "Project references host 'X' which doesn't exist in global config" (or "Host 'X' not found" for `--host`, `rr unlock`, and similar), with the code `HOST_NOT_FOUND`. The host doesn't exist in `~/.rr/config.yaml`. Either:

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

You can't name a task after a built-in command (`run`, `exec`, `sync`, `prune`, `pull`, `logs`, `provision`, and so on; see [Reserved task names](configuration.md#reserved-task-names)). One reserved name stops every task in the project from registering, so rename it:

```yaml
tasks:
  # Bad: "run" is reserved
  run:
    run: make run

  # Good: use a different name
  start:
    run: make run
```

### Config warning: "Unknown config key 'X' is ignored"

rr doesn't reject keys it doesn't recognize, but it warns about each one once per command: a `{"type":"phase","phase":"config","status":"warn",...}` event on stderr, with `details.file`, `key`, `message`, and `suggestion`, or a styled warning with `--pretty`. Usually it's a typo; fix or remove the key. The same kind of warning covers:

- The removed `output:` section ("The 'output' section has no effect and is no longer supported"). Delete the block.
- `defaults.host` in `~/.rr/config.yaml`. List your preferred host first under `hosts:` in `.rr.yaml` instead.
- `pull:` on a parallel task. Move it to the subtasks.
- `output:` on a task that isn't parallel. It only sets the display mode for parallel tasks.

## Task and output surprises

### "rr parses flags before the task sees them"

`rr test -k foo` fails with `CONFIG_INVALID` because rr tries to parse `-k` as its own flag. The same goes for `-v`, which is no longer an rr flag. Put task arguments after `--`:

```bash
rr test -- -k foo
```

### "parallel task 'X' doesn't accept extra arguments"

Parallel tasks reject extra args, including flags, with `CONFIG_INVALID` unless the task sets `forward_args: true`. With it, put flags after `--` (`rr test-backend -- -k foo`). Subtasks that are compound commands also need an `{args}` placeholder. For a one-off, run the command directly: `rr run "pytest tests/api -k foo"`.

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

There's no verbose flag: `-v` was removed, and `--verbose` only prints a deprecation warning. Every run's raw output is saved to `~/.rr/logs/`; the result event's `details.log_file` has the exact path, and `rr logs` lists recent runs.

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
