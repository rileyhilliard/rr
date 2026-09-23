package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachineMode_DefaultValue(t *testing.T) {
	oldPretty := prettyMode
	defer func() { prettyMode = oldPretty }()

	// MachineMode() returns !prettyMode (structured output is default)
	prettyMode = false
	assert.True(t, MachineMode())

	prettyMode = true
	assert.False(t, MachineMode())
}

func TestWriteJSONSuccess_BasicData(t *testing.T) {
	var buf bytes.Buffer

	data := map[string]string{"key": "value"}
	err := WriteJSONSuccess(&buf, data)
	require.NoError(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.True(t, env.Success)
	assert.Nil(t, env.Error)
	assert.NotNil(t, env.Data)

	// Verify data content
	dataMap, ok := env.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value", dataMap["key"])
}

func TestWriteJSONSuccess_ComplexData(t *testing.T) {
	var buf bytes.Buffer

	data := struct {
		Name  string   `json:"name"`
		Count int      `json:"count"`
		Items []string `json:"items"`
	}{
		Name:  "test",
		Count: 42,
		Items: []string{"a", "b", "c"},
	}

	err := WriteJSONSuccess(&buf, data)
	require.NoError(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.True(t, env.Success)
	dataMap, ok := env.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test", dataMap["name"])
	assert.Equal(t, float64(42), dataMap["count"]) // JSON numbers are float64
}

func TestWriteJSONSuccess_NilData(t *testing.T) {
	var buf bytes.Buffer

	err := WriteJSONSuccess(&buf, nil)
	require.NoError(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.True(t, env.Success)
	assert.Nil(t, env.Data)
	assert.Nil(t, env.Error)
}

func TestWriteJSONError_AllFields(t *testing.T) {
	var buf bytes.Buffer

	details := map[string]string{"host": "example.com"}
	err := WriteJSONError(&buf, ErrCodeSSHTimeout, "Connection timed out", "Check network connectivity", details)
	require.NoError(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.False(t, env.Success)
	assert.Nil(t, env.Data)
	require.NotNil(t, env.Error)

	assert.Equal(t, ErrCodeSSHTimeout, env.Error.Code)
	assert.Equal(t, "Connection timed out", env.Error.Message)
	assert.Equal(t, "Check network connectivity", env.Error.Suggestion)

	detailsMap, ok := env.Error.Details.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "example.com", detailsMap["host"])
}

func TestWriteJSONError_NoSuggestion(t *testing.T) {
	var buf bytes.Buffer

	err := WriteJSONError(&buf, ErrCodeUnknown, "Something went wrong", "", nil)
	require.NoError(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.False(t, env.Success)
	assert.Equal(t, ErrCodeUnknown, env.Error.Code)
	assert.Empty(t, env.Error.Suggestion)
	assert.Nil(t, env.Error.Details)
}

func assertExitOne(t *testing.T, err error) {
	t.Helper()
	code, ok := errors.GetExitCode(err)
	require.True(t, ok, "expected an ExitError, got %v", err)
	assert.Equal(t, 1, code)
}

func TestWriteJSONFromError_NilError(t *testing.T) {
	var buf bytes.Buffer

	err := WriteJSONFromError(&buf, nil)
	assertExitOne(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.False(t, env.Success)
	assert.Nil(t, env.Error)
}

func TestWriteJSONFromError_GenericError(t *testing.T) {
	var buf bytes.Buffer

	goErr := fmt.Errorf("something went wrong")
	err := WriteJSONFromError(&buf, goErr)
	assertExitOne(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.False(t, env.Success)
	require.NotNil(t, env.Error)
	assert.Equal(t, ErrCodeUnknown, env.Error.Code)
	assert.Equal(t, "something went wrong", env.Error.Message)
}

func TestWriteJSONFromError_StructuredError(t *testing.T) {
	var buf bytes.Buffer

	rrErr := errors.New(errors.ErrConfigNotFound, "Config file not found", "Run 'rr init' to create one")
	err := WriteJSONFromError(&buf, rrErr)
	assertExitOne(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.False(t, env.Success)
	require.NotNil(t, env.Error)
	assert.Equal(t, ErrCodeConfigNotFound, env.Error.Code)
	assert.Equal(t, "Config file not found", env.Error.Message)
	assert.Equal(t, "Run 'rr init' to create one", env.Error.Suggestion)
}

func TestWriteJSONFromError_WrappedStructuredError(t *testing.T) {
	var buf bytes.Buffer

	innerErr := errors.New(errors.ErrSSH, "Connection refused", "Check if SSH server is running")
	wrappedErr := fmt.Errorf("failed to connect: %w", innerErr)
	err := WriteJSONFromError(&buf, wrappedErr)
	assertExitOne(t, err)

	var env JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &env)
	require.NoError(t, err)

	assert.False(t, env.Success)
	require.NotNil(t, env.Error)
	assert.Equal(t, ErrCodeSSHConnectionFail, env.Error.Code)
}

func TestErrorToJSON_NilReturnsNil(t *testing.T) {
	result := ErrorToJSON(nil)
	assert.Nil(t, result)
}

func TestErrorToJSON_GenericError(t *testing.T) {
	err := fmt.Errorf("generic error message")
	result := ErrorToJSON(err)

	require.NotNil(t, result)
	assert.Equal(t, ErrCodeUnknown, result.Code)
	assert.Equal(t, "generic error message", result.Message)
	assert.Empty(t, result.Suggestion)
}

func TestMapErrorCode_Table(t *testing.T) {
	tests := []struct {
		internalCode string
		wantCode     string
	}{
		{errors.ErrConfig, ErrCodeConfigInvalid},
		{errors.ErrConfigNotFound, ErrCodeConfigNotFound},
		{errors.ErrHostNotFound, ErrCodeHostNotFound},
		{errors.ErrDependency, ErrCodeDependencyMissing},
		{errors.ErrSSH, ErrCodeSSHConnectionFail},
		{errors.ErrSync, ErrCodeRsyncFailed},
		{errors.ErrLock, ErrCodeLockHeld},
		{errors.ErrExec, ErrCodeCommandFailed},
		{"UNKNOWN_INTERNAL_CODE", ErrCodeUnknown},
		{"", ErrCodeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.internalCode, func(t *testing.T) {
			result := ErrorToJSON(errors.New(tt.internalCode, "some message", "some suggestion"))
			require.NotNil(t, result)
			assert.Equal(t, tt.wantCode, result.Code)
			assert.Equal(t, "some message", result.Message)
		})
	}
}

// TestMapErrorCode_IgnoresMessageText guards against classifying errors by
// sniffing their text: only the internal code decides the public code.
func TestMapErrorCode_IgnoresMessageText(t *testing.T) {
	tests := []struct {
		internalCode string
		message      string
		wantCode     string
	}{
		{errors.ErrConfig, "Config file not found", ErrCodeConfigInvalid},
		{errors.ErrConfig, "Couldn't find config file", ErrCodeConfigInvalid},
		{errors.ErrConfig, "Host 'x' not found", ErrCodeConfigInvalid},
		{errors.ErrExec, "Missing required tools: go", ErrCodeCommandFailed},
		{errors.ErrConfigNotFound, "Config has invalid syntax", ErrCodeConfigNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := ErrorToJSON(errors.New(tt.internalCode, tt.message, ""))
			assert.Equal(t, tt.wantCode, result.Code)
		})
	}
}

// publicErrorCodes returns every ErrCode* constant declared in json.go, read
// from the source so a newly added code can't be missed by the contract test.
func publicErrorCodes(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "json.go", nil, 0)
	require.NoError(t, err)

	codes := make(map[string]string)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "ErrCode") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				require.True(t, ok, "%s should be a string literal", name.Name)
				value, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				codes[name.Name] = value
			}
		}
	}
	require.NotEmpty(t, codes)
	return codes
}

