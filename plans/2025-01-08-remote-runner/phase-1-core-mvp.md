# Phase 1: Core MVP

> **Status:** NOT_STARTED

## Goal

Deliver the minimum viable tool that beats a shell script. Single host support, file sync via rsync, command execution via SSH, atomic locking, and streaming output with phase indicators.

## Success Criteria

- [ ] `rr sync` syncs files to configured remote
- [ ] `rr run "pytest"` syncs then runs command
- [ ] `rr exec "pytest"` runs command without sync
- [ ] Lock prevents concurrent runs on same remote
- [ ] `rr init` creates config interactively
- [ ] `rr setup <host>` configures SSH keys
- [ ] Output shows clear phase indicators (connecting, syncing, running)
- [ ] Exit code propagates correctly
- [ ] Config version is validated

## Phase Exit Criteria

- [ ] All 7 tasks completed
- [ ] `make test` passes with >70% coverage on new code
- [ ] Integration test passes against localhost SSH
- [ ] Manual testing: `rr run "echo hello"` works end-to-end

## Context Loading

```bash
# Read these before starting any task:
read internal/cli/root.go
read internal/errors/errors.go
read internal/ui/colors.go
read internal/ui/symbols.go

# Reference for patterns:
read ../../proof-of-concept.sh           # Full script for rsync/SSH patterns
read ../../ARCHITECTURE.md               # Lines 445-630 for config schema
```

---

## Execution Order

Tasks are organized by subsystem. Execute in order; parallelization noted where possible.

```
┌─────────────────────────────────────────────────────────────────┐
│ Task 1: Config Loading & Validation                             │
│   (foundation for everything else)                              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 2: SSH Client & Host Selection                             │
│   (required for sync, exec, lock)                               │
└─────────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ Task 3: Sync    │ │ Task 4: Lock    │ │ Task 5: Output  │
│ (parallel)      │ │ (parallel)      │ │ (parallel)      │
└─────────────────┘ └─────────────────┘ └─────────────────┘
          │                   │                   │
          └───────────────────┼───────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 6: CLI Commands (run, exec, sync, init, setup)             │
│   (integrates all subsystems)                                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 7: Integration Testing                                     │
└─────────────────────────────────────────────────────────────────┘
```

---

## Tasks

### Task 1: Config Loading & Validation

**Context:**
- Create: `internal/config/types.go`, `internal/config/loader.go`, `internal/config/expand.go`, `internal/config/validate.go`
- Read: `internal/errors/errors.go` (use structured errors)
- Reference: `../../ARCHITECTURE.md` lines 445-630 for schema

**Steps:**

1. [ ] Create `internal/config/types.go`:
   ```go
   type Config struct {
       Version       int                  `yaml:"version"`
       Hosts         map[string]Host      `yaml:"hosts"`
       Default       string               `yaml:"default"`
       LocalFallback bool                 `yaml:"local_fallback"`
       Sync          SyncConfig           `yaml:"sync"`
       Lock          LockConfig           `yaml:"lock"`
       Tasks         map[string]TaskConfig `yaml:"tasks"`
       Output        OutputConfig         `yaml:"output"`
   }
   ```
   - Define `Host` struct: `SSH []string`, `Dir string`, `Tags []string`, `Env map[string]string`
   - Define `SyncConfig`: `Exclude []string`, `Preserve []string`, `Flags []string`
   - Define `LockConfig`: `Enabled bool`, `Timeout time.Duration`, `Stale time.Duration`, `Dir string`
   - Define `TaskConfig`, `OutputConfig` with sensible defaults
   - Add `const CurrentConfigVersion = 1`

2. [ ] Create `internal/config/loader.go`:
   - `Load(path string) (*Config, error)` - load from specific path
   - `Find() (string, error)` - find config: --config flag -> .rr.yaml -> parent dirs -> ~/.config/rr/config.yaml
   - `LoadOrDefault() (*Config, error)` - load config or return empty for `rr init`
   - Merge with defaults using Viper

