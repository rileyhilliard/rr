# Example configurations

These are example configurations for common project types. `rr` uses two config files:

- **Global config** (`~/.rr/config.yaml`) - Your personal host definitions (not shared)
- **Project config** (`.rr.yaml`) - Shareable project settings

## Available examples

### Project configs (copy to your project as `.rr.yaml`)

| Example | Description |
|---------|-------------|
| [python-project.yaml](python-project.yaml) | Python project with pytest, mypy, ruff |
| [go-project.yaml](go-project.yaml) | Go project with tests and linting |
| [node-project.yaml](node-project.yaml) | Node.js/TypeScript project with Jest |
| [multi-host.yaml](multi-host.yaml) | Multi-host setup with load balancing |

### Global config (copy to `~/.rr/config.yaml`)

| Example | Description |
|---------|-------------|
| [global-config.yaml](global-config.yaml) | Example host definitions for all project types |

## Using an example

**1. Set up your global config** (one-time, if you haven't already)

Either use the interactive command:
```bash
rr host add
```

Or copy the example to your home directory:
```bash
mkdir -p ~/.rr
cp docs/examples/global-config.yaml ~/.rr/config.yaml
```

Then update the SSH aliases to match your actual hosts.

**2. Copy a project config to your project**

```bash
cd your-project
cp docs/examples/python-project.yaml .rr.yaml
```

**3. Update the host reference**

Edit `.rr.yaml` and change the `host:` field to match a host name from your global config:

```yaml
# Reference a host from ~/.rr/config.yaml
host: your-host-name
```

**4. Test the connection**

```bash
rr doctor
```

**5. Run a command**

```bash
rr run "make test"
```

## Customizing

All examples use sensible defaults. Common customizations:

- **Add more exclude patterns** for files you don't need on remote
- **Add preserve patterns** for dependencies installed on remote
- **Define tasks** for commands you run frequently
- **Adjust lock timeout** for longer-running operations
- **Add multiple hosts** for load balancing across machines
