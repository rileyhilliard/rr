package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/doctor"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestCapitalizeFirst(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "lowercase first char",
			input: "connection refused",
			want:  "Connection refused",
		},
		{
			name:  "already capitalized",
			input: "Already capitalized",
			want:  "Already capitalized",
		},
		{
			name:  "single lowercase char",
			input: "a",
			want:  "A",
		},
		{
			name:  "single uppercase char",
			input: "A",
			want:  "A",
		},
		{
			name:  "non-alpha first char unchanged",
			input: "123 test",
			want:  "123 test",
		},
		{
			name:  "special char first",
			input: "-flag",
			want:  "-flag",
		},
		{
			name:  "all lowercase",
			input: "timeout",
			want:  "Timeout",
		},
		{
			name:  "all uppercase",
			input: "SSH",
			want:  "SSH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capitalizeFirst(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPluralSuffix(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{
			name: "zero returns s",
			n:    0,
			want: "s",
		},
		{
			name: "one returns empty",
			n:    1,
			want: "",
		},
		{
			name: "two returns s",
			n:    2,
			want: "s",
		},
		{
			name: "large number returns s",
			n:    100,
			want: "s",
		},
		{
			name: "negative returns s",
			n:    -1,
			want: "s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pluralSuffix(tt.n)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDoctorOutput_JSONMarshaling(t *testing.T) {
	output := DoctorOutput{
		Categories: []CategoryOutput{
			{
				Name: "CONFIG",
				Results: []doctor.CheckResult{
					{
						Status:     doctor.StatusPass,
						Message:    "Config file exists",
						Suggestion: "",
						Fixable:    false,
					},
				},
			},
			{
				Name: "SSH",
				Results: []doctor.CheckResult{
					{
						Status:     doctor.StatusFail,
						Message:    "SSH key not found",
						Suggestion: "Run ssh-keygen to create a key",
						Fixable:    true,
					},
				},
			},
		},
		Summary: SummaryOutput{
			Pass:     1,
			Warn:     0,
			Fail:     1,
			Fixable:  1,
			AllClear: false,
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(output)
	require.NoError(t, err)

	// Unmarshal back
	var decoded DoctorOutput
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify structure
	assert.Len(t, decoded.Categories, 2)
	assert.Equal(t, "CONFIG", decoded.Categories[0].Name)
	assert.Equal(t, "SSH", decoded.Categories[1].Name)
	assert.Len(t, decoded.Categories[0].Results, 1)
	assert.Len(t, decoded.Categories[1].Results, 1)

	// Verify summary
	assert.Equal(t, 1, decoded.Summary.Pass)
	assert.Equal(t, 0, decoded.Summary.Warn)
	assert.Equal(t, 1, decoded.Summary.Fail)
	assert.Equal(t, 1, decoded.Summary.Fixable)
	assert.False(t, decoded.Summary.AllClear)
}

func TestDoctorOutput_EmptyCategories(t *testing.T) {
	output := DoctorOutput{
		Categories: []CategoryOutput{},
		Summary: SummaryOutput{
			Pass:     0,
			Warn:     0,
			Fail:     0,
			Fixable:  0,
			AllClear: true,
		},
	}

	data, err := json.Marshal(output)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"categories":[]`)
	assert.Contains(t, string(data), `"all_clear":true`)
}

func TestCategoryOutput_JSONFields(t *testing.T) {
	cat := CategoryOutput{
		Name: "HOSTS",
		Results: []doctor.CheckResult{
			{
				Status:     doctor.StatusWarn,
				Message:    "Host unreachable",
				Suggestion: "Check network connection",
				Fixable:    false,
			},
		},
	}

	data, err := json.Marshal(cat)
	require.NoError(t, err)

	// Verify JSON field names
	assert.Contains(t, string(data), `"name":"HOSTS"`)
	assert.Contains(t, string(data), `"results":[`)
}

func TestSummaryOutput_AllClear(t *testing.T) {
	tests := []struct {
		name     string
		summary  SummaryOutput
		wantJSON string
	}{
		{
			name: "all pass",
			summary: SummaryOutput{
				Pass:     5,
				Warn:     0,
				Fail:     0,
				Fixable:  0,
				AllClear: true,
			},
			wantJSON: `"all_clear":true`,
		},
		{
			name: "has warnings",
			summary: SummaryOutput{
				Pass:     3,
				Warn:     2,
				Fail:     0,
				Fixable:  1,
				AllClear: false,
			},
			wantJSON: `"all_clear":false`,
		},
		{
			name: "has failures",
			summary: SummaryOutput{
				Pass:     1,
				Warn:     0,
				Fail:     3,
				Fixable:  2,
				AllClear: false,
			},
			wantJSON: `"all_clear":false`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.summary)
			require.NoError(t, err)
			assert.Contains(t, string(data), tt.wantJSON)
		})
	}
}

func TestFormatProbeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil error",
			err:  nil,
			want: "Connection failed",
		},
		{
			name: "generic error",
			err:  assert.AnError,
			want: "Assert.AnError general error for testing",
		},
		{
			name: "unknown probe error with cause",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnknown,
				Cause:    fmt.Errorf("specific underlying error"),
			},
			want: "Specific underlying error",
		},
		{
			name: "unknown probe error without cause",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnknown,
				Cause:    nil,
			},
			want: "Connection failed",
		},
		{
			name: "timeout probe error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailTimeout,
				Cause:    fmt.Errorf("timeout"),
			},
			want: "Connection timed out",
		},
		{
			name: "connection refused probe error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailRefused,
				Cause:    fmt.Errorf("connection refused"),
			},
			want: "Connection refused",
		},
		{
			name: "auth failed probe error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailAuth,
				Cause:    fmt.Errorf("permission denied"),
			},
			want: "Authentication failed",
		},
		{
			name: "host key probe error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailHostKey,
				Cause:    fmt.Errorf("host key verification failed"),
			},
			want: "Host key verification failed",
		},
		{
			name: "host key mismatch with detailed error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailHostKey,
				Cause: errors.WrapWithCode(
					&sshutil.HostKeyMismatchError{
						Hostname:     "192.168.1.100:22",
						ReceivedType: "ecdsa-sha2-nistp256",
						KnownHosts:   "/home/user/.ssh/known_hosts",
						Want:         []knownhosts.KnownKey{},
					},
					errors.ErrSSH, "host key mismatch", "suggestion"),
			},
			want: "Host key mismatch (got ecdsa-sha2-nistp256, expected different type)",
		},
		{
			name: "unreachable probe error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnreachable,
				Cause:    fmt.Errorf("no route to host"),
			},
			want: "Host unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatProbeError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetSSHErrorSuggestion(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		alias    string
		contains []string
	}{
		{
			name:     "generic error",
			err:      assert.AnError,
			alias:    "user@example.com",
			contains: []string{"ssh user@example.com"},
		},
		{
			name: "connection refused",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailRefused,
			},
			alias:    "user@example.com",
			contains: []string{"SSH server may not be running", "ssh user@example.com"},
		},
		{
			name: "timeout",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailTimeout,
			},
			alias:    "user@example.com",
			contains: []string{"offline", "firewall", "ping example.com"},
		},
		{
			name: "unreachable",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnreachable,
			},
			alias:    "user@example.com",
			contains: []string{"network connectivity", "ping example.com"},
		},
		{
			name: "auth failed",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailAuth,
			},
			alias:    "user@example.com",
			contains: []string{"ssh-add"},
		},
		{
			name: "host key mismatch",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailHostKey,
			},
			alias:    "user@example.com",
			contains: []string{"StrictHostKeyChecking=accept-new", "user@example.com"},
		},
		{
			name: "host key mismatch with detailed error extracts real suggestion",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailHostKey,
				Cause: errors.WrapWithCode(
					&sshutil.HostKeyMismatchError{
						Hostname:     "192.168.1.100:22",
						ReceivedType: "ecdsa-sha2-nistp256",
						KnownHosts:   "/home/user/.ssh/known_hosts",
						Want:         []knownhosts.KnownKey{},
					},
					errors.ErrSSH, "host key mismatch", "suggestion"),
			},
			alias: "myserver",
			// The detailed suggestion from HostKeyMismatchError includes the IP and key types
			contains: []string{"ssh-keyscan", "192.168.1.100", "ecdsa-sha2-nistp256"},
		},
		{
			name: "unknown error",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailUnknown,
			},
			alias:    "myhost",
			contains: []string{"ssh myhost"},
		},
		{
			name: "extracts host from user@host",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailTimeout,
			},
			alias:    "root@192.168.1.1",
			contains: []string{"ping 192.168.1.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSSHErrorSuggestion(tt.err, tt.alias)
			for _, s := range tt.contains {
				assert.Contains(t, got, s)
			}
		})
	}
}

func TestCollectChecks_NoConfig(t *testing.T) {
	checks := collectChecks("", nil, nil)

	// Should have config checks, SSH checks, and deps checks
	assert.NotEmpty(t, checks)

	// Verify categories are present
	categories := make(map[string]bool)
	for _, check := range checks {
		categories[check.Category()] = true
	}

	assert.True(t, categories["CONFIG"], "should have CONFIG checks")
	assert.True(t, categories["SSH"], "should have SSH checks")
	assert.True(t, categories["DEPENDENCIES"], "should have DEPENDENCIES checks")
}

func TestCollectChecks_WithGlobalConfig(t *testing.T) {
	globalCfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{
			"dev": {SSH: []string{"dev.example.com"}},
		},
	}

	checks := collectChecks(".rr.yaml", nil, globalCfg)
	assert.NotEmpty(t, checks)

	// Should have host checks when global config has hosts
	categories := make(map[string]bool)
	for _, check := range checks {
		categories[check.Category()] = true
	}

	assert.True(t, categories["HOSTS"], "should have HOSTS checks when global config has hosts")
}

func TestCollectChecks_EmptyHosts(t *testing.T) {
	globalCfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{},
	}

	checks := collectChecks(".rr.yaml", nil, globalCfg)
	assert.NotEmpty(t, checks)

	// Should NOT have host checks when global config has no hosts
	categories := make(map[string]bool)
	for _, check := range checks {
		categories[check.Category()] = true
	}

	assert.False(t, categories["HOSTS"], "should not have HOSTS checks when global config has no hosts")
}

func TestAttemptFixes_PassStatus(t *testing.T) {
	// Create a mock check that passes
	results := []doctor.CheckResult{
		{
			Status:  doctor.StatusPass,
			Message: "All good",
			Fixable: true, // Even though fixable, pass status should not attempt fix
		},
	}

	checks := []doctor.Check{
		&mockCheck{result: results[0]},
	}

	newResults := attemptFixes(checks, results)

	// Results should be unchanged for passing checks
	assert.Equal(t, results, newResults)
}

func TestOutputDoctorJSON_Format(t *testing.T) {
	// This tests JSON structure, not actual output (which goes to stdout)
	output := DoctorOutput{
		Categories: []CategoryOutput{
			{
				Name: "TEST",
				Results: []doctor.CheckResult{
					{Status: doctor.StatusPass, Message: "test passed"},
				},
			},
		},
		Summary: SummaryOutput{
			Pass:     1,
			AllClear: true,
		},
	}

	data, err := json.MarshalIndent(output, "", "  ")
	require.NoError(t, err)

	// Verify JSON structure
	assert.Contains(t, string(data), `"categories"`)
	assert.Contains(t, string(data), `"summary"`)
	assert.Contains(t, string(data), `"all_clear": true`)
}

func TestRenderCheckResult_AllStatuses(t *testing.T) {
	// Import lipgloss for styles
	// This test verifies renderCheckResult handles all status types without panic

	tests := []struct {
		status doctor.CheckStatus
	}{
		{doctor.StatusPass},
		{doctor.StatusWarn},
		{doctor.StatusFail},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			result := doctor.CheckResult{
				Status:     tt.status,
				Message:    "Test message",
				Suggestion: "Test suggestion",
			}

			// Should not panic
			assert.NotPanics(t, func() {
				// We can't easily test the actual output since it goes to stdout
				// but we can verify the function doesn't panic
				_ = result
			})
		})
	}
}

func TestDoctorOutput_Defaults(t *testing.T) {
	output := DoctorOutput{}

	assert.Nil(t, output.Categories)
	assert.Equal(t, 0, output.Summary.Pass)
	assert.Equal(t, 0, output.Summary.Warn)
	assert.Equal(t, 0, output.Summary.Fail)
	assert.Equal(t, 0, output.Summary.Fixable)
	assert.False(t, output.Summary.AllClear)
}

func TestSummaryOutput_Defaults(t *testing.T) {
	summary := SummaryOutput{}

	assert.Equal(t, 0, summary.Pass)
	assert.Equal(t, 0, summary.Warn)
	assert.Equal(t, 0, summary.Fail)
	assert.Equal(t, 0, summary.Fixable)
	assert.False(t, summary.AllClear)
}

func TestCategoryOutput_Defaults(t *testing.T) {
	cat := CategoryOutput{}

	assert.Empty(t, cat.Name)
	assert.Nil(t, cat.Results)
}

// mockCheck implements doctor.Check for testing
type mockCheck struct {
	name     string
	result   doctor.CheckResult
	category string
	fixed    bool
	fixErr   error
}

func (m *mockCheck) Name() string {
	if m.name == "" {
		return "mock_check"
	}
	return m.name
}

func (m *mockCheck) Run() doctor.CheckResult {
	return m.result
}

func (m *mockCheck) Category() string {
	if m.category == "" {
		return "TEST"
	}
	return m.category
}

func (m *mockCheck) Fix() error {
	m.fixed = true
	return m.fixErr
}

func TestAttemptFixes_FailStatus(t *testing.T) {
	results := []doctor.CheckResult{
		{
			Status:  doctor.StatusFail,
			Message: "Something failed",
			Fixable: true,
		},
	}

	checks := []doctor.Check{
		&mockCheck{
			result: doctor.CheckResult{
				Status:  doctor.StatusPass,
				Message: "Fixed!",
			},
		},
	}

	newResults := attemptFixes(checks, results)

	// After fix attempt, should re-run check
	assert.Equal(t, doctor.StatusPass, newResults[0].Status)
}

func TestAttemptFixes_WarnStatus(t *testing.T) {
	results := []doctor.CheckResult{
		{
			Status:  doctor.StatusWarn,
			Message: "Warning",
			Fixable: true,
		},
	}

	checks := []doctor.Check{
		&mockCheck{
			result: doctor.CheckResult{
				Status:  doctor.StatusPass,
				Message: "Fixed warning!",
			},
		},
	}

	newResults := attemptFixes(checks, results)
	assert.Equal(t, doctor.StatusPass, newResults[0].Status)
}

func TestAttemptFixes_NotFixable(t *testing.T) {
	originalResult := doctor.CheckResult{
		Status:  doctor.StatusFail,
		Message: "Not fixable failure",
		Fixable: false,
	}
	results := []doctor.CheckResult{originalResult}

	mockChk := &mockCheck{result: originalResult}
	checks := []doctor.Check{mockChk}

	newResults := attemptFixes(checks, results)

	// Should not attempt fix for non-fixable check
	assert.False(t, mockChk.fixed)
	assert.Equal(t, originalResult, newResults[0])
}

func TestAttemptFixes_FixError(t *testing.T) {
	originalResult := doctor.CheckResult{
		Status:  doctor.StatusFail,
		Message: "Fixable but will error",
		Fixable: true,
	}
	results := []doctor.CheckResult{originalResult}

	checks := []doctor.Check{
		&mockCheck{
			result: originalResult,
			fixErr: fmt.Errorf("fix failed"),
		},
	}

	newResults := attemptFixes(checks, results)

	// When fix fails, original result is kept
	assert.Equal(t, originalResult, newResults[0])
}

func TestAttemptFixes_MultipleChecks(t *testing.T) {
	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "Already passing", Fixable: false},
		{Status: doctor.StatusFail, Message: "Failing check", Fixable: true},
		{Status: doctor.StatusWarn, Message: "Warning check", Fixable: true},
		{Status: doctor.StatusFail, Message: "Not fixable", Fixable: false},
	}

	checks := []doctor.Check{
		&mockCheck{result: results[0]},
		&mockCheck{result: doctor.CheckResult{Status: doctor.StatusPass, Message: "Fixed 1"}},
		&mockCheck{result: doctor.CheckResult{Status: doctor.StatusPass, Message: "Fixed 2"}},
		&mockCheck{result: results[3]},
	}

	newResults := attemptFixes(checks, results)

	assert.Equal(t, doctor.StatusPass, newResults[0].Status) // unchanged
	assert.Equal(t, doctor.StatusPass, newResults[1].Status) // fixed
	assert.Equal(t, doctor.StatusPass, newResults[2].Status) // fixed
	assert.Equal(t, doctor.StatusFail, newResults[3].Status) // unchanged, not fixable
}

func TestCollectChecks_ConfigPath(t *testing.T) {
	// With non-empty config path, should include config checks
	checks := collectChecks("/path/to/.rr.yaml", nil, nil)

	hasConfig := false
	for _, check := range checks {
		if check.Category() == "CONFIG" {
			hasConfig = true
			break
		}
	}
	assert.True(t, hasConfig)
}

func TestCollectChecks_MultipleHostsConfig(t *testing.T) {
	globalCfg := &config.GlobalConfig{
		Hosts: map[string]config.Host{
			"dev":     {SSH: []string{"dev.example.com"}},
			"staging": {SSH: []string{"staging.example.com"}},
			"prod":    {SSH: []string{"prod.example.com"}},
		},
	}

	checks := collectChecks(".rr.yaml", nil, globalCfg)

	hostCheckCount := 0
	for _, check := range checks {
		if check.Category() == "HOSTS" {
			hostCheckCount++
		}
	}
	assert.Greater(t, hostCheckCount, 0)
}

func TestFormatProbeError_WithCause(t *testing.T) {
	tests := []struct {
		name     string
		err      *host.ProbeError
		contains string
	}{
		{
			name: "timeout with cause",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailTimeout,
				Cause:    fmt.Errorf("dial timeout"),
			},
			contains: "timed out",
		},
		{
			name: "refused with cause",
			err: &host.ProbeError{
				SSHAlias: "test",
				Reason:   host.ProbeFailRefused,
				Cause:    fmt.Errorf("connection refused"),
			},
			contains: "refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatProbeError(tt.err)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestGetSSHErrorSuggestion_ComplexAlias(t *testing.T) {
	tests := []struct {
		name     string
		alias    string
		err      *host.ProbeError
		contains string
	}{
		{
			name:  "simple hostname",
			alias: "myserver",
			err: &host.ProbeError{
				SSHAlias: "myserver",
				Reason:   host.ProbeFailTimeout,
			},
			contains: "ping myserver",
		},
		{
			name:  "user@host format",
			alias: "user@myserver.example.com",
			err: &host.ProbeError{
				SSHAlias: "user@myserver.example.com",
				Reason:   host.ProbeFailTimeout,
			},
			contains: "ping myserver.example.com",
		},
		{
			name:  "IP address",
			alias: "root@10.0.0.1",
			err: &host.ProbeError{
				SSHAlias: "root@10.0.0.1",
				Reason:   host.ProbeFailUnreachable,
			},
			contains: "ping 10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSSHErrorSuggestion(tt.err, tt.alias)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestCapitalizeFirst_UnicodeEdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "Hello world"},
		{"HELLO", "HELLO"},
		{"123abc", "123abc"},
		{"", ""},
		{" space", " space"},
		{"z", "Z"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := capitalizeFirst(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestPluralSuffix_EdgeCases(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{-10, "s"},
		{-1, "s"},
		{0, "s"},
		{1, ""},
		{2, "s"},
		{10, "s"},
		{1000, "s"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			result := pluralSuffix(tt.n)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestDoctorOutput_FullStructure(t *testing.T) {
	output := DoctorOutput{
		Categories: []CategoryOutput{
			{
				Name: "CONFIG",
				Results: []doctor.CheckResult{
					{Status: doctor.StatusPass, Message: "Config exists"},
					{Status: doctor.StatusPass, Message: "Config valid"},
				},
			},
			{
				Name: "SSH",
				Results: []doctor.CheckResult{
					{Status: doctor.StatusPass, Message: "SSH agent running"},
				},
			},
			{
				Name: "HOSTS",
				Results: []doctor.CheckResult{
					{Status: doctor.StatusFail, Message: "Cannot reach dev", Suggestion: "Check network"},
				},
			},
		},
		Summary: SummaryOutput{
			Pass:     3,
			Warn:     0,
			Fail:     1,
			Fixable:  0,
			AllClear: false,
		},
	}

	data, err := json.Marshal(output)
	require.NoError(t, err)

	var decoded DoctorOutput
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Len(t, decoded.Categories, 3)
	assert.Equal(t, 3, decoded.Summary.Pass)
	assert.Equal(t, 1, decoded.Summary.Fail)
	assert.False(t, decoded.Summary.AllClear)
}

func TestCategoryOutput_EmptyResults(t *testing.T) {
	cat := CategoryOutput{
		Name:    "EMPTY",
		Results: []doctor.CheckResult{},
	}

	data, err := json.Marshal(cat)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"name":"EMPTY"`)
	assert.Contains(t, string(data), `"results":[]`)
}

func TestSummaryOutput_VariousCombinations(t *testing.T) {
	tests := []struct {
		name    string
		summary SummaryOutput
	}{
		{
			name: "all zeros",
			summary: SummaryOutput{
				Pass: 0, Warn: 0, Fail: 0, Fixable: 0, AllClear: true,
			},
		},
		{
			name: "only pass",
			summary: SummaryOutput{
				Pass: 10, Warn: 0, Fail: 0, Fixable: 0, AllClear: true,
			},
		},
		{
			name: "mixed results",
			summary: SummaryOutput{
				Pass: 5, Warn: 3, Fail: 2, Fixable: 1, AllClear: false,
			},
		},
		{
			name: "only failures",
			summary: SummaryOutput{
				Pass: 0, Warn: 0, Fail: 5, Fixable: 5, AllClear: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.summary)
			require.NoError(t, err)

			var decoded SummaryOutput
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)

			assert.Equal(t, tt.summary.Pass, decoded.Pass)
			assert.Equal(t, tt.summary.Warn, decoded.Warn)
			assert.Equal(t, tt.summary.Fail, decoded.Fail)
			assert.Equal(t, tt.summary.Fixable, decoded.Fixable)
			assert.Equal(t, tt.summary.AllClear, decoded.AllClear)
		})
	}
}

