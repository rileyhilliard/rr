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

## After Opening or Pushing to a PR

Wait for CI and CodeRabbit with the script instead of a hand-written `gh` loop:

```bash
./scripts/pr-wait.sh <pr-number>
```

It runs `gh pr checks --watch`, then prints unresolved CodeRabbit threads and any "review skipped" notice. It exits non-zero if a check failed, or right away if the PR has no CI checks (its base isn't `main`).
