# Structured Output (Agent/CI Mode)

rr defaults to structured output. Phase events are emitted as JSON lines to stderr. Command stdout/stderr passes through undecorated to stdout/stderr. No flags needed.

Use `--pretty` / `-p` to opt into human-readable output with spinners and colors.

The `--machine` / `-m` flag is kept for backward compatibility but is now a no-op.

## Phase Events (stderr)

During `rr run`, `rr exec`, or `rr <task>`, phase events are emitted as JSON lines to stderr. The lock is taken before sync:

```json
{"type":"phase","phase":"connect","status":"started","ts":"2026-01-15T10:30:00Z"}
{"type":"phase","phase":"connect","status":"complete","host":"m4-mini","duration_s":0.5,"ts":"..."}
{"type":"phase","phase":"lock","status":"started","ts":"..."}
{"type":"phase","phase":"lock","status":"complete","host":"m4-mini","duration_s":0.1,"ts":"..."}
{"type":"phase","phase":"sync","status":"started","ts":"..."}
{"type":"phase","phase":"sync","status":"complete","host":"m4-mini","duration_s":2.1,"ts":"..."}
{"type":"phase","phase":"exec","status":"started","details":{"command":"make test"},"ts":"..."}
```

After the command finishes:
```json
{"type":"result","status":"success","exit_code":0,"host":"m4-mini","duration_s":12.3,"details":{"exec_duration_s":10.1,"log_file":"/home/me/.rr/logs/run-20260115-103000/output.log"},"ts":"..."}
```

`--no-phases` suppresses the `phase` events; the `result` event is always emitted.

### Waiting for a lock

When the host's lock is held, rr emits one `waiting` event before it waits, naming the holder:

```json
{"type":"phase","phase":"lock","status":"waiting","host":"dev","details":{"message":"Waiting up to 5m0s for the lock on dev: 'make test' held by me@laptop (pid 4242, started 1m0s ago, this machine)","holders":[{"host":"dev","user":"me","pid":4242,"command":"make test","age_s":60,"same_machine":true}],"wait_timeout_s":300}}
```

That's a single host (the only candidate, or picked with `--host`/`--tag`), which waits up to `lock.timeout`. With several hosts all locked, the same details come on a `connect` `waiting` event with no `host`, and rr cycles through the hosts for up to `lock.wait_timeout`. Either wait ends with the lock, a `LOCK_HELD` error on timeout, or `INTERRUPTED` (exit 130) if rr is stopped with Ctrl+C or SIGTERM. Waiters poll every 2 seconds rather than queueing, so the order they get the lock in isn't guaranteed.

The `exec` event's `details.command` is the command that actually ran, after `{args}` substitution, appended task args, and path rewriting.

### Where the command runs

The connect event says where the command runs and why:

| Situation | Connect event | `details.reason` | Result `details.fallback` |
|-----------|---------------|------------------|---------------------------|
| `--local` | `status: complete`, `host: local` (see below) | `local_flag` | none |
| Local mode: `.rr.yaml` enables `local_fallback` and lists no `host`/`hosts`, or `local_fallback` is on and no hosts are configured at all; no `--host`/`--tag` | `status: complete`, `host: local` (see below) | `local_mode` | none |
| No host reachable, `local_fallback` on | `status: warn`, `host: local` (see below), `details.local_fallback: true` | `hosts_unreachable` | `{reason}` |
| Every host locked, `local_fallback` on | `status: warn`, `host: local` (see below), `details.local_fallback: true` | `all_hosts_locked` | `{reason, waited_s, holders}` |

The result of a `--local` or local-mode run carries the same value in `details.local_reason`; `details.fallback` appears only for runtime fallbacks, never alongside it. `--local` and local mode need no configured hosts. Transition note: rr binaries older than v0.27.0 report a `--local` run as a connect `warn` event with `reason: hosts_unreachable`. Treat that as `local_flag` when you passed `--local`.

