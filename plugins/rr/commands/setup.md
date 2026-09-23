---
name: rr:setup
description: Set up rr for a project - creates configs, verifies SSH connectivity, tests remote execution, and ensures dependencies are available on remote hosts.
allowed-tools:
    - Bash
    - Read
    - Edit
    - Write
    - Grep
    - Glob
load-skills:
    - rr
---

# rr Setup

Use the `rr` skill

Set up and verify rr for this project. Work through each step in order, fixing issues as they arise.

## Step 1: Global Config

Check if `~/.rr/config.yaml` exists with valid hosts:

```bash
cat ~/.rr/config.yaml
```

If missing or empty, ask the user for their remote machine details and help create the config. Refer to the rr skill for config format.

## Step 2: Project Config

Detect the project's tech stack by checking for `package.json`, `go.mod`, `pyproject.toml`, `Makefile`, `Cargo.toml`, etc.

Create or update `.rr.yaml` with:

-   Sync exclusions appropriate for the detected stack
-   Useful tasks based on the tooling (test, build, lint, etc.)

Refer to the rr skill for config format and task syntax.

## Step 3: Verify SSH

Run diagnostics:

```bash
rr doctor
```

If SSH fails, debug systematically:

1. Test manual SSH: `ssh <host-alias>`
2. Check SSH config: `grep -A5 "<host-alias>" ~/.ssh/config`
3. Common fixes:
    - Password prompt: needs key-based auth
    - Host not found: add to `~/.ssh/config`
    - Timeout: host unreachable, try alternative address
    - **"handshake failed" but ssh works**: Key not in agent. Run `ssh-add ~/.ssh/id_rsa`

**Important for passphrase-protected keys:** Ensure your SSH config includes `AddKeysToAgent yes` and `UseKeychain yes` (macOS) so keys are automatically loaded:

```
Host your-host
    HostName 192.168.x.x
    User youruser
    IdentityFile ~/.ssh/id_rsa
    AddKeysToAgent yes
    UseKeychain yes
```

This prevents "handshake failed" errors where `ssh` works but rr cannot connect.

## Step 4: Test Execution

Verify basic execution works:

```bash
rr exec "pwd"
```

Then test with sync:

```bash
rr run "ls -la"
```

If the remote directory is wrong, check the `dir` setting in global config.

## Step 5: Configure and Verify Requirements

rr supports declarative requirements via the `require:` field. Add required tools to `.rr.yaml`:

```yaml
# .rr.yaml
require:
  - go        # Go projects
  - node      # Node projects
  - python3   # Python projects
  - uv        # Python package manager
```

Then verify requirements with doctor:

```bash
rr doctor --requirements
```

**If tools are missing**, rr shows which ones and whether they can be auto-installed. Install the ones with built-in installers:

```bash
rr provision --check   # Report what's missing
rr provision --yes     # Install without prompts
```

### Manual Installation

For tools without built-in installers, install via SSH:

```bash
# uv (Python package manager)
ssh <host-alias> "curl -LsSf https://astral.sh/uv/install.sh | sh"

# bun (JavaScript runtime)
ssh <host-alias> "curl -fsSL https://bun.sh/install | bash"

# Node.js via nvm
ssh <host-alias> "curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash && source ~/.bashrc && nvm install node"

# Go
ssh <host-alias> "curl -LO https://go.dev/dl/go1.22.0.linux-amd64.tar.gz && sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz"
```

After installing, update the host's `setup_commands` in `~/.rr/config.yaml` to source the new tools:

```yaml
setup_commands:
    - source ~/.local/bin/env # uv
    - export PATH="$HOME/.bun/bin:$PATH" # bun
    - export PATH="$HOME/.local/bin:$PATH"
```

**If setup_commands reference missing files** (e.g., `source ~/.local/bin/env` fails):

1. Install the tool that creates that file (e.g., uv creates `~/.local/bin/env`)
2. Or remove/fix the setup_command in `~/.rr/config.yaml`

## Step 6: Final Verification

Run a real command to confirm everything works end-to-end:

```bash
rr test  # if task defined
# or
rr run "make test"  # or appropriate command for the project
```

## Troubleshooting Reference

