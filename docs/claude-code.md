---
title: Claude Code plugin
description: Install the rr plugin so Claude Code knows rr commands, config and troubleshooting.
group: guides
order: 10
---

# Claude Code Plugin

The rr plugin teaches [Claude Code](https://claude.ai/code) how to use the rr CLI, including commands, configuration, and troubleshooting.

## Install

```bash
/plugin marketplace add https://github.com/rileyhilliard/rr
/plugin install rr@rr
```

## What It Does

Once installed, Claude understands:

- **Commands**: `rr run`, `rr exec`, `rr sync`, `rr doctor`, `rr monitor`, and all subcommands
- **Two-config system**: Global hosts at `~/.rr/config.yaml`, project settings at `.rr.yaml`
- **Structured output**: Reading the JSON phase and result events rr writes to stderr by default, including test summaries, failures, and error codes
- **Task arguments**: `--` before flag-like args, `{args}` placeholders, and `forward_args` for parallel tasks
- **Workflows**: Initial setup, daily development, multi-host load balancing
- **Troubleshooting**: Connection issues, slow syncs, stuck locks

## Usage

### Guided Setup

Ask Claude to set up rr for your project. It runs `rr setup` from the project root, which:

1. Detects the stack (`go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, `Makefile`) and writes a `.rr.yaml` with matching `require:` and `tasks:` when the project has none
2. Reports the configured hosts and the remaining steps in order: adding hosts (remote machines, or this machine as a local host), checking SSH, testing execution, checking the tools each host needs
3. Prints a guide for each step and its common failures

The guide ships in the rr binary, so Claude only reads it when it runs `rr setup`. You can run `rr setup --pretty` yourself for the same report.

### General Help

The rr skill activates automatically when you ask about remote development, running commands on remote machines, or mention rr.

**Examples:**

```
Run the tests on my remote machine
Why is rr failing to connect?
Add a new host to my rr config
```

## Plugin Structure

```
.claude-plugin/
├── marketplace.json
└── plugin.json
plugins/rr/
└── skills/
    └── rr/
        ├── SKILL.md    # rr knowledge - auto-invoked when relevant
        └── reference/  # config, commands, tasks, requirements, JSON output, troubleshooting
```

- **Skills** (`rr`): Context Claude uses automatically when working with rr. Setup guidance lives in `rr setup`, not the plugin.

## Alternative Installation

### Personal installation (without plugin)

Copy to your Claude config directory:

```bash
# Skills
cp -r /path/to/rr/plugins/rr/skills/* ~/.claude/skills/

# Commands
cp /path/to/rr/plugins/rr/commands/*.md ~/.claude/commands/
```

## What Claude Learns

| Topic | Coverage |
|-------|----------|
| Commands | All CLI commands with flags and examples |
| Configuration | Global vs project config, all fields, variable expansion |
| Host management | Adding, removing, listing hosts, including this machine as a local host |
| Sync | Exclude/preserve patterns, rsync flags |
| Locking | How distributed locks work, unlocking stuck locks |
| Tasks | Defining and running named tasks, passing args, parallel tasks, dependencies |
| Output | JSON events, result details, error codes, exit codes |
| Load balancing | Multi-host setup, failover behavior |
| Troubleshooting | Common issues and fixes |

## Updating

```bash
/plugin update rr@rr
```

## Uninstalling

```bash
/plugin uninstall rr@rr
```
