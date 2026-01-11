# rr (Road Runner)

<p align="center">
 
![rr](https://github.com/user-attachments/assets/d0ba88a9-6220-49ad-87eb-a06b234fbfb5)

</p>

[![CI](https://github.com/rileyhilliard/rr/actions/workflows/ci.yml/badge.svg)](https://github.com/rileyhilliard/rr/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/rileyhilliard/rr)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/rileyhilliard/rr)](https://github.com/rileyhilliard/rr/releases)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Sync code to a remote machine and run commands there. That's it.

## Why

I got tired of my laptop fan spinning up and battery draining every time I ran tests. I have a beefy machine sitting in the corner doing nothing most of the time, so I wanted an easy way to sync code over and run heavy stuff there instead. `rr` is that.

```bash
rr run "make test"
```

This rsyncs your project to the remote host, runs `make test`, and streams the output back. If you switch from your home LAN to a VPN, it figures out which hostname works. If someone else is using the machine, it waits for them to finish.

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
rr test    # Same as: rr run "pytest -n auto"
rr build
```

## Config

The `.rr.yaml` file lives in your project root:

```yaml
version: 1

hosts:
    mini:
        ssh:
            - mac-mini.local # Try LAN first
            - mac-mini-tailscale # Fall back to VPN
        dir: ${HOME}/projects/${PROJECT}

default: mini

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
rr monitor              # TUI dashboard showing CPU/RAM/GPU across hosts
rr status               # Show connection and sync status
rr host list            # List configured hosts
rr host add             # Add a new host interactively
rr host remove mini     # Remove a host from config
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

If a previous run crashed and left a lock:

```bash
rr run --force-unlock "your command"
```

For more, see the [troubleshooting guide](docs/troubleshooting.md).

## Docs

-   [SSH setup guide](docs/ssh-setup.md) - Get passwordless SSH working
-   [Configuration reference](docs/configuration.md)
-   [Troubleshooting](docs/troubleshooting.md)
-   [Migration guide](docs/MIGRATION.md)
-   [Example configs](docs/examples/)
-   [Architecture](docs/ARCHITECTURE.md)
-   [Contributing](CONTRIBUTING.md)

## License

MIT
