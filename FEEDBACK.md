# Agent Compatibility Feedback - Round 2

Real-world friction from Claude Code agent sessions using rr on the OpenData project (24 sessions with rr activity, July 14-25 2026, analyzed via transcript mining). The goal driving this round: **rr should feel like an extension of the local dev environment**. Running tests remotely should be indistinguishable from running them locally - same commands, same paths, same trust in the results. Issues below are ranked by how badly they break that illusion.

Baseline first: the core loop is solid. Connect/sync/exec phases are fast (connect ~0.2-2.5s, warm sync ~0.7-2s), the structured JSON phase output is genuinely useful to agents, named tasks (`rr test-opendata`, `rr test-backend`) work reliably, and zero SSH failures or lock timeouts appeared across all 24 sessions. The friction is concentrated in the seams: extra args, worktrees, local paths, and what happens when things fall back or fail.

Each issue includes enough context for a fixing agent to implement the solution without additional investigation.

---

## Issue 1: Extra args appended to pipeline tasks silently target the wrong command

**Severity: HIGH (correctness - produces garbage output and wrong test scope with exit 0)**

### Problem

Extra CLI args are appended to the raw `run` string of a task:

- `internal/exec/task.go:71-73` (single-command tasks): `cmd = cmd + " " + strings.Join(args, " ")` - not even shell-quoted
- `internal/cli/parallel.go:299-310` (`forward_args` for parallel tasks): same append, though this path does `util.ShellQuote`

If the task's `run` is a shell pipeline, the appended args become arguments to the **last command in the pipeline**, not the test runner. OpenData's task:

```yaml
test-opendata:
  run: set -o pipefail && cd opendata && uv run pytest -n 4 --no-cov -qq --tb=short 2>&1 | grep -vE '^[.sxXfFEp]+\s*\[' | grep -v '^$'
```

So `rr test-opendata tests/test_cli/test_deploy_yaml.py` executes:

```
... pytest -n 4 ... | grep -vE '...' | grep -v '^$' tests/test_cli/test_deploy_yaml.py
```

The FULL suite runs (the path never reaches pytest), and the final `grep` reads the test file from disk instead of stdin - the agent sees the test file's own source code echoed back as "output". Observed exactly this in session 57526c9b (agent hit SIGPIPE/exit 141, then saw "pytest source code being echoed rather than structured results", burned 2 more invocations switching to `-m` mode) and session 590282f7 (4 invocations of trial-and-error for one logical operation). The same append lands after `--tb=short 2>&1` redirections in simpler cases, which is also wrong.

This is the worst kind of failure for an agent: exit code is fine, output looks like something ran, but the scope filter was silently ignored.

### Desired behavior

Support an explicit placeholder in task commands, and refuse the blind append when it would be ambiguous:

```yaml
test-opendata:
  run: set -o pipefail && cd opendata && uv run pytest {args:-.} -n 4 --no-cov -qq --tb=short 2>&1 | grep -vE '...'
```

1. If `run` contains `{args}` (or `{args:-default}`), substitute the shell-quoted extra args there. This is the fix that makes `rr test-opendata tests/foo.py` behave like local `pytest tests/foo.py`.
2. If `run` contains NO placeholder and the command contains a pipe, `;`, `&&` after the presumed target, or a redirection, **error instead of appending**: "task 'test-opendata' is a compound command; add an {args} placeholder to accept extra arguments". A wasted error beats a silent wrong-scope run.
3. Plain single-command tasks (no shell metacharacters) can keep the append for backward compatibility, but shell-quote it (`internal/exec/task.go:72` currently does not).

### Implementation guidance

- `internal/exec/task.go:68-82` - single-task append
- `internal/cli/parallel.go:286-320` - `buildSubtaskInfos` forward_args append
- Add placeholder substitution in one shared helper (e.g. `internal/config` or `internal/exec`), used by both paths
- `internal/util` already has `ShellQuote`

---

## Issue 2: Silent local fallback when all hosts are locked creates false-green results

