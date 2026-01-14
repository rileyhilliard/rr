package cli

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// Machine mode flag - when true, outputs JSON and suppresses human-friendly decorations
var machineMode bool

// MachineMode returns true if machine-readable output is enabled
func MachineMode() bool {
	return machineMode
}

// JSONEnvelope wraps command output in a consistent structure for machine parsing.
// All --json output should use this envelope.
type JSONEnvelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *JSONError  `json:"error,omitempty"`
}

// JSONError provides structured error information for machine parsing.
type JSONError struct {
	Code       string      `json:"code"`
	Message    string      `json:"message"`
	Suggestion string      `json:"suggestion,omitempty"`
	Details    interface{} `json:"details,omitempty"`
}

// Error codes for machine-readable output.
// These map to specific actions an LLM/automation can take.
const (
	ErrCodeConfigNotFound    = "CONFIG_NOT_FOUND"
	ErrCodeConfigInvalid     = "CONFIG_INVALID"
	ErrCodeHostNotFound      = "HOST_NOT_FOUND"
	ErrCodeSSHTimeout        = "SSH_TIMEOUT"
	ErrCodeSSHAuthFailed     = "SSH_AUTH_FAILED"
	ErrCodeSSHHostKey        = "SSH_HOST_KEY"
	ErrCodeSSHConnectionFail = "SSH_CONNECTION_FAILED"
	ErrCodeRsyncFailed       = "RSYNC_FAILED"
	ErrCodeLockHeld          = "LOCK_HELD"
	ErrCodeCommandFailed     = "COMMAND_FAILED"
	ErrCodeDependencyMissing = "DEPENDENCY_MISSING"
	ErrCodeUnknown           = "UNKNOWN"
)

// WriteJSONSuccess writes a successful response with data to the writer.
func WriteJSONSuccess(w io.Writer, data interface{}) error {
	env := JSONEnvelope{
		Success: true,
		Data:    data,
	}
	return writeJSONEnvelope(w, env)
}

// WriteJSONError writes an error response to the writer.
func WriteJSONError(w io.Writer, code, message, suggestion string, details interface{}) error {
	env := JSONEnvelope{
		Success: false,
		Error: &JSONError{
			Code:       code,
			Message:    message,
			Suggestion: suggestion,
			Details:    details,
		},
	}
	return writeJSONEnvelope(w, env)
}

// WriteJSONFromError converts a Go error to a JSON error response.
func WriteJSONFromError(w io.Writer, err error) error {
	jsonErr := ErrorToJSON(err)
	env := JSONEnvelope{
		Success: false,
		Error:   jsonErr,
	}
	return writeJSONEnvelope(w, env)
}

// writeJSONEnvelope writes the envelope with consistent formatting.
func writeJSONEnvelope(w io.Writer, env JSONEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// ErrorToJSON converts a Go error to a JSONError with appropriate code mapping.
func ErrorToJSON(err error) *JSONError {
	if err == nil {
		return nil
	}

	// Check if it's our structured error type
	if rrErr, ok := err.(*errors.Error); ok {
		return &JSONError{
			Code:       mapErrorCode(rrErr.Code, rrErr.Message),
			Message:    rrErr.Message,
			Suggestion: rrErr.Suggestion,
		}
	}

	// Check if it's a probe error (SSH-related)
	if probeErr, ok := err.(*host.ProbeError); ok {
		return probeErrorToJSON(probeErr)
	}

	// Generic error
	return &JSONError{
		Code:    ErrCodeUnknown,
		Message: err.Error(),
	}
}

// mapErrorCode maps internal error codes to machine-readable codes.
func mapErrorCode(internalCode, message string) string {
	// First check internal code
	switch internalCode {
	case errors.ErrConfig:
		// Distinguish between not found and invalid
		msgLower := strings.ToLower(message)
		if strings.Contains(msgLower, "not found") || strings.Contains(msgLower, "couldn't find") {
			return ErrCodeConfigNotFound
		}
		return ErrCodeConfigInvalid
	case errors.ErrSSH:
		return ErrCodeSSHConnectionFail
	case errors.ErrSync:
		return ErrCodeRsyncFailed
	case errors.ErrLock:
		return ErrCodeLockHeld
	case errors.ErrExec:
		return ErrCodeCommandFailed
	}

	return ErrCodeUnknown
}

// probeErrorToJSON converts a probe error to JSON with specific SSH error codes.
func probeErrorToJSON(probeErr *host.ProbeError) *JSONError {
	var code string
	var suggestion string

	switch probeErr.Reason {
	case host.ProbeFailTimeout:
		code = ErrCodeSSHTimeout
		suggestion = "Check if host is reachable: ping the hostname"
	case host.ProbeFailAuth:
		code = ErrCodeSSHAuthFailed
		suggestion = "Deploy SSH key: ssh-copy-id <hostname>"
	case host.ProbeFailHostKey:
		code = ErrCodeSSHHostKey
		suggestion = "Accept host key: ssh -o StrictHostKeyChecking=accept-new <hostname> exit"
	case host.ProbeFailDNS:
		code = ErrCodeSSHConnectionFail
		suggestion = "Check hostname spelling and SSH config"
	case host.ProbeFailRefused, host.ProbeFailConnReset, host.ProbeFailUnreachable:
		code = ErrCodeSSHConnectionFail
		suggestion = "Check if SSH server is running and host is reachable"
	default:
		code = ErrCodeSSHConnectionFail
	}

	return &JSONError{
		Code:       code,
		Message:    probeErr.Error(),
		Suggestion: suggestion,
		Details: map[string]interface{}{
			"reason": probeErr.Reason.String(),
			"alias":  probeErr.SSHAlias,
		},
	}
}
