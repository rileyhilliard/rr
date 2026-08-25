# Structured Output (Agent/CI Mode)

rr defaults to structured output. Phase events are emitted as JSON lines to stderr. Command stdout/stderr passes through undecorated to stdout/stderr. No flags needed.

Use `--pretty` / `-p` to opt into human-readable output with spinners and colors.

The `--machine` / `-m` flag is kept for backward compatibility but is now a no-op.

## Phase Events (stderr)

During `rr run` or `rr <task>`, phase events are emitted as JSON lines to stderr:

```json
{"type":"phase","phase":"connect","status":"started","ts":"2026-01-15T10:30:00Z"}
{"type":"phase","phase":"connect","status":"complete","host":"m4-mini","duration_s":0.5,"ts":"..."}
{"type":"phase","phase":"sync","status":"started","ts":"..."}
{"type":"phase","phase":"sync","status":"complete","host":"m4-mini","duration_s":2.1,"ts":"..."}
{"type":"phase","phase":"lock","status":"started","ts":"..."}
{"type":"phase","phase":"lock","status":"complete","host":"m4-mini","duration_s":0.1,"ts":"..."}
{"type":"phase","phase":"exec","status":"started","details":{"command":"make test"},"ts":"..."}
```

After the command finishes:
```json
{"type":"result","status":"success","exit_code":0,"host":"m4-mini","duration_s":12.3,"details":{"exec_duration_s":10.1},"ts":"..."}
```

## Phase Event Schema

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `"phase"` or `"result"` |
| `phase` | string | `"connect"`, `"sync"`, `"lock"`, `"exec"`, `"pull"` |
| `status` | string | `"started"`, `"complete"`, `"failed"`, `"skipped"` |
| `host` | string | Host name (on complete/failed) |
| `duration_s` | float | Duration in seconds (on complete) |
| `exit_code` | int | Process exit code (on result) |
| `error` | string | Error message (on failed) |
| `details` | object | Additional context (varies by phase) |
| `ts` | string | RFC3339 timestamp |

## Informational Commands (JSON Envelope)

Commands like `doctor`, `status`, `tasks`, `host list` emit a JSON envelope to stdout:

```json
{
  "success": true,
  "data": { /* command-specific */ },
  "error": null
}
```

**Failure:**
```json
{
  "success": false,
  "data": null,
  "error": {
    "code": "SSH_AUTH_FAILED",
    "message": "Authentication failed for host m1-mini",
    "suggestion": "Run: ssh-copy-id m1-mini"
  }
}
```

## Error Codes

| Code | Meaning | Action |
|------|---------|--------|
| `CONFIG_NOT_FOUND` | No .rr.yaml | Run `rr init` |
| `CONFIG_INVALID` | Schema error | Fix config syntax |
| `HOST_NOT_FOUND` | Unknown host name | Check `rr host list` |
| `SSH_TIMEOUT` | Connection timed out | Check network/VPN |
| `SSH_AUTH_FAILED` | Key rejected | Run `rr setup <host>` |
| `SSH_HOST_KEY` | Host key mismatch | Verify fingerprint |
| `SSH_CONNECTION_FAILED` | SSH connection error | Check host reachability |
| `RSYNC_FAILED` | File sync failed | Check disk space/permissions |
| `LOCK_HELD` | Another process has lock | Run `rr unlock` |
| `COMMAND_FAILED` | Remote command failed | Check command output |
| `DEPENDENCY_MISSING` | Required tool not found | Install missing dependency |
| `REQUIREMENTS_FAILED` | Required tools missing | Install tools or use --skip-requirements |

## Exit Code Contract

The process exit code always matches the remote command's exit code. JSON events are supplementary metadata on stderr, not the primary signal.

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

# Initialize project without prompts
rr init --non-interactive --host dev-box
```

## Troubleshooting Decision Tree

```
1. Run: rr doctor
2. Parse JSON output: check .success field
   - true  -> Setup OK
   - false -> Check .error.code

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

   LOCK_HELD:
     -> Run: rr unlock
     -> Retry original command

   REQUIREMENTS_FAILED:
     -> Check which tools are missing
     -> Install tools or run with --skip-requirements
```

## Parsing Phase Events

```bash
# Run command and capture phase events from stderr
rr run "make test" 2>events.jsonl

# Check result
tail -1 events.jsonl | jq '.exit_code'

# Get execution duration
tail -1 events.jsonl | jq '.details.exec_duration_s'
```

## When to Use rr vs Local Execution

```text
IF .rr.yaml exists AND rr status shows healthy hosts:
  -> Use rr for tests, builds, remote commands

IF no .rr.yaml OR all hosts unhealthy:
  -> Check if local_fallback is enabled in config
  -> If yes: rr will run locally automatically
  -> If no: run commands locally
```

For the most current flag and command details, run `rr --help` or `rr <command> --help`.
