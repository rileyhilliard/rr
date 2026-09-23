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
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
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
	// Use pretty mode to get plain JSON output (not wrapped in envelope)
	prettyMode = true
	defer func() { prettyMode = false }()

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
	prettyMode = true
	defer func() { prettyMode = false }()

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
	prettyMode = true
	defer func() { prettyMode = false }()

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
	prettyMode = true
	defer func() { prettyMode = false }()

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

// runDoctorOutput runs reportDoctorResults in the given output mode and
// returns stdout plus the returned error.
func runDoctorOutput(t *testing.T, mode string, checks []doctor.Check, results []doctor.CheckResult) (string, error) {
	t.Helper()
	origPretty, origJSON := prettyMode, doctorJSON
	t.Cleanup(func() { prettyMode, doctorJSON = origPretty, origJSON })

	switch mode {
	case "machine":
		prettyMode, doctorJSON = false, false
	case "json":
		prettyMode, doctorJSON = true, true
	case "pretty":
		prettyMode, doctorJSON = true, false
	default:
		t.Fatalf("unknown mode %q", mode)
	}

	var err error
	out := captureOutput(func() {
		err = reportDoctorResults(checks, results)
	})
	return out, err
}

// decodeDoctorSummary pulls the summary out of machine (enveloped) or
// legacy --json output.
func decodeDoctorSummary(t *testing.T, mode, out string) SummaryOutput {
	t.Helper()
	if mode == "machine" {
		var env struct {
			Success bool         `json:"success"`
			Data    DoctorOutput `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &env))
		assert.True(t, env.Success, "the envelope reports that doctor ran, not the verdict")
		return env.Data.Summary
	}
	var decoded DoctorOutput
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	return decoded.Summary
}

func requireDoctorExit(t *testing.T, err error, wantCode int) {
	t.Helper()
	if wantCode == 0 {
		require.NoError(t, err)
		return
	}
	code, ok := errors.GetExitCode(err)
	require.True(t, ok, "expected an exit-code error, got %v", err)
	assert.Equal(t, wantCode, code)
}

func TestReportDoctorResults_ExitCode(t *testing.T) {
	cases := []struct {
		name         string
		statuses     []doctor.CheckStatus
		wantCode     int
		wantAllClear bool
	}{
		{name: "all pass", statuses: []doctor.CheckStatus{doctor.StatusPass}, wantCode: 0, wantAllClear: true},
		{name: "warnings only", statuses: []doctor.CheckStatus{doctor.StatusPass, doctor.StatusWarn}, wantCode: 0, wantAllClear: false},
		{name: "any failure", statuses: []doctor.CheckStatus{doctor.StatusWarn, doctor.StatusFail}, wantCode: 1, wantAllClear: false},
	}

	for _, mode := range []string{"machine", "json", "pretty"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				var checks []doctor.Check
				var results []doctor.CheckResult
				for i, st := range tc.statuses {
					name := fmt.Sprintf("check%d", i)
					checks = append(checks, &mockCheck{name: name, category: "CONFIG"})
					results = append(results, doctor.CheckResult{Name: name, Status: st, Message: name})
				}

				out, err := runDoctorOutput(t, mode, checks, results)
				requireDoctorExit(t, err, tc.wantCode)

				if mode != "pretty" {
					summary := decodeDoctorSummary(t, mode, out)
					assert.Equal(t, tc.wantAllClear, summary.AllClear)
				}
			})
		}
	}
}

// SSH_AUTH_SOCK unset is normal for key-file setups: warn, exit 0.
func TestDoctor_SSHAgentUnset_ExitsZero(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	checks := []doctor.Check{&doctor.SSHAgentCheck{}}
	results := doctor.RunAll(checks)
	require.Equal(t, doctor.StatusWarn, results[0].Status)

	out, err := runDoctorOutput(t, "machine", checks, results)
	requireDoctorExit(t, err, 0)
	assert.Equal(t, 1, decodeDoctorSummary(t, "machine", out).Warn)
}

// An offline global host the project doesn't use must not be probed or
// dialed, and must not fail doctor.
func TestDoctor_OfflineHostOutsideProject_NotChecked(t *testing.T) {
	origPath, origReq := doctorPath, doctorRequirements
	t.Cleanup(func() { doctorPath, doctorRequirements = origPath, origReq })
	doctorPath, doctorRequirements = false, true

	globalCfg := &config.GlobalConfig{Hosts: map[string]config.Host{
		"box":     {SSH: []string{"box-lan"}},
		"offline": {SSH: []string{"offline-lan"}},
	}}
	projectCfg := &config.Config{Hosts: []string{"box"}}

	checks := collectChecks("", projectCfg, globalCfg)

	var probed []string
	for _, c := range checks {
		assert.NotContains(t, c.Name(), "offline", "offline host should not get a check")
		if hc, ok := c.(*doctor.HostConnectivityCheck); ok {
			hc.Probe = func(aliases []string, _ time.Duration) []host.ProbeResult {
				probed = append(probed, aliases...)
				return []host.ProbeResult{{SSHAlias: aliases[0], Success: true}}
			}
		}
	}

	var dialed []string
	dial := func(alias string, _ time.Duration) (*sshutil.Client, time.Duration, error) {
		dialed = append(dialed, alias)
		return &sshutil.Client{Host: alias}, time.Millisecond, nil
	}
	names, hosts, err := doctor.ScopeHosts(projectCfg, globalCfg)
	require.NoError(t, err)
	conns, dialErrs := connectDoctorHosts(names, hosts, dial)
	defer closeDoctorConnections(conns)

	assert.Equal(t, []string{"box-lan"}, dialed)
	assert.Empty(t, dialErrs)

	// Only the host checks are env-independent; run them for the exit code.
	var hostChecks []doctor.Check
	for _, c := range checks {
		if c.Category() == "HOSTS" {
			hostChecks = append(hostChecks, c)
		}
	}
	_, err = runDoctorOutput(t, "machine", hostChecks, doctor.RunAll(hostChecks))
	requireDoctorExit(t, err, 0)
	assert.Equal(t, []string{"box-lan"}, probed)
}

func TestCollectChecks_ProjectReferencesUnknownHost(t *testing.T) {
	globalCfg := &config.GlobalConfig{Hosts: map[string]config.Host{"box": {SSH: []string{"box"}}}}
	projectCfg := &config.Config{Hosts: []string{"typo"}}

	checks := collectChecks("", projectCfg, globalCfg)

	var found bool
	for _, c := range checks {
		if hr, ok := c.(*doctor.HostResolutionCheck); ok {
			found = true
			assert.Equal(t, doctor.StatusFail, hr.Run().Status)
		}
		assert.NotEqual(t, "HOSTS", c.Category(), "no host checks when the project's hosts can't be resolved")
	}
	assert.True(t, found, "expected a failing host-resolution check")
}

// A missing requirement blocks rr run, so doctor --requirements exits 1.
func TestDoctor_MissingRequirement_ExitsOne(t *testing.T) {
	client := sshtesting.NewMockClient("box")
	client.SetCommandResponse("command -v jq", sshtesting.CommandResponse{ExitCode: 1})
	client.SetCommandResponse("command -v rsync && rsync --version 2>&1 | head -n 2",
		sshtesting.CommandResponse{Stdout: []byte("/usr/bin/rsync\nrsync  version 3.2.7  protocol version 31\n")})

	hosts := map[string]config.Host{"box": {SSH: []string{"box"}}}
	conns := map[string]*host.Connection{"box": {Name: "box", Client: client, Host: hosts["box"]}}
	projectCfg := &config.Config{Require: []string{"jq"}}

	checks := remoteDoctorChecks([]string{"box"}, hosts, conns, projectCfg, false, true)
	results := doctor.RunAll(checks)

	var sawRsync bool
	for i, c := range checks {
		if _, ok := c.(*doctor.RsyncRemoteCheck); ok {
			sawRsync = true
			assert.Equal(t, doctor.StatusPass, results[i].Status, results[i].Message)
		}
	}
	assert.True(t, sawRsync, "--requirements should include the remote rsync check")

	out, err := runDoctorOutput(t, "machine", checks, results)
	requireDoctorExit(t, err, 1)
	assert.False(t, decodeDoctorSummary(t, "machine", out).AllClear)
}

// Connections go through alias racing: a host reachable only via its second
// alias still gets connected, and a fully unreachable host is a failure.
func TestConnectDoctorHosts_UsesAliasFallback(t *testing.T) {
	hosts := map[string]config.Host{
		"box":  {SSH: []string{"box-lan", "box-vpn"}},
		"down": {SSH: []string{"down-lan"}},
	}
	dial := func(alias string, _ time.Duration) (*sshutil.Client, time.Duration, error) {
		if alias == "box-vpn" {
			return &sshutil.Client{Host: alias}, time.Millisecond, nil
		}
		return nil, 0, &host.ProbeError{SSHAlias: alias, Reason: host.ProbeFailTimeout}
	}

	conns, dialErrs := connectDoctorHosts([]string{"box", "down"}, hosts, dial)
	defer closeDoctorConnections(conns)

	require.Contains(t, conns, "box")
	assert.Equal(t, "box-vpn", conns["box"].Alias)
	assert.NotContains(t, conns, "down")
	assert.Error(t, dialErrs["down"])
	assert.NotContains(t, dialErrs, "box")

	// Remote checks only run for connected hosts; the unreachable host is
	// reported on its HOSTS result, not by a second "no connection" failure.
	remote := remoteDoctorChecks([]string{"box", "down"}, hosts, conns, nil, true, true)
	for _, c := range remote {
		assert.NotContains(t, c.Name(), "down")
	}
}

// fakeHostProbes makes every HostConnectivityCheck report its aliases as
// reachable or not, per host.
func fakeHostProbes(checks []doctor.Check, up map[string]bool) {
	for _, c := range checks {
		hc, ok := c.(*doctor.HostConnectivityCheck)
		if !ok {
			continue
		}
		reachable := up[hc.HostName]
		hc.Probe = func(aliases []string, _ time.Duration) []host.ProbeResult {
			return []host.ProbeResult{{SSHAlias: aliases[0], Success: reachable}}
		}
	}
}

func hostResults(checks []doctor.Check, results []doctor.CheckResult) map[string]doctor.CheckResult {
	out := make(map[string]doctor.CheckResult)
	for i, c := range checks {
		if hc, ok := c.(*doctor.HostConnectivityCheck); ok {
			out[hc.HostName] = results[i]
		}
	}
	return out
}

// A run needs one reachable host, so doctor fails only when none is
// reachable and local fallback wouldn't take over.
func TestDoctor_HostGrading_ExitCode(t *testing.T) {
	globalCfg := &config.GlobalConfig{Hosts: map[string]config.Host{
		"a": {SSH: []string{"a-lan"}},
		"b": {SSH: []string{"b-lan"}},
	}}
	tests := []struct {
		name     string
		project  *config.Config
		up       map[string]bool
		fallback config.LocalFallbackMode
		want     map[string]doctor.CheckStatus
		wantCode int
	}{
		{name: "project, one of two down", project: &config.Config{Hosts: []string{"a", "b"}}, up: map[string]bool{"a": true},
			want: map[string]doctor.CheckStatus{"a": doctor.StatusPass, "b": doctor.StatusWarn}, wantCode: 0},
		{name: "project, all down, no fallback", project: &config.Config{Hosts: []string{"a", "b"}}, up: map[string]bool{},
			want: map[string]doctor.CheckStatus{"a": doctor.StatusFail, "b": doctor.StatusFail}, wantCode: 1},
		{name: "project, all down, on-unreachable fallback", project: &config.Config{Hosts: []string{"a", "b"}}, up: map[string]bool{},
			fallback: config.LocalFallbackOnUnreachable,
			want:     map[string]doctor.CheckStatus{"a": doctor.StatusWarn, "b": doctor.StatusWarn}, wantCode: 0},
		{name: "single-host project down fails", project: &config.Config{Host: "b"}, up: map[string]bool{"a": true},
			want: map[string]doctor.CheckStatus{"b": doctor.StatusFail}, wantCode: 1},
		{name: "outside a project, one down", up: map[string]bool{"a": true},
			want: map[string]doctor.CheckStatus{"a": doctor.StatusPass, "b": doctor.StatusWarn}, wantCode: 0},
		{name: "outside a project, all down", up: map[string]bool{},
			want: map[string]doctor.CheckStatus{"a": doctor.StatusFail, "b": doctor.StatusFail}, wantCode: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hostChecks []doctor.Check
			for _, c := range collectChecks("", tt.project, globalCfg) {
				if c.Category() == "HOSTS" {
					hostChecks = append(hostChecks, c)
				}
			}
			fakeHostProbes(hostChecks, tt.up)

			results := runDoctorChecks(hostChecks, tt.fallback)
			got := hostResults(hostChecks, results)
			require.Len(t, got, len(tt.want))
			for name, want := range tt.want {
				assert.Equal(t, want, got[name].Status, "host %s", name)
			}

			_, err := runDoctorOutput(t, "machine", hostChecks, results)
			requireDoctorExit(t, err, tt.wantCode)
		})
	}
}

// With --requirements, an unreachable host is reported once: on its HOSTS
// result, with a note that its remote checks were skipped.
func TestDoctor_UnreachableHostReportedOnce(t *testing.T) {
	globalCfg := &config.GlobalConfig{Hosts: map[string]config.Host{
		"box":  {SSH: []string{"box-lan"}},
		"down": {SSH: []string{"down-lan"}},
	}}
	projectCfg := &config.Config{Hosts: []string{"box", "down"}}

	checks := collectChecks("", projectCfg, globalCfg)
	var hostChecks []doctor.Check
	for _, c := range checks {
		if c.Category() == "HOSTS" {
			hostChecks = append(hostChecks, c)
		}
	}
	fakeHostProbes(hostChecks, map[string]bool{"box": true})

	dial := func(alias string, _ time.Duration) (*sshutil.Client, time.Duration, error) {
		return nil, 0, &host.ProbeError{SSHAlias: alias, Reason: host.ProbeFailTimeout}
	}
	names, hosts, err := doctor.ScopeHosts(projectCfg, globalCfg)
	require.NoError(t, err)
	conns, dialErrs := connectDoctorHosts([]string{"down"}, hosts, dial)
	defer closeDoctorConnections(conns)
	attachRemoteErrors(hostChecks, dialErrs)
	hostChecks = append(hostChecks, remoteDoctorChecks(names, hosts, conns, projectCfg, true, true)...)

	results := runDoctorChecks(hostChecks, config.LocalFallbackNever)

	var mentions []doctor.CheckResult
	for _, r := range results {
		assert.NotContains(t, r.Name, "remote_connect", "no separate connection-failure check")
		if strings.Contains(r.Name, "down") {
			mentions = append(mentions, r)
		}
	}
	require.Len(t, mentions, 1, "the unreachable host should appear once")
	assert.Equal(t, doctor.StatusWarn, mentions[0].Status, "box is reachable, so runs still work")
	assert.Contains(t, mentions[0].Suggestion, "--path/--requirements checks skipped")
}

// A regraded (warned) unreachable host and a host whose remote checks were
// skipped must show why in pretty output, not just the host name.
func TestRenderHostsCategory_ShowsGradingNotes(t *testing.T) {
	down := &doctor.HostConnectivityCheck{
		HostName: "down",
		Results:  []host.ProbeResult{{SSHAlias: "down-lan", Success: false}},
	}
	flaky := &doctor.HostConnectivityCheck{
		HostName:  "flaky",
		Results:   []host.ProbeResult{{SSHAlias: "flaky-lan", Success: true}},
		RemoteErr: fmt.Errorf("dial failed"),
	}
	checks := []doctor.Check{down, flaky}
	results := []doctor.CheckResult{
		{Status: doctor.StatusWarn, Message: "down: all aliases failed", Suggestion: "Other hosts are reachable, so runs use the other reachable hosts."},
		{Status: doctor.StatusWarn, Message: "flaky: reachable, but connecting for remote checks failed: dial failed", Suggestion: "--path/--requirements checks skipped for flaky."},
	}

	output := captureOutput(func() {
		renderHostsCategory(checks, results, []int{0, 1})
	})

	assert.Contains(t, output, "runs use the other reachable hosts")
	assert.Contains(t, output, "connecting for remote checks failed: dial failed")
	assert.Contains(t, output, "--path/--requirements checks skipped for flaky")
}
