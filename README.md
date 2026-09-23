# rr (Road Runner)

<p align="center">

![demo-extended](https://github.com/user-attachments/assets/418782f4-04ac-4f93-a04e-dddf4f9f6125)

</p>

<p align="center">
  <strong>Run your test suite on dedicated machines instead of your laptop, split across as many hosts as you have.</strong>
</p>

<p align="center">
  <a href="https://github.com/rileyhilliard/rr/actions/workflows/ci.yml"><img src="https://github.com/rileyhilliard/rr/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/github/go-mod/go-version/rileyhilliard/rr" alt="Go"></a>
  <a href="https://github.com/rileyhilliard/rr/releases"><img src="https://img.shields.io/github/v/release/rileyhilliard/rr" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

---

rr syncs your working tree to one or more remote machines over rsync and SSH, runs a command there, and streams the output back. It picks whichever host is free, locks it so two runs don't collide, and can split one test suite into shards that run on several hosts at once.

```bash
rr run "make test"           # sync, run on the first free host, stream output back
rr test                      # a named task; parallel tasks fan out across every host
rr test-api -- tests/auth -x # forward args to scope a task to one path
```

It's a single Go binary with a YAML config. Each remote needs SSH access and rsync. There's no agent or daemon to install on it.

## Contents

-   [Why rr](#why-rr)
-   [Built for coding agents](#built-for-coding-agents)
-   [Quick start](#quick-start)
-   [Install](#install)
-   [Setup](#setup)
-   [Usage](#usage)
-   [Parallel execution](#parallel-execution)
-   [Configuration](#configuration)
-   [How it works](#how-it-works)
-   [Commands](#commands)
-   [Troubleshooting](#troubleshooting)
-   [Documentation](#documentation)
-   [Contributing](#contributing)

## Why rr

Coding agents that work test-first run the test suite constantly: after every change, before every commit, and once more to confirm. On a real project each of those runs takes minutes and pins every core on the machine. Run two agents in separate worktrees, or let one agent fan out subagents, and the runs start fighting over CPU, ports, and test databases on the same laptop. Tests fail because runs interfere with each other, and the machine you're working on becomes unusable while they do.

rr moves those runs onto machines whose only job is running tests. Your laptop edits code and the runners run it. A few spare mini PCs or an old workstation in the closet is enough.

Splitting the suite is where the time goes down. Break a suite into shards (by package, by directory, backend vs frontend) and rr runs each shard on a different host at the same time. The speedup is close to linear: two hosts finish in roughly half the time, three in roughly a third. The floor is your slowest shard, so it pays to keep shards similar in size.

[OpenData](https://tryopendata.ai) is the heaviest user of rr today. Its suite has around 14,000 tests across a Python data pipeline, a FastAPI backend, and TypeScript frontend and MCP packages, and one command runs all of it:

```yaml
# Simplified from OpenData's .rr.yaml
local_fallback: never   # a run that silently lands on the laptop must never count as a pass

tasks:
    test:
        description: Run all tests in parallel
        setup: (cd opendata && uv sync --quiet) & (cd backend && uv sync --quiet) & bun install --frozen-lockfile & wait
        parallel:
            - test-opendata
            - test-backend-api
            - test-backend-services
            - test-backend-infra
            - test-frontend-fast
            - test-mcp-fast
        fail_fast: false

    test-opendata:
        description: Run OpenData tests (extra args forward to pytest)
        run: cd opendata && uv run pytest {args} -n 4 --no-cov -q --tb=short
```

Agents working on OpenData iterate with scoped runs like `rr test-opendata -- tests/test_services/test_foo.py -x`, which run only the tests touching their change, and treat the full `rr test` as the gate before a commit. None of it runs on the laptop.

I built rr for two reasons. The first was my laptop: the fan spun up and the battery drained every time I ran tests, while a few much faster machines sat idle in the corner.

The bigger one was multi-agent coding. When several agents work on the same project at once, each in its own worktree, each one runs the test suite whenever it wants to check its work. They don't know about each other. Run locally, their suites start at the same moment on the same machine, compete for CPU, and collide on shared ports, test databases, and caches, so tests fail for reasons that have nothing to do with the code. rr puts those runs in a queue. Each run takes a lock on a free runner, and the next one goes to another host or waits its turn, so every agent gets a clean run of its suite and none of them step on each other.

![Pi7_GIF_CMP](https://github.com/user-attachments/assets/9ffe029e-af38-4a77-8c77-156fae378db7)

### What else you could use

| Tool                   | Where it falls short for this                                         |
| ---------------------- | --------------------------------------------------------------------- |
| `rsync && ssh`         | Works for one host. No locking, no failover, no splitting across hosts |
| CI (GitHub Actions)    | Needs a push per iteration and queues for minutes. Too slow for an inner loop |
| Ansible                | Inventory files and playbooks to run one command                      |
| DevPod / Tilt          | Container-first. More than you need to sync and run                   |
| VS Code Remote         | Tied to the IDE. Doesn't help a CLI agent                             |

## Built for coding agents

Most of rr's defaults assume the caller is an agent or a script, and a person watching is the less common case.

Output is structured by default. rr writes JSON phase events (connect, sync, exec) to stderr and ends with a single result event. Your command's stdout and stderr pass through untouched, so test output reads the same as it would locally. Add `--pretty` (or `-p`) when a human is watching and you want spinners and colors.

```bash
$ rr run "go test ./..."
{"type":"phase","phase":"connect","status":"complete","host":"m4-mini","duration_s":0.21,...}
{"type":"phase","phase":"sync","status":"complete","host":"m4-mini","duration_s":0.84,...}
ok      github.com/you/project/internal/api    3.112s
{"type":"result","status":"success","host":"m4-mini","duration_s":4.9,"exit_code":0,"details":{...}}
```

The other defaults each solve a problem that came up with real agents:

| Behavior                  | Why it matters for an agent                                                                                                                                                         |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Error codes               | Failures carry a code (`LOCK_HELD`, `SSH_TIMEOUT`, `DEPENDENCY_MISSING`, `HOST_NOT_FOUND`, `CONFIG_NOT_FOUND`, ...) and a suggested fix, so the agent can branch on the code without parsing prose. Codes are set where the error happens, not guessed from the message. |
| Parsed test results       | For pytest, Jest, and Go test, the result carries pass/fail counts (`details.summary`) and each failing test with its `file:line` and message (`details.failures`), so the agent doesn't scroll back through the output. |
| Zero-test detection       | A run that collected no tests keeps its exit code but sets `details.no_tests`, so a bad path filter doesn't look like a passing suite.                                                  |
| Loud local fallback       | If `local_fallback` is on and rr ends up running locally because hosts were unreachable or all locked, it emits a warning event and sets `details.fallback`; for locked hosts it lists who holds each lock. A deliberate local run (`--local`, or local mode) isn't a fallback, and its connect event says so in `details.reason`. Set `local_fallback: false` if a local run should never count. |
| Path rewriting and hints  | Absolute local paths in a command (`/Users/you/project/tests/...`) are rewritten to the remote copy. A failure that looks like a local-path mistake gets a hint explaining the mapping.   |
| Scoped runs               | Extra arguments forward into the task (`rr test-api -- tests/auth -x`), or into an `{args}` placeholder, so an agent can run one file without a new task definition.                           |
| Worktree isolation        | Each git worktree syncs to its own remote directory, so parallel agents on separate branches never overwrite each other's tree. `rr prune` removes directories for deleted worktrees.      |
| Locking and queueing      | One run per host at a time. Extra runs move to the next free host or wait in line rather than competing for the same CPU and ports.                                                    |

If you use [Claude Code](https://claude.ai/code), the `rr` plugin teaches Claude how to set up and use rr:

```bash
/plugin marketplace add https://github.com/rileyhilliard/rr
/plugin install rr@rr
```

Then run `/rr:setup` from your project root. It writes the configs, checks SSH connectivity, runs a test command remotely, and makes sure the tools your project needs exist on each host. See [docs/claude-code.md](docs/claude-code.md) for details.

## Quick start

```bash
# 1. Install
brew install rileyhilliard/tap/rr    # or see Install below

# 2. Set up in your project
cd your-project
rr init                               # creates .rr.yaml

# 3. Run something (--pretty for human-readable output)
rr --pretty run "make test"
```

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

Grab the binary for your platform from [releases](https://github.com/rileyhilliard/rr/releases). rr runs on macOS, Linux, and Windows under WSL.

## Setup

You need `ssh` and `rsync` on your machine, and passwordless SSH access to each remote. If `ssh user@yourhost` logs in without asking for a password, you're set. If it doesn't, follow the [SSH setup guide](docs/ssh-setup.md), or run `rr setup <host>` to configure keys and test the connection.

```bash
rr init          # creates .rr.yaml with interactive prompts
rr doctor        # checks SSH, rsync, config, and each host
```

If `rr init` doesn't pick up your SSH config, you can add hosts by hand. See the [configuration docs](docs/configuration.md).

## Usage

```bash
rr run "make test"    # sync files, then run the command
rr exec "git status"  # run without syncing (faster for quick checks)
rr sync               # sync only
rr pull coverage.xml  # copy a file back from the remote
```

Define named tasks in `.rr.yaml`:

```yaml
tasks:
    test:
        run: pytest -n auto
    build:
        run: make build
```

Then run them by name:

```bash
rr test                    # same as: rr run "pytest -n auto"
rr test tests/test_api.py  # extra args are appended: pytest -n auto tests/test_api.py
rr test -- -k login -x     # use -- before flags so rr doesn't parse them itself
rr tasks                   # list available tasks
```

If the arguments need to go somewhere other than the end of the command, put `{args}` where they belong: `run: pytest {args} --tb=short`.

![demo-tasks](https://github.com/user-attachments/assets/8d902e99-9b7a-4fa9-a2fa-bbdef8365e3b)

## Parallel execution

![parallel](https://github.com/user-attachments/assets/95d48195-f179-45b4-8fd3-739b88041049)

A parallel task lists other tasks. rr sends each one to a free host and runs them at the same time:

```yaml
# .rr.yaml
tasks:
    test-backend:
        run: cd backend && pytest
    test-frontend:
        run: cd frontend && npm test
    test-pipeline:
        run: cd pipeline && python -m pytest

    test:
        parallel: [test-backend, test-frontend, test-pipeline]
        fail_fast: false     # keep going if one fails
        max_parallel: 3      # optional concurrency cap
```

With `--pretty`, each task animates while it runs and then switches to pass or fail in place:

```
$ rr --pretty test
◉ test-backend [m1-linux]
◉ test-frontend [m1-mini]
◉ test-pipeline [m4-mini]

Parallel Execution Summary

  ◉ test-backend on m1-linux (52.9s)
  ◉ test-frontend on m1-mini (4.0s)
  ◉ test-pipeline on m4-mini (40.0s)

  ◉ 3 passed  ✕ 0 failed  ● 3 total  (52.9s)
  Hosts: m1-linux, m1-mini, m4-mini
```

Run in sequence on one machine, those three would take about 97 seconds. In parallel the total is the slowest task, 52.9 seconds. Add hosts and split big tasks into shards, and the total keeps approaching the size of your largest shard.

Parallel tasks can reference other parallel tasks, and rr flattens the tree:

```yaml
tasks:
    test-pipeline:
        parallel: [pipeline-1, pipeline-2, pipeline-3]
    test-backend:
        parallel: [backend-1, backend-2, backend-3]
    test:
        parallel: [test-pipeline, test-backend, frontend]   # expands to 7 tasks
```

`rr test-pipeline` runs only the pipeline shards, and `rr test` runs everything. A shard you add to `test-pipeline` is picked up by `test` automatically. To keep a subtask on specific machines (a GPU box, say, or away from a production host), give it a `hosts:` list, and the parallel scheduler honors it.

Each parallel run writes per-task logs and a `summary.json` to `~/.rr/logs/<task>-<timestamp>/`. `rr logs` lists recent runs. A subtask's `pull:` runs after the whole group finishes, pass or fail, and lands in `<dest>/<subtask>/` so shards don't overwrite each other's reports. Subtasks that share a host also share its remote directory, so give each one its own output path.

Output modes:

```bash
rr test                    # structured JSON events (default)
rr --pretty test           # animated progress
rr --pretty test --stream  # live output with [host:task] prefixes
rr --pretty test --verbose # full output as each task completes
rr test --dry-run          # show the plan without running anything
```

## Configuration

rr uses two config files so your personal machine list stays out of the repo:

| File    | Location            | Purpose                            | Commit to git? |
| ------- | ------------------- | ---------------------------------- | -------------- |
| Global  | `~/.rr/config.yaml` | Your hosts, SSH paths, directories | No             |
| Project | `.rr.yaml`          | Sync rules, tasks, host selection  | Yes            |

The global config defines your machines:

```yaml
# ~/.rr/config.yaml
version: 1

hosts:
    mini:
        ssh:
            - mac-mini.local     # try LAN first
            - mac-mini-tailscale # fall back to VPN
        dir: ${HOME}/projects/${PROJECT}
        require: [go, node]      # tools that must exist on this host
```

The project config is shared with your team:

```yaml
# .rr.yaml
version: 1
hosts: [mini]            # optional; defaults to all hosts
local_fallback: false    # never quietly run on the laptop

require: [go, golangci-lint]

sync:
    exclude: [.git, node_modules, .venv] # bare patterns: .git is a file in linked worktrees

tasks:
    build:
        run: go build ./...
        require: [go]
```

`${PROJECT}` expands to the git repo name (or the directory name outside a repo). In a linked git worktree it expands to `repo@worktree-name`, which gives each worktree its own remote directory (turn this off with `sync.worktree_isolation: false`). `rr provision` checks the `require:` lists and offers to install missing tools. See the [configuration docs](docs/configuration.md) for every option.

## How it works

Each host can list several SSH targets. rr dials all of them at once and prefers the earlier ones: if a later target connects first, rr gives earlier targets another 500ms to connect before using it. Dead targets don't stall the connection, and the order you list them in still decides the winner. Put the fastest path first:

```yaml
hosts:
    gpu-box:
        ssh:
            - 192.168.1.50    # LAN, fastest
            - gpu-tailscale   # VPN, works from anywhere
            - user@backup-gpu # backup machine
```

| Your location | What happens                   |
| ------------- | ------------------------------ |
| Home (LAN)    | Uses `192.168.1.50`            |
| Coffee shop   | LAN unreachable, uses Tailscale |
| gpu-box down  | Falls through to `backup-gpu`  |

Sync is rsync with defaults that skip `.git` and dependency folders and keep remote-only directories (like a `.venv` built on the remote) in place. Each sync writes a `.rr-source` marker recording which machine and directory the tree came from. If you sync over a mirror owned by a different checkout, rr warns you before overwriting it.

Before running a command, rr takes a lock on the host by atomically creating a directory under `/tmp` on the remote. If the host is locked, rr tries the next one. If every host is locked, it cycles through them until one frees up. The holder refreshes the lock every 30 seconds. A lock that stops being refreshed goes stale after 90 seconds (`lock.stale`), and one left by a killed rr process on your own machine is cleared right away. `rr unlock` clears one by hand. [ARCHITECTURE.md](docs/ARCHITECTURE.md) covers the internals.

rr recognizes pytest, Jest, and Go test output from the command and parses the counts and failures into the result event. Parallel runs report the same per task.

## Commands

```bash
# Core workflow
rr run "make test"      # sync + run
rr exec "git status"    # run without syncing
rr sync                 # sync only
rr pull "dist/*.whl"    # copy files back from the remote

# Tasks
rr test                 # run a named task
rr tasks                # list tasks

# Hosts and environment
rr host list            # list configured hosts
rr host add             # add a host interactively
rr host remove mini     # remove a host
rr setup mini           # set up SSH keys for a host and test the connection
rr provision            # install tools listed in require:

# Monitoring and diagnosis
rr monitor              # TUI dashboard: CPU, RAM, GPU, disk, network per host
rr monitor --once --json # one-shot snapshot for scripts
rr status               # connection and sync status
rr doctor               # check config, SSH, and dependencies
rr logs                 # recent parallel-run logs

# Maintenance
rr unlock               # release a stuck lock
rr prune                # remove remote dirs for deleted worktrees
rr update               # update to the latest release
rr completion zsh       # shell completions (also bash, fish, powershell)
```

Flags that come up often: `--pretty` for human output, `--host <name>` to pin a host, `--tag <tag>` to pick hosts by tag, and `--local` to run on this machine.

## Troubleshooting

Start with `rr doctor`. It checks your config, SSH setup, and dependencies on the project's hosts, and exits 1 only when something would actually block a run (warnings exit 0):

```bash
rr doctor
```

![demo-doctor](https://github.com/user-attachments/assets/b7f6dc8c-649d-439d-ba1f-5ceb237f680c)

| Problem                     | Fix                                                                                    |
| --------------------------- | -------------------------------------------------------------------------------------- |
| Connection fails            | Run `rr doctor`. Make sure `ssh user@host` works without a password.                   |
| Command not found on remote | The remote non-interactive PATH differs from your shell's. Add `shell: "zsh -l -c"` to the host. |
| Sync is slow                | Check your exclude patterns. Syncing `node_modules` or `.git` makes it much slower.    |
| Lock stuck                  | Run `rr unlock`. A lock whose holder died goes stale after 90 seconds.                 |

The [troubleshooting guide](docs/troubleshooting.md) covers more.

## Documentation

| Guide                                      | Description                         |
| ------------------------------------------ | ----------------------------------- |
| [SSH Setup](docs/ssh-setup.md)             | Get passwordless SSH working        |
| [Configuration](docs/configuration.md)     | All config options                  |
| [Claude Code](docs/claude-code.md)         | The rr plugin and `/rr:setup`       |
| [Troubleshooting](docs/troubleshooting.md) | Common issues and fixes             |
| [Architecture](docs/ARCHITECTURE.md)       | How rr works under the hood         |
| [Migration](docs/MIGRATION.md)             | Upgrading from older versions       |
| [Examples](docs/examples/)                 | Sample configs for different setups |

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## License

[MIT](LICENSE)
