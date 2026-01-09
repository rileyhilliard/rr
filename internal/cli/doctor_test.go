package cli

import (
	"encoding/json"
	"testing"

	"github.com/rileyhilliard/rr/internal/doctor"
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