3. [ ] Create `internal/config/expand.go`:
   - `Expand(s string) string` - expand `${PROJECT}`, `${USER}`, `${HOME}`
   - Get PROJECT from: git repo name, or directory name
   - Apply expansion to Host.Dir

4. [ ] Create `internal/config/validate.go`:
   - `Validate(cfg *Config) error` - validate config
   - Check version == CurrentConfigVersion (error if higher, warn if missing)
   - Check at least one host defined (except for `rr init`)
   - Check reserved task names: `run, exec, sync, init, setup, status, monitor, doctor, help, version, completion, update`
   - Return structured errors with line numbers where possible

5. [ ] Create `internal/config/config_test.go`:
   - Test loading valid config
   - Test validation errors
   - Test variable expansion
   - Test version checking

**Verify:** `go test ./internal/config/...`

---

### Task 2: SSH Client & Host Selection

**Context:**
- Create: `pkg/sshutil/client.go`, `pkg/sshutil/exec.go`, `internal/host/probe.go`, `internal/host/selector.go`
- Read: `internal/config/types.go` (Host struct)
- Read: `internal/errors/errors.go` (use ErrSSH)
- Reference: `../../proof-of-concept.sh` lines 70-83 for SSH probe pattern

**Steps:**

1. [ ] Create `pkg/sshutil/client.go`:
   - `Client` struct wrapping `*ssh.Client`
   - `Dial(host string, timeout time.Duration) (*Client, error)`
   - Parse `~/.ssh/config` using `kevinburke/ssh_config` for host settings
   - Support SSH agent authentication (check `SSH_AUTH_SOCK`)
   - Support key file authentication (`~/.ssh/id_ed25519`, `~/.ssh/id_rsa`)
   - `Close() error`

2. [ ] Create `pkg/sshutil/exec.go`:
   - `(c *Client) Exec(cmd string) (stdout, stderr []byte, exitCode int, err error)`
   - `(c *Client) ExecStream(cmd string, stdout, stderr io.Writer) (exitCode int, err error)`
   - Handle PTY allocation for interactive commands
   - Propagate exit codes correctly (reference proof-of-concept.sh line 421 for PIPESTATUS pattern)

3. [ ] Create `internal/host/probe.go`:
   - `Probe(sshAlias string, timeout time.Duration) (latency time.Duration, err error)`
   - Quick TCP connection test + SSH handshake
   - Return structured error with reason (timeout, refused, auth failed)