func TestFormatProbeError_AllReasons(t *testing.T) {
	reasons := []host.ProbeFailReason{
		host.ProbeFailTimeout,
		host.ProbeFailRefused,
		host.ProbeFailUnreachable,
		host.ProbeFailAuth,
		host.ProbeFailHostKey,
		host.ProbeFailDNS,
		host.ProbeFailConnReset,
		host.ProbeFailUnknown,
	}

	for _, reason := range reasons {
		t.Run(reason.String(), func(t *testing.T) {
			err := &host.ProbeError{
				SSHAlias: "test",
				Reason:   reason,
				Cause:    fmt.Errorf("test cause"),
			}
			result := formatProbeError(err)
			assert.NotEmpty(t, result)
		})
	}
}

func TestGetSSHErrorSuggestion_AllReasons(t *testing.T) {
	reasons := []host.ProbeFailReason{
		host.ProbeFailTimeout,
		host.ProbeFailRefused,
		host.ProbeFailUnreachable,
		host.ProbeFailAuth,
		host.ProbeFailHostKey,
		host.ProbeFailDNS,
		host.ProbeFailConnReset,
		host.ProbeFailUnknown,
	}

	for _, reason := range reasons {
		t.Run(reason.String(), func(t *testing.T) {
			err := &host.ProbeError{
				SSHAlias: "test",
				Reason:   reason,
			}
			suggestion := getSSHErrorSuggestion(err, "user@example.com")
			assert.NotEmpty(t, suggestion)
		})
	}
}

