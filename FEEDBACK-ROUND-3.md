# Agent Compatibility Feedback - Round 3

From one Claude Code session on the OpenData project (2026-07-29, implementing MCP behavioral logging). Small sample, but it surfaced two issues that Round 2 does not cover. They had to line up to produce the failure: Issue 8 made a command run zero tests, and Issue 9 hid the nonzero exit that would have made that obvious. Together they produced a **false pass** — a command reporting success while testing nothing. That's the most expensive failure mode for an agent, because it doesn't just waste a round trip: it actively misleads, and the agent goes on to report a green suite to the user.

Round 2's framing still holds ("rr should feel like an extension of the local dev environment"). Both issues below are seams where that illusion breaks *silently* rather than loudly.

**One correction from the last revision:** Issue 9 originally blamed rr for discarding exit codes. It doesn't — that claim was wrong, and the retraction is inline below with the verified mechanism (`pipefail`). Flagging it up front because the wrong version read as "verified."

Baseline: connect/sync stayed fast all session (connect ~0.3s, warm sync ~0.9s), no lock contention, no SSH failures. `rr -m test-backend` on the real suite worked exactly as advertised — 1490 tests, clean JSON envelope, one genuine pre-existing flake correctly reported as a failure.

---

## Issue 8: Relative paths break when the agent's local cwd is a subdirectory

**Severity: HIGH (correctness — reports success having run zero tests)**

### Problem

Round 2's Issue 4 covers *absolute* local paths, and the OpenData `pre-bash.sh` hook now has a guard that leaves absolute-path commands local (`.claude/hooks/pre-bash.sh:213`). But there is no equivalent handling for the much more common case: a **relative** path that is valid in the agent's local cwd and invalid at the remote project root.

rr executes at the remote project root (`~/rr/opendata`). An agent working inside a package — which is the normal way to work in this monorepo — writes paths relative to *that package*:

```
# local cwd: ~/Projects/opendata/backend
uv run pytest tests/scripts/test_cluster_search_terms.py -q -k classify
```

The hook wraps this in `rr run '...'`, rr runs it from `~/rr/opendata`, and `backend/tests/...` is where the file actually lives. Result:

```
ERROR: file or directory not found: tests/scripts/test_cluster_search_terms.py
no tests ran in 0.05s
{"type":"result","status":"success","exit_code":0,...}
```

Same shape for the frontend runner from `~/Projects/opendata/mcp`:

```
bunx vitest run tests/tools/tracking.test.ts
-> Test Files  1 failed (1) / Tests  no tests
{"type":"result","status":"success","exit_code":0,...}
```

Both reported success. I only caught it because the test *count* looked wrong ("no tests ran" where I expected 3), not because anything failed. An agent that trusted the exit code would have reported a passing suite.

The workaround I converged on was `./node_modules/.bin/vitest run ...`, which is invisible to the hook's matcher — so the escape hatch is accidental and undiscoverable.

### Where this comes from in the code

`setupWorkDir` (`internal/cli/workflow.go:159-179`) deliberately replaces the caller's cwd with the project root, and its own comment states the goal:

```go
// setupWorkDir determines the working directory.
// Uses the project root (where .rr.yaml is located) as the default,
// allowing rr to work correctly from subdirectories.
```

Working correctly from subdirectories is exactly what doesn't happen. The project root is the right *sync* root and the right *remote* dir, but using it as the command's working directory silently changes the meaning of every relative path the caller wrote. The offset that was thrown away is the only thing needed to preserve it, and it's discarded before any command assembly happens.

Note this also limits the Round 2 Issue 4 fix. `RewriteLocalPaths(cmd, wf.WorkDir, remoteProjectDir)` (`run.go:243`, impl `pathrewrite.go:28`) is prefix substitution on *absolute* paths — it can't see a relative one, and by then `WorkDir` is the project root rather than where the caller actually stood.

### Desired behavior

1. **Preserve the caller's subdirectory as the remote working directory.** Keep the project root for sync and locking, but record the cwd-relative offset and start the remote shell there (or prepend `cd <offset> && `). That one change makes `rr run` behave identically to running the command locally, which is the stated Round 2 goal.
2. **At minimum, fail fast locally.** Before connecting, if the command references a relative path that doesn't exist relative to the remote root but does exist relative to the caller's cwd, error out with the corrected command. Both paths are known locally, so this costs nothing.

### Implementation guidance

- `internal/cli/workflow.go:159-179` (`setupWorkDir`) is where the offset is currently discarded — capture it on `WorkflowContext` alongside `WorkDir`.
- Command assembly for the remote path is `internal/cli/run.go` (around `:243`, where `RewriteLocalPaths` already hooks in). There is no `exec_cmd.go`.
- Keep `WorkDir` semantics for `ExecuteLocal` (`run.go:92`) intact — the local-fallback path genuinely wants the project root, so the offset should be applied on the remote branch only.
- Interaction with Round 2's Issue 3: worktrees and the main checkout share one remote dir, so the offset must be computed against the *invoking tree's* root, not a cached one.

### Downstream note

The consuming project has since fixed this on its own side — the Claude Code hook that wraps test commands now prefixes the package offset before handing the command to `rr` (`opendata/.claude/hooks/pre-bash.sh`, `check_rewrite_tests_to_rr`). That's a workaround in one repo's tooling, not a fix; every other rr caller still has the sharp edge. Worth knowing that the reproduction is no longer live from that project.

---

## Issue 9: A pipe in the command silences a failing exit code

