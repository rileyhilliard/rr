# rr (Road Runner)

<p align="center">
  <img src="assets/rr.gif" alt="Meep meep!" width="400">
</p>

[![Go](https://img.shields.io/github/go-mod/go-version/rileyhilliard/rr)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/rileyhilliard/rr)](https://github.com/rileyhilliard/rr/releases)

Sync code to a remote machine and run commands there. That's it.

## Why

I got tired of my laptop fan spinning up and battery draining every time I ran tests. I have a beefy machine sitting in the corner doing nothing most of the time, so I wanted an easy way to sync code over and run heavy stuff there instead. `rr` is that.

```bash
rr run "make test"
```

This rsyncs your project to the remote host, runs `make test`, and streams the output back. If you switch from your home LAN to a VPN, it figures out which hostname works. If someone else is using the machine, it waits for them to finish.

## Install

```bash
# macOS/Linux
curl -sSL https://raw.githubusercontent.com/rileyhilliard/rr/main/scripts/install.sh | bash

# Homebrew
brew install rileyhilliard/tap/rr

# Go
go install github.com/rileyhilliard/rr/cmd/rr@latest
```

## Setup

```bash
rr init          # Creates .rr.yaml in your project
rr setup myhost  # Walks you through SSH key setup (if needed)
rr doctor        # Checks that everything works
```

## Usage

```bash
rr run "make test"    # Sync + run
rr exec "git status"  # Run without syncing
rr sync               # Just sync
```

## Config

The `.rr.yaml` file lives in your project root:

```yaml
version: 1

hosts:
  mini:
    ssh:
      - mac-mini.local      # Try LAN first
      - mac-mini-tailscale  # Fall back to VPN
    dir: ~/projects/${PROJECT}

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

1. **Host fallback**: You list multiple SSH aliases per host. `rr` tries them in order until one works. Useful when you're sometimes on LAN, sometimes on VPN.

2. **File sync**: Wraps rsync with sane defaults. Excludes `.git`, `node_modules`, etc. Preserves remote-only directories so you don't nuke your venv every sync.

3. **Locking**: Creates a lock file on the remote before running commands. If someone else has the lock, you wait. No more stepping on each other's test runs.

4. **Output formatting**: Auto-detects pytest, jest, go test, cargo and formats failures nicely. You can turn this off if you hate it.

## Other commands

```bash
rr monitor              # TUI dashboard showing CPU/RAM/GPU across hosts
rr completion bash      # Shell completions (also zsh, fish)
```

## Docs

- [Configuration reference](docs/configuration.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Example configs](docs/examples/)
- [Architecture](ARCHITECTURE.md)
- [Contributing](CONTRIBUTING.md)

## License

MIT
