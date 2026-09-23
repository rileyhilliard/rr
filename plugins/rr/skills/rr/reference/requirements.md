# Remote Environment Bootstrap

The `require` field declares tools that must exist on remote hosts before commands run. rr verifies requirements after connecting and taking the lock, before file sync, so a missing tool fails fast. The check is skipped for local execution.

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

1. **Connect**: SSH connection established and lock acquired
2. **Check**: Each required tool verified with `command -v <tool>` (results are cached per host)
3. **Fail**: If any are missing, the command stops with "Missing required tools: ..." and suggests `rr provision`

## Built-in Installers

rr includes installers (macOS and Linux) for about 40 common tools. `rr provision` installs missing tools that have one.

**Supported tools:**
- Languages and runtimes: `go`, `node`, `python`/`python3`, `rust`/`rustc`, `ruby`, `java`/`javac`, `deno`, `bun`
- Package managers: `npm`, `yarn`, `pnpm`, `pip`, `uv`/`uvx`, `cargo`, `gem`
- Build and infra: `make`, `git`, `docker`, `kubectl`, `terraform`, `aws`, `gcloud`
- Utilities: `jq`, `curl`, `wget`, `rsync`, `ripgrep`/`rg`, `fd`, `fzf`, `tree`, `htop`, `tmux`, `vim`, `nvim`/`neovim`, `chromium`

Tools without an installer (e.g. `golangci-lint`, `ruff`, `nvidia-smi`) have to be installed by hand.

```bash
rr provision              # Check all project hosts and offer to install
rr provision --check      # Report only
rr provision --host mini  # One host
rr provision --yes        # No prompts
```

## CLI Flags

### Skip Requirements

Skip requirement checking (only `rr run` and `rr exec` have this flag):

```bash
rr run --skip-requirements "make test"
rr exec --skip-requirements "echo hello"
```

### Doctor Integration

Check requirement status with `rr doctor`:

```bash
rr doctor --requirements
```

Output shows which tools are satisfied, missing, or installable, plus whether rsync is on each host's non-interactive PATH. A missing requirement is a failure (doctor exits 1), since `rr run` refuses to start without it. Unreachable hosts are reported once on their host check and skipped here.

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
Missing required tools: cargo (can install), golangci-lint
Run 'rr provision' to install missing tools.
```

`rr run` and `rr exec` also accept `--skip-requirements` to bypass the check. Named tasks don't have that flag, so for a task the fix is `rr provision` or adding the tool to the host.

In structured output this is an error envelope on stderr with code `DEPENDENCY_MISSING`. rr binaries before this release used `COMMAND_FAILED` for the same error, so match on either code plus a message starting `Missing required tools` if you need to support both.

## Validation

Tool names are validated to prevent command injection. Valid names contain:
- Alphanumeric characters
- Hyphens, underscores, periods
- Plus signs (e.g., `g++`)

Invalid tool names (containing shell metacharacters) are rejected.