**Severity: HIGH (correctness - agent believed tests passed that never ran remotely)**

### Problem

With `local_fallback: true`, when every host's lock is held, rr falls back to local execution (`internal/cli/workflow.go:493` documents this; `EventLocalFallback` at `internal/cli/workflow.go:262-293`, selector in `internal/host/selector.go`). In session 0ae6ea9e, an agent launched `rr test-backend` in the background, then `rr test-frontend` while the first still held locks on both m4-mini and m1-linux. The second invocation exited 0. The agent's own retrospective: "Exit code 0 is misleading - the frontend test actually failed to run due to a lock: both remote hosts are held by pid 46822, which is my own rr test-backend run." Recovery required `ps aux | grep rr`, manual `kill`, and a re-run - pure toil, and worse, a window where a false green could have shipped.

Two compounding gaps:

1. The fallback (or lock-wait outcome) is not loud enough in structured output for an agent to notice. Agents read the exit code and the tail of output; a fallback needs to be in the final result envelope, not just a connection-phase line.
2. rr doesn't recognize that the lock holder is *the same machine, same user, possibly the same agent session*. "All remotes are busy with YOUR other rr run" is actionable; a generic fallback is not.

### Desired behavior

1. The final result envelope always carries where and why: `"host": "local", "fallback": {"reason": "all_hosts_locked", "holders": [{"host": "m4-mini", "pid": 46822, "command": "rr test-backend", "age_s": 312, "same_machine": true}]}`. Pretty mode prints an unmissable warning line.
2. When every skipped host is locked by a holder from the same local machine, prefer **waiting** (bounded by `lock.wait_timeout`) over local fallback, or at minimum say explicitly: "all hosts locked by your own runs (pid 46822: rr test-backend); waiting up to 1m / falling back to local".
3. Consider `local_fallback: never | on-unreachable | always`. `on-unreachable` (hosts down -> local OK, hosts merely busy -> wait/error) matches what a test workflow actually wants. Falling back to local because the hosts are *busy* is rarely the right call for a test suite that exists to be run remotely.

### Implementation guidance

- `internal/cli/workflow.go:262-293` (event handler), `setupWorkflowLoadBalanced` load-balance path
- `internal/host/selector.go` - `EventLocalFallback` emission; selector knows which hosts were skipped and why
- Lock info files already record holder identity (`internal/lock/info.go`) - plumb holder details into the skip events so the envelope can report them
- Result envelope: `internal/cli/phase_reporter.go` / `internal/cli/json.go`

---

## Issue 3: Git worktrees sync to the same remote directory as the main checkout

**Severity: HIGH (correctness + the single biggest source of agent hesitation)**

### Problem

`${PROJECT}` resolves from the git origin remote name (`internal/config/expand.go:107-140`: `getProject` -> `getGitRepoName` -> `git remote get-url origin`). A git worktree has the same origin as its main checkout, so the main checkout at `~/Projects/opendata` and every worktree under `~/Projects/opendata/.claude/worktrees/<branch>` all sync to the same remote dir `~/rr/opendata` - with `--delete` in the sync flags.

Claude Code runs parallel sessions in worktrees as a standard workflow. Consequences observed across sessions:

- **Cross-clobber**: each run from a different tree silently replaces the remote tree. Locking serializes concurrent runs but does nothing about A-then-B sequences: after worktree A tests, a `rr exec` from the main checkout session operates on A's code.
- **Wrong code tested**: in session 57526c9b the agent edited in a worktree but invoked rr from the main checkout - rr synced the main checkout and tested code without the fix. The run "passed" (false green for the change under test). The agent needed a full diagnose-and-retry cycle.
- **Constant hesitation**: sessions 295ee2f1, 154d4b1a, 57526c9b all contain multi-step reasoning pauses like "rr syncs from the repo root, not the worktree... let me check where .rr.yaml is and run it correctly". Every worktree session pays this tax; it is the opposite of "feels local".
- **Hand-tuned workarounds**: OpenData's `.rr.yaml` carries this comment, which is really an rr bug report: "No trailing slashes on .git / .venv / node_modules: in git worktrees .git is a FILE and node_modules may be a symlink to the main checkout. Dir-only patterns miss those, and rsync then tries to replace the remote's real .git/ directory with a file -> partial transfer (exit 23)."

