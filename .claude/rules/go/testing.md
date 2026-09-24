---
paths:
  - "**/*_test.go"
---

# Go Testing Patterns

When writing tests, load the `ce:writing-tests` skill for general patterns.

## Running Tests

```bash
rr test                 # Run all unit tests
rr test-integration     # Run integration tests
rr test-all             # Run unit + integration in parallel
rr test-v               # Tests with verbose output
rr test-race            # Tests with race detector

# Single test
rr run "go test ./internal/lock/... -run TestLockAcquisition -v"
```

## Table-Driven Tests

```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {
            name:     "valid input",
            input:    "foo",
            expected: "FOO",
        },
        {
            name:    "empty input",
            input:   "",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Transform(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

## Testify Assertions

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// Use require for critical checks (stops test on failure)
require.NoError(t, err)
require.NotNil(t, result)

// Use assert for additional checks (continues on failure)
assert.Equal(t, expected, actual)
assert.Contains(t, str, "substring")
```

## Integration Tests

SSH tests run against the Docker server from `./scripts/ci-ssh-server.sh` and skip when its env vars aren't set. Setup and the env vars are in `tests/integration/README.md`. Use `RequireSSH(t)` or `GetSSHConnection(t)` from `tests/integration/ssh_helper_test.go` rather than reading the env yourself.

CI fails the integration job if any test skips, so a new `t.Skip` there needs a reason that can't happen on the CI runner.

## Gotchas

- **Pass `SkipLock: true` to `cli.Run` / `cli.RunTask` against the shared Docker host unless the test is about locking.** The lock is per host, and its holder is the `go test` process, which is still alive. If a run can't release the lock (for example, the test drops the connection), the holder never looks dead, and the next test waits out `lock.stale` (90s) for it.
- **To check what rr prints, run the built binary as a subprocess or assert on an injected writer.** Swapping `os.Stderr` in-process misses any writer that grabbed the fd before the swap, so a test can pass against code that still leaks. `buildRRBinary` and `runRRBinary` in `tests/integration/structured_output_test.go` build rr once per package run and capture both streams.
- **A red check has to fail on the assertion under test, at the call site under test.** When you revert a fix to prove a test catches it, read the failure message, not just the exit code. If the fix has several call sites (`lostConnectionAsResult` has three: two in `task.go`, one in `run.go`), revert the one the test exercises, or the "failure" means nothing.

## Flaky Tests

When fixing flaky tests, load the `ce:fixing-flaky-tests` skill.

| Symptom | Likely Cause |
|---------|--------------|
| Passes alone, fails in suite | Shared state |
| Random timing failures | Race condition |
| Works locally, fails in CI | Environment differences |
