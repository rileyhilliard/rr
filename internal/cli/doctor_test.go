package cli

import (
	"encoding/json"
	"fmt"
	"testing"

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
