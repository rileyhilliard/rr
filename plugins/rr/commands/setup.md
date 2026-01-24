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

## Step 3: Configure SSH Agent (Important for macOS)

rr uses Go's SSH library which requires keys to be loaded in the ssh-agent. The native `ssh` command's Keychain integration (`UseKeychain yes`) doesn't work for rr.

**One-time setup for persistent keys:**

```bash
# Add to ~/.ssh/config (under Host * or at the top)
Host *
  AddKeysToAgent yes
  UseKeychain yes
```

Then load your keys:

```bash
# Add keys with Keychain storage (persists across restarts)
ssh-add --apple-use-keychain ~/.ssh/id_ed25519
ssh-add --apple-use-keychain ~/.ssh/id_rsa

# Check what keys are loaded
ssh-add -l
```

**Why this matters:** Without `AddKeysToAgent yes`, you'd need to run `ssh-add` after every restart. With it, keys are automatically added to the agent when you use them.

**Security notes:** This is Apple's recommended approach - the Keychain is encrypted with your login credentials and only stores the passphrase, not the key. For higher security needs, use hardware keys (YubiKey), set agent timeouts (`ssh-add -t 3600`), or use `AddKeysToAgent confirm` to prompt before each use.

## Step 4: Verify SSH

Run diagnostics:

```bash
rr doctor
```

If SSH fails, debug systematically:

1. Check agent has keys: `ssh-add -l`
2. Test manual SSH: `ssh <host-alias>`
3. Check SSH config: `grep -A5 "<host-alias>" ~/.ssh/config`
4. Common fixes:
    - "encrypted keys" error: run `ssh-add --apple-use-keychain <key-path>`
    - Password prompt: needs key-based auth
    - Host not found: add to `~/.ssh/config`
    - Timeout: host unreachable, try alternative address

## Step 5: Test Execution

Verify basic execution works:

```bash
rr exec "pwd"
```

Then test with sync:

```bash
rr run "ls -la"
```

If the remote directory is wrong, check the `dir` setting in global config.

## Step 6: Configure and Verify Requirements

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

**If tools are missing**, rr shows which ones and whether they can be auto-installed.

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

## Step 7: Final Verification

Run a real command to confirm everything works end-to-end:

```bash
rr test  # if task defined
# or
rr run "make test"  # or appropriate command for the project
```

## Troubleshooting Reference

| Problem             | Fix                                                         |
| ------------------- | ----------------------------------------------------------- |
| SSH works, rr fails | Add `AddKeysToAgent yes` to `~/.ssh/config`, run `ssh-add`  |
| SSH fails           | Check `ssh <alias>` manually, verify `~/.ssh/config`        |
| "command not found" | Add `shell: "zsh -l -c"` or `setup_commands` to host config |
| Sync slow           | Add large dirs to `sync.exclude`                            |
| Lock stuck          | `rr unlock`                                                 |
| Wrong directory     | Check `dir` in global config                                |

## LLM Workflow (Machine Interface)

When setting up rr programmatically, use `--machine` for structured JSON output that's easier to parse.

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
rr doctor --machine 2>&1
```

**Parse response:**
- `success: true` with config checks passing -> Project config OK
- `error.code == "CONFIG_NOT_FOUND"` -> Run `rr init --non-interactive --host <host>`

### Step 3: Verify SSH Agent (macOS)

```bash
ssh-add -l 2>&1
```

**Parse response:**
- Lists keys -> Agent OK
- "no identities" or error -> Keys not loaded

**If keys not loaded:**
```bash
# Check if AddKeysToAgent is configured
grep -i "AddKeysToAgent" ~/.ssh/config

# Load keys with Keychain
ssh-add --apple-use-keychain ~/.ssh/id_ed25519
ssh-add --apple-use-keychain ~/.ssh/id_rsa
```

**Why this matters:** rr uses Go's SSH library which requires keys in the agent. The native ssh command's Keychain integration doesn't work for rr.

### Step 4: Verify Connectivity

```bash
rr status --machine
```

**Parse `data.hosts[]` array:**

```
FOR each host in data.hosts:
  IF host.healthy == true:
    -> Host OK
  ELSE:
    -> FOR each alias in host.aliases:
      -> Parse alias.error for diagnosis:
         "timeout" -> Network/VPN issue
         "auth" -> Key not deployed or not in agent
         "host key" -> First connection
         "encrypted" -> Keys not in ssh-agent
```

**Fix connectivity issues:**
- Encrypted keys: Run `ssh-add --apple-use-keychain <key-path>`
- Timeout: Check `ping <hostname>`, verify network/VPN
- Auth: Run `ssh-copy-id <alias>` or `rr setup <host>`
- Host key: `ssh -o StrictHostKeyChecking=accept-new <alias> exit`

### Step 5: Test Execution

```bash
rr exec "echo rr-test-ok" 2>&1
echo "Exit code: $?"
```

**Expected:** Output contains "rr-test-ok", exit code 0

**If fails:** Parse error and check:
- Lock issues: `rr unlock` then retry
- Directory issues: Verify `dir` in `~/.rr/config.yaml`

### Step 6: Verify Requirements

Use the `require` field and doctor command:

```bash
# Check if .rr.yaml has require field
grep -A5 "require:" .rr.yaml

# Verify requirements with doctor
rr doctor --requirements --machine
```

**Parse response:**
- `success: true` with all requirements satisfied -> Requirements OK
- Requirements missing -> Check which tools need installation

**IF tools missing:**
- Check if auto-installable (doctor shows "(can install)")
- Install via SSH directly (see installation commands above)
- Or add `--skip-requirements` to bypass checks

### Step 7: Final Verification

```bash
rr test 2>&1
echo "Exit code: $?"
```

Exit code 0 = Setup complete
