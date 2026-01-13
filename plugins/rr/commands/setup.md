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

## Step 5: Verify Dependencies

Based on the detected tech stack, check if required tools exist on the remote:

```bash
# Examples - run whichever applies
rr exec "go version"
rr exec "node --version"
rr exec "python3 --version"
rr exec "uv --version"
```

If "command not found":

1. **Tool installed but not in PATH** (common): Add `shell: "zsh -l -c"` to host config, or add `setup_commands` to source the environment
2. **Tool not installed**: Help install via `rr exec`
3. **PATH needs updating**: Add `env` section to host config

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