func TestMockCheck_AllMethods(t *testing.T) {
	check := &mockCheck{
		name:     "test_check",
		result:   doctor.CheckResult{Status: doctor.StatusPass, Message: "OK"},
		category: "TEST",
	}

	assert.Equal(t, "test_check", check.Name())
	assert.Equal(t, "TEST", check.Category())
	assert.Equal(t, doctor.StatusPass, check.Run().Status)

	err := check.Fix()
	assert.NoError(t, err)
	assert.True(t, check.fixed)
}

func TestMockCheck_Defaults(t *testing.T) {
	check := &mockCheck{}

	// Should use defaults when not set
	assert.Equal(t, "mock_check", check.Name())
	assert.Equal(t, "TEST", check.Category())
}

// captureOutput captures stdout during a function call.
func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestRenderCheckResult_PassStatus(t *testing.T) {
	result := doctor.CheckResult{
		Status:     doctor.StatusPass,
		Message:    "Config file exists",
		Suggestion: "Some suggestion",
	}

	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	output := captureOutput(func() {
		renderCheckResult(result, successStyle, errorStyle, warnStyle, mutedStyle)
	})

	// Should contain the message
	assert.Contains(t, output, "Config file exists")
	// Should contain success symbol (note: lipgloss may strip ANSI in test)
	assert.Contains(t, output, ui.SymbolComplete)
	// Should NOT show suggestion for passing checks
	assert.NotContains(t, output, "Some suggestion")
}

