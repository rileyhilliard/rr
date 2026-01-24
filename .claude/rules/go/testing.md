---
paths:
  - "**/*_test.go"
---

# Go Testing Patterns

Extends the universal testing rules with Go-specific patterns.

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

Integration tests use environment variables:

```go
func TestSSHConnection(t *testing.T) {
    host := os.Getenv("RR_TEST_SSH_HOST")
    if host == "" {
        t.Skip("RR_TEST_SSH_HOST not set")
    }
    // ...
}
```

Required env vars for SSH tests:
- `RR_TEST_SSH_HOST`
- `RR_TEST_SSH_KEY`
- `RR_TEST_SSH_USER`
