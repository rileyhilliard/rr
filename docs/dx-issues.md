# DX Issues Found During Manual Testing

This document captures developer experience issues discovered during manual testing of `rr` on January 9, 2025.

## Critical Bugs

### 1. Lock Acquisition Never Succeeds (HIGH PRIORITY)

**Symptom:** `rr run` and `rr exec` with locking enabled spin on "Acquiring lock..." until timeout, even when no lock exists.

**Observed Behavior:**
- The lock directory is never created on the remote
- Direct SSH mkdir commands work fine
- The same SSH client code works in isolation

**Impact:** Locking is completely broken in production use.

**Workaround:** Disable locking in `.rr.yaml`:
```yaml
lock:
  enabled: false
```

**Investigation Notes:**
- `mkdir` via direct SSH client test returns exit code 0
- The lock loop appears to keep retrying without creating the directory
- May be a connection state issue between sync and lock phases

---

## SSH/Config Issues

### 2. SSH Config Parsing Fails Silently on `Match` Directives

**Symptom:** Host aliases from `~/.ssh/config` don't resolve, connection fails with "no such host".

**Root Cause:** The `kevinburke/ssh_config` library doesn't support `Match` directives. When it encounters one, the entire config parsing fails silently.

**Impact:** Users with modern SSH configs (using `Match` blocks for conditional settings) can't use SSH aliases.

**Current Behavior:** Silent failure - no error message shown.

**Recommended Fix:**
1. Catch the parsing error and log a warning
2. Try to parse entries before the `Match` block
3. Or switch to a library that supports `Match` directives

**Workaround:** Use explicit `user@host` format in `.rr.yaml` instead of SSH aliases.

### 3. Encrypted SSH Keys Require Manual `ssh-add`

**Symptom:** Connection fails with "no SSH authentication methods available" even when `ssh <host>` works.

**Root Cause:** macOS's `ssh` command integrates with Keychain automatically. Go's `crypto/ssh` library doesn't. Encrypted keys fail to parse without passphrase.

**Current Error Message:**
```
no SSH authentication methods available
Check your SSH keys and SSH agent: ssh-add -l
```

**Recommended Fix:** Improve the error message to be more specific:
```
SSH key at ~/.ssh/id_ed25519 is encrypted (passphrase protected).
Add it to your SSH agent: ssh-add --apple-use-keychain ~/.ssh/id_ed25519
```

### 4. knownhosts Key Mismatch False Positives

**Symptom:** "knownhosts: key mismatch" error even when keys match.

**Observed:** Server has multiple key types (RSA, ECDSA, ED25519). Go may negotiate a different key type than what's in known_hosts.

**Workaround:** Add all key types to known_hosts:
```bash
ssh-keyscan -t rsa,ecdsa,ed25519 <host> >> ~/.ssh/known_hosts
```

---

## rsync Issues

### 5. Modern rsync Required But Not Documented

**Symptom:** Sync fails with "unrecognized option `--info=progress2`"

**Root Cause:** macOS ships with `openrsync` (version 2.6.9 from 2006) which doesn't support modern flags. `--info=progress2` requires rsync 3.1+.

**Current Error Message:**
```
rsync failed: rsync: unrecognized option `--info=progress2'
Check that the remote directory exists and you have write permissions
```

**Issues:**
1. The error message is misleading (suggests permissions issue)
2. Documentation doesn't mention rsync version requirement
3. No graceful fallback for older rsync versions

**Recommended Fixes:**
1. Detect rsync version and fall back to `--progress` for older versions
2. Document rsync 3.1+ requirement in README
3. Add rsync version check to `rr doctor`
4. Improve error message to suggest installing modern rsync

### 6. PATH Not Inherited for Homebrew rsync

**Symptom:** System rsync (openrsync) is used even when Homebrew rsync is installed.

**Root Cause:** `/opt/homebrew/bin` often comes after `/usr/bin` in PATH, or isn't in PATH for non-login shells.

**Workaround:** Add to `~/.zshrc`:
```bash
export PATH="/opt/homebrew/bin:$PATH"
```

---

## Config Issues

### 7. Tilde Expansion Doesn't Work in Config

**Symptom:** Commands fail with "no such file or directory: ~/rr-sync/..."

**Root Cause:** `~` is not expanded when used in `dir` or `remote_dir` config values.

**Workaround:** Use `${HOME}` instead of `~`, or use absolute paths:
```yaml
hosts:
  myhost:
    dir: ${HOME}/rr/project               # Works (default since v0.4)
    dir: /Users/username/rr-sync/project  # Also works
    # dir: ~/rr-sync/project              # Doesn't work
```

**Status:** `rr init` now defaults to `${HOME}/rr/${PROJECT}` which works correctly.

### 8. Missing Dependency Handling

**Symptom:** Commands like `go test` fail with "command not found" on remote.

**Root Cause:** Remote machine may not have required tools installed, and SSH sessions don't source `.zshrc` by default.

**Workaround:** Source shell config in commands:
```bash
rr run "source ~/.zshrc && go test ./..."
```

**Recommended Fixes:**
1. Add a `shell` config option to specify shell init (e.g., `bash -l -c`)
2. Or add a `setup_commands` config for running before each command
3. Improve error messages when commands aren't found

---

## UX/DX Improvements

### 9. `rr init` Requires TTY

**Symptom:** `rr init` fails in non-interactive environments with "could not open a new TTY".

**Recommended Fix:** Add `--non-interactive` flag with reasonable defaults or accept config from stdin.

### 10. `rr doctor` Shows "Unknown error" for All Failures

**Symptom:** Host check shows "Unknown error" without details.

**Current Output:**
```
✗ mini: Unknown error
```

**Recommended Fix:** Show the actual error message:
```
✗ mini: SSH handshake failed (key mismatch)
```

---

## Summary

| Issue | Severity | Workaround Available |
|-------|----------|---------------------|
| Lock never succeeds | Critical | Yes (disable locking) |
| SSH config Match fails | High | Yes (use explicit host) |
| Encrypted keys need ssh-add | Medium | Yes (run ssh-add) |
| knownhosts mismatch | Medium | Yes (add all key types) |
| Modern rsync required | High | Yes (install via brew) |
| PATH not inherited | Medium | Yes (update .zshrc) |
| Tilde not expanded | Medium | Yes (use absolute paths) |
| Missing deps on remote | Medium | Yes (source .zshrc) |
| init requires TTY | Low | No |
| Unknown error messages | Low | No |
