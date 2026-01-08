package doctor

import (
	"testing"
)

func TestCheckStatus_String(t *testing.T) {
	tests := []struct {
		status   CheckStatus
		expected string
	}{
		{StatusPass, "pass"},
		{StatusWarn, "warn"},
		{StatusFail, "fail"},
		{CheckStatus(99), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			if got := tc.status.String(); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// mockCheck is a test implementation of Check.
type mockCheck struct {
	name     string
	category string
	result   CheckResult
	fixErr   error
	fixCalls int
}

func (m *mockCheck) Name() string     { return m.name }
func (m *mockCheck) Category() string { return m.category }
func (m *mockCheck) Run() CheckResult { return m.result }
func (m *mockCheck) Fix() error {
	m.fixCalls++
	return m.fixErr
}

func TestRunAll(t *testing.T) {
	checks := []Check{
		&mockCheck{
			name:     "check1",
			category: "TEST",
			result:   CheckResult{Name: "check1", Status: StatusPass, Message: "OK"},
		},
		&mockCheck{
			name:     "check2",
			category: "TEST",
			result:   CheckResult{Name: "check2", Status: StatusFail, Message: "Failed"},
		},
	}

	results := RunAll(checks)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Status != StatusPass {
		t.Errorf("expected first check to pass")
	}
	if results[1].Status != StatusFail {
		t.Errorf("expected second check to fail")
	}
}

func TestRunAllParallel(t *testing.T) {
	checks := []Check{
		&mockCheck{
			name:     "check1",
			category: "TEST",
			result:   CheckResult{Name: "check1", Status: StatusPass},
		},
		&mockCheck{
			name:     "check2",
			category: "TEST",
			result:   CheckResult{Name: "check2", Status: StatusWarn},
		},
		&mockCheck{
			name:     "check3",
			category: "TEST",
			result:   CheckResult{Name: "check3", Status: StatusFail},
		},
	}

	results := RunAllParallel(checks)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify all results are present (order should be preserved)
	if results[0].Status != StatusPass {
		t.Errorf("expected first result to be pass")
	}
	if results[1].Status != StatusWarn {
		t.Errorf("expected second result to be warn")
	}
	if results[2].Status != StatusFail {
		t.Errorf("expected third result to be fail")
	}
}

func TestGroupByCategory(t *testing.T) {
	checks := []Check{
		&mockCheck{name: "c1", category: "A"},
		&mockCheck{name: "c2", category: "B"},
		&mockCheck{name: "c3", category: "A"},
	}

	grouped := GroupByCategory(checks)

	if len(grouped["A"]) != 2 {
		t.Errorf("expected 2 checks in category A, got %d", len(grouped["A"]))
	}
	if len(grouped["B"]) != 1 {
		t.Errorf("expected 1 check in category B, got %d", len(grouped["B"]))
	}
}

func TestCountByStatus(t *testing.T) {
	results := []CheckResult{
		{Status: StatusPass},
		{Status: StatusPass},
		{Status: StatusWarn},
		{Status: StatusFail},
	}

	counts := CountByStatus(results)

	if counts[StatusPass] != 2 {
		t.Errorf("expected 2 pass, got %d", counts[StatusPass])
	}
	if counts[StatusWarn] != 1 {
		t.Errorf("expected 1 warn, got %d", counts[StatusWarn])
	}
	if counts[StatusFail] != 1 {
		t.Errorf("expected 1 fail, got %d", counts[StatusFail])
	}
}

func TestHasFailures(t *testing.T) {
	tests := []struct {
		name     string
		results  []CheckResult
		expected bool
	}{
		{
			name:     "all pass",
			results:  []CheckResult{{Status: StatusPass}, {Status: StatusPass}},
			expected: false,
		},
		{
			name:     "with warn only",
			results:  []CheckResult{{Status: StatusPass}, {Status: StatusWarn}},
			expected: false,
		},
		{
			name:     "with fail",
			results:  []CheckResult{{Status: StatusPass}, {Status: StatusFail}},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasFailures(tc.results); got != tc.expected {
				t.Errorf("HasFailures() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestHasIssues(t *testing.T) {
	tests := []struct {
		name     string
		results  []CheckResult
		expected bool
	}{
		{
			name:     "all pass",
			results:  []CheckResult{{Status: StatusPass}, {Status: StatusPass}},
			expected: false,
		},
		{
			name:     "with warn",
			results:  []CheckResult{{Status: StatusPass}, {Status: StatusWarn}},
			expected: true,
		},
		{
			name:     "with fail",
			results:  []CheckResult{{Status: StatusPass}, {Status: StatusFail}},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasIssues(tc.results); got != tc.expected {
				t.Errorf("HasIssues() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestFixableCount(t *testing.T) {
	results := []CheckResult{
		{Status: StatusPass, Fixable: true},  // Pass, not counted
		{Status: StatusFail, Fixable: true},  // Counted
		{Status: StatusFail, Fixable: false}, // Not counted
		{Status: StatusWarn, Fixable: true},  // Counted
	}

	if got := FixableCount(results); got != 2 {
		t.Errorf("FixableCount() = %d, want 2", got)
	}
}

func TestSummary(t *testing.T) {
	tests := []struct {
		name     string
		results  []CheckResult
		contains string
	}{
		{
			name:     "all good",
			results:  []CheckResult{{Status: StatusPass}},
			contains: "Everything looks good",
		},
		{
			name:     "one issue",
			results:  []CheckResult{{Status: StatusFail}},
			contains: "1 issue found",
		},
		{
			name:     "multiple issues",
			results:  []CheckResult{{Status: StatusFail}, {Status: StatusWarn}},
			contains: "2 issues found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Summary(tc.results)
			if got != tc.contains {
				t.Errorf("Summary() = %q, want %q", got, tc.contains)
			}
		})
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		n        int
		expected string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{10, "s"},
	}

	for _, tc := range tests {
		if got := pluralize(tc.n); got != tc.expected {
			t.Errorf("pluralize(%d) = %q, want %q", tc.n, got, tc.expected)
		}
	}
}
