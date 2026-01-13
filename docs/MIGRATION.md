# Migration guide

This document covers breaking changes and upgrade instructions between versions.

## Contents

- [Version compatibility](#version-compatibility)
- [v0.4.x to v0.5.x](#v04x-to-v05x-global-config-separation) (current)
- [v1.x to v2.x](#v1x-to-v2x-future)
- [v0.x to v1.x](#v0x-to-v1x)
- [Troubleshooting upgrades](#troubleshooting-upgrades)

## Version compatibility

rr uses semantic versioning. The config file includes a `version` field to help with migrations:

```yaml
version: 1  # Current schema version
```

When the schema changes in incompatible ways, the version number bumps and rr will warn you if your config needs updating.

## v0.4.x to v0.5.x (Global Config Separation)

Host definitions have moved from `.rr.yaml` to `~/.rr/config.yaml`. This allows you to define hosts once and share project configs with your team.

### What changed

- **Hosts are now global**: Host definitions moved from project `.rr.yaml` to `~/.rr/config.yaml`
- **Projects reference hosts by name**: Instead of defining hosts, projects now just reference them with `host: <name>`
- **New global defaults**: Settings like `default`, `local_fallback`, and `probe_timeout` are now in the global config

### Migration steps

**1. Create your global config**

Move your host definitions to `~/.rr/config.yaml`:

```yaml
# ~/.rr/config.yaml
version: 1

hosts:
  gpu-box:
    ssh:
      - gpu-local
      - gpu-vpn
    dir: ~/projects/${PROJECT}

  mini:
    ssh:
      - mini-local
    dir: ~/dev/${PROJECT}

defaults:
  host: gpu-box
  local_fallback: false
  probe_timeout: 2s
```

Or use the CLI:
```bash
rr host add  # Interactive host setup
```

**2. Update your project configs**

Replace host definitions with a reference:

```yaml
# Before (.rr.yaml)
version: 1
hosts:
  gpu-box:
    ssh: [gpu-local, gpu-vpn]
    dir: ~/projects/${PROJECT}
default: gpu-box
local_fallback: false
probe_timeout: 2s
sync:
  exclude:
    - .git/
    - node_modules/

# After (.rr.yaml)
version: 1
host: gpu-box  # Just reference the global host
sync:
  exclude:
    - .git/
    - node_modules/
```

**3. Remove old host fields**

Delete these from your `.rr.yaml` (they now live in global config):
- `hosts:` section
- `default:`
- `local_fallback:`
- `probe_timeout:`

### Quick migration

If you want to keep your current setup working:

```bash
# Copy hosts from project to global config
mkdir -p ~/.rr
cat > ~/.rr/config.yaml << 'EOF'
version: 1
hosts:
  # Paste your hosts here from .rr.yaml
EOF

# Update .rr.yaml to reference hosts by name
# Add a hosts section with your preferred order (first = highest priority)
```

### Benefits

- **One place for hosts**: Define each machine once, use it in any project
- **Shareable project configs**: Team members can share `.rr.yaml` without overwriting each other's SSH settings
- **Personal machine names**: Use your own names for hosts without affecting others

## v1.x to v2.x (future)

No breaking changes yet. This section will be updated when v2 is released.

## v0.x to v1.x

If you're upgrading from early pre-release versions, here's what changed:

### Config changes

**Hosts moved to global config**

Host definitions now live in `~/.rr/config.yaml` (global) instead of `.rr.yaml` (per-project). This allows you to commit `.rr.yaml` to version control without including personal SSH settings.

```yaml
# Before: hosts in .rr.yaml (per-project)
# After: hosts in ~/.rr/config.yaml (global)
```

Move your `hosts:` section from `.rr.yaml` to `~/.rr/config.yaml`. Your project's `.rr.yaml` now references hosts by name:

```yaml
# ~/.rr/config.yaml (global - not shared)
version: 1
hosts:
  mini:
    ssh: [mini-local, mini-tailscale]
    dir: ~/projects/${PROJECT}

# .rr.yaml (project - can be committed)
version: 1
host: mini   # Reference to global host
sync:
  exclude:
    - .git/
```

**Schema version added**

Add `version: 1` at the top of both config files:

```yaml
# Before (v0.x)
hosts:
  mini:
    ssh: [mini-local]

# After (v1.x) in ~/.rr/config.yaml
version: 1

hosts:
  mini:
    ssh: [mini-local]
    dir: ~/projects/${PROJECT}
```

**Host SSH field is now a list**

SSH aliases must be specified as a list, even if you only have one:

```yaml
# Before (v0.x) - single string worked
hosts:
  mini:
    ssh: mini-local

# After (v1.x) - must be a list
hosts:
  mini:
    ssh:
      - mini-local
```

**Lock config restructured**

Lock settings moved under a `lock` key:

```yaml
# Before (v0.x)
lock_timeout: 5m
lock_stale: 10m

# After (v1.x)
lock:
  enabled: true
  timeout: 5m
  stale: 10m
```

**Task `run` field renamed from `command`**

Tasks now use `run` instead of `command`:

```yaml
# Before (v0.x)
tasks:
  test:
    command: pytest

# After (v1.x)
tasks:
  test:
    run: pytest
```

### CLI changes

**`rr exec` replaces `rr run --no-sync`**

The `--no-sync` flag was removed. Use `rr exec` instead:

```bash
# Before
rr run --no-sync "make test"

# After
rr exec "make test"
```

**`rr status` replaces `rr hosts`**

The `hosts` command was renamed to `status`:

```bash
# Before
rr hosts

# After
rr status
```

**Host management moved to subcommands**

Host operations are now under `rr host`:

```bash
# Before
rr add-host mini
rr remove-host mini

# After
rr host add
rr host remove mini
```

### Environment variable changes

**`RR_HOST` environment variable**

You can set the preferred host via environment variable (equivalent to `--host` flag):

```bash
export RR_HOST=gpu-box
rr run "make test"  # Uses gpu-box
```

## Troubleshooting upgrades

### "Unknown field" errors

If you see errors about unknown fields after upgrading:

```
Error: Invalid configuration in .rr.yaml
  Line 5: Unknown field 'command' in task definition
```

Check the migration notes above for renamed fields. The error message usually suggests the correct field name.

### Config validation failures

Run `rr doctor` to diagnose config issues:

```bash
rr doctor
```

This checks your config syntax and reports any problems with actionable suggestions.

### Checking your version

```bash
rr version
```

Compare with the latest release at https://github.com/rileyhilliard/rr/releases.

### Clean install

If you're having persistent issues, try a clean install:

```bash
# Homebrew
brew uninstall rr
brew install rileyhilliard/tap/rr

# Go
go clean -cache
go install github.com/rileyhilliard/rr/cmd/rr@latest
```

### Getting help

If you're stuck:

1. Check [Troubleshooting](troubleshooting.md)
2. Run `rr doctor` for diagnostics
3. Open an issue at https://github.com/rileyhilliard/rr/issues
