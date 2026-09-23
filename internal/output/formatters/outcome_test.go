package formatters

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pytestFailureLog = `============================= test session starts ==============================
collected 3 items

tests/test_math.py::test_add PASSED [33%]
tests/test_math.py::test_sub PASSED [66%]
tests/test_math.py::test_div FAILED [100%]

=================================== FAILURES ===================================
_________________________________ test_div _________________________________

    def test_div():
>       assert divide(1, 0) == 0
E       ZeroDivisionError: division by zero

tests/test_math.py:12: ZeroDivisionError
=========================== short test summary info ============================
FAILED tests/test_math.py::test_div - ZeroDivisionError: division by zero
========================= 1 failed, 2 passed in 0.03s ==========================
`

func TestParseRunOutcome(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		log          string
		wantSummary  *TestSummary
		wantFailures int
		wantNoTests  bool
		wantPiped    bool
	}{
		{
			name:         "pytest with a failure",
			command:      "pytest tests/",
			log:          pytestFailureLog,
			wantSummary:  &TestSummary{Passed: 2, Failed: 1},
			wantFailures: 1,
		},
		{
			name:        "pytest collected nothing",
			command:     "pytest -k typo",
			log:         "collected 0 items\n\n===== no tests ran in 0.05s =====\n",
			wantSummary: &TestSummary{NoTests: true},
			wantNoTests: true,
		},
		{
			name:        "piped zero-test run",
			command:     "pytest -k typo | tail -5",
			log:         "collected 0 items\n\n===== no tests ran in 0.05s =====\n",
			wantSummary: &TestSummary{NoTests: true},
			wantNoTests: true,
			wantPiped:   true,
		},
		{
			name:        "collect-only in the effective command is intentional",
			command:     "pytest tests/ --collect-only",
			log:         "collected 0 items\n\n===== no tests ran in 0.05s =====\n",
			wantSummary: nil,
		},
		{
			name:    "unrecognized output",
			command: "make build",
			log:     "compiling...\ndone\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := ParseRunOutcome(tt.command, []byte(tt.log))
			assert.Equal(t, tt.wantSummary, o.Summary)
			assert.Len(t, o.Failures, tt.wantFailures)
			assert.Equal(t, tt.wantNoTests, o.NoTests)
			assert.Equal(t, tt.wantPiped, o.PipedExitCode)
		})
	}
}

// TestParseRunOutcome_FailureDetail checks failures keep file, line, and the
// full message: truncation is the renderer's job, not the parser's.
func TestParseRunOutcome_FailureDetail(t *testing.T) {
	long := strings.Repeat("x", 2000)
	log := strings.Replace(pytestFailureLog, "E       ZeroDivisionError: division by zero",
		"E       ZeroDivisionError: "+long, 1)

	o := ParseRunOutcome("pytest tests/", []byte(log))
	require.Len(t, o.Failures, 1)
	f := o.Failures[0]
	assert.Equal(t, "test_div", f.TestName)
	assert.Equal(t, "tests/test_math.py", f.File)
	assert.Equal(t, 12, f.Line)
	assert.Contains(t, f.Message, long)
}

// TestParseRunOutcome_MatchesExtractors pins that the single-pass parser
// agrees with the standalone extractors it replaces for single runs.
func TestParseRunOutcome_MatchesExtractors(t *testing.T) {
	o := ParseRunOutcome("pytest tests/", []byte(pytestFailureLog))
	summary, ok := ExtractTestSummary("pytest tests/", []byte(pytestFailureLog))
	require.True(t, ok)
	require.NotNil(t, o.Summary)
	assert.Equal(t, summary, *o.Summary)
	assert.Equal(t, ExtractFailures("pytest tests/", []byte(pytestFailureLog)), o.Failures)
}
