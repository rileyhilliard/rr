# `.claude/`: Claude Code config

```
settings.json        shared config: hooks wiring, read-only allow rules, destructive-command deny rules
settings.local.json  your machine-only overrides (gitignored, create as needed)
hooks/               hooks, described below
```

Project instructions live in `AGENTS.md` at the repo root, which every coding agent reads. The root `CLAUDE.md` imports it and adds only Claude-specific notes, so edit `AGENTS.md` for anything that isn't Claude-specific.

## Hooks

| Hook | Event | What it does |
| --- | --- | --- |
| `session-start.sh` | SessionStart | Adds branch, dirty-file count, a warning on `main`, a pointer to `.stacks/BRIEF.md` while `AGENTS.md` is still a skeleton, uninstalled dependencies (a `package.json` without `node_modules/` or a `pyproject.toml` without `.venv/`, at the root or one directory down, with the install command), and compose services that aren't running. At most 15 lines. |
| `block-destructive.sh` | PreToolUse `Bash` | Denies commands that destroy work or data: `rm` of `/`, `~`, `.`, `..` or a bare `*`, a hard git reset, a forced git clean, `git checkout .`, a forced checkout or switch, `git restore .`, `git branch -D`, dropping the stash, force push (`--force-with-lease` and `--force-if-includes` are allowed), a push that deletes remote branches (`--mirror`, `--delete`, `:<ref>`), `--no-verify`, `chmod 777`, a recursive `chown` of `/` or `~`, `dd` or `mkfs` on a disk, a fork bomb, a download piped into a shell, `sudo`, `DROP TABLE`, `docker compose down -v`, publishing a release, infrastructure teardown (`terraform destroy`, `pulumi destroy`, `kubectl delete`, a recursive `aws s3 rm`). Git, release and infrastructure rules match only the command being run, so a commit message or PR body that mentions one is allowed. The other shell rules match the raw command text, so write docs that name these commands with Edit/Write, not a shell heredoc. |
| `detect-secrets.sh` | PreToolUse `Edit\|Write` | Denies any write to `.env`, `.env.*` or `.envrc` other than `.env.example` (or `.sample`, `.template`), and writes containing credential-shaped strings (cloud and SaaS keys, private keys, tokens, connection strings with passwords). The reason names the kind of secret, never the matched text. |
| `lint-on-write.sh` | PostToolUse `Edit\|Write` | Runs the project's linter on the file just written (ruff, biome or eslint, golangci-lint or gofmt, shellcheck) and hands diagnostics back to the agent, one line each. Formatting isn't reported, since `format-changed.sh` fixes it. Rust and Java files get no per-write lint, since clippy checks a whole crate and Maven starts a JVM; run the project's lint target. Also records the path for `format-changed.sh`. |
| `format-changed.sh` | Stop | Formats the files this session wrote with the formatters the project has configured (ruff, biome or prettier, gofmt, rustfmt with the edition from the nearest `Cargo.toml`, and Spotless through `./mvnw` for Java), then forgets them. Ruff's safe fixes run too, except removing unused imports (one written this turn may be used next turn). `lint-on-write.sh` records each path written through Edit/Write in a per-session list under `$TMPDIR/stacks-hooks/`, keyed by the session id. Files you or other agents in the same checkout are editing aren't touched; no session id means nothing is formatted. |
| `_destructive-patterns.sh` | (sourced) | The pattern list behind `block-destructive.sh`. Reuse it from any hook that approves or rewrites Bash commands. |
| `smoke.sh` | (not wired) | The `hooks-smoke` verify step: feeds each guard a deny and a near miss, and checks every hook config present parses and wires its guard. Add a case here when you add a guard. |
| `_lib.sh` | (sourced) | Shared helpers: the payload reader, the deny and context responses, project root, `find_up`, a portable `with_timeout`, the per-session file list. |

How they behave:

