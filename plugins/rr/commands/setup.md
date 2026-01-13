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

## Step 5: Verify and Install Dependencies

For EACH host in the project config, check if required tools exist. Do not skip hosts or disable them - fix them.

Based on the detected tech stack, check each host:

```bash
# @example Test each host individually
rr exec --host <hostname> "go version"      # Go projects
rr exec --host <hostname> "node --version"  # Node projects
rr exec --host <hostname> "python3 --version && uv --version"  # Python projects
rr exec --host <hostname> "bun --version"   # If using bun
```

**If a tool is missing, INSTALL IT on that host. Do not disable the host.**

Common installation commands (run via `ssh <host-alias>` directly):

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
| "command not found" | Add `shell: "zsh -l -c"` or `setup_commands` to host config |
| Sync slow           | Add large dirs to `sync.exclude`                            |
| Lock stuck          | `rr unlock`                                                 |
| Wrong directory     | Check `dir` in global config                                |
