# rr - Remote Runner

Sync code to remote machines and execute commands with smart host fallback.

## Installation

Coming soon. For now, build from source:

```bash
git clone https://github.com/rileyhilliard/rr.git
cd rr
go build -o rr ./cmd/rr
```

## Quick start

```bash
# Create a config file
rr init

# Sync files and run a command
rr run "pytest tests/"

# Run a named task (defined in .rr.yaml)
rr test

# Check host connectivity
rr status
```

## Documentation

- [Architecture](ARCHITECTURE.md) - Design decisions and system overview
- [Contributing](CONTRIBUTING.md) - Development setup and guidelines

## License

MIT License. See [LICENSE](LICENSE) for details.