// TestErrorCodes_EveryPublicCodeIsProduced is the D-1 contract: every public
// error code except UNKNOWN is reachable from at least one internal error
// code (or, for the SSH-specific codes, from a probe failure reason).
func TestErrorCodes_EveryPublicCodeIsProduced(t *testing.T) {
	produced := make(map[string]bool)
	for _, public := range internalToPublicCode {
		produced[public] = true
	}
	for reason := host.ProbeFailUnknown; reason <= host.ProbeFailConnReset; reason++ {
		produced[probeErrorToJSON(&host.ProbeError{Reason: reason}).Code] = true
	}

	for name, code := range publicErrorCodes(t) {
		if code == ErrCodeUnknown {
			assert.False(t, produced[code], "UNKNOWN should only be the fallback, never a mapping target")
			continue
		}
		assert.True(t, produced[code], "%s (%s) is not produced by any internal code", name, code)
	}
}

func TestErrorToJSON_ProbeError(t *testing.T) {
	probeErr := &host.ProbeError{
		SSHAlias: "test-host",
		Reason:   host.ProbeFailTimeout,
		Cause:    fmt.Errorf("dial timeout"),
	}

	result := ErrorToJSON(probeErr)

	require.NotNil(t, result)
	assert.Equal(t, ErrCodeSSHTimeout, result.Code)
	assert.NotEmpty(t, result.Suggestion)
	assert.NotNil(t, result.Details)

	details, ok := result.Details.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "connection timed out", details["reason"])
	assert.Equal(t, "test-host", details["alias"])
}