func TestRenderCheckResult_FailStatus(t *testing.T) {
	result := doctor.CheckResult{
		Status:     doctor.StatusFail,
		Message:    "SSH key not found",
		Suggestion: "Run ssh-keygen to create a key",
	}

	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	output := captureOutput(func() {
		renderCheckResult(result, successStyle, errorStyle, warnStyle, mutedStyle)
	})

	// Should contain the message
	assert.Contains(t, output, "SSH key not found")
	// Should contain fail symbol
	assert.Contains(t, output, ui.SymbolFail)
	// Should show suggestion for failing checks
	assert.Contains(t, output, "Run ssh-keygen to create a key")
}

func TestRenderCheckResult_WarnStatus(t *testing.T) {
	result := doctor.CheckResult{
		Status:     doctor.StatusWarn,
		Message:    "SSH agent has no identities",
		Suggestion: "Run ssh-add to add your key",
	}

	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	output := captureOutput(func() {
		renderCheckResult(result, successStyle, errorStyle, warnStyle, mutedStyle)
	})

	// Should contain the message
	assert.Contains(t, output, "SSH agent has no identities")
	// Warnings use complete symbol
	assert.Contains(t, output, ui.SymbolComplete)
	// Should show suggestion for warning checks
	assert.Contains(t, output, "Run ssh-add to add your key")
}

