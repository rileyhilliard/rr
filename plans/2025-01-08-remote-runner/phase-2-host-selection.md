# Phase 2: Smart Host Selection

> **Status:** NOT_STARTED

## Goal

Add the connection magic: multiple SSH aliases per host with ordered fallback, connection caching, local fallback option, and diagnostic commands (`status`, `doctor`).

## Success Criteria

- [ ] Tool tries SSH aliases in order until one connects
- [ ] Connection fallback is visible in output
- [ ] `rr status` shows all hosts and their connectivity
- [ ] `rr doctor` diagnoses common issues with `--json` output
- [ ] Connection is cached within a session (no re-probe)
- [ ] Local fallback works when configured
- [ ] Host tags work for filtering

## Phase Exit Criteria

- [ ] All 5 tasks completed
- [ ] `make test` passes with >70% coverage on new code
- [ ] Manual testing: fallback works when first host unreachable
- [ ] `rr doctor --json` produces valid JSON

## Context Loading

```bash
# Read before starting:
read internal/host/selector.go
read internal/host/probe.go
read internal/config/types.go
read internal/errors/errors.go

# Reference:
read ../../proof-of-concept.sh           # Lines 60-83 for host fallback
read ../../ARCHITECTURE.md               # Lines 784-811 for host selection flow
```

---

## Execution Order

```
┌─────────────────────────────────────────────────────────────────┐
│ Task 1: Multi-Alias Fallback & Connection Caching               │
└─────────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┴───────────────────┐
          ▼                                       ▼
┌─────────────────────────────┐     ┌─────────────────────────────┐
│ Task 2: Local Fallback      │     │ Task 3: Status & Doctor     │
│ (parallel)                  │     │ (parallel)                  │
└─────────────────────────────┘     └─────────────────────────────┘
          │                                       │
          └───────────────────┬───────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 4: Host Tags & Probe Timeout Config                        │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 5: Connection Output Improvements                          │
└─────────────────────────────────────────────────────────────────┘
```

---

## Tasks

### Task 1: Multi-Alias Fallback & Connection Caching

**Context:**
- Modify: `internal/host/selector.go`, `internal/host/probe.go`
- Create: `internal/host/cache.go`
- Read: `internal/config/types.go` (Host.SSH array)
- Reference: `../../proof-of-concept.sh` lines 70-83 for fallback logic

**Steps:**

1. [ ] Update `internal/host/selector.go`:
   - `Select()` tries each SSH alias in `Host.SSH` array in order
   - On connection failure, try next alias
   - Return `Connection` with `Alias` field showing which succeeded
   - Emit events for output layer: "trying alias X", "alias X failed: reason", "connected via alias Y"

2. [ ] Create `internal/host/cache.go`:
   - In-memory connection cache: `map[hostName]*Connection`
   - `Get(hostName) *Connection` - return cached or nil
   - `Set(hostName, conn)` - cache connection
   - `Clear(hostName)` - remove on error
   - `CloseAll()` - cleanup on exit
   - Thread-safe with mutex

3. [ ] Update `internal/host/probe.go`:
   - Add `ProbeAll(aliases []string, timeout time.Duration) []ProbeResult`
   - `ProbeResult`: `Alias string`, `Latency time.Duration`, `Error error`
   - Probe sequentially (stop on first success for Select, all for Status)

4. [ ] Update `internal/cli/run.go` and `exec_cmd.go`:
   - Use cache for connection reuse
   - Show fallback in output when it happens

5. [ ] Create `internal/host/selector_test.go`:
   - Test fallback when first alias fails
   - Test cache hit/miss
   - Test all aliases fail scenario

**Verify:** `go test ./internal/host/...`

---

### Task 2: Local Fallback

**Context:**
- Modify: `internal/host/selector.go`
- Modify: `internal/exec/executor.go` (if exists) or create `internal/exec/local.go`
- Modify: `internal/sync/sync.go`
- Read: `internal/config/types.go` (LocalFallback field)

**Steps:**

1. [ ] Update `internal/host/selector.go`:
   - If all remote hosts fail and `config.LocalFallback == true`:
     - Return special `Connection` with `IsLocal: true`
     - Emit warning: "All remote hosts unreachable, falling back to local execution"
   - If `LocalFallback == false` and all fail:
     - Return structured error listing all failures

2. [ ] Create `internal/exec/local.go`:
   - `ExecuteLocal(cmd string, stdout, stderr io.Writer) (exitCode int, err error)`
   - Use `os/exec` to run command locally
   - Same interface as SSH execution
   - Handle working directory

3. [ ] Update `internal/sync/sync.go`:
   - If `conn.IsLocal`, skip sync entirely (already local)
   - Or optionally sync to a local temp directory for consistency

4. [ ] Update `internal/cli/run.go`:
   - Handle local execution path
   - Show clear indication that running locally

5. [ ] Create tests for local fallback

**Verify:**
```bash
# With unreachable host and local_fallback: true
cat > .rr.yaml << 'EOF'
version: 1
hosts:
  test:
    ssh: [nonexistent.local]
    dir: /tmp/rr-test
local_fallback: true
EOF

./rr run "echo running locally"  # Should run locally with warning
```

---

### Task 3: Status & Doctor Commands

**Context:**
- Create: `internal/cli/status.go`, `internal/cli/doctor.go`
- Create: `internal/doctor/checks.go`, `internal/doctor/config.go`, `internal/doctor/ssh.go`, `internal/doctor/hosts.go`, `internal/doctor/deps.go`, `internal/doctor/remote.go`
- Read: `internal/host/probe.go` (ProbeAll)
- Reference: `../../ARCHITECTURE.md` lines 293-361 for doctor output

