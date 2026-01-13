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
- **Workflows**: Initial setup, daily development, multi-host load balancing
- **Troubleshooting**: Connection issues, slow syncs, stuck locks

## Usage

### Guided Setup

Run from your project root to set up rr:

```
/rr:setup
```

This walks through:
1. Creating/verifying global config with hosts
2. Creating project config with appropriate sync exclusions and tasks
3. Verifying SSH connectivity
4. Testing remote execution
5. Checking dependencies on the remote host

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
├── commands/
│   └── setup.md        # /rr:setup - guided setup workflow
└── skills/
    └── rr/
        └── SKILL.md    # rr knowledge - auto-invoked when relevant
```

- **Commands** (`/rr:setup`): User-invoked actions
- **Skills** (`rr`): Context Claude uses automatically when working with rr

## Alternative Installation

### Personal installation (without plugin)

Copy to your Claude config directory:

```bash
# Skills
cp -r /path/to/rr/plugins/rr/skills/* ~/.claude/skills/

# Commands
cp /path/to/rr/plugins/rr/commands/*.md ~/.claude/commands/
```

### Project-level

When working in the rr repository, skills and commands are automatically available.

## What Claude Learns

| Topic | Coverage |
|-------|----------|
| Commands | All CLI commands with flags and examples |
| Configuration | Global vs project config, all fields, variable expansion |
| Host management | Adding, removing, listing hosts |
| Sync | Exclude/preserve patterns, rsync flags |
| Locking | How distributed locks work, unlocking stuck locks |
| Tasks | Defining and running named tasks |
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