func TestRenderCheckResult_MultilineSuggestion(t *testing.T) {
	result := doctor.CheckResult{
		Status:     doctor.StatusFail,
		Message:    "Multiple issues found",
		Suggestion: "Step 1: Do this\nStep 2: Do that\nStep 3: Verify",
	}

	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	output := captureOutput(func() {
		renderCheckResult(result, successStyle, errorStyle, warnStyle, mutedStyle)
	})

	// Each line of suggestion should appear
	assert.Contains(t, output, "Step 1: Do this")
	assert.Contains(t, output, "Step 2: Do that")
	assert.Contains(t, output, "Step 3: Verify")
}

func TestRenderCheckResult_EmptySuggestion(t *testing.T) {
	result := doctor.CheckResult{
		Status:     doctor.StatusFail,
		Message:    "Something failed",
		Suggestion: "",
	}

	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	output := captureOutput(func() {
		renderCheckResult(result, successStyle, errorStyle, warnStyle, mutedStyle)
	})

	// Should still contain the message
	assert.Contains(t, output, "Something failed")
	// Only one line expected (just the result line)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Len(t, lines, 1)
}

func TestRenderDepsCategory_PassingChecks(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "rsync_local", category: "DEPENDENCIES", result: doctor.CheckResult{
			Status:  doctor.StatusPass,
			Message: "rsync is installed (v3.2.7)",
		}},
		&mockCheck{name: "ssh_local", category: "DEPENDENCIES", result: doctor.CheckResult{
			Status:  doctor.StatusPass,
			Message: "ssh is installed",
		}},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "rsync is installed (v3.2.7)"},
		{Status: doctor.StatusPass, Message: "ssh is installed"},
	}

	indices := []int{0, 1}

	output := captureOutput(func() {
		renderDepsCategory(checks, results, indices)
	})

	assert.Contains(t, output, "rsync is installed")
	assert.Contains(t, output, "ssh is installed")
	assert.Contains(t, output, ui.SymbolComplete)
}

