package cli

import (
	"encoding/json"
	stderrors "errors"
	"io"
	"os"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// Machine mode flag - kept for backward compatibility (now a no-op since structured is default)
var machineMode bool

// prettyMode flag - when true, use human-readable output with spinners and colors
var prettyMode bool

// suppressPhases - when true, intermediate phase events are suppressed (result events still emitted)
var suppressPhases bool

// MachineMode returns true if machine-readable output is enabled.
// With structured output as default, this is always true unless --pretty is set.
func MachineMode() bool {
	return !prettyMode
}

// PrettyMode returns true if pretty (human-readable) output is enabled
func PrettyMode() bool {
	return prettyMode
}

// PhaseEvent represents a structured event emitted during workflow execution.
type PhaseEvent struct {
	Type     string                 `json:"type"`
	Phase    string                 `json:"phase,omitempty"`
	Status   string                 `json:"status"`
	Host     string                 `json:"host,omitempty"`
	Duration float64                `json:"duration_s,omitempty"`
	ExitCode *int                   `json:"exit_code,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Details  map[string]interface{} `json:"details,omitempty"`
	TS       string                 `json:"ts"`
}

// WritePhaseEvent writes a structured phase event to stderr as a JSON line.
// It's safe to call from concurrent goroutines.
// Intermediate phase events (type "phase") are suppressed when --no-phases is set.
// Result events (type "result") are always emitted.
func WritePhaseEvent(event PhaseEvent) {
	if suppressPhases && event.Type == "phase" {
		return
	}
	event.TS = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	// One write per event, so events from concurrent parallel workers
	// can't interleave within a line.
	os.Stderr.Write(append(data, '\n'))
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

// WriteJSONFromError converts a Go error to a JSON error response. The
// returned error is always an ExitError(1): the envelope already reported
// the failure, so callers returning it up to Execute() get a non-zero exit
// without a duplicate error message.
func WriteJSONFromError(w io.Writer, err error) error {
	jsonErr := ErrorToJSON(err)
	env := JSONEnvelope{
		Success: false,
		Error:   jsonErr,
	}
	_ = writeJSONEnvelope(w, env)
	return errors.NewExitError(1)
}

// writeJSONEnvelope writes the envelope with consistent formatting.
func writeJSONEnvelope(w io.Writer, env JSONEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// ErrorToJSON converts a Go error to a JSONError with appropriate code mapping.
// Uses errors.As() to properly handle wrapped errors.
func ErrorToJSON(err error) *JSONError {
	if err == nil {
		return nil
	}

	// Check if it's our structured error type (handles wrapped errors)
	var rrErr *errors.Error
	if stderrors.As(err, &rrErr) {
		return &JSONError{
			Code:       mapErrorCode(rrErr.Code),
			Message:    rrErr.Message,
			Suggestion: rrErr.Suggestion,
		}
	}

	// Check if it's a probe error (SSH-related, handles wrapped errors)
	var probeErr *host.ProbeError
	if stderrors.As(err, &probeErr) {
		return probeErrorToJSON(probeErr)
	}

	// Generic error
	return &JSONError{
		Code:    ErrCodeUnknown,
		Message: err.Error(),
	}
}

// internalToPublicCode maps each internal error code to its machine-readable
// public code. Errors are classified where they're created, so this is a pure
// lookup: message text never affects the result.
var internalToPublicCode = map[string]string{
	errors.ErrConfig:         ErrCodeConfigInvalid,
	errors.ErrConfigNotFound: ErrCodeConfigNotFound,
	errors.ErrHostNotFound:   ErrCodeHostNotFound,
	errors.ErrDependency:     ErrCodeDependencyMissing,
	errors.ErrSSH:            ErrCodeSSHConnectionFail,
	errors.ErrSync:           ErrCodeRsyncFailed,
	errors.ErrLock:           ErrCodeLockHeld,
	errors.ErrExec:           ErrCodeCommandFailed,
}

// mapErrorCode maps an internal error code to its public code, or UNKNOWN.
func mapErrorCode(internalCode string) string {
	if code, ok := internalToPublicCode[internalCode]; ok {
		return code
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
