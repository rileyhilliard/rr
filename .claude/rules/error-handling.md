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
return errors.New(errors.ErrConfigNotFound, "config file not found", "Run 'rr init' to create one")

// Good: wrap with context
return errors.WrapWithCode(err, errors.ErrSSH, "connection failed", "Check if host is reachable")
```

The code is the contract: `mapErrorCode` (`internal/cli/json.go`) turns it into the public code agents branch on (`ErrConfigNotFound` -> `CONFIG_NOT_FOUND`, `ErrHostNotFound` -> `HOST_NOT_FOUND`, `ErrDependency` -> `DEPENDENCY_MISSING`, and so on) by table lookup, never by reading the message. Pick the code that says what failed where the error is created.

## Key Principles

- Never swallow errors silently
- Include actionable suggestions when possible
- Preserve error context when wrapping
- Use appropriate error codes from the `errors` package
