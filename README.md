# rr (Road Runner)

<p align="center">

![demo-extended](https://github.com/user-attachments/assets/418782f4-04ac-4f93-a04e-dddf4f9f6125)

</p>

<p align="center">
  <strong>Run heavy workloads on remote machines without the hassle.</strong>
</p>

<p align="center">
  <a href="https://github.com/rileyhilliard/rr/actions/workflows/ci.yml"><img src="https://github.com/rileyhilliard/rr/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/github/go-mod/go-version/rileyhilliard/rr" alt="Go"></a>
  <a href="https://github.com/rileyhilliard/rr/releases"><img src="https://img.shields.io/github/v/release/rileyhilliard/rr" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

---

**rr** syncs your code to a remote machine and runs commands there. One command, no context switching.

```bash
rr run "make test"
```

Your project rsyncs to the remote, the command runs, and output streams back. Works from home, a coffee shop, or anywhere—rr figures out which connection works.

## Contents

-   [Why rr](#why-rr)
-   [Features](#features)
-   [Quick Start](#quick-start)
-   [Install](#install)
-   [Setup](#setup)
-   [Usage](#usage)
-   [Configuration](#configuration)
-   [How It Works](#how-it-works)
-   [Commands](#commands)
-   [Troubleshooting](#troubleshooting)
-   [Documentation](#documentation)
-   [Contributing](#contributing)

## Why rr

I got tired of my laptop fan spinning up and battery draining every time I ran tests. I have a few beefy machines sitting in the corner doing nothing most of the time, so I wanted an easy way to sync code over and run heavy stuff on those instead.

![Pi7_GIF_CMP](https://github.com/user-attachments/assets/9ffe029e-af38-4a77-8c77-156fae378db7)

**Especially useful for agentic coding:** If you're doing TDD with Claude Code, Cursor, or similar tools, AI agents want to run tests constantly to verify their work. With multiple sub-agents running in parallel—or multiple features being developed simultaneously—your test suite gets hammered from all directions. `rr` queues test runs across your machine pool so agents aren't all fighting over localhost (and failing tests because they're stomping on each other).

**The problem with existing tools:**

| Tool           | Issue                                            |
| -------------- | ------------------------------------------------ |
| `rsync && ssh` | Manual, repetitive, no connection failover       |
| Ansible        | Requires inventory files, YAML playbooks, Python |
| DevPod/Tilt    | Container-focused, overkill for "sync + run"     |
| VS Code Remote | IDE-coupled, doesn't solve CLI workflows         |

rr fills the gap: **a single binary that handles sync, execution, locking, load balancing, and connection failover**—nothing more.

## Features

-   **Smart connection failover** — Configure multiple SSH paths (LAN, Tailscale, backup host) and `rr` picks the first one that works
-   **File sync with rsync** — Excludes `.git`, `node_modules`, etc. by default; preserves remote-only dirs like `.venv`
-   **Distributed locking** — Prevents concurrent runs on shared machines; auto-detects and waits if someone else is working
-   **Load balancing** — Distribute work across multiple machines automatically
-   **Agentic coding friendly** — Queues parallel test runs from AI agents (Claude Code, Cursor, etc.) across your machine pool
-   **Claude Code skill** — Install the `rr` skill and let Claude set up your environment and troubleshoot issues
-   **Named tasks** — Define `test`, `build`, `deploy` commands in config, run with `rr test`
-   **Output formatters** — Auto-formats pytest, Jest, Go test, and Cargo output for readable failure summaries
-   **Real-time monitor** — TUI dashboard showing CPU, RAM, GPU across all your hosts
-   **Zero dependencies** — Single Go binary, no runtime requirements
-   **Works anywhere** — macOS, Linux, Windows (WSL)

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

That's the whole flow. `rr` syncs your code to the remote, runs the command, and streams output back.

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
rr test                    # Same as: rr run "pytest -n auto"
rr build
rr test tests/test_api.py  # Pass extra args: pytest -n auto tests/test_api.py
rr tasks                   # List all available tasks
```

![demo-tasks](https://github.com/user-attachments/assets/8d902e99-9b7a-4fa9-a2fa-bbdef8365e3b)

## Configuration

rr uses two config files to separate personal settings from shareable project settings:

| File    | Location            | Purpose                            | Commit to git? |
| ------- | ------------------- | ---------------------------------- | -------------- |
| Global  | `~/.rr/config.yaml` | Your hosts, SSH paths, directories | No             |
| Project | `.rr.yaml`          | Sync rules, tasks, host selection  | Yes            |

**Global config** — your machine definitions:

```yaml
# ~/.rr/config.yaml
version: 1

hosts:
    mini:
        ssh:
            - mac-mini.local # Try LAN first
            - mac-mini-tailscale # Fall back to VPN
        dir: ${HOME}/projects/${PROJECT}
```

**Project config** — shareable settings:

```yaml
# .rr.yaml
version: 1
hosts: [mini] # Optional: defaults to all hosts

sync:
    exclude: [.git/, node_modules/, .venv/]
```

`${PROJECT}` expands to your local directory name. See [configuration docs](docs/configuration.md) for all options.

## How It Works

**Connection failover:** `rr` tries each SSH path in order and uses the first one that connects. Configure your fastest option first:

```yaml
hosts:
    gpu-box:
        ssh:
            - 192.168.1.50 # LAN - fastest
            - gpu-tailscale # VPN - works from anywhere
            - user@backup-gpu # Backup machine
```

| Your location | What happens                   |
| ------------- | ------------------------------ |
| Home (LAN)    | Uses `192.168.1.50`            |
| Coffee shop   | LAN times out → uses Tailscale |
| gpu-box busy  | Skips to `backup-gpu`          |

**File sync:** Wraps rsync with smart defaults. Excludes build artifacts, preserves remote-only directories (like `.venv` installed on the remote).

**Locking:** Creates a lock before running commands. If a host is locked, `rr` tries the next host. If all hosts are locked, it round-robins until one frees up.

**Output formatting:** Auto-detects pytest, Jest, Go test, Cargo and formats failures for readability. Disable with `--format=generic`.

## Commands

```bash
# Core workflow
rr run "make test"      # Sync + run command
rr exec "git status"    # Run without syncing
rr sync                 # Sync only

# Tasks
rr test                 # Run named task
rr tasks                # List available tasks

# Monitoring & status
rr monitor              # TUI dashboard: CPU/RAM/GPU across hosts
rr status               # Show connection and sync status
rr doctor               # Diagnose issues

# Host management
rr host list            # List configured hosts
rr host add             # Add a new host interactively
rr host remove mini     # Remove a host

# Maintenance
rr unlock               # Release a stuck lock
rr update               # Update to latest version
rr completion bash      # Shell completions (also: zsh, fish, powershell)
```

## Troubleshooting

Start with `rr doctor` — it checks your config, SSH setup, and dependencies:

```bash
rr doctor
```

![demo-doctor](https://github.com/user-attachments/assets/b7f6dc8c-649d-439d-ba1f-5ceb237f680c)

**Common issues:**

| Problem                     | Solution                                                                             |
| --------------------------- | ------------------------------------------------------------------------------------ |
| Connection fails            | Run `rr doctor` to check SSH. Ensure you can `ssh user@host` without a password.     |
| Command not found on remote | Remote PATH differs from interactive shell. Add `shell: "zsh -l -c"` to host config. |
| Sync is slow                | Check exclude patterns — syncing `node_modules` or `.git` tanks performance.         |
| Lock stuck                  | Run `rr unlock` to release. Locks auto-expire after 10 minutes.                      |

For more, see the [troubleshooting guide](docs/troubleshooting.md).

## Documentation

| Guide                                      | Description                         |
| ------------------------------------------ | ----------------------------------- |
| [SSH Setup](docs/ssh-setup.md)             | Get passwordless SSH working        |
| [Configuration](docs/configuration.md)     | All config options explained        |
| [Troubleshooting](docs/troubleshooting.md) | Common issues and fixes             |
| [Architecture](docs/ARCHITECTURE.md)       | How `rr` works under the hood       |
| [Migration](docs/MIGRATION.md)             | Upgrading from older versions       |
| [Examples](docs/examples/)                 | Sample configs for different setups |

## Claude Code Integration

If you use [Claude Code](https://claude.ai/code), install the `rr` skill to let Claude help set up and manage your `rr` environment:

```bash
/plugin marketplace add https://github.com/rileyhilliard/rr
/plugin install rr@rr
```

Once installed, Claude can help with setup and troubleshooting. To set up `rr` for your project. From your project root, run:

```
/rr:setup
```

This walks through creating configs, verifying SSH connectivity, testing remote execution, and ensuring dependencies are available on your remote hosts.

See [docs/claude-code.md](docs/claude-code.md) for details.

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## License

[MIT](LICENSE)