4. [ ] Create `internal/host/selector.go`:
   - `Select(hosts map[string]Host, preferred string) (*Connection, error)`
   - `Connection` struct: `Name string`, `Alias string`, `Client *sshutil.Client`, `Host Host`
   - For Phase 1: try first SSH alias only, no fallback chain (that's Phase 2)
   - Cache connection for reuse within session

5. [ ] Create tests: `pkg/sshutil/client_test.go`, `internal/host/selector_test.go`
   - Use test helpers from `tests/integration/setup_test.go`
   - Skip if `RR_TEST_SKIP_SSH=1`

**Verify:** `go test ./pkg/sshutil/... ./internal/host/...`

---

### Task 3: File Sync via rsync

**Context:**
- Create: `internal/sync/sync.go`, `internal/sync/rsync.go`, `internal/sync/progress.go`
- Read: `internal/host/selector.go` (Connection struct)
- Read: `internal/config/types.go` (SyncConfig)
- Reference: `../../proof-of-concept.sh` lines 346-377 for rsync command pattern

**Steps:**

1. [ ] Create `internal/sync/rsync.go`:
   - `FindRsync() (string, error)` - locate rsync binary
   - `Version() (string, error)` - get rsync version
   - `CheckRemote(conn *Connection) error` - verify rsync exists on remote

2. [ ] Create `internal/sync/sync.go`:
   - `Sync(conn *Connection, localDir string, config SyncConfig, progress io.Writer) error`
   - Build rsync command with:
     - `-az --delete --force` base flags
     - `--filter='P <pattern>'` for preserve patterns
     - `--exclude <pattern>` for exclude patterns
     - Custom flags from config
   - Reference proof-of-concept.sh lines 352-374 for exact rsync invocation
   - Shell out to rsync (not reimplementing)
   - Stream progress to output handler

3. [ ] Create `internal/sync/progress.go`:
   - Parse rsync `--info=progress2` output
   - Extract: file count, bytes transferred, percentage
   - `ParseProgress(line string) *Progress`

4. [ ] Create `internal/sync/sync_test.go`:
   - Test rsync command building
   - Test progress parsing
   - Integration test with temp directories

**Verify:** `go test ./internal/sync/...`

---

### Task 4: Atomic Locking

**Context:**
- Create: `internal/lock/lock.go`, `internal/lock/info.go`
- Read: `internal/host/selector.go` (Connection struct)
- Read: `internal/config/types.go` (LockConfig)
- Reference: `../../proof-of-concept.sh` lines 296-344 for lock implementation

**Steps:**

1. [ ] Create `internal/lock/info.go`:
   - `LockInfo` struct: `User string`, `Hostname string`, `Started time.Time`, `PID int`
   - `(i *LockInfo) Age() time.Duration`
   - `(i *LockInfo) Marshal() ([]byte, error)` - JSON serialization
   - `ParseLockInfo(data []byte) (*LockInfo, error)`

2. [ ] Create `internal/lock/lock.go`:
   - `Lock` struct: `Dir string`, `Info LockInfo`, `conn *Connection`
   - `Acquire(conn *Connection, config LockConfig, projectHash string) (*Lock, error)`:
     - Lock dir: `config.Dir` or `/tmp` + `/rr-<projectHash>.lock/`
     - Use `mkdir` as atomic primitive (reference proof-of-concept.sh line 315)
     - Write `info.json` with holder details
     - Check for stale locks (age > config.Stale)
     - Wait with polling if locked (up to config.Timeout)
     - Reference proof-of-concept.sh lines 307-330 for wait loop
   - `(l *Lock) Release() error` - remove lock directory
   - `ForceRelease(conn *Connection, lockDir string) error` - force remove stale lock

3. [ ] Create `internal/lock/lock_test.go`:
   - Test acquire/release
   - Test stale detection
   - Test concurrent acquisition (should block)

**Verify:** `go test ./internal/lock/...`

---

### Task 5: Output Streaming & Phase Indicators

**Context:**
- Create: `internal/output/stream.go`, `internal/output/state.go`, `internal/output/formatter.go`
- Create: `internal/ui/spinner.go`, `internal/ui/phase.go`
- Read: `internal/ui/colors.go`, `internal/ui/symbols.go` (from Phase 0)
- Reference: `../../ARCHITECTURE.md` lines 148-260 for output design
- Reference: `../../proof-of-concept.sh` lines 139-142 for divider style

**Steps:**

1. [ ] Create `internal/ui/spinner.go`:
   - Spinner component with Bubble Tea
   - States: pending (○), in-progress (◐ animated), success (●), failed (✗), skipped (⊘)
   - `NewSpinner(label string) *Spinner`
   - `(s *Spinner) Start()`, `Stop()`, `Success()`, `Fail()`

2. [ ] Create `internal/ui/phase.go`:
   - Phase display component
   - Show timing for each phase: `● Connected (0.3s)`
   - Color coding: blue (progress), green (success), red (fail), gray (timing)
   - Divider line between phases and command output

3. [ ] Create `internal/output/stream.go`:
   - `StreamHandler` that multiplexes stdout/stderr
   - Line buffering
   - ANSI code passthrough
   - `NewStreamHandler(stdout, stderr io.Writer) *StreamHandler`

4. [ ] Create `internal/output/state.go`:
   - Track current phase: Connecting, Syncing, Locking, Running, Done
   - Emit phase transitions with timing
   - `PhaseTracker` struct with `Start(phase)`, `Complete()`, `Fail(err)`

5. [ ] Create `internal/output/formatter.go`:
   - `Formatter` interface: `Name()`, `ProcessLine(line string)`, `Summary(exitCode int)`
   - `GenericFormatter` - simple passthrough with error highlighting
   - Detect lines starting with "error:", "Error:", "ERROR" and color red
   - More formatters in Phase 3

6. [ ] Create tests for all components

**Verify:** `go test ./internal/output/... ./internal/ui/...`

---

### Task 6: CLI Commands (run, exec, sync, init, setup)

**Context:**
- Create: `internal/cli/run.go`, `internal/cli/exec_cmd.go`, `internal/cli/sync.go`, `internal/cli/init.go`, `internal/cli/setup.go`
- Read: All internal packages created in Tasks 1-5
- Reference: `../../ARCHITECTURE.md` lines 86-127 for command structure

**Steps:**

1. [ ] Create `internal/cli/run.go`:
   - Load config
   - Select host (first available)
   - Show "Connecting..." phase with spinner
   - Sync files, show "Syncing..." phase
   - Acquire lock, show "Acquiring lock..." phase
   - Execute command, stream output through formatter
   - Release lock
   - Show summary with total timing
   - Propagate exit code

2. [ ] Create `internal/cli/exec_cmd.go` (named to avoid conflict with `exec` package):
   - Same as run but skip sync phase
   - Share common logic with run.go

3. [ ] Create `internal/cli/sync.go`:
   - Just sync, no command execution
   - Show file count and timing

4. [ ] Create `internal/cli/init.go`:
   - Use Huh for interactive prompts
   - Ask for: SSH host/alias, fallback host (optional), remote directory
   - Test connection before saving
   - Write minimal `.rr.yaml` with version: 1
   - Handle existing config (ask to overwrite)

5. [ ] Create `internal/cli/setup.go`:
   - Create `internal/setup/keys.go`: `FindLocalKeys()`, `GenerateKey(path)`
   - Create `internal/setup/copy.go`: `CopyKey(host, keyPath)` using ssh-copy-id
   - Take host name as argument
   - Check for local SSH keys, offer to generate if missing
   - Test connection to SSH alias
   - If auth fails, offer to copy key
   - Show success summary

6. [ ] Wire all commands to root.go

**Verify:**
```bash
go build -o rr ./cmd/rr
./rr init  # Interactive test
./rr sync
./rr run "echo hello"
./rr exec "pwd"
./rr setup --help
```

---

### Task 7: Integration Testing

**Context:**
- Create: `tests/integration/mvp_test.go`
- Read: `tests/integration/setup_test.go` (test helpers)
- All internal packages

**Steps:**

1. [ ] Create `tests/integration/mvp_test.go`:
   - Test config loading from temp file
   - Test SSH connection to test host
   - Test file sync to temp directory
   - Test lock acquire/release
   - Test full `rr run` workflow
   - Test exit code propagation

2. [ ] Add test for lock contention:
   - Start long-running command
   - Try to acquire lock in parallel
   - Verify second caller waits

3. [ ] Add test for output formatting:
   - Capture stdout during run
   - Verify phase indicators appear
   - Verify timing is shown

4. [ ] Update Makefile `test-integration` target if needed

5. [ ] Document any manual testing steps in `tests/integration/README.md`

**Verify:**
```bash
make test
RR_TEST_SSH_HOST=localhost make test-integration
```

---

## Verification

After all tasks complete:

```bash
# Create test config
cat > .rr.yaml << 'EOF'
version: 1
hosts:
  test:
    ssh: [localhost]
    dir: /tmp/rr-test
sync:
  exclude:
    - .git/
    - __pycache__/
lock:
  timeout: 5m
  stale: 10m
EOF

# Test commands
./rr status      # Should show host info (stub OK for Phase 1)
./rr sync        # Should sync files
./rr run "ls -la"  # Should sync then run
./rr exec "pwd"    # Should just run

# Test locking (in two terminals)
./rr run "sleep 30" &
./rr run "echo test"  # Should wait for lock

# Test init
rm .rr.yaml
./rr init  # Should create new config
```