**Severity: HIGH (correctness — this is the mechanism that turns every other path bug into a false green)**

> **Corrected 2026-07-29.** An earlier version of this issue blamed rr for discarding the exit code. That was wrong, and the fixing agent caught it. rr propagates exit codes faithfully; the loss happens in the remote shell, because a pipeline returns its *last* stage's status. The corrected diagnosis is below. Keeping the retraction visible because the original claim would have sent someone hunting a bug that doesn't exist.

### Problem

A run that collected nothing can come back as `"status":"success","exit_code":0`. The variable is whether the command contains a pipe. Controlled test — identical command, only the pipe differs:

```
$ rr run 'cd backend && uv run pytest tests/x.py -q -k zzz_no_match 2>&1 | tail -4'
{"type":"result","status":"success","exit_code":0,...}

$ rr run 'cd backend && uv run pytest tests/x.py -q -k zzz_no_match'
{"type":"result","status":"failed","exit_code":5,...}
```

The remote wrapper doesn't set `set -o pipefail`, so `pytest ... | tail` returns `tail`'s status (0) and pytest's exit 5 vanishes. rr faithfully reports what the shell told it.

This matters because agents pipe rr output constantly, for exactly the reason Round 2's Issue 6 documented: to tame verbose output. The false-passing command in this session was `rr run 'uv run pytest ... -q -k classify 2>&1 | tail -8'`. Both defects had to line up — the wrong path (Issue 8) made it collect nothing, and the pipe hid the nonzero exit.

**pytest signals this correctly.** Verified empirically on 3.11 / pytest 7.4:

| Scenario | pytest exit |
|---|---|
| File/dir argument not found (**this session's case**) | **4** (usage error) |
| File found, `-k` selects nothing | **5** (no tests collected) |

Both nonzero, both meaning "you didn't run what you asked for," and both surfaced correctly by rr when the command isn't piped.

This is the amplifier for Issue 8 above, for Round 2's Issues 1/3/4, and for the coverage-gap case already noted as papercut D. Papercut D was filed "low priority, high trust payoff" — this is a second independent occurrence, so the frequency argument is stronger than it looked. It also subsumes papercut E below, which described the client-side half of the same mechanism.

### Desired behavior

1. **Set `pipefail` in the remote wrapper**, so a failing stage anywhere in a pipeline surfaces. This is the whole bug.
2. Treating "ran nothing" as a distinct outcome (`"status":"no_tests"` rather than inferring it from `Failed == 0`) is still worth doing, but it's now a clarity improvement rather than a correctness fix — with `pipefail` set, exit 4/5 already surfaces as `status: "failed"`. Deprioritize accordingly.

### Implementation guidance

Two cautions on `pipefail`, because it's a behavior change for existing tasks:

- **It's bash/zsh-only** — under `dash`/`sh` it's an error. Either guarantee the wrapper runs under bash, or invoke as `bash -o pipefail -c '...'`.
- **Some existing tasks will newly fail.** OpenData's own config already sets it by hand, which is evidence users hit this and worked around it locally:

  ```yaml
  run: set -o pipefail && cd opendata && uv run pytest ... | grep -vE '^[.sxXfFEp]+\s*\[' | grep -v '^$'
  ```

  Note `grep -v` exits 1 when it filters *everything*, so a fully-quiet passing run could newly report failure. Worth auditing built-in tasks that pipe through `grep`/`head` before flipping the default, and possibly making it opt-out per task.

For the secondary formatter work: `pytestSummaryPattern` (`pytest.go:109`) requires `\d+\s+\w+ in [\d.]+s`, which `no tests ran in 0.05s` doesn't match, so counters stay at zero; `TestPytestNoTestsCollected` (`pytest_test.go:264`) pins that. `Summary()` at `pytest.go:268` already handles zero-results-plus-nonzero-exit correctly. `gotest.go:260-319` already tracks `pkgNoTests` — worth checking whether all three formatters can share one representation.

---

## Smaller papercut

**E. Piping `rr`'s own output produces a false pass, not just truncated output.** The client-side twin of Issue 9, and worth stating separately because the fix is different: Issue 9 is a pipe *inside* the remote command (fixed by `pipefail` in the wrapper), while this is a pipe around the `rr` invocation itself (`rr test-backend | tail -8`), which no wrapper change can reach. Round 2's Issue 6 documents that agents pipe rr output and lose the signal; the consequence is worse than lost output, since the shell returns *tail's* status and a failed run reports exit 0. Verified: `bash -c 'exit 4' | tail -3` gives exit 0; with `pipefail` it gives 4. If rr detects a non-TTY it could warn once, and the docs should state the exit-code consequence, not only the truncation one.

---

## Evidence index

| Issue | Session | Raw failure |
|-------|---------|-------------|
| 8 - relative path from subdir | 2026-07-29 MCP logging | `~/.rr/logs/run-20260729-144421/output.log`, `run-20260729-162812` |
| 9 - pipe silences failing exit | same | same logs; envelope `"status":"success","exit_code":0` with `ERROR: file or directory not found` + `no tests ran` in output. Original command: `rr run 'uv run pytest tests/scripts/test_cluster_search_terms.py -q -k classify 2>&1 \| tail -8'` |
| 9 - controlled A/B | 2026-07-29 follow-up | `run-20260729-185346` (piped -> success/0) vs `run-20260729-185351` (unpiped, same command -> failed/5) |
| E - pipe around rr masks exit | same | `run-20260729-144421` (command ended `\| tail -8`) |
