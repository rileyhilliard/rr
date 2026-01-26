# Tasks Reference

Define reusable commands in `.rr.yaml`.

## Basic Tasks

```yaml
tasks:
  test:
    description: Run all tests
    run: pytest -v

  build:
    description: Build the project
    run: make build
    env:
      CGO_ENABLED: "0"
```

Run with: `rr test`, `rr build`

## Task Arguments

Extra arguments are appended to single-command tasks:

```bash
rr test tests/test_api.py    # Runs: pytest -v tests/test_api.py
rr test -k "test_login"      # Runs: pytest -v -k "test_login"
```

**Note:** Args are only supported for tasks with a single `run` command, not multi-step tasks.

## Task-Specific Requirements

Tasks can declare their own required tools:

```yaml
tasks:
  build:
    run: cargo build --release
    require: [cargo, rustc]

  lint:
    run: golangci-lint run
    require: [golangci-lint]
```

Requirements are merged: project + host + task.

## Multi-Step Tasks

```yaml
tasks:
  deploy:
    description: Build and deploy
    steps:
      - name: Build
        run: make build
      - name: Test
        run: make test
        on_fail: stop
      - name: Deploy
        run: ./deploy.sh
```

### Step Options

| Field | Default | Purpose |
|-------|---------|---------|
| `name` | `step N` | Display name |
| `run` | required | Command to execute |
| `on_fail` | `stop` | What to do on failure (`stop`, `continue`) |

### Step Progress Output

```
━━━ Step 1/3: Build ━━━
$ make build
[output...]
● Step 1/3: Build (2.3s)

━━━ Step 2/3: Test ━━━
$ make test
[output...]
● Step 2/3: Test (45.1s)
```

## Parallel Tasks

Run multiple tasks concurrently across available hosts:

```yaml
tasks:
  test-all:
    description: Run all tests in parallel
    parallel:
      - test
      - lint
      - vet
    fail_fast: false    # Continue even if one fails
    timeout: 10m

  quick-check:
    description: Fast verification
    parallel:
      - vet
      - lint
    fail_fast: true     # Stop on first failure
    max_parallel: 2     # Limit concurrency
```

Run with: `rr test-all`, `rr quick-check`

### Setup Phase (Once Per Host)

When parallel subtasks need shared setup (dependencies, migrations, etc.), use `setup` to avoid redundant work:

```yaml
tasks:
  test-all:
    setup: pip install -r requirements.txt    # Runs once per host
    parallel:
      - test-unit
      - test-integration
      - test-e2e

  test-unit:
    run: pytest tests/unit -v
  test-integration:
    run: pytest tests/integration -v
  test-e2e:
    run: pytest tests/e2e -v
```

**How it works:**
- Setup runs exactly once per host, after file sync but before any subtasks
- If a host runs 3 subtasks, setup runs once (not 3 times)
- Setup failure aborts all subtasks on that host
- Works with both remote and local execution

**Common use cases:**
- Dependency installation (`uv sync`, `npm install`)
- Database migrations or resets
- Build artifacts needed by multiple tests
- Environment configuration

### Parallel Task Options

| Field | Default | Purpose |
|-------|---------|---------|
| `parallel` | required | List of subtask names |
| `setup` | none | Command to run once per host before subtasks |
| `fail_fast` | `false` | Stop on first failure |
| `timeout` | none | Overall timeout |
| `max_parallel` | unlimited | Max concurrent tasks |

### Parallel Task Flags

| Flag | Purpose |
|------|---------|
| `--stream` | Real-time interleaved output with `[host:task]` prefixes |
| `--verbose` | Full output per task on completion |
| `--quiet` | Summary only |
| `--fail-fast` | Stop on first failure (overrides config) |
| `--max-parallel N` | Limit concurrent tasks |
| `--dry-run` | Show plan without executing |
| `--local` | Force local execution |

### Output Modes

- **progress** (default): Live status indicators with spinners
- **stream**: Real-time output with `[host:task]` prefixes
- **verbose**: Full output shown when each task completes
- **quiet**: Summary only at the end

```bash
rr test-all --stream    # See all output in real-time
rr test-all --dry-run   # Preview what would run
rr test-all --local     # Run locally without remote hosts
```

### Work-Stealing Distribution

Tasks are distributed using a work-stealing queue. All subtasks go into a shared channel, and each host pulls tasks as it becomes available.

**Performance-based optimization:** After the first task completes on each host, rr tracks completion times to identify slow hosts. Slow hosts wait before grabbing additional tasks, giving fast hosts priority. This improves distribution across heterogeneous machines (e.g., M4 vs M1).

For example, with 6 tasks across 3 hosts of varying speeds:
- Without optimization: 2-2-2 distribution (round-robin pattern)
- With optimization: 3-2-1 distribution (fast host grabs more work)

### Log Storage

Task output is saved to `~/.rr/logs/<task>-<timestamp>/`:
- One file per subtask
- Summary file with timing and results

## Host Restrictions

Restrict tasks to specific hosts:

```yaml
tasks:
  gpu-train:
    description: Train model on GPU
    run: python train.py
    hosts: [gpu-box]  # Only runs on gpu-box

  build:
    run: make build
    hosts: [fast, gpu-box]  # Multiple allowed hosts
```
