# rr

Sync code and run commands on remote machines.

## Features

- **Sync and run** - Rsync files to a remote host and execute commands in one step
- **Multi-host fallback** - Configure multiple SSH aliases per host; `rr` tries each until one connects
- **Distributed locking** - Prevent concurrent executions on shared hosts
- **Smart output formatting** - Auto-detect test frameworks (pytest, jest, go test, cargo) for better output
- **Named tasks** - Define reusable command sequences in your config
- **Doctor diagnostics** - Built-in troubleshooting for SSH, rsync, and config issues
- **Real-time monitoring** - TUI dashboard showing CPU, RAM, GPU across all hosts

## Installation

### Quick install (macOS/Linux)

```bash
curl -sSL https://raw.githubusercontent.com/rileyhilliard/rr/main/scripts/install.sh | bash
```

### Homebrew (macOS/Linux)

```bash
# Install directly
brew install rileyhilliard/tap/rr

# Or tap first, then install
brew tap rileyhilliard/tap
brew install rr
```

### Go install

```bash
go install github.com/rileyhilliard/rr/cmd/rr@latest
```

### From source

```bash
git clone https://github.com/rileyhilliard/rr.git
cd rr
make build
./rr --help
```

## Quick start

### 1. Create a config file

```bash
rr init
```

This creates `.rr.yaml` in your project directory with interactive prompts for your SSH host.

### 2. Set up SSH (if needed)

```bash
rr setup myserver
```

Guides you through SSH key generation and deployment to the remote host.

### 3. Run commands remotely

```bash
# Sync files and run a command
rr run "make test"

# Run without syncing (files already there)
rr exec "git status"

# Just sync, no command
rr sync
```

## Configuration

The `.rr.yaml` file lives in your project root. Here's a basic example:

```yaml
version: 1

hosts:
  mini:
    ssh:
      - mac-mini.local
      - mac-mini-tailscale
    dir: ~/projects/${PROJECT}

default: mini

sync:
  exclude:
    - .git/
    - node_modules/
    - .venv/
  preserve:
    - node_modules/
    - .venv/
```

### Key sections

| Section | Purpose |
|---------|---------|
| `hosts` | Remote machines and their SSH connection details |
| `sync` | Files to exclude from sync, files to preserve on remote |
| `lock` | Distributed locking settings |
| `tasks` | Named command sequences |
| `output` | Terminal output formatting |

See [docs/configuration.md](docs/configuration.md) for the complete reference.

## Commands

| Command | Description |
|---------|-------------|
| `rr run <cmd>` | Sync files and run command on remote |
| `rr exec <cmd>` | Run command without syncing |
| `rr sync` | Sync files to remote |
| `rr init` | Create `.rr.yaml` config file |
| `rr setup <host>` | Configure SSH keys and test connection |
| `rr doctor` | Diagnose connection and config issues |
| `rr monitor` | Real-time resource dashboard for all hosts |
| `rr completion` | Generate shell completions |

### Common flags

```bash
--host <name>      # Target a specific host
--tag <tag>        # Target hosts with a specific tag
--probe-timeout    # SSH connection timeout (e.g., 5s, 2m)
```

## Shell completions

### Bash

```bash
rr completion bash > /etc/bash_completion.d/rr
# Or for user-local:
rr completion bash > ~/.local/share/bash-completion/completions/rr
```

### Zsh

```bash
rr completion zsh > "${fpath[1]}/_rr"
# Then add to ~/.zshrc:
autoload -U compinit && compinit
```

### Fish

```bash
rr completion fish > ~/.config/fish/completions/rr.fish
```

## Monitoring hosts

Watch CPU, RAM, GPU, and network across all configured hosts:

```bash
rr monitor
```

The dashboard shows real-time metrics with color-coded thresholds. Keyboard controls:

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Select host |
| `Enter` | Expand host details |
| `Esc` | Return to list |
| `s` | Cycle sort order |
| `r` | Force refresh |
| `q` | Quit |

Options:

```bash
rr monitor --hosts mini,server  # Monitor specific hosts
rr monitor --interval 1s        # Update every second
```

## Troubleshooting

Run `rr doctor` to diagnose common issues:

```bash
rr doctor
```

This checks:
- SSH key configuration
- Host connectivity
- rsync availability (local and remote)
- Config file validity
- Lock file status

See [docs/troubleshooting.md](docs/troubleshooting.md) for solutions to common problems.

## Documentation

- [Configuration reference](docs/configuration.md) - All config options explained
- [Troubleshooting guide](docs/troubleshooting.md) - Common errors and fixes
- [Architecture](ARCHITECTURE.md) - Design decisions and system overview
- [Contributing](CONTRIBUTING.md) - Development setup and guidelines

## License

MIT License. See [LICENSE](LICENSE) for details.
