# Agent Compatibility Feedback

Real-world friction encountered using rr with Claude Code (AI coding agent) during a multi-package test coverage task on the OpenData project. Each issue below includes enough context for a fixing agent to understand the problem and implement the solution without additional investigation.

---

## Issue 1: `rr run` should default to structured (agent-friendly) output

### Problem

rr's primary consumers are increasingly AI agents (Claude Code, Cursor, Copilot Workspace) and CI systems, not humans watching terminals. But `rr run` defaults to human-oriented output: spinners, colored progress bars, lipgloss-styled dividers, and inline rsync progress updates. Agents can't reliably parse this output to determine:

- When sync finished and command execution started
- When command execution completed
- What the exit code was
- Which host ran the command

The existing `--machine` / `-m` flag has a proper `JSONEnvelope` structure (defined in `internal/cli/json.go`) but is only implemented for metadata commands (`tasks`, `host list`, `doctor`, `status`). The `run` and `exec` commands always use `output.NewStreamHandler` with lipgloss-decorated output regardless of flags.

### Current behavior

```
# What rr run outputs today (human-friendly, agent-hostile):
[spinner] Connecting to m4-mini...
[spinner] Syncing files...
  1,234 files  45.2MB  100%
─────────────────────────────────────────────
$ pytest -v
... (test output interleaved with decorations) ...
───────────
✓ Completed on m4-mini (12.3s total, 10.1s exec)
```

An agent sees this as undifferentiated text. It cannot reliably distinguish rr's chrome from the actual command output without fragile regex parsing.

### Desired behavior

**rr should be agent-first.** Structured output should be the DEFAULT. Human-readable decorated output should be the opt-in mode (via `--pretty` or `--human` flag, or auto-detected when stdout is a TTY).

Default output (no flags, stdout is not a TTY):
```json
{"phase": "sync", "status": "started", "host": "m4-mini"}
{"phase": "sync", "status": "complete", "files": 1234, "bytes": 47421440, "duration_ms": 2100}
{"phase": "exec", "status": "started", "command": "pytest -v", "host": "m4-mini"}
```
Then the raw command stdout/stderr streams through undecorated, followed by:
```json
{"phase": "exec", "status": "complete", "exit_code": 0, "duration_ms": 10100}
{"result": {"success": true, "exit_code": 0, "host": "m4-mini", "total_duration_ms": 12300, "sync_duration_ms": 2100, "exec_duration_ms": 10100}}
```

When stdout IS a TTY (human at terminal), show the current pretty output with spinners and colors.

### Implementation guidance

**Key files:**
- `internal/cli/run.go` - The `Run()` function (line 42) is the main workflow. Currently always creates `output.NewStreamHandler(os.Stdout, os.Stderr)` at line 69.
- `internal/cli/json.go` - Already has `JSONEnvelope`, `WriteJSONSuccess`, `WriteJSONError`. Extend with phase-level events.
- `internal/config/types.go` - `OutputConfig` (line 364) has `Format`, `Color`, `Verbosity` fields. Add a mode concept here.

