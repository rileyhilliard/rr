# Remote Environment Bootstrap

The `require` field declares tools that must exist on remote hosts before commands run. rr verifies requirements after SSH connect but before file sync, providing early failure with actionable error messages.

## Configuration

Requirements can be specified at three levels:

### Project Level

Applied to all hosts and tasks:

```yaml
# .rr.yaml
require:
  - go
  - node
  - golangci-lint
```

### Host Level

Applied to a specific host:

```yaml
# ~/.rr/config.yaml
hosts:
  gpu-box:
    ssh: [gpu.local]
    dir: ~/projects/${PROJECT}
    require:
      - nvidia-smi
      - python3
      - cuda
```

### Task Level

Applied when running a specific task:

```yaml
# .rr.yaml
tasks:
  build:
    run: cargo build --release
    require:
      - cargo
      - rustc

  lint:
    run: golangci-lint run
    require:
      - golangci-lint
```

## Merge Order

Requirements from all levels are merged (deduplicated):

1. Project `require` from `.rr.yaml`
2. Host `require` from `~/.rr/config.yaml`
3. Task `require` from `.rr.yaml`

## How It Works

1. **Connect**: SSH connection established
2. **Check**: Each required tool verified with `command -v <tool>`
3. **Report**: Missing tools listed with suggestions
4. **Fail or Install**: If tools are missing, show error or offer installation

## Built-in Installers

rr includes installers for 40+ common tools. When a required tool is missing and has a built-in installer, rr can auto-install it.

**Supported tools include:**
- Languages: `go`, `node`, `python3`, `rust`, `ruby`
- Package managers: `uv`, `pip`, `npm`, `bun`, `cargo`
- Build tools: `make`, `cmake`, `ninja`
- Linters: `golangci-lint`, `eslint`, `ruff`
- Version managers: `nvm`, `pyenv`, `rustup`
- Utilities: `jq`, `yq`, `ripgrep`, `fd`, `fzf`
- And more...

## CLI Flags

### Skip Requirements

Skip requirement checking:

```bash
rr run --skip-requirements "make test"
rr exec --skip-requirements "echo hello"
```

### Doctor Integration

Check requirement status with `rr doctor`:

```bash
rr doctor --requirements
```

Output shows which tools are satisfied, missing, or installable.

## Example Workflows

### Python Project

```yaml
# .rr.yaml
require:
  - python3
  - uv

tasks:
  test:
    run: uv run pytest -v

  lint:
    run: uv run ruff check .
    require: [ruff]
```

### Go Project

```yaml
# .rr.yaml
require:
  - go

tasks:
  test:
    run: go test ./...

  lint:
    run: golangci-lint run
    require: [golangci-lint]

  build:
    run: go build -o bin/app ./cmd/app
```

### Multi-Language Project

```yaml
# .rr.yaml
require:
  - node
  - go

tasks:
  frontend:
    run: npm run build
    require: [npm]

  backend:
    run: go build ./...

  test-all:
    parallel:
      - frontend
      - backend
```

### GPU Machine

```yaml
# ~/.rr/config.yaml
hosts:
  gpu-box:
    ssh: [gpu.local, gpu-tailscale]
    dir: ~/ml/${PROJECT}
    require:
      - nvidia-smi
      - python3
      - cuda
    env:
      CUDA_VISIBLE_DEVICES: "0"
```

## Error Messages

When requirements are missing, rr shows actionable errors:

```text
✗ Requirements check failed on gpu-box:
  Missing: cargo, rustc

  Suggestion: 2 tools can be auto-installed.
  Run with missing tools to trigger installation prompts,
  or install manually and retry.
```

## Validation

Tool names are validated to prevent command injection. Valid names contain:
- Alphanumeric characters
- Hyphens, underscores, periods
- Plus signs (e.g., `g++`)

Invalid tool names (containing shell metacharacters) are rejected.
