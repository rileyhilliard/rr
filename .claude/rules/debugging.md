---
paths:
  - "**/*"
---

# Debugging

When investigating bugs or unexpected behavior, load the `ce:systematic-debugging` skill.

## Four-Phase Approach

1. **Reproduce** - Get a reliable repro case
2. **Trace** - Follow the code path
3. **Identify** - Find the root cause (not symptoms)
4. **Verify** - Confirm the fix doesn't break other things

## Project-Specific Tools

```bash
# Run with verbose output
rr test-v

# Run with race detector
rr test-race

# Doctor command for diagnostics
rr doctor
```
