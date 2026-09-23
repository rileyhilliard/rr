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

Extra arguments are appended to single-command tasks. Flag-like args must come after `--`, because rr parses flags first:

```bash
rr test tests/test_api.py      # Runs: pytest -v tests/test_api.py
rr test -- -k "test_login"     # Runs: pytest -v -k test_login
```

Args are shell-quoted before they reach the remote shell, so `rr test -- -k "a or b"` stays one argument and globs/`$VARS` in args are not expanded.

Use an `{args}` placeholder to put args somewhere other than the end:

```yaml
tasks:
  test:
    run: pytest {args:-tests/} -n 4 | tail -20
```

- `{args}` - replaced by the args (empty if none)
- `{args:-default}` - replaced by the args, or `default` when none are given
- `{{args}}` - a literal `{args}`

A task whose `run` is a compound command (pipes, `&&`, `;`, redirections, `$()`, backticks) errors when given args without a placeholder, since appended args would land on the last command in the pipeline.

Args are only supported for tasks with a single `run` command. Multi-step tasks reject them.

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

### Nested Parallel Tasks

Parallel tasks can reference other parallel tasks. `rr` automatically flattens the hierarchy:

```yaml
tasks:
  # Split test suites for parallelization
  opendata-1:
    run: pytest opendata --test-group 1
  opendata-2:
    run: pytest opendata --test-group 2
  opendata-3:
    run: pytest opendata --test-group 3

  backend-1:
    run: pytest backend --test-group 1
  backend-2:
    run: pytest backend --test-group 2

  frontend:
    run: npm test

  # Group by component
  test-opendata:
    parallel: [opendata-1, opendata-2, opendata-3]
  test-backend:
    parallel: [backend-1, backend-2]

  # Reference parallel tasks - expands to 6 tasks
  test:
    parallel: [test-opendata, test-backend, frontend]
```

Running `rr test` expands to: `opendata-1`, `opendata-2`, `opendata-3`, `backend-1`, `backend-2`, `frontend`

**Benefits:**
- Run `rr test-opendata` for just opendata splits
- Run `rr test` for everything
- Add splits to `test-opendata` and `test` automatically includes them
- Circular references are detected at config validation

Use `--dry-run` to see the expanded task list.

### Running Tasks Multiple Times (Flake Detection)

List the same task multiple times to run it concurrently across hosts:

```yaml
tasks:
  flake-test:
    description: Run tests 5x to detect flakiness
    parallel:
      - test
      - test
      - test
      - test
      - test
    fail_fast: false
```

Each instance runs independently and is distributed across available hosts via work-stealing. This is useful for detecting flaky tests by running the same test suite multiple times in parallel.

For ad-hoc flake detection without config changes, use `--repeat` (on `rr run` and single-command tasks without `depends`):

```bash
rr test --repeat 5           # Run test task 5x
rr run --repeat 5 "pytest"   # Run raw command 5x
```

### Parallel Task Options

| Field | Default | Purpose |
|-------|---------|---------|
| `parallel` | required | List of subtask names |
| `setup` | none | Command to run once per host before subtasks |
| `fail_fast` | `false` | Stop on first failure |
| `timeout` | none | Per-subtask timeout |
| `max_parallel` | unlimited | Max concurrent tasks |
| `forward_args` | `false` | Forward CLI args to every subtask |
| `output` | `progress` | Output mode: `progress`, `stream`, `verbose`, or `quiet`. Any other value is a config error; on a non-parallel task it's ignored with a config warning |

Subtasks get the same environment and setup as single tasks. Env merges host `env`, then `defaults.env`, then the subtask's `env` (later wins). Host `setup_commands` and `defaults.setup` run after the `cd` into the project dir.

### Pulling files from subtasks

Put `pull:` on the subtasks that produce files, not on the parallel task (there it has no effect and draws a config warning):

```yaml
tasks:
  test-all:
    parallel: [test-unit, test-integration]
  test-unit:
    run: pytest tests/unit --junitxml=reports/unit.xml
    pull:
      - src: reports/unit.xml
        dest: ./artifacts
  test-integration:
    run: pytest tests/integration --junitxml=reports/integration.xml
    pull:
      - src: reports/integration.xml
        dest: ./artifacts
```

Pulls run after every subtask finishes, pass or fail, one at a time from the host each subtask ran on. Each subtask's files land in `<dest>/<subtask>/` (here `./artifacts/test-unit/unit.xml`), so shards can't overwrite each other locally. A failed pull is reported (a `pull` `failed` event with `details.task`) and doesn't change the exit code. Nothing is pulled for local runs or after Ctrl+C.

Subtasks that run on the same host share one remote directory, so they must write to distinct paths. Two subtasks both writing `reports/junit.xml` on one host overwrite each other before the pull.

### Forwarding Args to Subtasks

Parallel tasks reject extra args, flags included, unless `forward_args: true` is set. The error is `CONFIG_INVALID`, with a hint to set `forward_args: true` and pass flags after `--`:

```yaml
tasks:
  test-backend:
    parallel: [test-backend-api, test-backend-services]
    forward_args: true
  test-backend-api:
    run: pytest tests/api {args}
  test-backend-services:
    run: pytest tests/services {args}
```

`rr test-backend -- -k bond` runs both subtasks with `-k bond`. Subtasks that are compound commands need an `{args}` placeholder, and multi-step subtasks can't receive forwarded args. A filter that matches nothing in one subtask leaves it with zero tests (pytest exits 5); the result event lists those under `no_tests_tasks`.

### Parallel Task Flags

| Flag | Purpose |
|------|---------|
| `--stream` | Real-time interleaved output with `[host:task]` prefixes |
| `--verbose` | Full output per task on completion |
| `--quiet` | Summary only |
| `--fail-fast` | Stop on first failure (overrides config) |
| `--max-parallel N` | Limit concurrent tasks |
| `--no-logs` | Don't save output to log files |
| `--dry-run` | Show plan without executing |
| `--local` | Force local execution |
| `--host` / `--tag` | Restrict the host pool |

### Output Modes

These apply to `--pretty` mode. In the default structured mode, a parallel task prints nothing to stdout unless you pass `--stream` (prefixed live output) or `--verbose`, and reports everything in one result event on stderr (`total`, `passed`, `failed`, `log_dir`, and per-subtask `failures`).


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
- `summary.json` with timing and results

Single-command runs (`rr run`, `rr exec`, single tasks) also log to `~/.rr/logs/<name>-<timestamp>/output.log`, reported as `details.log_file`. Manage with `rr logs` and `rr logs clean`.

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

Restrictions also apply to subtasks inside parallel tasks: a restricted subtask only runs on its allowed hosts, and fails with the restriction named if none of them is available. `--host`/`--tag` that excludes every allowed host fails up front.
