---
paths:
  - "**/*_test.go"
  - "**/test_*.go"
  - "internal/**/*_test.go"
---

# Testing Rules

When writing tests, load the `ce:writing-tests` skill for general patterns.

## Go Testing Quick Reference

```bash
rr test                 # Run all unit tests
rr test-integration     # Run integration tests
rr test-all             # Run unit + integration in parallel
rr test-v               # Tests with verbose output
rr test-race            # Tests with race detector

# Single test
rr run "go test ./internal/lock/... -run TestLockAcquisition -v"
```

## Project Conventions

- Use `testify/assert` and `testify/require`
- Table-driven tests preferred
- Integration tests use env vars: `RR_TEST_SSH_HOST`, `RR_TEST_SSH_KEY`, `RR_TEST_SSH_USER`
- Use `./scripts/ci-ssh-server.sh` for Docker-based SSH testing

## Flaky Tests

When fixing flaky tests, load the `ce:fixing-flaky-tests` skill.

| Symptom | Likely Cause |
|---------|--------------|
| Passes alone, fails in suite | Shared state |
| Random timing failures | Race condition |
| Works locally, fails in CI | Environment differences |