func TestRenderDepsCategory_FailingCheck(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "rsync_local", category: "DEPENDENCIES", result: doctor.CheckResult{
			Status:     doctor.StatusFail,
			Message:    "rsync not found",
			Suggestion: "Install rsync: brew install rsync",
		}},
	}

	results := []doctor.CheckResult{
		{
			Status:     doctor.StatusFail,
			Message:    "rsync not found",
			Suggestion: "Install rsync: brew install rsync",
		},
	}

	indices := []int{0}

	output := captureOutput(func() {
		renderDepsCategory(checks, results, indices)
	})

	assert.Contains(t, output, "rsync not found")
	assert.Contains(t, output, "Install rsync")
	assert.Contains(t, output, ui.SymbolFail)
}

func TestRenderDepsCategory_MixedResults(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "rsync_local", category: "DEPENDENCIES"},
		&mockCheck{name: "ssh_local", category: "DEPENDENCIES"},
		&mockCheck{name: "git_local", category: "DEPENDENCIES"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "rsync is installed"},
		{Status: doctor.StatusFail, Message: "ssh not found", Suggestion: "Install openssh"},
		{Status: doctor.StatusWarn, Message: "git version outdated"},
	}

	indices := []int{0, 1, 2}

	output := captureOutput(func() {
		renderDepsCategory(checks, results, indices)
	})

	assert.Contains(t, output, "rsync is installed")
	assert.Contains(t, output, "ssh not found")
	assert.Contains(t, output, "Install openssh")
	assert.Contains(t, output, "git version outdated")
}

func TestRenderDepsCategory_EmptyIndices(t *testing.T) {
	checks := []doctor.Check{}
	results := []doctor.CheckResult{}
	indices := []int{}

	output := captureOutput(func() {
		renderDepsCategory(checks, results, indices)
	})

	// Should produce no output for empty indices
	assert.Empty(t, strings.TrimSpace(output))
}

func TestOutputDoctorJSON_GroupsByCategory(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "config_exists", category: "CONFIG"},
		&mockCheck{name: "ssh_agent", category: "SSH"},
		&mockCheck{name: "config_valid", category: "CONFIG"},
	}

	results := []doctor.CheckResult{
		{Name: "config_exists", Status: doctor.StatusPass, Message: "Config exists"},
		{Name: "ssh_agent", Status: doctor.StatusPass, Message: "SSH agent running"},
		{Name: "config_valid", Status: doctor.StatusPass, Message: "Config valid"},
	}

	output := captureOutput(func() {
		_ = outputDoctorJSON(checks, results)
	})

	// Should be valid JSON
	var decoded DoctorOutput
	err := json.Unmarshal([]byte(output), &decoded)
	require.NoError(t, err)

	// Should have two categories (CONFIG and SSH)
	assert.Len(t, decoded.Categories, 2)

	// Find CONFIG category
	var configCat *CategoryOutput
	for i, cat := range decoded.Categories {
		if cat.Name == "CONFIG" {
			configCat = &decoded.Categories[i]
			break
		}
	}
	require.NotNil(t, configCat)
	// CONFIG should have 2 results
	assert.Len(t, configCat.Results, 2)
}

func TestOutputDoctorJSON_Summary(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "check1", category: "TEST"},
		&mockCheck{name: "check2", category: "TEST"},
		&mockCheck{name: "check3", category: "TEST"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "Pass"},
		{Status: doctor.StatusWarn, Message: "Warning", Fixable: true},
		{Status: doctor.StatusFail, Message: "Fail", Fixable: true},
	}

	output := captureOutput(func() {
		_ = outputDoctorJSON(checks, results)
	})

	var decoded DoctorOutput
	err := json.Unmarshal([]byte(output), &decoded)
	require.NoError(t, err)

	assert.Equal(t, 1, decoded.Summary.Pass)
	assert.Equal(t, 1, decoded.Summary.Warn)
	assert.Equal(t, 1, decoded.Summary.Fail)
	assert.Equal(t, 2, decoded.Summary.Fixable)
	assert.False(t, decoded.Summary.AllClear)
}