Which `host` these runs report depends on whether the global config has a local host (`local: true`):

- **With a local host** (say `dev`), all four run on it, as `--host dev` would. The connect events (the fallback `warn` included) and the result say `host: "dev"`, a `lock` phase runs on `dev` (and can emit `lock` `waiting`), and the sync phase is skipped with `reason: in_place`. The run also gets `dev`'s `require:` checks, so it can fail with `DEPENDENCY_MISSING`. `details.reason`, `details.local_reason` and `details.fallback` still say why the run is here, so branch on those, not on `host == "local"`. A host can't be named `local`, so `host: "local"` always means the case below.
- **Without one**, they report `host: "local"`, take no lock, and skip sync with `reason: local`.

In a parallel run, a host that becomes unavailable is dropped for the rest of the run, and the subtask its worker had picked up moves to another host. That emits one connect `warn` event with the `host`, `details.task`, `details.error` (`{code, message, suggestion}`), and `details.reason`: `connection_lost` when the connection died mid-run, `connect_failed` when the host was never reached. A subtask that was running when its connection dropped isn't moved, since it may have partly run. It fails with `Lost the connection before the command finished`. For a single `rr run` or task, that same error shows up as `details.error` on a failed result with `exit_code: -1`, and `log_file` still points at the partial output (tasks with `depends` don't write a run log, so they have no `log_file`).

### Config warnings

Config problems that don't stop a run are reported once per invocation as a `config` phase event, before any other event:

```json
{"type":"phase","phase":"config","status":"warn","details":{"file":".rr.yaml","key":"output","message":"The 'output' section has no effect and is no longer supported","suggestion":"Remove the 'output:' block from .rr.yaml. Use --pretty for human-readable output."}}
```

They cover unknown keys (usually typos), the removed `output:` section, the removed `defaults.host` global key, `pull:` on a parallel task, and `output:` on a non-parallel task. The deprecated `--verbose` flag emits the same event shape with `details.flag: "--verbose"` instead of `file`/`key`.

## Phase Event Schema

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `"phase"` or `"result"` |
| `phase` | string | `"config"`, `"connect"`, `"sync"`, `"lock"`, `"exec"`, `"pull"` |
| `status` | string | Phase: `"started"`, `"complete"`, `"failed"`, `"skipped"`, `"warn"`, `"waiting"` (lock and connect), plus `"pruned"`/`"invalidated"` for sync. Result: `"success"`, `"failed"` |
| `host` | string | Host name (on complete/failed, lock `waiting`, and connect `warn`; on sync notices during parallel runs, the host that synced) |
| `duration_s` | float | Duration in seconds (on complete) |
| `exit_code` | int | Process exit code (on result) |
| `error` | string | Error message (on failed) |
| `details` | object | Additional context (varies by phase) |
| `ts` | string | RFC3339 timestamp |

## Result Details

The `details` object on the result event can include:

| Key | Meaning |
|-----|---------|
| `exec_duration_s` | Time spent running the command |
| `log_file` | Raw output log for this run |
| `summary` | `{passed, failed, skipped, errors}` from pytest, jest/vitest, or go test output |
| `failures` | `[{name, file, message}]`, `file` as `path:line` (only on failure) |
| `no_tests` | `true` when the runner reported collecting zero tests |
| `piped_exit_code` | `true` when zero tests ran and the command has a pipe, so the exit code may come from a later stage |
| `hint` | Explanation for a failure that looks like a local-vs-remote path mistake |
| `local_reason` | `local_flag` or `local_mode` on a `--local` or local-mode run |
| `fallback` | `{reason}` when rr ran locally because no host was reachable (`hosts_unreachable`), or `{reason, waited_s, holders}` when every host was locked (`all_hosts_locked`) |
| `path_rewrites` | Count of local absolute paths rewritten to remote paths |
| `remote_cwd` | Subdirectory (relative to the project root) the command ran in |
| `broken_pipe` | `true` when the stdout consumer closed early (e.g. `\| head`) |

