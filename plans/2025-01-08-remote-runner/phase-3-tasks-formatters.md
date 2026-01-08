# Phase 3: Tasks and Formatters

> **Status:** NOT_STARTED

## Goal

Add power-user features: named task definitions, multi-step tasks, and output formatters that parse pytest/jest/go test output to extract and summarize failures.

## Success Criteria

- [ ] `rr test` runs a named task from config
- [ ] Multi-step tasks run in sequence with `on_fail: continue` support
- [ ] Pytest formatter extracts failures with file:line and message
- [ ] Jest formatter works similarly
- [ ] Go test formatter works similarly
- [ ] Auto-detection selects correct formatter
- [ ] Shell completions include task names

## Phase Exit Criteria

- [ ] All 6 tasks completed
- [ ] `make test` passes with >70% coverage on new code
- [ ] Sample pytest output produces correct summary
- [ ] Tab completion works for task names

## Context Loading

```bash
# Read before starting:
read internal/config/types.go
read internal/cli/run.go
read internal/output/formatter.go

# Reference for pytest parsing:
read ../../proof-of-concept.sh           # Lines 97-131 for failure extraction
read ../../proof-of-concept.sh           # Lines 145-218 for failure display
read ../../ARCHITECTURE.md               # Lines 993-1087 for formatter architecture
```

---

## Execution Order

```
┌─────────────────────────────────────────────────────────────────┐
│ Task 1: Task Definitions & Execution                            │
└─────────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ Task 2: Pytest  │ │ Task 3: Jest    │ │ Task 4: Go Test │
│ Formatter       │ │ Formatter       │ │ Formatter       │
│ (parallel)      │ │ (parallel)      │ │ (parallel)      │
└─────────────────┘ └─────────────────┘ └─────────────────┘
          │                   │                   │
          └───────────────────┼───────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 5: Formatter Summary Display                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 6: Shell Completions for Tasks                             │
└─────────────────────────────────────────────────────────────────┘
```

---

## Tasks

### Task 1: Task Definitions & Execution

**Context:**
- Modify: `internal/config/types.go`, `internal/config/validate.go`
- Create: `internal/config/tasks.go`, `internal/exec/task.go`
- Modify: `internal/cli/root.go`
- Reference: `../../ARCHITECTURE.md` lines 556-597 for task schema

**Steps:**

1. [ ] Update `internal/config/types.go`:
   - Ensure `TaskConfig` has: `Description`, `Run`, `Hosts`, `Env`, `Steps`, `OnFail`
   - `StepConfig`: `Name`, `Run`, `OnFail` (continue/stop, default stop)
   - `OnFail` enum type

2. [ ] Create `internal/config/tasks.go`:
   - `GetTask(cfg *Config, name string) (*TaskConfig, error)`
   - Validate task exists
   - Merge task env with host env (task overrides host)

3. [ ] Update `internal/config/validate.go`:
   - Check reserved task names: `run, exec, sync, init, setup, status, monitor, doctor, help, version, completion, update`
   - Return helpful error: "Task name 'run' conflicts with built-in command. Rename your task."

4. [ ] Create `internal/exec/task.go`:
   - `ExecuteTask(conn *Connection, task *TaskConfig, output *StreamHandler) (int, error)`
   - Handle single-command tasks (just `Run` field)
   - Handle multi-step tasks with `Steps` array
   - Track step results for summary
   - Implement `on_fail: continue` logic
   - Environment variable injection

5. [ ] Update `internal/cli/root.go`:
   - After loading config, dynamically register task commands
   - Each task becomes first-class command: `rr test` not `rr task test`
   - Task command delegates to `run` logic with task config

6. [ ] Create `internal/exec/task_test.go`:
   - Test single command execution
   - Test multi-step with all passing
   - Test multi-step with failure + on_fail: continue
   - Test multi-step with failure + on_fail: stop

**Verify:**
```bash
cat > .rr.yaml << 'EOF'
version: 1
hosts:
  test:
    ssh: [localhost]
    dir: /tmp/rr-test
tasks:
  hello:
    run: echo "Hello, World!"
  ci:
    steps:
      - name: step1
        run: echo "step 1"
      - name: step2
        run: echo "step 2"
        on_fail: continue
      - name: step3
        run: echo "step 3"
EOF

./rr hello
./rr ci
./rr --help  # Should show hello and ci as commands
```

---

### Task 2: Pytest Formatter

**Context:**
- Create: `internal/output/formatters/pytest.go`
- Read: `internal/output/formatter.go` (Formatter interface)
- Reference: `../../proof-of-concept.sh` lines 97-131 for parsing patterns
- Reference: `../../proof-of-concept.sh` lines 145-218 for display patterns

**Steps:**

1. [ ] Create `internal/output/formatters/pytest.go`:
   - Implement `Formatter` interface
   - `Name() string` returns "pytest"
   - `Detect(command string, output []byte) int`:
     - Return 100 if command contains "pytest"
     - Return 80 if output contains "collected X items"
     - Return 0 otherwise

2. [ ] Implement `ProcessLine(line string) (display string, data *LineData)`:
   - Parse `PASSED`, `FAILED`, `SKIPPED`, `ERROR` lines
   - Extract test name from lines like `tests/test_foo.py::test_bar PASSED`
   - Extract file:line from failure output
   - Reference proof-of-concept.sh `extract_failed_tests()` line 99-107

3. [ ] Implement failure detail extraction:
   - Parse "FAILURES" section (between `= FAILURES =` and `= short test summary =`)
   - Extract traceback with file:line
   - Extract assertion error messages
   - Reference proof-of-concept.sh `extract_failure_details()` lines 118-131

4. [ ] Implement `Summary(exitCode int) *Summary`:
   - Return counts: passed, failed, skipped
   - Return list of `Failure` structs with file, line, test name, message

