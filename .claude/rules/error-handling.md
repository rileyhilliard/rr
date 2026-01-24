---
paths:
  - "**/*.go"
---

# Error Handling

When designing error handling, load the `ce:handling-errors` skill.

## Project Pattern

Always use structured errors from `internal/errors`:

```go
// Good: includes code, message, and actionable suggestion
return errors.New(errors.ErrConfig, "config file not found", "Run 'rr init' to create one")

// Good: wrap with context
return errors.WrapWithCode(err, errors.ErrSSH, "connection failed", "Check if host is reachable")
```

## Key Principles

- Never swallow errors silently
- Include actionable suggestions when possible
- Preserve error context when wrapping
- Use appropriate error codes from the `errors` package
