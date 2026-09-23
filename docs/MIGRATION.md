# Migration guide

This document covers breaking changes and upgrade instructions between versions.

## Contents

- [Version compatibility](#version-compatibility)
- [Upgrading to the next release](#upgrading-to-the-next-release-unreleased)
- [Upgrading to v0.26.0](#upgrading-to-v0260-worktree-pruning)
- [Upgrading to v0.24.0](#upgrading-to-v0240-commands-run-in-your-current-subdirectory)
- [Upgrading to v0.23.0](#upgrading-to-v0230-task-args-worktrees-excludes-fallback)
- [Upgrading to v0.22.0](#upgrading-to-v0220-parallel-tasks-reject-extra-args)
- [Upgrading to v0.21.0](#upgrading-to-v0210-structured-output-by-default)
- [Upgrading to v0.10.0](#upgrading-to-v0100-defaultshost-removed)
- [v0.5.x to v0.6.0](#v05x-to-v060-global-config-separation)
- [Troubleshooting upgrades](#troubleshooting-upgrades)

## Version compatibility

rr uses semantic versioning. The config file includes a `version` field to help with migrations:

```yaml
version: 1  # Current schema version
```

When the schema changes in incompatible ways, the version number bumps. rr refuses to load a config whose `version` is newer than it supports and tells you to upgrade rr. The schema is still at `version: 1`: every change below happened without a bump, so check the sections for the releases you're skipping.

rr is pre-1.0, so breaking changes ship in minor releases. [CHANGELOG.md](../CHANGELOG.md) has the full detail for each version.

## Upgrading to the next release (unreleased)

These changes are on `main` and ship in the next minor release. Most affect scripts and agents that read rr's exit codes and structured output.

**`rr doctor` exits 1 when a check fails.** It used to exit 0 whatever it found. Now it exits 1 on any failed check and 0 when there are only warnings. The JSON envelope still says `success: true`, since doctor itself ran, and `data.summary.all_clear` still carries the verdict. A script that runs `rr doctor` under `set -e` now stops on a real failure:

```bash
rr doctor || echo "doctor found a blocker"
```

Checks were regraded at the same time, so a missing SSH agent, a missing default key file, no `.rr.yaml`, or an offline host that the project doesn't use are warnings, not failures. An unreachable host fails only when no host in scope is reachable and `local_fallback` wouldn't take over.

**Error codes changed for three kinds of failure.** Codes used to be guessed from the error message, and some were wrong. If you branch on `error.code`, update these:

| Failure | Old code | New code |
| --- | --- | --- |
| No `.rr.yaml` found, or `--config` points at a missing file | `CONFIG_INVALID` | `CONFIG_NOT_FOUND` |
| Host name not in the global config (`--host`, `hosts:`, `rr unlock`, `rr provision`, `rr host remove`, `rr monitor`) | `CONFIG_INVALID` or `CONFIG_NOT_FOUND` | `HOST_NOT_FOUND` |
| rsync missing locally or on the host | `RSYNC_FAILED` | `DEPENDENCY_MISSING` |
| `ssh-copy-id` missing (`rr setup`) | `SSH_CONNECTION_FAILED` | `DEPENDENCY_MISSING` |
| Tools from `require:` missing on the host | `COMMAND_FAILED` | `DEPENDENCY_MISSING` |

**`-v` is no longer an rr flag.** The global `-v`/`--verbose` flag had no effect, and `-v` swallowed task arguments: `rr test -v` ran the task without `-v`. Put task flags after `--`:

```bash
rr test -- -v      # passes -v to the task
rr test -v         # CONFIG_INVALID, with a hint to use --
```

`--verbose` still parses so existing scripts keep working, but it's hidden and emits a `config` warn event. Use `RR_DEBUG=1` for debug logs.

**Remove the `output:` section from `.rr.yaml`.** It was validated but never read. rr now warns about it (a `config` warn event, or a styled warning with `--pretty`) and ignores it. Unknown keys and the old `defaults.host` get the same kind of warning, so a typo no longer silently does nothing. Configs that loaded before still load.

**Host, task, and env names are case-sensitive.** rr used to lowercase every key in its config files, so host `MyBox` was stored as `mybox`, task `Build` ran as `rr build`, and `env: {FOO: bar}` exported `foo=bar`. Keys are now kept exactly as written. If you worked around the old behavior, match the case used in the config: `hosts: [MyBox]` and `--host MyBox`, `rr Build`, and `$FOO` in commands that read the variable. Task and host names containing a dot now load as written instead of being split apart. Env var names must be valid shell names (letters, digits, underscores, not starting with a digit); anything else fails validation with an error naming the key.

**`pull`, `logs`, and `provision` are reserved task names.** Tasks with those names already collided with the built-in commands. Rename them.

**Local runs report a different connect event.** `--local` and local mode (project `local_fallback` with no `hosts:` listed) emit a connect `complete` event with `host: "local"` and `details.reason` of `local_flag` or `local_mode`, and the result carries `details.local_reason` with the same value. They used to emit a connect `warn` event with `reason: hosts_unreachable`. Consumers that matched `hosts_unreachable` to detect local runs should match the new reasons. Real fallbacks still warn with `hosts_unreachable` or `all_hosts_locked`. `--local` also works with no hosts configured now.

**`rr tasks` fails on an invalid config.** It exits non-zero with an error envelope on stderr, the same error `rr <task>` gives. It used to write error envelopes to stdout. A project with no hosts configured still lists its tasks.

**Parallel subtasks get the full env and setup.** Subtasks now merge host `env`, then `defaults.env`, then the task's `env`, and run host `setup_commands` plus `defaults.setup` after the `cd` into the project directory, the same as single tasks. If a subtask depended on not seeing `defaults.env`, or on setup running before the `cd`, adjust it.

**Parallel subtask `pull:` now runs.** Each subtask's files land in `<dest>/<subtask>/`, or `<dest>/<subtask>_<index>/` for a subtask listed more than once. Subtasks that run on the same host share one remote directory, so give each one its own output path (a different junit file per subtask, say) or they overwrite each other on the remote. `pull:` set on the parallel task itself does nothing and warns; move it to the subtasks.

## Upgrading to v0.26.0 (worktree pruning)

- **Syncs delete stale worktree directories.** `sync.prune_worktrees` defaults to `true`: after each sync, rr removes remote `<repo>@<worktree>` directories whose git worktree no longer exists locally. If you keep anything in those remote directories that you need after deleting the local worktree, set `sync.prune_worktrees: false` and run `rr prune` yourself.
- **`prune` is a reserved task name.** A task called `prune` in `.rr.yaml` now fails validation. Rename it.
- **Go 1.26.8 is required to build from source** (`go install`, `make build`).

## Upgrading to v0.24.0 (commands run in your current subdirectory)

`rr run` and `rr exec` now `cd` into the subdirectory you invoked them from before running. Before, every ad-hoc command ran at the project root.

```bash
cd backend
rr run "make"               # now uses backend/Makefile, not the root one
rr run "cat ../README.md"   # root-relative paths need ../
rr run --cwd . "make"       # run at the project root, the old behavior
```

Named tasks are unaffected: `rr test` runs the same way from any directory. The directory a command ran in is reported as `details.remote_cwd`.

## Upgrading to v0.23.0 (task args, worktrees, excludes, fallback)

**Task args are shell-quoted.** Extra args appended to a single-command task arrive as quoted arguments, so `rr test "-k foo bar"` is one argument and globs or `$VARS` in appended args are no longer expanded remotely. Put globs in the task's `run` string instead.

**Compound tasks need an `{args}` placeholder.** A task whose `run` contains pipes, `&&`, redirections, `$()`, or backticks errors when given extra args, since before they silently landed on the last command in the pipeline. Mark where args go:

```yaml
tasks:
  test:
    run: pytest {args:-.} -n 4 | tail -20
```

**Worktrees get their own remote directory.** In a linked git worktree, `${PROJECT}` expands to `repo@worktree-name`. The first sync from an existing worktree is a cold sync into the new directory, and the old shared directory is left in place for you to delete. To keep the old layout, set `sync.worktree_isolation: false`.

**Default excludes use bare patterns.** `.git/`, `.venv/`, and `node_modules/` became `.git`, `.venv`, and `node_modules`. A custom `sync.exclude` list replaces the defaults, so update yours the same way if it has the trailing-slash forms and you sync from worktrees (where `.git` is a file).

**`local_fallback` takes a mode.** Valid values are `never`, `on-unreachable`, and `always`. Booleans still work (`true` = `always`, `false` = `never`). With `always`, when every host is locked by a live process, rr now waits up to `lock.wait_timeout` (default `1m`) before running locally, where before it fell back immediately.

**Local paths in commands are rewritten.** `rr run`/`rr exec` rewrite absolute paths under the project to the remote project directory, and task args become project-relative. Set `rewrite_paths: false` (in the project config or global `defaults`) to pass paths through untouched.

## Upgrading to v0.22.0 (parallel tasks reject extra args)

Passing args to a parallel task used to drop them silently. It's now an error. To forward args to each subtask, set `forward_args: true` on the parallel task (with `{args}` placeholders in the subtasks where needed), or run the command ad hoc with `rr run "<command> <args>"`.

## Upgrading to v0.21.0 (structured output by default)

**Output is structured JSON unless you ask for `--pretty`.** All commands emit JSON phase events (connect, sync, lock, exec) on stderr and pass command stdout/stderr through undecorated. Spinners, colors, and formatted summaries need `--pretty` / `-p`:

```bash
# Before
rr run "make test"             # human-readable
rr run --machine "make test"   # JSON

# After
rr run --pretty "make test"    # human-readable
rr run "make test"             # JSON
```

`--machine` / `-m` still parses but does nothing. Drop it from scripts at your convenience. `--no-phases` (v0.22.0) suppresses the intermediate phase events and keeps the final result event. `rr monitor` is still an interactive TUI.

**Locks go stale much sooner, down from 10 minutes.** Active locks are refreshed by a 30-second heartbeat, and a lock without a refresh for `lock.stale` is reclaimed. The current default is `90s` in a project with `.rr.yaml`. `lock.timeout` no longer has to be larger than `lock.stale`.

**Sync respects `.gitignore`.** `sync.respect_gitignore` defaults to `true`, so gitignored files stop reaching the remote. If a remote command needs a gitignored file (a generated config, a local `.env`), set `sync.respect_gitignore: false`. Negation patterns (`!path`) were mishandled before v0.22.3, so use v0.22.3 or later if you rely on them.

**AI agent directories are excluded.** `.claude/`, `.cursor/`, `.aider/`, and `.copilot/` are no longer synced by default.

## Upgrading to v0.10.0 (`defaults.host` removed)

`defaults.host` in `~/.rr/config.yaml` no longer does anything. Host priority comes from the order of the `hosts` list in `.rr.yaml`, first entry first:

```yaml
# .rr.yaml
hosts:
  - gpu-box   # tried first
  - mini
```

Move your preferred host to the top of that list and delete `defaults.host`. A leftover `defaults.host` doesn't error; it has no effect, and current rr versions print a config warning about it.

## v0.5.x to v0.6.0 (global config separation)

Host definitions have moved from `.rr.yaml` to `~/.rr/config.yaml`. This allows you to define hosts once and share project configs with your team.

### What changed

- **Hosts are now global**: Host definitions moved from project `.rr.yaml` to `~/.rr/config.yaml`
- **Projects reference hosts by name**: Instead of defining hosts, projects now just reference them with `host: <name>`
- **New global defaults**: Settings like `default`, `local_fallback`, and `probe_timeout` are now in the global config

Later releases changed some of this: v0.10.0 removed `defaults.host` (see [above](#upgrading-to-v0100-defaultshost-removed)), and `.rr.yaml` accepts `local_fallback` again as a per-project override.

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

## Troubleshooting upgrades

### A setting stopped having an effect

rr doesn't reject config keys it doesn't recognize. Current versions print a config warning for each one (a `config` warn event, or a styled warning with `--pretty`) that names the key and the file; older versions skipped them silently. If a setting seems to do nothing after an upgrade, look for that warning, then check the sections above and [configuration.md](configuration.md) for the key's current name and location.

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