Parallel tasks emit a single result event with no `host`. Its details hold `total`, `passed`, `failed`, `log_dir`, `failures` (per subtask: `task`, `host`, `exit_code`, `log_file`, `error` as `{code, message, suggestion}` when rr hit an error running it, and parsed test failures or an `output_tail`), and `no_tests: true` plus `no_tests_tasks` (the subtask names) when some subtasks collected nothing.

During a parallel run, sync notices (`invalidated`, `warn`, `pruned`) carry the top-level `host` that synced, since each host syncs once for all its subtasks. Subtask `pull:` runs after every subtask finishes, pass or fail, and emits `pull` phase events with `host` and `details.task`. A failed pull is a `pull` `failed` event and doesn't change the exit code.

## Informational Commands (JSON Envelope)

Commands like `doctor`, `status`, `tasks`, `host list` emit a JSON envelope to stdout. When any command fails before (or instead of) running a command, the same envelope with `success: false` goes to stderr and rr exits 1. That includes `rr tasks` with an invalid config and an unknown command name:

```json
{
  "success": true,
  "data": { /* command-specific */ }
}
```

**Failure:**
```json
{
  "success": false,
  "error": {
    "code": "SSH_AUTH_FAILED",
    "message": "probe m1-mini failed: authentication failed (ssh: handshake failed: ...)",
    "suggestion": "Deploy SSH key: ssh-copy-id <hostname>",
    "details": {"reason": "authentication failed", "alias": "m1-mini"}
  }
}
```

## Error Codes