| Problem             | Fix                                                         |
| ------------------- | ----------------------------------------------------------- |
| SSH fails           | Check `ssh <alias>` manually, verify `~/.ssh/config`        |
| "handshake failed"  | Key not in agent: `ssh-add`, add `AddKeysToAgent yes` to SSH config |
| "command not found" | Add `shell: "zsh -l -c"` or `setup_commands` to host config |
| Sync slow           | Add large dirs to `sync.exclude`                            |
| Lock stuck          | `rr unlock`                                                 |
| Wrong directory     | Check `dir` in global config                                |

## LLM Workflow (Machine Interface)

rr emits structured JSON by default (`doctor`, `status`, and `tasks` print a `{"success":...,"data":...}` envelope on stdout), so no extra flags are needed.

### Step 1: Check Global Config

```bash
cat ~/.rr/config.yaml 2>/dev/null
```

**IF missing or empty:**
- Ask user for SSH hostname/alias and remote directory
- Create config with non-interactive command:

```bash
rr host add --name <name> --ssh "<alias>" --dir "~/projects/\${PROJECT}" --skip-probe
```

### Step 2: Check Project Config

```bash
rr doctor
```

**Parse response:**
- `data.summary.all_clear == true` -> Setup OK
- Otherwise look at `data.categories[].results[]` entries with a non-zero `status` (`0` pass, `1` warn, `2` fail). Doctor exits 1 when any check fails and 0 on warnings only; a warning (no SSH agent, an unreachable host while another is reachable) doesn't block runs
- A `config_file` warning "No project config (.rr.yaml) found" -> Run `rr init --non-interactive --host <host>`

### Step 3: Verify Connectivity

```bash
rr status
```

**Parse `data.hosts[]` array:**

```
FOR each host in data.hosts:
  IF host.healthy == true:
    -> Host OK
  ELSE:
    -> FOR each alias in host.aliases:
      -> Parse alias.error for diagnosis:
         "connection timed out" -> Network/VPN issue
         "authentication failed" -> Key not deployed or not in the agent
         "host key verification failed" -> Unknown key (first connection) or changed key
```

**Fix connectivity issues:**
- Timeout: Check `ping <hostname>`, verify network/VPN
- Auth: Run `ssh-copy-id <alias>` or `rr setup <host>`
- Unknown host key (first connection): verify the host's fingerprint through a trusted channel first (for example, run `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the host's console), then accept it with `ssh -o StrictHostKeyChecking=accept-new <alias> exit`
- Changed host key: don't accept it automatically. A changed key can mean the host was reinstalled or that the connection is being intercepted. Confirm the new fingerprint through a trusted channel, then remove the old entry with `ssh-keygen -R <hostname>` and connect again

### Step 4: Test Execution

```bash
rr exec "echo rr-test-ok"
echo "Exit code: $?"
```

stdout has the command output; stderr has JSON phase events and a final `{"type":"result",...}` line.

**Expected:** Output contains "rr-test-ok", exit code 0

**If fails:** If the command ran and exited nonzero, stderr ends with a `{"type":"result","status":"failed",...}` event: read its `exit_code` and `details`. If rr failed before the command started (connection, lock, sync), stderr has a JSON error envelope instead: read `error.code` and `error.suggestion`, and check:
- Lock issues (`LOCK_HELD`): `rr unlock <host>` then retry
- Directory issues: Verify `dir` in `~/.rr/config.yaml`

### Step 5: Verify Requirements

Use the `require` field and doctor command:

```bash
# Check if .rr.yaml has require field
grep -A5 "require:" .rr.yaml

# Verify requirements with doctor
rr doctor --requirements
```

**Parse response:**
- `REQUIREMENTS` category results all `"pass"` -> Requirements OK
- Otherwise the `message` lists missing tools, marked "(can install)" when rr has an installer

**IF tools missing:**
- Run `rr provision --yes` for tools marked "(can install)"
- Install the rest via SSH (see installation commands above)
- Or add `--skip-requirements` to `rr run`/`rr exec` to bypass checks

### Step 6: Final Verification

```bash
rr test
echo "Exit code: $?"
```

Exit code 0 = Setup complete