func TestErrorToJSON_WrappedProbeError(t *testing.T) {
	probeErr := &host.ProbeError{
		SSHAlias: "test-host",
		Reason:   host.ProbeFailAuth,
	}
	wrappedErr := fmt.Errorf("connection failed: %w", probeErr)

	result := ErrorToJSON(wrappedErr)

	require.NotNil(t, result)
	assert.Equal(t, ErrCodeSSHAuthFailed, result.Code)
}

func TestProbeErrorToJSON_AllReasons(t *testing.T) {
	tests := []struct {
		reason   host.ProbeFailReason
		wantCode string
	}{
		{host.ProbeFailTimeout, ErrCodeSSHTimeout},
		{host.ProbeFailAuth, ErrCodeSSHAuthFailed},
		{host.ProbeFailHostKey, ErrCodeSSHHostKey},
		{host.ProbeFailDNS, ErrCodeSSHConnectionFail},
		{host.ProbeFailRefused, ErrCodeSSHConnectionFail},
		{host.ProbeFailConnReset, ErrCodeSSHConnectionFail},
		{host.ProbeFailUnreachable, ErrCodeSSHConnectionFail},
		{host.ProbeFailUnknown, ErrCodeSSHConnectionFail},
	}

	for _, tt := range tests {
		t.Run(tt.reason.String(), func(t *testing.T) {
			probeErr := &host.ProbeError{
				SSHAlias: "test-host",
				Reason:   tt.reason,
			}

			result := probeErrorToJSON(probeErr)

			require.NotNil(t, result)
			assert.Equal(t, tt.wantCode, result.Code)
			assert.NotEmpty(t, result.Message)

			// All probe errors should have details
			details, ok := result.Details.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "test-host", details["alias"])
		})
	}
}

func TestProbeErrorToJSON_Suggestions(t *testing.T) {
	tests := []struct {
		reason         host.ProbeFailReason
		wantSuggestion string
		wantContains   []string
	}{
		{
			reason:       host.ProbeFailTimeout,
			wantContains: []string{"ping"},
		},
		{
			reason:       host.ProbeFailAuth,
			wantContains: []string{"ssh-copy-id"},
		},
		{
			reason:       host.ProbeFailHostKey,
			wantContains: []string{"StrictHostKeyChecking"},
		},
		{
			reason:       host.ProbeFailDNS,
			wantContains: []string{"hostname", "SSH config"},
		},
		{
			reason:       host.ProbeFailRefused,
			wantContains: []string{"SSH server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.reason.String(), func(t *testing.T) {
			probeErr := &host.ProbeError{
				SSHAlias: "test-host",
				Reason:   tt.reason,
			}

			result := probeErrorToJSON(probeErr)

			for _, substr := range tt.wantContains {
				assert.Contains(t, result.Suggestion, substr,
					"suggestion should contain %q", substr)
			}
		})
	}
}

func TestJSONEnvelope_Structure(t *testing.T) {
	// Test that JSON envelope marshals with correct field names
	env := JSONEnvelope{
		Success: true,
		Data:    "test",
	}

	data, err := json.Marshal(env)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"success":true`)
	assert.Contains(t, string(data), `"data":"test"`)
	assert.NotContains(t, string(data), `"error"`) // omitempty
}

func TestJSONEnvelope_ErrorStructure(t *testing.T) {
	env := JSONEnvelope{
		Success: false,
		Error: &JSONError{
			Code:       "TEST_CODE",
			Message:    "Test message",
			Suggestion: "Test suggestion",
			Details:    map[string]string{"key": "value"},
		},
	}

	data, err := json.Marshal(env)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"success":false`)
	assert.Contains(t, string(data), `"code":"TEST_CODE"`)
	assert.Contains(t, string(data), `"message":"Test message"`)
	assert.Contains(t, string(data), `"suggestion":"Test suggestion"`)
	assert.NotContains(t, string(data), `"data"`) // omitempty
}

