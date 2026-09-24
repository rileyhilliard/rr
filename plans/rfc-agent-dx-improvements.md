# RFC: Agent DX improvements from the v0.27.x session

Status: revised after devils-advocate review, 2026-09-23

## Context

One long session covered the v0.27.0 audit fixes (PRs #244-#247), v0.27.1 (#248), and #249. That meant five PRs, two releases, manual QA against real runners, and runner disk cleanup. The session transcript shows where the agent lost time or shipped a bug that a guard would have caught. This RFC proposes up to five changes, ranked by how often the friction would come back.

## 1. Guard: structured mode keeps stderr pure JSON and leaves stdout alone

**What happened.** Plain text leaked into the structured stream in four separate places. Each was found by hand during QA, not by a test:

- the lock-steal notice, which was printed twice as plain text
- the `depends` stage display, written to stdout
- spinner frames from `rr update`
- a ProxyJump warning, which was checked by hand

The contract is that structured mode puts JSON lines on stderr, and stdout carries only the command's own output. Nothing enforces it across commands. `scripts/e2e-test.sh test_json_output` only greps for a key.

**Proposal.** Add one table-driven integration test, `tests/integration/structured_output_test.go`. It runs the real `rr` binary as a subprocess against the Docker SSH host, so every writer is caught no matter which fd it holds. An in-process `dup2` would also swallow test2json output, panics, and `-race` reports. The binary gets built once per package run with `sync.Once` and `go build -o` into `t.TempDir()`, and `scripts/e2e-test.sh` already builds the same way.

The child process doesn't inherit the in-process `sshutil.StrictHostKeyChecking = false` that `setupParallelSSHHome` sets. Give it a known_hosts file seeded with `ssh-keyscan`, or `StrictHostKeyChecking no` in the temp ssh config if rr honors that. Check which one works.

Cases:

- `rr run "echo out"`
- a `depends` task
- a multi-step task
- a parallel task
- `rr sync`
- `rr run` against a planted dead-holder lock

For each case, assert two things:

- every stderr line parses as JSON
- stdout is exactly the command's output

Move the existing all-JSON assertion from `lock_warn_test.go` into this table so it isn't duplicated. Keep that file's event-count checks, which are specific to the lock.

Scope, honestly: this covers the `depends`-stdout leak and the lock-notice leak from this session, plus any new leak on those paths. It doesn't cover the `rr update` spinner or the ProxyJump warning, because the Docker host can't reproduce either. It's still the only automated check on the contract.

## 2. `scripts/pr-wait.sh`: wait for CI and CodeRabbit, print a compact digest

**What happened.** The agent hand-wrote a `gh` polling loop for every PR, at least five times. Each loop was slightly different, and one grabbed the previous run. The raw CodeRabbit bodies are long: they include "Script executed" blocks, tweet prompts, and HTML comments, so the output had to be grepped by hand.

**Proposal.** Add `scripts/pr-wait.sh <pr>`. CodeRabbit posts a check on the PR (it shows up in `gh pr checks`), so waiting is just `gh pr checks <pr> --watch`, which already tracks the head SHA. Don't write a custom poll loop. The script:

1. Fails with a clear message if the PR has no checks at all. That happens with a stacked PR or one whose base isn't `main`, which CI ignores. Without this check the script would report an instant green.
2. Runs `gh pr checks <pr> --watch` and keeps its exit status.
3. Prints a digest of **unresolved** CodeRabbit threads, using GraphQL `reviewThreads { isResolved, comments(first:1) { path line body author } }`, because REST doesn't expose resolution. Each line shows `path:line`, the severity/title line, and the first paragraph of the body, with `<details>` blocks and HTML comments stripped.
4. Prints the latest `coderabbitai[bot]` issue comment when it says the review was skipped. CodeRabbit edits that comment in place; a skip never becomes a review.

Reference it from `.claude/commands/merge-release.md` (or that file's skill replacement, see item 6) and from `.claude/rules/verification.md`. lefthook's shellcheck covers it.

## 3. Integration tests fail, not skip, when SSH is required

**What happened.** CI's integration job didn't install rsync, so the sync integration tests skipped and the job stayed green. That went unnoticed until a manual check found it. Locally, the agent had to count skips by hand each time ("145 pass, 0 skip") to trust a run.

**Proposal.** Handle this in CI only, with no changes to test code. After the integration step in `.github/workflows/ci.yml`, fail the job if `integration-results.json` has any `"Action":"skip"` event, and print the skipped test names and their skip reasons. That covers every package the job runs (`tests/integration`, `pkg/sshutil`, `internal/cli`), and on ubuntu-latest there's no legitimate skip. The rsync install that motivated this is already fixed at ci.yml:185; this stops the next missing dependency from hiding the same way.

Don't set anything in `.rr.yaml`. `rr test-integration` runs on the runners, which have no Docker SSH host, so skips there are expected.

## 4. Testing gotchas in `.claude/rules/go/testing.md`

**What happened.** Three traps cost real time:

- **The integration lock leak.** A `cli.Run` whose connection drops can't release the per-host lock. The lock's holder is the still-running `go test` process, so it never looks dead, and the next subtest waited 91s for it to go stale. The fix was `SkipLock: true`, but that's only knowable by reading lock internals.
- **A red check that passed for the wrong reason.** A test swapped `os.Stderr` to capture output, but the code under test wrote through a writer bound to the original fd, so the old code "passed". Nobody noticed until the unit-level test was checked against old code.
- **A red check that missed the code under test.** `lostConnectionAsResult` has three call sites (task.go twice, run.go once). Reverting the wrong call site gave a pass that meant nothing.

**Proposal.** Add a short "Gotchas" section to `.claude/rules/go/testing.md`:

- Integration tests that call `cli.Run` or `cli.RunTask` against the shared Docker host should pass `SkipLock: true` unless they're testing locking. Explain why.
- To check what rr prints, run the built binary as a subprocess (the item 1 helper) or assert on an injected writer. Swapping `os.Stderr` misses writers that captured the fd before the swap.
- A red check has to fail on the assertion under test, at the call site under test. Read the failure message, not just the exit code, before calling it red.

Point to the helpers from item 1 once they exist.

## 5. Environment notes in `.claude/CLAUDE.md`

**What happened.** Environment mismatches caused repeated small failures. Each was cheap, but together they added up:

- `golangci-lint` on PATH is 2.8.0 (go1.25), the pin is 2.13.2, and the toolchain is go1.26, so bare `golangci-lint` fails. `rr lint` and `make lint-local` both run the pinned binary. `settings.json` allows `Bash(golangci-lint:*)`, which invites the wrong one.
- The local shell is zsh. An unmatched glob is an error there (`no matches found: --include=*.go`, `out*`), and the `c` alias breaks function definitions. The runners' login shell can be zsh too (m4), so remote scripts need `ssh host bash -s < script`.
- macOS sed has no `\b`. Use perl for word-boundary edits.
- `rr version` is correct; `rr --version` doesn't exist.
- Integration tests need Docker plus `./scripts/ci-ssh-server.sh`. Docker Desktop may be stopped.
- CI only runs for PRs based on `main`. A stacked PR, or one whose base changed, gets no CI and no automatic CodeRabbit review until it's retargeted, and retargeting doesn't re-trigger CI. A push does.

**Proposal.** Add an "Agent environment notes" section of about ten lines to `.claude/CLAUDE.md`, including "never run bare `golangci-lint`". The existing `rr lint` lines are already correct, so leave them alone. Remove `Bash(golangci-lint:*)` from the allow list in `.claude/settings.json`; `make` stays allowed, and lefthook only calls make targets.

## 6. Bring `.claude/` up to current conventions

The user asked for this separately from the five friction items. The findings below were checked against https://code.claude.com/docs/en/skills.md and https://code.claude.com/docs/en/memory.md, and against the repo itself.

- **`commands/` is superseded by skills.** Move `.claude/commands/merge-release.md` to `.claude/skills/merge-release/SKILL.md`.
  - Frontmatter: `name`, `description`, `argument-hint: "[patch|minor|major]"`, and `disable-model-invocation: true`, because tagging a release is outward-facing and should only run when a person asks for it.
  - Update the "Skills/commands" row in `.claude/rules/writing.md`.
- **The release skill disagrees with the repo's own guards and with how v0.27.1 shipped.**
  - Steps 3 and 6 run `git reset --hard origin/main`, which `.claude/settings.json` denies. Use `git switch main && git pull --ff-only`.
  - v0.27.1 renamed `## [Unreleased]` to the version heading inside the feature PR, then tagged main after the squash merge, with no separate changelog PR. Make that the primary path, and keep the separate changelog PR only as the fallback when the change is already merged.
  - Point the "wait for review" step at `scripts/pr-wait.sh`.
- **Two testing rules overlap.** `.claude/rules/testing.md` and `.claude/rules/go/testing.md` both load for `*_test.go`.
  - Fold the unique parts of `testing.md` (the `rr test*` commands and the flaky-test table) into `go/testing.md`, then delete `testing.md`.
  - `go/testing.md` also repeats the `RR_TEST_SSH_*` env list from CLAUDE.md. Keep one copy and point to `tests/integration/README.md`.
- **Runtime files are ignored only on this machine.** `.claude/settings.local.json`, `.claude/scheduled_tasks.lock` and `.claude/worktrees/` are covered only by the user's global gitignore and `.git/info/exclude`. Add all three to the repo `.gitignore` so a contributor's clone doesn't commit them.

Not changed: `.claude/CLAUDE.md` is a valid location, `rules/` with `paths:` frontmatter is current, and the `Bash(x:*)` permission syntax still works. No hooks: the audit's hook suggestion didn't name a real event, and a goimports hook was already dropped above.

## Considered and dropped

- **A PostToolUse hook that runs goimports on edited `.go` files.** The unused-import errors cost one compile cycle each, and lefthook already fixes formatting at commit time. The payoff is low.
- **A QA smoke script.** The manual QA scenarios changed with each PR, so a script would drift. Item 1 covers the part that repeats.
- **Retargeting CI to run on all PRs.** Stacked PRs were a one-off in this session. The note in item 5 is enough.

## Review changes

The devils-advocate review changed four items:

- **Item 1:** switched from in-process fd capture to a subprocess, and narrowed the coverage claim.
- **Item 2:** uses `gh pr checks --watch` and GraphQL thread resolution, and handles CodeRabbit's skip comment.
- **Item 3:** moved to a CI-only skip gate, since setting it in `.rr.yaml` would break `rr test-integration` on the runners.
- **Item 5:** leaves the lint lines alone.

## Rollout

The work is one PR, `chore: agent DX guards and notes`, with no product code changes. Items 1 and 3 must pass in CI, and item 3 must show zero skips. Item 1 must also be shown to catch a leak: temporarily revert one fix from #249, such as the `depends` StageHandler gate, and confirm the new test fails.
