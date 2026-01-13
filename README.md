# rr (Road Runner)

<p align="center">

![demo-extended](https://github.com/user-attachments/assets/418782f4-04ac-4f93-a04e-dddf4f9f6125)

</p>

[![CI](https://github.com/rileyhilliard/rr/actions/workflows/ci.yml/badge.svg)](https://github.com/rileyhilliard/rr/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/rileyhilliard/rr)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/rileyhilliard/rr)](https://github.com/rileyhilliard/rr/releases)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Sync code to a remote machine and run commands there. That's it.

## Why

I got tired of my laptop fan spinning up and battery draining every time I ran tests. I have a few beefy machines sitting in the corner doing nothing most of the time, so I wanted an easy way to sync code over and run heavy stuff on those instead. `rr` is that.

```bash
rr run "make test"
```

This rsyncs your project to the remote host, runs `make test`, and streams the output back. If you switch from your home LAN to a VPN, it figures out which hostname works. If someone else is using the machine, it waits for them to finish.

![Pi7_GIF_CMP](https://github.com/user-attachments/assets/9ffe029e-af38-4a77-8c77-156fae378db7)

## Quick start

```bash
# 1. Install
brew install rileyhilliard/tap/rr    # or see Install section below

# 2. Set up in your project
cd your-project
rr init                               # Creates .rr.yaml config

# 3. Run something
rr run "make test"                    # Syncs files + runs command
```

That's the whole flow. rr syncs your code to the remote, runs the command, and streams output back.

## Install

**Homebrew (macOS/Linux)**

```bash
brew install rileyhilliard/tap/rr
```

**Install script**

```bash
curl -sSL https://raw.githubusercontent.com/rileyhilliard/rr/main/scripts/install.sh | bash
```

**Go install**

```bash
go install github.com/rileyhilliard/rr/cmd/rr@latest
```

**Manual download**

Grab the binary for your platform from [releases](https://github.com/rileyhilliard/rr/releases).

## Setup

**Prerequisite:** You need passwordless SSH access to your remote machine. If you can run `ssh user@yourhost` without entering a password, you're good. If not, see the [SSH setup guide](docs/ssh-setup.md).

```bash
rr init          # Creates .rr.yaml - interactive prompts walk you through it
rr doctor        # Verifies everything works
```

![demo-doctor](https://github.com/user-attachments/assets/b7f6dc8c-649d-439d-ba1f-5ceb237f680c)

If `rr init` doesn't detect your SSH config, you can add hosts manually. See [configuration docs](docs/configuration.md).

## Usage

```bash
rr run "make test"    # Sync files, then run command
rr exec "git status"  # Run command without syncing (faster for quick checks)
rr sync               # Just sync files, no command
```

You can also define named tasks in your config:

```yaml
tasks:
    test:
        run: pytest -n auto
    build:
        run: make build
```

Then run them by name:

```bash
rr test                    # Same as: rr run "pytest -n auto"
rr build
rr test tests/test_api.py  # Pass extra args: pytest -n auto tests/test_api.py
rr tasks                   # List all available tasks
```

![demo-tasks](https://github.com/user-attachments/assets/8d902e99-9b7a-4fa9-a2fa-bbdef8365e3b)

## Config

`rr` uses two config files:

**Global config** (`~/.rr/config.yaml`) - Your personal host definitions, not shared with team:

```yaml
version: 1

hosts:
    mini:
        ssh:
            - mac-mini.local # Try LAN first
            - mac-mini-tailscale # Fall back to VPN
        dir: ${HOME}/projects/${PROJECT}
```

**Project config** (`.rr.yaml` in project root) - Shareable settings for your project:

```yaml
version: 1

# Reference hosts from global config (optional - uses all hosts if omitted)
hosts:
    - mini

sync:
    exclude:
        - .git/
        - node_modules/
        - .venv/
```

`rr` tries each SSH alias in order until one connects. The `${PROJECT}` variable expands to your local directory name.

See [docs/configuration.md](docs/configuration.md) for all options.

## What it actually does

1. **Smart connection failover**: `rr` tries SSH connections in order and picks the first one that's reachable AND not busy. This handles three scenarios automatically:

    ```yaml
    # ~/.rr/config.yaml
    hosts:
        gpu-box:
            ssh:
                - 192.168.1.50 # LAN - fastest, try first
                - gpu-tailscale # Tailscale - works from anywhere
                - user@backup-gpu # Different machine - last resort
    ```

    | Where you are      | What happens                            |
    | ------------------ | --------------------------------------- |
    | Home office (LAN)  | Uses `192.168.1.50` (fastest)           |
    | Coffee shop        | LAN fails, uses Tailscale automatically |
    | gpu-box is busy    | Skips to `backup-gpu`                   |
    | gpu-box is offline | Same - falls back down the list         |

    No manual switching. Run `rr run "make test"` from anywhere and it figures out how to reach your code.

2. **File sync**: Wraps rsync with sane defaults. Excludes `.git`, `node_modules`, etc. Preserves remote-only directories so you don't nuke your venv every sync.

3. **Locking with load balancing**: Creates a lock file on the remote before running commands. If a host is locked, `rr` immediately tries the next configured host. If all hosts are locked, it either falls back to local (if enabled) or round-robins until one becomes free.

4. **Output formatting**: Auto-detects pytest, jest, go test, cargo and formats failures nicely. You can turn this off if you hate it.

## Other commands

```bash
rr tasks                # List available tasks
rr monitor              # TUI dashboard showing CPU/RAM/GPU across hosts
rr status               # Show connection and sync status
rr host list            # List configured hosts
rr host add             # Add a new host interactively
rr host remove mini     # Remove a host from config
rr unlock               # Release a stuck lock (shows picker if multiple hosts)
rr unlock --all         # Release locks on all configured hosts
rr update               # Update rr to latest version
rr update --check       # Just check if update is available
rr completion bash      # Shell completions (also zsh, fish, powershell)
```

## Troubleshooting

**Connection issues?**

```bash
rr doctor    # Diagnoses SSH, config, and dependency problems
```

**Command not found on remote?**

rr will show you what's in the remote's PATH and suggest fixes. Make sure your command is available in a non-interactive shell.

**Sync seems slow?**

Check your exclude patterns - syncing node_modules or .git will tank performance:

```yaml
sync:
    exclude:
        - .git/
        - node_modules/
        - .venv/
```

**Lock stuck?**

If a previous run crashed or lost connection and left a lock behind:

```bash
rr unlock              # Release lock (shows picker if multiple hosts)
rr unlock gpu-box      # Release lock on specific host
rr unlock --all        # Release locks on all hosts
```

The lock is project-specific, so this only affects the current directory's project.

For more, see the [troubleshooting guide](docs/troubleshooting.md).

## Claude Code integration

If you use [Claude Code](https://claude.ai/code), install the rr plugin to teach Claude how to use the CLI:

```bash
/plugin marketplace add https://github.com/rileyhilliard/rr
/plugin install rr@rr
```

Claude will then understand rr commands, the two-config system, and can help with setup and troubleshooting. See [docs/claude-code.md](docs/claude-code.md) for details.

## Docs

-   [SSH setup guide](docs/ssh-setup.md) - Get passwordless SSH working
-   [Configuration reference](docs/configuration.md)
-   [Troubleshooting](docs/troubleshooting.md)
-   [Migration guide](docs/MIGRATION.md)
-   [Example configs](docs/examples/)
-   [Architecture](docs/ARCHITECTURE.md)
-   [Claude Code plugin](docs/claude-code.md) - AI-assisted rr usage
-   [Contributing](CONTRIBUTING.md)

## License

MIT

<p align="center">
  ![that&#39;s all folks GIF by Space Jam](https://github.com/user-attachments/assets/bb29dbcd-1c3d-4480-8acc-429fba16849d)
</p>