func TestJSONError_OmitsEmptyFields(t *testing.T) {
	jsonErr := JSONError{
		Code:    "TEST",
		Message: "Test",
		// Suggestion and Details empty
	}

	data, err := json.Marshal(jsonErr)
	require.NoError(t, err)

	assert.NotContains(t, string(data), `"suggestion"`)
	assert.NotContains(t, string(data), `"details"`)
}

func TestWriteJSONEnvelope_Formatting(t *testing.T) {
	var buf bytes.Buffer

	err := WriteJSONSuccess(&buf, map[string]string{"test": "value"})
	require.NoError(t, err)

	output := buf.String()

	// Should be indented with 2 spaces
	assert.Contains(t, output, "\n  ")
	// Should end with newline
	assert.True(t, output[len(output)-1] == '\n')
}

func TestErrorCodes_AreUnique(t *testing.T) {
	codes := []string{
		ErrCodeConfigNotFound,
		ErrCodeConfigInvalid,
		ErrCodeHostNotFound,
		ErrCodeSSHTimeout,
		ErrCodeSSHAuthFailed,
		ErrCodeSSHHostKey,
		ErrCodeSSHConnectionFail,
		ErrCodeRsyncFailed,
		ErrCodeLockHeld,
		ErrCodeCommandFailed,
		ErrCodeDependencyMissing,
		ErrCodeUnknown,
	}

	seen := make(map[string]bool)
	for _, code := range codes {
		assert.False(t, seen[code], "duplicate error code: %s", code)
		seen[code] = true
	}
}

func TestErrorCodes_Format(t *testing.T) {
	// All error codes should be UPPER_SNAKE_CASE
	codes := []string{
		ErrCodeConfigNotFound,
		ErrCodeConfigInvalid,
		ErrCodeHostNotFound,
		ErrCodeSSHTimeout,
		ErrCodeSSHAuthFailed,
		ErrCodeSSHHostKey,
		ErrCodeSSHConnectionFail,
		ErrCodeRsyncFailed,
		ErrCodeLockHeld,
		ErrCodeCommandFailed,
		ErrCodeDependencyMissing,
		ErrCodeUnknown,
	}

	for _, code := range codes {
		// Should not contain lowercase letters
		for _, r := range code {
			if r >= 'a' && r <= 'z' {
				t.Errorf("error code %q contains lowercase letter", code)
				break
			}
		}
	}
}

// captureStderr captures os.Stderr output during fn execution.
// Uses defer to restore os.Stderr even if fn panics.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() {
		os.Stderr = old
		r.Close()
	}()
	fn()
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestWritePhaseEvent_SuppressPhasesBehavior(t *testing.T) {
	tests := []struct {
		name         string
		suppress     bool
		event        PhaseEvent
		wantEmpty    bool
		wantContains string
	}{
		{
			name:      "phase event suppressed when flag set",
			suppress:  true,
			event:     PhaseEvent{Type: "phase", Phase: "connect", Status: "started"},
			wantEmpty: true,
		},
		{
			name:      "sync phase event suppressed when flag set",
			suppress:  true,
			event:     PhaseEvent{Type: "phase", Phase: "sync", Status: "started"},
			wantEmpty: true,
		},
		{
			name:         "result event not suppressed even when flag set",
			suppress:     true,
			event:        PhaseEvent{Type: "result", Status: "success"},
			wantEmpty:    false,
			wantContains: `"type":"result"`,
		},
		{
			name:      "phase event emitted when flag not set",
			suppress:  false,
			event:     PhaseEvent{Type: "phase", Phase: "connect", Status: "started"},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := suppressPhases
			defer func() { suppressPhases = old }()
			suppressPhases = tt.suppress

			output := captureStderr(t, func() {
				WritePhaseEvent(tt.event)
			})

			if tt.wantEmpty {
				assert.Empty(t, output)
			} else {
				assert.NotEmpty(t, output)
			}
			if tt.wantContains != "" {
				assert.Contains(t, output, tt.wantContains)
			}
		})
	}
}
