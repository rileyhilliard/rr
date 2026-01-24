# Machine Interface (LLM/CI Mode)

Use `--machine` (or `-m`) for structured JSON output with consistent envelope format.

## Global Flag

```bash
rr --machine <command>   # JSON output with success/error envelope
rr -m doctor             # Short form
```

## Commands with JSON Output

| Command | Purpose |
|---------|---------|
| `rr doctor --machine` | Full diagnostic with structured results |
| `rr status --machine` | Host connectivity check |
| `rr tasks --machine` | List available tasks |
| `rr host list --machine` | List configured hosts |

## JSON Envelope Format

All `--machine` output follows this structure:

**Success:**
```json
{
  "success": true,
  "data": { /* command-specific output */ },
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
1. Run: rr doctor --machine
2. Parse: response.success
   - true  -> Setup OK
   - false -> Check response.error.code

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
     -> Inform user about host key verification
     -> Run: ssh -o StrictHostKeyChecking=accept-new <hostname> exit

   LOCK_HELD:
     -> Run: rr unlock
     -> Retry original command

   REQUIREMENTS_FAILED:
     -> Check which tools are missing
     -> Install tools or run with --skip-requirements
```

## When to Use rr vs Local Execution

```text
IF .rr.yaml exists AND rr status --machine shows healthy hosts:
  -> Use rr for tests, builds, remote commands

IF no .rr.yaml OR all hosts unhealthy:
  -> Check if local_fallback is enabled in config
  -> If yes: rr will run locally automatically
  -> If no: run commands locally with Bash tool
```

## Example: Parsing Doctor Output

```bash
result=$(rr doctor --machine 2>&1)
success=$(echo "$result" | jq -r '.success')

if [ "$success" = "true" ]; then
  echo "All checks passed"
else
  code=$(echo "$result" | jq -r '.error.code')
  suggestion=$(echo "$result" | jq -r '.error.suggestion')
  echo "Error: $code"
  echo "Fix: $suggestion"
fi
```

## Example: Checking Host Status

```bash
rr status --machine | jq '.data.hosts[] | select(.healthy == false) | .name'
```

Returns names of unhealthy hosts.