### Desired behavior

1. **Worktree-aware project identity.** Detect a linked worktree (`git rev-parse --git-common-dir` differs from `.git`, or `.git` is a file). Derive a distinct remote dir, e.g. `${PROJECT}` -> `opendata@figure-canvas-viewport` (worktree dir basename). Every tree gets its own remote mirror; no cross-clobber, no wrong-code runs. Make it default-on with a config escape hatch (`sync.worktree_isolation: false`) since the cost is a cold first sync (fresh `.venv`/`node_modules` on remote via existing `preserve` + setup flows).
2. **Provenance stamp regardless.** Write a `.rr-source` marker in the remote dir recording local source path, branch, and HEAD sha on every sync. If the incoming sync's source differs from the marker, emit a structured warning phase event: "remote ~/rr/opendata was last synced from worktree figure-canvas-viewport (branch feat/x); now syncing from main checkout". Even with isolation off, this converts silent clobber into an observable event.
3. **Worktree-safe default excludes.** rr should exclude `.git` whether it is a directory or a file (bare pattern, no trailing slash), and handle symlinked `node_modules`, so projects don't have to discover rsync exit-23 themselves. Check the exclude normalization wherever defaults and user patterns are assembled (`internal/sync/sync.go:185+`, init template in `internal/cli/init.go`).
4. `rr status` (or `rr doctor`) should print which local tree maps to which remote dir, so "where will this sync?" has a one-command answer.

### Implementation guidance

- `internal/config/expand.go:107-140` - project name resolution; add worktree detection here
- `internal/sync/sync.go` - marker file write/check belongs in the sync phase
- `internal/cli/init.go` - default exclude template
- Docs: worktree section in README/skill docs; today the behavior is undocumented and every agent re-derives it

---

## Issue 4: Local absolute paths inside commands fail on the remote with no pre-flight help

**Severity: HIGH (frequency - the most common individual failure across sessions)**

### Problem

Agents naturally write commands with absolute local paths, because that's how everything else in their environment works:

- `rr run "cd /Users/rileyhilliard/Projects/opendata/backend && uv run pytest tests/api/test_figures.py -x -q"` -> `zsh:cd:1: no such file or directory` (session 49e9f912, run from a worktree)
- `rr run 'cd /Users/rileyhilliard/Projects/openchart && bunx vitest run ...'` -> path is a *different repo* that was never synced (session bc8750d9; a PreToolUse hook auto-wraps test commands in rr, so this class recurs)
- Multi-line script with relative paths + git assumptions -> three stacked failures (session 8b9a17d2)

Each failure costs a full connect+sync round trip before the shell error surfaces, and the error (`no such file or directory`) doesn't explain the actual problem (local path on a remote machine).

### Desired behavior

This is the highest-leverage "extension of local dev" feature:

1. **Rewrite the known prefix.** rr knows the local sync root and the remote dir. Before executing, rewrite occurrences of the local project root prefix in the command to the remote dir (`/Users/riley/Projects/opendata/backend` -> `~/rr/opendata/backend`). Then `rr run "cd <local-abs-path> && pytest"` just works, exactly like it does locally. Gate behind a config flag if there's appetite for caution, but this transform is well-defined: exact-prefix match on the sync source path.
2. **Warn on other absolute local paths.** If the command references absolute paths under `$HOME` or `/Users/`/`/home/` that are NOT under the sync root (the openchart case), emit a pre-flight warning or error before connecting: "command references /Users/.../openchart which is outside the synced project and won't exist on m4-mini". Fail fast locally instead of after sync.
3. When a remote command fails and stderr contains `no such file or directory` matching a local-looking path, append a hint to the result: "this path exists locally but not on the remote; rr syncs <local root> to <remote dir>".

### Implementation guidance

