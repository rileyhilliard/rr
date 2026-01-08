# Example configurations

These are example `.rr.yaml` configurations for common project types.

## Available examples

| Example | Description |
|---------|-------------|
| [python-project.yaml](python-project.yaml) | Python project with pytest, mypy, ruff |
| [go-project.yaml](go-project.yaml) | Go project with tests and linting |
| [node-project.yaml](node-project.yaml) | Node.js/TypeScript project with Jest |
| [multi-host.yaml](multi-host.yaml) | Multi-host setup with tags for team use |

## Using an example

1. Copy the example to your project root:
   ```bash
   cp docs/examples/python-project.yaml .rr.yaml
   ```

2. Update the host SSH aliases to match your setup:
   ```yaml
   hosts:
     build-box:
       ssh:
         - your-host.local      # Your actual hostname
         - your-host-tailscale  # Your VPN alias
   ```

3. Test the connection:
   ```bash
   rr doctor
   ```

4. Run a command:
   ```bash
   rr run "make test"
   ```

## Customizing

All examples use sensible defaults. Common customizations:

- **Add more exclude patterns** for files you don't need on remote
- **Add preserve patterns** for dependencies installed on remote
- **Define tasks** for commands you run frequently
- **Adjust lock timeout** for longer-running operations