**Steps:**

1. [ ] Create `internal/cli/status.go`:
   - Load config
   - Probe all configured hosts in parallel using `ProbeAll`
   - Display connectivity table:
     ```
     Hosts:
       ● mini
         ● mini-local: Connected (12ms)
         ● mini (tailscale): Connected (48ms)
       ✗ gpu-box
         ✗ gpu.local: Connection refused

     Default: mini
     Selected: mini (via mini-local)
     ```
   - Show which host would be used for next command
   - Add `--json` flag for machine-readable output

2. [ ] Create `internal/doctor/checks.go`:
   - Define `Check` interface: `Name() string`, `Run() CheckResult`
   - `CheckResult`: `Status` (pass/warn/fail), `Message string`, `Suggestion string`
   - `RunAll(checks []Check) []CheckResult`

3. [ ] Create individual check files:
   - `internal/doctor/config.go`: config file exists, schema valid, no reserved task names
   - `internal/doctor/ssh.go`: SSH key exists, SSH agent running, key permissions
   - `internal/doctor/hosts.go`: connectivity to all hosts
   - `internal/doctor/deps.go`: rsync installed locally and remotely
   - `internal/doctor/remote.go`: working directory exists, write permission, stale locks

4. [ ] Create `internal/cli/doctor.go`:
   - Run all checks
   - Display formatted report per ARCHITECTURE.md lines 293-361
   - Add `--json` flag for machine-readable output
   - Add `--fix` flag for auto-fixes where possible

5. [ ] Add tests for each check type

**Verify:**
```bash
./rr status
./rr status --json | jq .
./rr doctor
./rr doctor --json | jq .
```

---

### Task 4: Host Tags & Probe Timeout Config

**Context:**
- Modify: `internal/config/types.go`, `internal/config/validate.go`
- Modify: `internal/host/selector.go`
- Modify: `internal/cli/run.go`, `internal/cli/exec_cmd.go`, `internal/cli/sync.go`

**Steps:**

1. [ ] Update `internal/config/types.go`:
   - Ensure `Host.Tags []string` exists
   - Add `ProbeTimeout time.Duration` to config root (default 2s)

2. [ ] Update `internal/host/selector.go`:
   - Add `SelectByTag(hosts map[string]Host, tag string) (*Connection, error)`
   - Filter hosts to those containing the tag before selection
   - Error if no hosts match tag

3. [ ] Update CLI commands:
   - Add `--tag` flag to run/exec/sync commands
   - Add `--probe-timeout` flag to override config
   - Pass to selector

4. [ ] Add tests for tag filtering

**Verify:**
```bash
cat > .rr.yaml << 'EOF'
version: 1
hosts:
  cpu-box:
    ssh: [localhost]
    dir: /tmp/cpu
    tags: [fast, cpu]
  gpu-box:
    ssh: [gpu.local]
    dir: /tmp/gpu
    tags: [gpu, cuda]
EOF

./rr run --tag=cpu "echo on cpu box"
./rr run --probe-timeout=5s "echo with longer timeout"
```

---

### Task 5: Connection Output Improvements

**Context:**
- Modify: `internal/ui/phase.go`
- Create: `internal/ui/connection.go`
- Modify: `internal/output/state.go`
- Reference: `../../ARCHITECTURE.md` lines 196-206 for fallback output

**Steps:**

1. [ ] Create `internal/ui/connection.go`:
   - Connection progress display component
   - Show each alias attempt with result:
     ```
     ◐ Connecting...
       ○ mini-local                                         timeout (2s)
       ● mini (tailscale)                                        0.3s
     ● Connected to mini via mini (tailscale)                    2.3s
     ```
   - Animate spinner during probe
   - Indent sub-items under main phase

2. [ ] Update `internal/output/state.go`:
   - Add connection phase details: `AddConnectionAttempt(alias, result)`
   - Track which alias connected
   - Emit sub-phase events for UI

3. [ ] Ensure output is clean in quiet mode (`--quiet`):
   - Only show final result, not individual attempts
   - Errors still shown

4. [ ] Add visual tests (compare output to expected strings)

**Verify:** Visual inspection during `./rr run "echo test"` with first host unreachable

---

## Verification

After all tasks complete:

```bash
# Test host selection with multiple aliases
cat > .rr.yaml << 'EOF'
version: 1
hosts:
  test:
    ssh:
      - nonexistent.local    # Will fail
      - localhost            # Will succeed
    dir: /tmp/rr-test
probe_timeout: 2s
EOF

./rr status    # Should show both aliases, localhost connected
./rr run "hostname"  # Should fallback to localhost with visible output

# Test doctor
./rr doctor
./rr doctor --json | jq .

# Test local fallback
cat > .rr.yaml << 'EOF'
version: 1
hosts:
  test:
    ssh: [nonexistent.local]
    dir: /tmp/rr-test
local_fallback: true
EOF

./rr run "echo local"  # Should run locally with warning

# Test tags
cat > .rr.yaml << 'EOF'
version: 1
hosts:
  fast:
    ssh: [localhost]
    dir: /tmp/fast
    tags: [cpu]
  slow:
    ssh: [slow.local]
    dir: /tmp/slow
    tags: [gpu]
EOF

./rr run --tag=cpu "echo on fast host"
```