- `internal/cli/run.go:304-320` (`runCommand`, command assembly at line 317) and `internal/cli/exec_cmd.go:24`
- Sync root and remote dir are both known in `SetupWorkflow` (`internal/cli/workflow.go:146-166` for workdir; host dir from config) - do the scan/rewrite between setup and exec
- Keep the scan simple: exact prefix match for rewrite, regex for the warning class

---

## Issue 5: `rr run <name> "cmd"` joins all args into one broken command

**Severity: MEDIUM (easy fix, seen in 3+ sessions, each costing a full remote round trip)**

### Problem

`rr run` and `rr exec` join all positional args with spaces (`internal/cli/run.go:317`, `internal/cli/exec_cmd.go:24`; `Args: cobra.MinimumNArgs(1)` in `internal/cli/commands.go:69`). Agents repeatedly guess a host or task goes first:

- `rr run backend "cd backend && uv run pytest ..."` -> remote runs `backend cd backend && ...` -> `zsh:1: command not found: backend` exit 127 (session 8da69aa1)
- `rr run m4-mini "cd opendata && uv run pytest ..."` -> same shape; agent then ran `rr --help` and needed 3-4 attempts total (session f0344357)

The failure arrives *after* connect+sync, and "command not found: backend" doesn't point at the real mistake.

### Desired behavior

Cheap pre-flight validation when `len(args) > 1`:

- If `args[0]` matches a configured host name -> error: "'m4-mini' is a host, not part of the command. Use: rr run --host m4-mini \"...\""
- If `args[0]` matches a defined task name -> error: "'test-backend' is a task. Use: rr test-backend, or rr run \"...\" for ad-hoc commands"
- Both checks are exact string lookups against data already loaded; no behavior change for legitimate multi-arg commands like `rr run make test`.

### Implementation guidance

- `internal/cli/commands.go:57-96` (runCmd/execCmd RunE) - do the lookup before calling `runCommand`/`execCommand`
- Host names: global config hosts map; task names: project config tasks map - both available via `config.LoadResolved`

---

## Issue 6: Agents pipe rr output to tame it, and the pipes destroy the signal

**Severity: MEDIUM (drives re-runs of entire test suites just to see the result)**

### Problem

Full-suite output is large, so agents wrap rr in `| tail -40` or `| grep`. Observed consequences:

- SIGPIPE exit 141 mid-stream when the downstream closes early (session 57526c9b)
- Only the final JSON envelope survives `tail`, losing per-shard pytest counts - "I can't see the actual pytest pass counts or confirm chat tests ran. Let me re-run without the truncating pipe" (session 295ee2f1) - a full suite re-run purely to recover output
- Trial-and-error escalation through `tail -20` -> `tail -30` -> no pipe (session 590282f7)

The parallel path already saves logs (`SaveLogs: true` in `internal/cli/task.go` parallel config; `internal/parallel/logs`), but agents didn't discover them; single `rr run` output isn't saved at all.

### Desired behavior

Make piping unnecessary:

1. **Always save full output** (run/exec/task, not just parallel) to a local log file, and put the path in the final result envelope: `"log_file": "~/.rr/logs/opendata/2026-07-25T.../test-opendata.log"`. An agent can then `grep` the file instead of re-running the suite.
2. **Parse a test summary into the envelope.** rr already has output format detection (`output.format: auto/pytest/jest/...`). When the format is recognized, the result envelope should include `"summary": {"passed": 4167, "failed": 0, "skipped": 12}`. That single field eliminates most of the reason agents pipe at all.
3. **Native `--tail N`**: print the last N lines of command output after the envelope, so "show me the end" doesn't require a shell pipe that can SIGPIPE the stream.
4. Tolerate EPIPE on the stream writers (keep writing the log file, stop writing stdout) so a closed downstream produces a clean result instead of exit 141.

### Implementation guidance

- `internal/cli/run.go:69` - `output.NewStreamHandler(os.Stdout, os.Stderr)`; tee to a log file here
- `internal/output` - formatters for summary extraction
- `internal/cli/phase_reporter.go` / `json.go` - envelope fields

