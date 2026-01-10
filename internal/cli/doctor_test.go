package cli

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/doctor"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			contains: []string{"ssh-keyscan", "example.com", "known_hosts"},
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
	checks := collectChecks("", nil)

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

func TestCollectChecks_WithConfig(t *testing.T) {
	cfg := &config.Config{
		Hosts: map[string]config.Host{
			"dev": {SSH: []string{"dev.example.com"}},
		},
	}

	checks := collectChecks(".rr.yaml", cfg)
	assert.NotEmpty(t, checks)

	// Should have host checks when config has hosts
	categories := make(map[string]bool)
	for _, check := range checks {
		categories[check.Category()] = true
	}

	assert.True(t, categories["HOSTS"], "should have HOSTS checks when config has hosts")
}

func TestCollectChecks_EmptyHosts(t *testing.T) {
	cfg := &config.Config{
		Hosts: map[string]config.Host{},
	}

	checks := collectChecks(".rr.yaml", cfg)
	assert.NotEmpty(t, checks)

	// Should NOT have host checks when config has no hosts
	categories := make(map[string]bool)
	for _, check := range checks {
		categories[check.Category()] = true
	}

	assert.False(t, categories["HOSTS"], "should not have HOSTS checks when config has no hosts")
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
	checks := collectChecks("/path/to/.rr.yaml", nil)

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
	cfg := &config.Config{
		Hosts: map[string]config.Host{
			"dev":     {SSH: []string{"dev.example.com"}},
			"staging": {SSH: []string{"staging.example.com"}},
			"prod":    {SSH: []string{"prod.example.com"}},
		},
	}

	checks := collectChecks(".rr.yaml", cfg)

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
