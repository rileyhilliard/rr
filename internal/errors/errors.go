package errors

import (
	"errors"
	"fmt"
	"strings"
)

// Error codes for categorizing errors
const (
	ErrConfig = "CONFIG"
	ErrSSH    = "SSH"
	ErrSync   = "SYNC"
	ErrLock   = "LOCK"
	ErrExec   = "EXEC"
)

// Error represents a structured error with code, message, suggestion, and optional cause.
// Follows the ARCHITECTURE.md error message design:
//
//	✗ <What failed>
//
//	  <Why it failed - technical details>
//
//	  <How to fix it - actionable steps>
type Error struct {
	Code       string
	Message    string
	Suggestion string
	Cause      error
}

// New creates a new structured error with the given code, message, and suggestion.
func New(code, message, suggestion string) *Error {
	return &Error{
		Code:       code,
		Message:    message,
		Suggestion: suggestion,
	}
}

// Wrap wraps an existing error with a message, defaulting to ErrSSH code.
func Wrap(err error, message string) *Error {
	return &Error{
		Code:    ErrSSH,
		Message: message,
		Cause:   err,
	}
}

// WrapWithCode wraps an existing error with a specific code, message, and suggestion.
func WrapWithCode(err error, code, message, suggestion string) *Error {
	return &Error{
		Code:       code,
		Message:    message,
		Suggestion: suggestion,
		Cause:      err,
	}
}

// NewNotImplemented creates an error for commands that aren't implemented yet.
func NewNotImplemented(command string) *Error {
	return &Error{
		Code:       ErrExec,
		Message:    fmt.Sprintf("'%s' command not implemented yet", command),
		Suggestion: "This feature is coming soon",
	}
}

// Error implements the error interface with formatted output following ARCHITECTURE.md design.
func (e *Error) Error() string {
	var b strings.Builder

	// First line: failure symbol + main message
	b.WriteString(fmt.Sprintf("✗ %s\n", e.Message))

	// Include cause if present (why it failed)
	if e.Cause != nil {
		b.WriteString(fmt.Sprintf("\n  %s\n", e.Cause.Error()))
	}

	// Include suggestion if present (how to fix)
	if e.Suggestion != "" {
		b.WriteString(fmt.Sprintf("\n  %s\n", e.Suggestion))
	}

	return b.String()
}

// Unwrap returns the underlying cause for use with errors.Is/errors.As.
func (e *Error) Unwrap() error {
	return e.Cause
}

// IsCode checks if an error is a structured Error with the given code.
func IsCode(err error, code string) bool {
	if err == nil {
		return false
	}
	var rrErr *Error
	if errors.As(err, &rrErr) {
		return rrErr.Code == code
	}
	return false
}
