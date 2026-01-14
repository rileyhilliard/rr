package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachineMode_DefaultValue(t *testing.T) {
	// Reset to default
	oldMode := machineMode
	defer func() { machineMode = oldMode }()

	machineMode = false
	assert.False(t, MachineMode())

	machineMode = true
	assert.True(t, MachineMode())
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

func TestWriteJSONFromError_NilError(t *testing.T) {
	var buf bytes.Buffer

	err := WriteJSONFromError(&buf, nil)
	require.NoError(t, err)

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
	require.NoError(t, err)

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

	rrErr := errors.New(errors.ErrConfig, "Config file not found", "Run 'rr init' to create one")
	err := WriteJSONFromError(&buf, rrErr)
	require.NoError(t, err)

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
	require.NoError(t, err)

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

func TestErrorToJSON_AllInternalErrorCodes(t *testing.T) {
	tests := []struct {
		name         string
		internalCode string
		message      string
		wantCode     string
	}{
		{
			name:         "config not found",
			internalCode: errors.ErrConfig,
			message:      "Config file not found",
			wantCode:     ErrCodeConfigNotFound,
		},
		{
			name:         "config couldn't find",
			internalCode: errors.ErrConfig,
			message:      "Couldn't find config file",
			wantCode:     ErrCodeConfigNotFound,
		},
		{
			name:         "config invalid",
			internalCode: errors.ErrConfig,
			message:      "Config file has invalid syntax",
			wantCode:     ErrCodeConfigInvalid,
		},
		{
			name:         "ssh error",
			internalCode: errors.ErrSSH,
			message:      "SSH connection failed",
			wantCode:     ErrCodeSSHConnectionFail,
		},
		{
			name:         "sync error",
			internalCode: errors.ErrSync,
			message:      "Rsync failed",
			wantCode:     ErrCodeRsyncFailed,
		},
		{
			name:         "lock error",
			internalCode: errors.ErrLock,
			message:      "Lock is held",
			wantCode:     ErrCodeLockHeld,
		},
		{
			name:         "exec error",
			internalCode: errors.ErrExec,
			message:      "Command failed",
			wantCode:     ErrCodeCommandFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.internalCode, tt.message, "some suggestion")
			result := ErrorToJSON(err)

			require.NotNil(t, result)
			assert.Equal(t, tt.wantCode, result.Code)
			assert.Equal(t, tt.message, result.Message)
		})
	}
}

func TestErrorToJSON_ConfigNotFoundVsInvalid(t *testing.T) {
	tests := []struct {
		message  string
		wantCode string
	}{
		{"Config file not found", ErrCodeConfigNotFound},
		{"couldn't find config", ErrCodeConfigNotFound},
		{"NOT FOUND anywhere", ErrCodeConfigNotFound},
		{"Config has invalid syntax", ErrCodeConfigInvalid},
		{"Failed to parse config", ErrCodeConfigInvalid},
		{"Schema validation error", ErrCodeConfigInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			err := errors.New(errors.ErrConfig, tt.message, "")
			result := ErrorToJSON(err)

			assert.Equal(t, tt.wantCode, result.Code)
		})
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

func TestMapErrorCode_UnknownCode(t *testing.T) {
	result := mapErrorCode("UNKNOWN_INTERNAL_CODE", "Some message")
	assert.Equal(t, ErrCodeUnknown, result)
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