| Code | Meaning | Action |
|------|---------|--------|
| `CONFIG_NOT_FOUND` | No `.rr.yaml`, or the `--config` file doesn't exist. Also a checkout nested in another one (a worktree under the main checkout) with no `.rr.yaml` of its own: the message names the outer `.rr.yaml` rr skipped | Run `rr init`, or for a nested checkout commit or copy `.rr.yaml` into it (see `suggestion`) |
| `CONFIG_INVALID` | Config or flag error (bad value, reserved task name, extra flags on a parallel task without `forward_args`) | Fix config, or follow `suggestion` |
| `HOST_NOT_FOUND` | A host name (`--host`, `rr unlock`, `rr provision`, `rr host remove`, `rr monitor`, or the project's `hosts:`) doesn't match a configured host | Check `rr host list` |
| `SSH_TIMEOUT` | Connection timed out | Check network/VPN |
| `SSH_AUTH_FAILED` | Key rejected | Run `rr setup <host>` |
| `SSH_HOST_KEY` | Host key unknown or changed | Unknown key: verify the fingerprint through a trusted channel, then `ssh -o StrictHostKeyChecking=accept-new <alias> exit`. Changed key: never auto-accept; verify the new fingerprint, then `ssh-keygen -R <host>` |
| `SSH_CONNECTION_FAILED` | SSH connection error | Check host reachability |
| `RSYNC_FAILED` | File sync failed | Check disk space/permissions |
| `LOCK_HELD` | Another process has lock | Run `rr unlock` |
| `COMMAND_FAILED` | Remote command failed | Check command output |
| `DEPENDENCY_MISSING` | A required tool is missing: local or remote `rsync`, `ssh-copy-id` (for `rr setup`), or a `require:` tool (`Missing required tools: ...`) | `rr provision`, or install it |
| `INTERRUPTED` | rr was stopped (Ctrl+C, SIGTERM) while it waited for a lock, on one host or across several; nothing ran. rr exits 130 | Don't retry on your own; the user stopped it |
| `UNKNOWN` | Unclassified error | Read `message` |

Codes are set where the error is created, never guessed from the message. Transition note: rr binaries older than v0.27.0 report missing required tools as `COMMAND_FAILED` with a message starting `Missing required tools`, and an unknown host as `CONFIG_NOT_FOUND` or `CONFIG_INVALID`. Treat `COMMAND_FAILED` + `Missing required tools` the same as `DEPENDENCY_MISSING`. Always read `message` and `suggestion`.

## Exit Code Contract

When the command runs, rr's exit code is the command's exit code (a parallel task exits 1 if any subtask failed). A command cut off by a dropped connection has `exit_code: -1` and `details.error`, and rr exits 255. When rr fails before the command runs (config, SSH, lock, sync, requirements), it exits 1 and writes an error envelope to stderr instead of a result event. The exception is `INTERRUPTED`: stopped while waiting for a lock, rr exits 130, the same as Ctrl+C during the command. Check for a `"type":"result"` line to tell the two apart.

`rr doctor` is the exception: it exits 1 when any check fails and 0 when there are only warnings, but its envelope still says `success: true`, because doctor itself ran. `data.summary.fail` counts failures; `data.summary.all_clear` is true only when nothing failed or warned.

## Non-Interactive Commands

For CI/automation, use flag-based commands:

```bash
# Add host without prompts
rr host add --name dev-box \
  --ssh "dev.local,dev-tailscale" \
  --dir '~/projects/${PROJECT}' \
  --tag fast \
  --env "DEBUG=1" --env "PATH=/custom/bin:$PATH" \
  --skip-probe

# Add this machine as a local host (runs in place, no SSH or sync)
rr host add --local --name dev --tag fast

# Initialize project without prompts
rr init --non-interactive --host dev-box
```

## Troubleshooting Decision Tree

```
1. Run: rr doctor
2. Parse JSON output:
   - .success false          -> Check .error.code (doctor couldn't run)
   - .data.summary.all_clear -> true means setup OK
   - otherwise read .data.categories[].results[] with status 2 (fail) or 1 (warn); 0 is pass
     (exit 1 means at least one check failed; warnings alone exit 0)

3. Based on error.code:

   CONFIG_NOT_FOUND:
     -> Run: rr init --non-interactive --host <host>

   SSH_TIMEOUT:
     -> Check network: ping <hostname>
     -> Try alternate SSH alias

   SSH_AUTH_FAILED:
     -> Run: rr setup <hostname>
     -> Or: ssh-copy-id <hostname>

   SSH_HOST_KEY:
     -> Run: ssh -o StrictHostKeyChecking=accept-new <hostname> exit

   CONFIG_NOT_FOUND naming a .rr.yaml above this checkout:
     -> This is a nested worktree without its own .rr.yaml
     -> Copy or commit .rr.yaml into it (the suggestion has the cp command)

   LOCK_HELD:
     -> Message names the holder; wait if it's a live run
     -> Run: rr unlock <host>  (or rr unlock --all)
     -> Retry original command

   INTERRUPTED:
     -> The user stopped rr while it waited for a lock; don't retry

   DEPENDENCY_MISSING (or COMMAND_FAILED with "Missing required tools" on older rr):
     -> Run: rr provision --yes
     -> Or install manually; rr run/exec also accept --skip-requirements

   HOST_NOT_FOUND:
     -> Run: rr host list, then fix the name
```

## Parsing Phase Events

```bash
# Run command and capture phase events from stderr
rr run "make test" 2>events.jsonl

# Check result (stderr also carries the command's own stderr, so filter)
grep '"type":"result"' events.jsonl | jq '.exit_code'

# Get execution duration
grep '"type":"result"' events.jsonl | jq '.details.exec_duration_s'
```

## When to Use rr vs Local Execution

```text
IF .rr.yaml exists AND rr status shows healthy hosts:
  -> Use rr for tests, builds, remote commands

IF no .rr.yaml OR all hosts unhealthy:
  -> Check local_fallback (never / on-unreachable / always)
  -> on-unreachable or always: rr runs locally when no host is reachable
     (always also falls back when every host stays locked past lock.wait_timeout)
  -> never: run commands locally yourself
```

For the most current flag and command details, run `rr --help` or `rr <command> --help`.