---

## Issue 7: Stale locks from dead local processes require manual forensics

**Severity: MEDIUM**

### Problem

When a local rr process is killed (agent timeout, Ctrl+C on a wrapper, background task cancellation), its locks persist until the 10m stale threshold (`internal/lock/lock.go:145-155`, `isLockStale` at line 146). Session 0ae6ea9e shows the recovery ritual: `ps aux | grep "rr test"`, manual `kill 46822 46812`, then per-host `rr unlock m4-mini`, `rr unlock m1-linux`. Project memory in the OpenData repo institutionalizes this ("If rr shows Lock timeout errors: rr unlock m4-mini / rr unlock m1-linux") - a documented workaround is a missing feature.

### Desired behavior

1. **Dead-holder fast path**: lock info records the holder's machine and pid. On conflict, if the holder machine is *this* machine and the pid is not running, steal immediately - don't wait 10 minutes for staleness. This covers the dominant case (agent's own killed process).
2. **`rr unlock --all`**: release locks on every host in the project config in one command.
3. Lock-conflict errors should always show holder command + age: "held by rr test-backend (pid 46822, started 5m ago, this machine)" - `internal/lock/lock.go:140-142` prints holder but not age/command/machine consistently.

### Implementation guidance

- `internal/lock/lock.go` acquire path (lines ~100-160), `internal/lock/info.go` (holder metadata)
- `internal/cli/unlock.go` for `--all`

---

## Smaller papercuts

**A. Parallel tasks reject extra args with a dead-end suggestion.** `rr test-backend tests/chat/` -> `CONFIG_INVALID: parallel task 'test-backend' doesn't accept extra arguments` (`internal/cli/task.go:711-714`). The error is clear, but the suggestion ("use rr run ...") drops all the task's environment/setup value, and `forward_args` (`internal/config/types.go:212-215`) is not mentioned. Once Issue 1's `{args}` placeholder exists, mention `forward_args` in this error, and note in docs that forwarding a path filter to all subtasks means some subtasks may collect zero tests (pytest exits 4/5 on empty collection - worth calling out).

**B. Remote has no `.git`, and failures are cryptic.** Agents occasionally embed `git stash list` etc. in remote commands and get `fatal: not a git repository` (session 8b9a17d2). Document prominently that the remote is a synced snapshot, not a clone. Optional: when a remote command's stderr contains `not a git repository`, append a one-line hint in the result.

**C. Ad-hoc `rr run` skips the environment setup that tasks get.** A bare `rr run "cd opendata && uv run pytest ..."` hit a stale env (missing `redis` module) that `rr test-opendata` avoids because the task/setup does `uv sync` (session f0344357). Consider a project-level `setup` (or `before_run`) that applies to ad-hoc runs too, or document that `rr run` executes against whatever state the last task left.

**D. Coverage-gap doctor check (idea).** A session shipped a false-green because backend shard globs omitted `tests/chat/` (4,167 tests passed while 88 chat tests never ran). That's a project config bug, but `rr doctor` could optionally compare test directories on disk against the union of task glob patterns and flag uncovered dirs. Low priority, high trust payoff.

---

## Evidence index

| Issue | Sessions (transcript ids) |
|-------|---------------------------|
| 1 - args after pipeline | 57526c9b, 590282f7 |
| 2 - silent local fallback | 0ae6ea9e |
| 3 - worktree remote-dir collision | 57526c9b, 295ee2f1, 154d4b1a, 49e9f912 + `.rr.yaml` workaround comments |
| 4 - local abs paths on remote | 49e9f912, bc8750d9, 8b9a17d2 |
| 5 - `rr run <name>` arg joining | 8da69aa1, f0344357 |
| 6 - output piping | 57526c9b, 295ee2f1, 590282f7 |
| 7 - stale locks | 0ae6ea9e, OpenData project memory |

Transcripts live under `~/.claude/projects/-Users-rileyhilliard-Projects-opendata*/<id>.jsonl` if a fixing agent needs the raw failure text.