- **A block means ask the user.** A guard's deny tells the agent why; the agent asks you instead of rephrasing the command, splitting it up, or writing the file another way. Never work around a deny.
- **Accidents, not adversaries.** The shell guards match regexes against the command or file text, not a shell parse. Quoting, `eval`, aliases and scripts that run a command indirectly get past them, and so does a machine without `grep`, `sed` or `awk`.
- **Quiet when there's nothing to do.** A hook whose tool isn't installed exits 0 with no output. The hooks read their input with jq when it's on PATH and with a small awk reader otherwise, so they decide the same way on a machine without jq.
- **Guards deny; everything else advises.** Only guards block anything: `block-destructive.sh`, `detect-secrets.sh`, and any guard listed above. Lint results arrive as context and never block a write.
- **Nothing is downloaded.** Linters and formatters run only from the project's own install (`.venv`, `node_modules/.bin`) or from PATH. The hooks never call `npx`, `bunx`, or `uv run`, which could fetch packages or sync environments mid-session.
- **Timeouts everywhere.** Each external call runs under a timeout, so a stuck linter or Docker daemon can't stall the session.
- Scripts are invoked as `bash "${CLAUDE_PROJECT_DIR}/.claude/hooks/<name>.sh"`, so they don't need the executable bit, and they target bash 3.2 (the macOS default) under `set -euo pipefail`.

To try a hook by hand, pipe it a payload:

```bash
echo '{"tool_name":"Bash","tool_input":{"command":"git push --force"}}' | bash .claude/hooks/block-destructive.sh
```

### Turning one off

`/hooks` in Claude Code lists what's wired. To run a session without hooks, set `"disableAllHooks": true` in `settings.local.json` and remove it afterwards; the `permissions.deny` rules still apply. There's no per-hook switch: to drop one hook for good, delete its entry from `settings.json`.

### Changing them

- Add a shell destructive pattern to `_destructive-patterns.sh`, not to `block-destructive.sh`.
- Project-specific checks (generated files that must not be hand-edited, a missing symlink at session start) go in the relevant hook or a new one wired in `settings.json`. A hook that fires on every tool call should cost milliseconds.
- If the project has hook tests, add a case for each new pattern: one command that must be denied and one near miss that must pass.

## Settings

`settings.json` is shared and committed. Some keys (permission allow rules, plugin marketplaces) only take effect after each person trusts the folder in Claude Code; deny rules apply right away. Put anything personal (extra permissions, env, personal MCP servers) in `settings.local.json`.

`permissions.allow` starts with read-only git only (`status`, `diff`, `log`, `show`, `branch --show-current`). Add the project's own non-destructive commands (lint, test, typecheck) as they settle; leave deploy, publish, and anything destructive to prompt.

`permissions.deny` repeats the worst destructive shapes (`rm -rf /`, `rm -rf ~`, `rm -rf .`, force push, `git reset --hard`, `--no-verify` on commit and push) so they stay blocked with hooks turned off. They are exact rules on purpose: a wildcard such as `Bash(rm -rf /*)` would match every `rm -rf /some/abs/path`, and a deny rule can't be overridden by an allow rule. It also denies reading `.env`, `.env.local`, `.env.*.local`, `.env.production` and `.envrc`, at the root and in any subdirectory (`.env.example` stays readable), and editing `.stacks/manifest.json`: the manifest records the verify steps the project was generated with, and `stacks verify` fails a changed step, so the agent changes it only by asking you.

Depending on which stacks fragments are installed, `settings.json` may also:

- register the `claude-essentials` marketplace and enable its `ce` plugin (skills for testing, error handling and writing; review and log-reading agents), or
- register the `rr` marketplace and enable the `rr` plugin (the rr skill and `/rr:setup`), pre-approving rr's read-only commands (`rr doctor`, `rr status`, `rr tasks`). `rr run`/`exec` and named tasks still prompt, because they run commands on remote hosts.

Both take effect once each person trusts the folder in Claude Code.
