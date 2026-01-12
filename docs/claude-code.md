# Claude Code plugin

The rr plugin teaches [Claude Code](https://claude.ai/code) how to use the rr CLI, including commands, configuration, and troubleshooting.

## Install

```bash
claude /plugin install rileyhilliard/rr
```

## What it does

Once installed, Claude understands:

- **Commands**: `rr run`, `rr exec`, `rr sync`, `rr doctor`, `rr monitor`, and all subcommands
- **Two-config system**: Global hosts at `~/.rr/config.yaml`, project settings at `.rr.yaml`
- **Workflows**: Initial setup, daily development, multi-host load balancing
- **Troubleshooting**: Connection issues, slow syncs, stuck locks

## Usage

The plugin activates automatically when you ask about remote development, running commands on remote machines, or mention rr specifically.

**Examples:**

```
How do I set up rr for this project?
Run the tests on my remote machine
Why is rr failing to connect?
Add a new host to my rr config
```

You can also invoke it directly:

```
/rr
```

## Alternative installation

### Personal skill (without plugin)

Copy the skill to your personal Claude skills directory:

```bash
cp -r /path/to/rr/skills/rr ~/.claude/skills/
```

### Project-level

When working in the rr repository, the skill is automatically available.

## What Claude learns

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

To get the latest version of the plugin:

```bash
claude /plugin update rileyhilliard/rr
```

## Uninstalling

```bash
claude /plugin uninstall rileyhilliard/rr
```
