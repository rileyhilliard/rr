---
paths:
  - "**/*"
---

# Verification

Before claiming work is complete, load the `ce:verification-before-completion` skill.

## Always Verify

```bash
rr verify         # lint + unit tests
rr verify-all     # lint + unit + integration tests
```

## Before PRs

Run the E2E validation for CLI changes:

```bash
./scripts/e2e-test.sh           # Full test suite
./scripts/e2e-test.sh --quick   # Fast mode
```