**Approach:**
1. Auto-detect: if stdout is a TTY, use pretty mode. If not (piped, redirected, or agent), use structured mode.
2. Allow override: `--pretty` forces human mode, `--machine`/`-m` forces structured mode regardless of TTY detection.
3. In structured mode, the `Run()` function should:
   - Emit JSON phase events to stderr (so they don't mix with command stdout)
   - Stream command stdout/stderr through unmodified (no spinners, no lipgloss, no dividers)
   - Emit a final JSON result envelope to stderr after command completes
4. The existing `MachineMode()` bool in `json.go` already gates behavior in other commands. Wire `run.go` into this same mechanism.

**Exit code contract:** The process exit code should always match the remote command's exit code (already works this way). The JSON envelope is supplementary metadata, not the primary signal.

---

## Issue 2: Auto-exclude `.claude/` in default sync config

### Problem

Claude Code creates a `.claude/` directory in project roots containing:
- `worktrees/` - Full git worktree checkouts (can be 1-10+ GB)
- `plans/` - Implementation plans
- `projects/` - Memory and conversation logs
- `settings.json` - Agent config

When rr syncs a project to a remote host, it rsyncs this entire directory. In the OpenData project, `.claude/worktrees/` was 3GB, making every `rr run` invocation take minutes for the sync phase instead of seconds.

The worktrees directory is especially problematic because it contains full recursive copies of the project source, including their own `node_modules/`, `.venv/`, etc. Even though those are excluded by name, the worktree itself is a multi-GB git checkout.

### Current default excludes

From `internal/config/types.go` line 452:
```go
Exclude: []string{
    ".git/",
    ".venv/",
    "__pycache__/",
    "*.pyc",
    "node_modules/",
    ".mypy_cache/",
    ".pytest_cache/",
    ".ruff_cache/",
    ".DS_Store",
    "*.log",
},
```

### Fix

Add `.claude/` to the default exclude list:
```go
Exclude: []string{
    ".git/",
    ".claude/",
    ".venv/",
    "__pycache__/",
    // ...
},
```

Also consider adding other AI agent directories that are becoming common:
- `.cursor/` (Cursor AI)
- `.aider/` (Aider)
- `.copilot/` (GitHub Copilot)

These directories are never needed on remote execution hosts. They contain agent-local state (conversation history, memory, worktrees) that has zero relevance to building/testing code.

**File:** `internal/config/types.go`, the `DefaultConfig()` function around line 452.

---

## Issue 3: Respect `.gitignore` patterns during sync — RESOLVED

### Original problem

The 3GB `.claude/worktrees/` sync issue would never have occurred if rr respected `.gitignore`. That directory is already gitignored (it's not tracked source code), along with most other large directories that shouldn't be synced (`dist/`, `build/`, `.next/`, coverage reports, etc.).

`respect_gitignore: true` (default on) shipped using this issue's originally suggested implementation: `--filter=':- .gitignore'`, letting rsync read and apply `.gitignore` directly.

### Bug found in that implementation

`--filter=':- .gitignore'` has no support for git's `!pattern` negation. Rsync's merge-file syntax only understands a bare `!` on its own line as "clear every rule read so far" — not per-pattern re-inclusion. Any `.gitignore` with the extremely common "ignore broadly, carve out an exception" idiom:

```
data/
!frontend/tests/mocks/data/
```

silently dropped `frontend/tests/mocks/data/` from every `rr sync`, even though `git status`/`git check-ignore` correctly reported it as NOT ignored. This caused real, hard-to-diagnose test failures (missing mock fixtures) on a project using exactly this pattern.

### Fix

`internal/sync/sync.go`'s `gitignoreFilterArgs()` (replacing the bare `--filter=':- .gitignore'` call) now reads `.gitignore` itself and translates it into explicit rsync `+`/`-` filter rules:

- Git resolves a `.gitignore` "last matching pattern wins" (top to bottom); rsync's filter list is "first matching rule wins" — rules are emitted in **reversed** file order to reproduce that.
- Per `git help gitignore`, "it is not possible to re-include a file if a parent directory of that file is excluded." A `!negation` is only live if every ancestor directory of the negated path independently resolves to not-excluded (a bare directory-unit exclude like `build/` always poisons descendants regardless of order, unless `build/` is itself separately re-included; a wildcard exclude like `build/*` never poisons the directory itself, so a child negation after it works normally). Dead negations are dropped, matching git exactly, instead of emitting an rsync include git would never have honored.
- Live negations also emit `+` rules for every ancestor directory, since an excluded ancestor stops rsync from descending into it at all regardless of what rules come later.

Covered by `TestBuildArgs_GitignoreTranslation` in `internal/sync/sync_test.go`, including a case verified against real git for a multi-level alternating exclude/re-include chain.

**Edge case still open:** only the repo-root `.gitignore` is read (matching the original `':- .gitignore'` scope for a single project checkout). Nested per-directory `.gitignore` files are not merged — extend `gitignoreFilterArgs` to walk the tree if that's needed later.

---

## Issue 4: Stale locks from interrupted agent sessions

### Problem

When an AI agent's session is interrupted (context window exhausted, user cancels, process killed, or Claude Code compacts its conversation), any in-progress `rr` command dies without releasing its lock. The next `rr` invocation then hits "Lock timeout" and fails, requiring manual `rr unlock`.

This happens frequently with AI agents because:
- Context windows get exhausted mid-command
- Users cancel and restart conversations
- Agent processes timeout
- The agent has no way to register cleanup handlers for external processes

Current lock config (from `DefaultConfig()`):
```go
Lock: LockConfig{
    Enabled:     true,
    Timeout:     5 * time.Minute,
    WaitTimeout: 1 * time.Minute,
    Stale:       10 * time.Minute,  // <-- locks are stale after 10 minutes
    Dir:         "/tmp/rr-locks",
},
```

So if an agent dies mid-execution, the lock sits for up to 10 minutes before being considered stale. During that window, all subsequent `rr` invocations fail.

### Desired behavior

Two improvements:

**1. Auto-steal stale locks by default**

The `Stale` timeout (10 min) exists but is only checked passively. rr should automatically claim a lock that's older than `Stale` without requiring manual `rr unlock`. Currently, encountering a stale lock still returns an error rather than silently taking over.

If this already works (hard to tell from code inspection alone), then the issue is that 10 minutes is too long for agent workflows. Consider reducing the default `Stale` to 5 minutes, matching the `Timeout` value. An rr command that's been running for 5+ minutes without heartbeat is almost certainly dead.

**2. Lock heartbeat mechanism**

Currently locks are created once with a timestamp. There's no way to distinguish "process is alive and running a 20-minute test suite" from "process died 5 minutes ago." A heartbeat (touch the lock file every 30s while the command runs) would allow much more aggressive stale detection:

- Lock with heartbeat updated in last 60s = alive, don't steal
- Lock with heartbeat older than 60s = dead, safe to steal immediately

This eliminates the "wait for stale timeout" window entirely for crashed processes while still protecting legitimately running commands.

### Implementation guidance

**Lock file location:** `/tmp/rr-locks/` (configurable via `Lock.Dir`)
**Lock creation:** Likely in `internal/` somewhere that handles locking (search for `Lock.Dir` or lock file creation)

For heartbeat:
- Spawn a goroutine when lock is acquired that touches (updates mtime of) the lock file every 30 seconds
- When checking for stale locks, compare the lock file's mtime against `time.Now()` rather than the lock creation time
- If mtime is older than 60 seconds, the holder is dead - steal the lock

For reduced stale default:
- Change `Stale: 10 * time.Minute` to `Stale: 5 * time.Minute` in `DefaultConfig()`
- Or better: if heartbeat is implemented, reduce to `Stale: 90 * time.Second` (3 missed heartbeats = dead)
