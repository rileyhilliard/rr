# rr Claude Code skills

This directory contains Claude Code skills that teach Claude how to use the `rr` CLI.

For full documentation, see [docs/claude-code.md](../docs/claude-code.md).

## Quick install

```bash
/plugin marketplace add https://github.com/rileyhilliard/rr
/plugin install rr@rr
```

## Available skills

### rr

Teaches Claude to use the rr CLI for syncing code and running commands on remote machines.

**Triggers automatically when:**
- User wants to run tests, builds, or commands remotely
- User needs to sync files to a remote host
- User is setting up or troubleshooting rr configuration

**Invoke manually:** `/rr`

## Alternative installation

### Personal skill

```bash
cp -r skills/rr ~/.claude/skills/
```

### Project-level

The skill is automatically available when working in this repository.

## What Claude learns

- Two-config system (global `~/.rr/config.yaml` + project `.rr.yaml`)
- All rr commands and their flags
- Host management and load balancing
- Common workflows and troubleshooting
- Tasks and variable expansion