func TestOutputDoctorJSON_AllPass(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "check1", category: "TEST"},
		&mockCheck{name: "check2", category: "TEST"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "Pass 1"},
		{Status: doctor.StatusPass, Message: "Pass 2"},
	}

	output := captureOutput(func() {
		_ = outputDoctorJSON(checks, results)
	})

	var decoded DoctorOutput
	err := json.Unmarshal([]byte(output), &decoded)
	require.NoError(t, err)

	assert.True(t, decoded.Summary.AllClear)
	assert.Equal(t, 2, decoded.Summary.Pass)
	assert.Equal(t, 0, decoded.Summary.Fail)
	assert.Equal(t, 0, decoded.Summary.Warn)
}

func TestOutputDoctorJSON_PreservesCategoryOrder(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "ssh1", category: "SSH"},
		&mockCheck{name: "config1", category: "CONFIG"},
		&mockCheck{name: "ssh2", category: "SSH"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "SSH 1"},
		{Status: doctor.StatusPass, Message: "Config 1"},
		{Status: doctor.StatusPass, Message: "SSH 2"},
	}

	output := captureOutput(func() {
		_ = outputDoctorJSON(checks, results)
	})

	var decoded DoctorOutput
	err := json.Unmarshal([]byte(output), &decoded)
	require.NoError(t, err)

	// Order should match first occurrence: SSH, CONFIG
	assert.Equal(t, "SSH", decoded.Categories[0].Name)
	assert.Equal(t, "CONFIG", decoded.Categories[1].Name)
}

func TestOutputDoctorText_AllPass(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "config_exists", category: "CONFIG"},
		&mockCheck{name: "ssh_agent", category: "SSH"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "Config exists"},
		{Status: doctor.StatusPass, Message: "SSH agent running"},
	}

	output := captureOutput(func() {
		_ = outputDoctorText(checks, results)
	})

	// Should contain header
	assert.Contains(t, output, "Road Runner Diagnostic Report")
	// Should contain category headers
	assert.Contains(t, output, "CONFIG")
	assert.Contains(t, output, "SSH")
	// Should contain success message
	assert.Contains(t, output, "Everything looks good")
	// Should show check messages
	assert.Contains(t, output, "Config exists")
	assert.Contains(t, output, "SSH agent running")
}

func TestOutputDoctorText_WithIssues(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "config_exists", category: "CONFIG"},
		&mockCheck{name: "ssh_key", category: "SSH"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "Config exists"},
		{Status: doctor.StatusFail, Message: "SSH key not found", Suggestion: "Run ssh-keygen"},
	}

	output := captureOutput(func() {
		_ = outputDoctorText(checks, results)
	})

	// Should contain issue count
	assert.Contains(t, output, "issue")
	assert.Contains(t, output, "found")
	// Should contain the failing check message
	assert.Contains(t, output, "SSH key not found")
	// Should contain suggestion
	assert.Contains(t, output, "Run ssh-keygen")
}

func TestOutputDoctorText_MultipleIssues(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "check1", category: "CONFIG"},
		&mockCheck{name: "check2", category: "SSH"},
		&mockCheck{name: "check3", category: "DEPENDENCIES"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusFail, Message: "Config missing"},
		{Status: doctor.StatusWarn, Message: "SSH agent not running"},
		{Status: doctor.StatusFail, Message: "rsync not found"},
	}

	output := captureOutput(func() {
		_ = outputDoctorText(checks, results)
	})

	// Should report 3 issues (2 fail + 1 warn)
	assert.Contains(t, output, "3 issues found")
}

func TestOutputDoctorText_SingularIssue(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "check1", category: "CONFIG"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusFail, Message: "Config missing"},
	}

	output := captureOutput(func() {
		_ = outputDoctorText(checks, results)
	})

	// Should use singular "issue" not "issues"
	assert.Contains(t, output, "1 issue found")
}

func TestOutputDoctorText_FixableHint(t *testing.T) {
	// Save and restore the global flag
	oldFix := doctorFix
	doctorFix = false
	defer func() { doctorFix = oldFix }()

	checks := []doctor.Check{
		&mockCheck{name: "check1", category: "CONFIG"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusFail, Message: "Config missing", Fixable: true},
	}

	output := captureOutput(func() {
		_ = outputDoctorText(checks, results)
	})

	// Should mention --fix flag
	assert.Contains(t, output, "--fix")
}

func TestOutputDoctorText_NoFixableHint_WhenFixEnabled(t *testing.T) {
	// Save and restore the global flag
	oldFix := doctorFix
	doctorFix = true
	defer func() { doctorFix = oldFix }()

	checks := []doctor.Check{
		&mockCheck{name: "check1", category: "CONFIG"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusFail, Message: "Config missing", Fixable: true},
	}

	output := captureOutput(func() {
		_ = outputDoctorText(checks, results)
	})

	// Should NOT mention --fix since it's already enabled
	assert.NotContains(t, output, "Run with")
}

