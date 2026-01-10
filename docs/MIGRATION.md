# Migration guide

This document covers breaking changes and upgrade instructions between versions.

## Contents

- [Version compatibility](#version-compatibility)
- [v1.x to v2.x](#v1x-to-v2x-future)
- [v0.x to v1.x](#v0x-to-v1x)
- [Troubleshooting upgrades](#troubleshooting-upgrades)

## Version compatibility

rr uses semantic versioning. The config file includes a `version` field to help with migrations:

```yaml
version: 1  # Current schema version
```

When the schema changes in incompatible ways, the version number bumps and rr will warn you if your config needs updating.

## v1.x to v2.x (future)

No breaking changes yet. This section will be updated when v2 is released.

## v0.x to v1.x

If you're upgrading from early pre-release versions, here's what changed:

### Config changes

**Schema version added**

Add `version: 1` at the top of your `.rr.yaml`:

```yaml
# Before (v0.x)
hosts:
  mini:
    ssh: [mini-local]

# After (v1.x)
version: 1

hosts:
  mini:
    ssh: [mini-local]
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

You can now set the default host via environment variable:

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
