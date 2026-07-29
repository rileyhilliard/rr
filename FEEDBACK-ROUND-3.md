# Agent Compatibility Feedback - Round 3

From one Claude Code session on the OpenData project (2026-07-29, implementing MCP behavioral logging). Small sample, but it surfaced two issues that Round 2 does not cover, and both produced **false passes** — commands that reported success while running zero tests. That failure mode is the most expensive one for an agent, because it doesn't just waste a round trip: it actively misleads, and the agent goes on to report a green suite to the user.

Round 2's framing still holds ("rr should feel like an extension of the local dev environment"). Both issues below are seams where that illusion breaks *silently* rather than loudly.

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

## Issue 9: "Zero tests collected" is reported as success

**Severity: HIGH (correctness — this is the mechanism that turns every other path bug into a false green)**

### Problem

A run that collected nothing comes back as `"status":"success","exit_code":0`. Two things combine to produce that, and the second one is the surprise:

**1. The formatter has no zero-results signal.** `pytestSummaryPattern` (`pytest.go:109`) requires `\d+\s+\w+ in [\d.]+s`. The line pytest actually emits is `no tests ran in 0.05s` — no leading count, so the pattern doesn't match and the counters simply stay at zero. `TestPytestNoTestsCollected` (`pytest_test.go:264`) pins `Passed: 0, Failed: 0, Skipped: 0`, making "ran nothing" structurally identical to "nothing failed."

**2. `Summary()` already has the right branch, but it's gated on the exit code.** `pytest.go:268`:

```go
if len(f.results) == 0 && exitCode != 0 {
    return "pytest failed with exit code " + strconv.Itoa(exitCode)
}
```

So rr *does* know that zero results plus a nonzero exit is a failure. It never fires here, because the exit code reaching it is 0. That's the real defect — not the missing formatter state, but a discarded exit code.

**pytest was signaling this correctly.** Verified empirically on 3.11 / pytest 7.4:

| Scenario | pytest exit |
|---|---|
| File/dir argument not found (**this session's case**) | **4** (usage error) |
| File found, `-k` selects nothing | **5** (no tests collected) |

The false-passing run was exit **4**, not 5. So the runner returned a nonzero exit for a usage error and rr reported success anyway. Fixing exit-code propagation alone would have caught this session's bug without touching the formatter at all.

This is the amplifier for Issue 8 above, for Round 2's Issues 1/3/4, and for the coverage-gap case already noted as papercut D. Any bug that causes the runner to select nothing comes out green. Papercut D was filed "low priority, high trust payoff" — this is a second independent occurrence, so the frequency argument is stronger than it looked.

### Desired behavior

In priority order:

1. **Stop normalizing the remote command's exit code to 0.** This is the highest-value fix and the narrowest. pytest 4 and 5 are both nonzero and both mean "you didn't run what you asked for." The `Summary()` branch at `pytest.go:268` starts working the moment the real exit code reaches it.
2. **Treat "ran nothing" as its own outcome** rather than inferring it from `Failed == 0`. Surface it in the JSON envelope as something other than `status: "success"` — `"status":"no_tests"`, or `success: false` with a reason. Agents branch on that field.
3. Same treatment for the jest/vitest formatter — `Test Files 1 failed / Tests no tests` also came back as success this session.

### Implementation guidance

- Exit-code propagation is the first thing to trace; the formatter change is secondary and partly redundant once (1) lands.
- `internal/output/formatters/pytest.go:109` (the summary pattern), `:268` (the already-correct branch), `jest.go` for the vitest path.
- `pytest_test.go:264` and `jest_test.go:288` both assert the permissive behavior and would need to assert the new state instead. `gotest.go:260-319` already tracks `pkgNoTests` — worth checking whether all three can share one representation.
- Strict by default, not opt-in. An agent that asked for tests and got none has a bug, essentially always.

---

## Smaller papercut

**E. Piping produces a false pass, not just truncated output.** Round 2's Issue 6 documents that agents pipe rr output and destroy the signal. Worth adding that the consequence is worse than lost output: `rr ... | tail -8` returns *tail's* exit status, so a failed run reports exit 0. Combined with Issue 9, a piped run of a broken command is indistinguishable from a clean pass. If rr detects it's writing to a non-TTY it could warn once, or the docs could state the exit-code consequence explicitly rather than only the truncation one.

---

## Evidence index

| Issue | Session | Raw failure |
|-------|---------|-------------|
| 8 - relative path from subdir | 2026-07-29 MCP logging | `~/.rr/logs/run-20260729-144421/output.log`, `run-20260729-162812` |
| 9 - zero tests as success | same | same logs; envelope `"status":"success","exit_code":0` with `no tests ran` in output |
| E - pipe masks exit code | same | `run-20260729-144421` (command ended `| tail -8`) |