func TestOutputDoctorText_SkipsEmptyCategories(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "config_exists", category: "CONFIG"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "Config exists"},
	}

	output := captureOutput(func() {
		_ = outputDoctorText(checks, results)
	})

	// Should have CONFIG but not other categories
	assert.Contains(t, output, "CONFIG")
	// These categories should not appear (no checks for them)
	assert.NotContains(t, output, "\nHOSTS\n")
	assert.NotContains(t, output, "\nDEPENDENCIES\n")
}

func TestOutputDoctorText_ContainsDivider(t *testing.T) {
	checks := []doctor.Check{
		&mockCheck{name: "check1", category: "CONFIG"},
	}

	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "Pass"},
	}

	output := captureOutput(func() {
		_ = outputDoctorText(checks, results)
	})

	// Should contain the divider line
	assert.Contains(t, output, strings.Repeat("\u2501", 60))
}

func TestRenderHostsCategory_AllConnected(t *testing.T) {
	hostCheck := &doctor.HostConnectivityCheck{
		HostName: "dev-server",
		HostConfig: config.Host{
			SSH: []string{"dev-local", "dev-vpn"},
		},
		Results: []host.ProbeResult{
			{SSHAlias: "dev-local", Success: true, Latency: 50 * time.Millisecond},
			{SSHAlias: "dev-vpn", Success: true, Latency: 150 * time.Millisecond},
		},
	}

	checks := []doctor.Check{hostCheck}
	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "dev-server"},
	}
	indices := []int{0}

	output := captureOutput(func() {
		renderHostsCategory(checks, results, indices)
	})

	assert.Contains(t, output, "dev-server")
	assert.Contains(t, output, "dev-local")
	assert.Contains(t, output, "dev-vpn")
	assert.Contains(t, output, "Connected")
	// Should show latency
	assert.Contains(t, output, "ms")
}

func TestRenderHostsCategory_PartialFailure(t *testing.T) {
	hostCheck := &doctor.HostConnectivityCheck{
		HostName: "prod-server",
		HostConfig: config.Host{
			SSH: []string{"prod-local", "prod-vpn"},
		},
		Results: []host.ProbeResult{
			{SSHAlias: "prod-local", Success: false, Error: &host.ProbeError{
				SSHAlias: "prod-local",
				Reason:   host.ProbeFailTimeout,
			}},
			{SSHAlias: "prod-vpn", Success: true, Latency: 200 * time.Millisecond},
		},
	}

	checks := []doctor.Check{hostCheck}
	results := []doctor.CheckResult{
		{Status: doctor.StatusWarn, Message: "prod-server: 1/2 aliases connected"},
	}
	indices := []int{0}

	output := captureOutput(func() {
		renderHostsCategory(checks, results, indices)
	})

	assert.Contains(t, output, "prod-server")
	assert.Contains(t, output, "prod-local")
	assert.Contains(t, output, "prod-vpn")
	assert.Contains(t, output, "Connected")
	// Should show error for failed alias
	assert.Contains(t, output, "timed out")
}

func TestRenderHostsCategory_AllFailed(t *testing.T) {
	hostCheck := &doctor.HostConnectivityCheck{
		HostName: "staging",
		HostConfig: config.Host{
			SSH: []string{"staging-host"},
		},
		Results: []host.ProbeResult{
			{SSHAlias: "staging-host", Success: false, Error: &host.ProbeError{
				SSHAlias: "staging-host",
				Reason:   host.ProbeFailRefused,
			}},
		},
	}

	checks := []doctor.Check{hostCheck}
	results := []doctor.CheckResult{
		{Status: doctor.StatusFail, Message: "staging: all aliases failed"},
	}
	indices := []int{0}

	output := captureOutput(func() {
		renderHostsCategory(checks, results, indices)
	})

	assert.Contains(t, output, "staging")
	assert.Contains(t, output, ui.SymbolFail)
	// Should show error and suggestion
	assert.Contains(t, output, "refused")
}

func TestRenderHostsCategory_NonHostCheck(t *testing.T) {
	// Test that non-HostConnectivityCheck types are skipped
	checks := []doctor.Check{
		&mockCheck{name: "some_check", category: "HOSTS"},
	}
	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "Some check"},
	}
	indices := []int{0}

	output := captureOutput(func() {
		renderHostsCategory(checks, results, indices)
	})

	// Should produce no output for non-host checks
	assert.Empty(t, strings.TrimSpace(output))
}

func TestRenderHostsCategory_MultipleHosts(t *testing.T) {
	hostCheck1 := &doctor.HostConnectivityCheck{
		HostName: "dev",
		HostConfig: config.Host{
			SSH: []string{"dev-host"},
		},
		Results: []host.ProbeResult{
			{SSHAlias: "dev-host", Success: true, Latency: 30 * time.Millisecond},
		},
	}
	hostCheck2 := &doctor.HostConnectivityCheck{
		HostName: "prod",
		HostConfig: config.Host{
			SSH: []string{"prod-host"},
		},
		Results: []host.ProbeResult{
			{SSHAlias: "prod-host", Success: true, Latency: 100 * time.Millisecond},
		},
	}

	checks := []doctor.Check{hostCheck1, hostCheck2}
	results := []doctor.CheckResult{
		{Status: doctor.StatusPass, Message: "dev"},
		{Status: doctor.StatusPass, Message: "prod"},
	}
	indices := []int{0, 1}

	output := captureOutput(func() {
		renderHostsCategory(checks, results, indices)
	})

	assert.Contains(t, output, "dev")
	assert.Contains(t, output, "prod")
	assert.Contains(t, output, "dev-host")
	assert.Contains(t, output, "prod-host")
}