5. [ ] Create `internal/output/formatters/pytest_test.go`:
   - Test with sample pytest output (passing)
   - Test with sample pytest output (failures)
   - Test edge cases (no tests, all skipped)

**Verify:** `go test ./internal/output/formatters/...`

---

### Task 3: Jest Formatter

**Context:**
- Create: `internal/output/formatters/jest.go`
- Read: `internal/output/formatter.go` (Formatter interface)

**Steps:**

1. [ ] Create `internal/output/formatters/jest.go`:
   - Implement `Formatter` interface
   - `Name() string` returns "jest"
   - `Detect(command string, output []byte) int`:
     - Return 100 if command contains "jest" or "vitest"
     - Return 80 if output contains "PASS" or "FAIL" with jest-style formatting
     - Return 0 otherwise

2. [ ] Implement `ProcessLine(line string)`:
   - Parse `PASS src/foo.test.js` and `FAIL src/bar.test.js` lines
   - Parse individual test results: `✓ should do something (5ms)`
   - Parse failure details with stack traces

3. [ ] Implement `Summary(exitCode int) *Summary`:
   - Return counts from Jest's summary line
   - Return failures with file, test name, error message

4. [ ] Create `internal/output/formatters/jest_test.go`:
   - Test with sample jest output

**Verify:** `go test ./internal/output/formatters/...`

---

### Task 4: Go Test Formatter

**Context:**
- Create: `internal/output/formatters/gotest.go`
- Read: `internal/output/formatter.go` (Formatter interface)

**Steps:**

1. [ ] Create `internal/output/formatters/gotest.go`:
   - Implement `Formatter` interface
   - `Name() string` returns "gotest"
   - `Detect(command string, output []byte) int`:
     - Return 100 if command contains "go test"
     - Return 80 if output matches go test patterns
     - Return 0 otherwise

2. [ ] Implement `ProcessLine(line string)`:
   - Parse `ok` and `FAIL` package lines
   - Parse `--- PASS:` and `--- FAIL:` test lines
   - Extract timing from test results
   - Extract file:line from failure output

3. [ ] Implement `Summary(exitCode int) *Summary`:
   - Return counts
   - Return failures with package, test name, error message

4. [ ] Create `internal/output/formatters/gotest_test.go`:
   - Test with sample go test output

**Verify:** `go test ./internal/output/formatters/...`

---

### Task 5: Formatter Summary Display

**Context:**
- Create: `internal/ui/summary.go`
- Modify: `internal/output/stream.go`
- Reference: `../../proof-of-concept.sh` lines 145-218 for `print_suite_failures()`
- Reference: `../../ARCHITECTURE.md` lines 207-238 for failure display

**Steps:**

1. [ ] Create `internal/ui/summary.go`:
   - `RenderSummary(summary *Summary, exitCode int) string`
   - Display failure count with icon: `✗ 2 tests failed`
   - List each failure with clickable path:
     ```
       tests/test_auth.py:42
         test_login_expired
         AssertionError: Expected 401, got 200
     ```
   - Color: red for failures, file paths in cyan
   - Reference proof-of-concept.sh `print_suite_failures()` for exact format

2. [ ] Update `internal/output/stream.go`:
   - After command completes, call formatter's `Summary()`
   - If exit code != 0 and formatter has failures, render summary
   - Separate summary from command output with divider

3. [ ] Add tests for summary rendering

**Verify:**
```bash
# Create a Python file with a failing test
mkdir -p /tmp/rr-test
cat > /tmp/rr-test/test_example.py << 'EOF'
def test_pass():
    assert True

def test_fail():
    assert False, "This should fail"
EOF

./rr run "cd /tmp/rr-test && python -m pytest test_example.py -v"
# Should show formatted failure summary
```

---

### Task 6: Shell Completions for Tasks

**Context:**
- Modify: `internal/cli/root.go`, `internal/cli/completion.go`

**Steps:**

1. [ ] Update `internal/cli/completion.go`:
   - Add completion function that reads config
   - Return task names for completion
   - `ValidArgsFunction` for dynamic completion

2. [ ] Update `internal/cli/root.go`:
   - Register completion for task commands
   - Task names should appear in tab completion

3. [ ] Test completion generation:
   ```bash
   ./rr completion bash > /tmp/rr.bash
   source /tmp/rr.bash
   ./rr <TAB>  # Should show task names
   ```

4. [ ] Add tests for completion output

**Verify:**
```bash
./rr completion bash > /tmp/rr.bash
source /tmp/rr.bash
./rr <TAB>  # Should show: ci hello help run exec sync ...
```

---

## Verification

After all tasks complete:

```bash
# Test task execution
cat > .rr.yaml << 'EOF'
version: 1
hosts:
  test:
    ssh: [localhost]
    dir: /tmp/rr-test
tasks:
  test:
    description: Run all tests
    run: pytest -v
  lint:
    run: echo "linting..."
  ci:
    steps:
      - name: lint
        run: echo "linting..."
      - name: test
        run: echo "testing..."
        on_fail: continue
      - name: build
        run: echo "building..."
EOF

./rr test      # Run single task
./rr ci        # Run multi-step task
./rr lint      # Another task
./rr --help    # Should show tasks in help

# Test formatters with real pytest output
mkdir -p /tmp/rr-test
cat > /tmp/rr-test/test_example.py << 'EOF'
def test_pass():
    assert True

def test_fail():
    assert 1 == 2, "Math is broken"
EOF

./rr run "cd /tmp/rr-test && python -m pytest -v"
# Should show formatted failure summary

# Test completions
./rr completion bash > /tmp/rr.bash
source /tmp/rr.bash
./rr <TAB>
```
