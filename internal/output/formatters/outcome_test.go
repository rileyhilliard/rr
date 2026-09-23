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
			name:        "pytest per-test lines",
			command:     "pytest tests/",
			log:         "collected 3 items\n\ntests/t.py::test_pass PASSED [33%]\ntests/t.py::test_fail FAILED [66%]\ntests/t.py::test_skip SKIPPED [100%]\n===== 1 failed, 1 passed, 1 skipped in 0.03s =====\n",
			wantSummary: &TestSummary{Passed: 1, Failed: 1, Skipped: 1},
		},
		{
			// -q/-qq emit no per-test lines and an undecorated summary;
			// counts must come from the bare summary line.
			name:        "pytest -qq bare summary",
			command:     "bash -c 'cd opendata && uv run pytest \"$@\" -n 4 --no-cov -qq --tb=short' rr tests/foo.py",
			log:         "bringing up nodes...\n5 passed in 4.20s\n",
			wantSummary: &TestSummary{Passed: 5},
		},
		{
			name:        "pytest -qq bare summary with failures",
			command:     "bash -c 'cd opendata && uv run pytest \"$@\" -n 4 --no-cov -qq --tb=short' rr tests/foo.py",
			log:         "bringing up nodes...\n2 failed, 3 passed, 1 skipped in 1.10s\n",
			wantSummary: &TestSummary{Passed: 3, Failed: 2, Skipped: 1},
		},
		{
			name:         "go test",
			command:      "go test ./...",
			log:          "=== RUN   TestExample\n--- PASS: TestExample (0.00s)\n=== RUN   TestFail\n    example_test.go:15: Expected 1, got 2\n--- FAIL: TestFail (0.00s)\nFAIL\nexit status 1\nFAIL\texample\t0.005s\n",
			wantSummary:  &TestSummary{Passed: 1, Failed: 1},
			wantFailures: 1,
		},
		{
			// A passing vitest/jest run used to yield no summary at all.
			name:        "jest passing run",
			command:     "bunx jest",
			log:         " PASS  src/utils.test.ts\n   ✓ works (2ms)\n\nTest Suites: 1 passed, 1 total\nTests:       5 passed, 5 total\n",
			wantSummary: &TestSummary{Passed: 5},
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

// TestParseRunOutcome_MatchesExtractFailures pins that the single-pass
// parser finds the same failures as ExtractFailures, which the parallel
// summary still uses.
func TestParseRunOutcome_MatchesExtractFailures(t *testing.T) {
	o := ParseRunOutcome("pytest tests/", []byte(pytestFailureLog))
	assert.Equal(t, ExtractFailures("pytest tests/", []byte(pytestFailureLog)), o.Failures)
}

// TestParseRunOutcome_NoTests is the regression suite for false-green test
// runs: a command that executed zero tests previously reported success with
// no signal at all in the envelope. The "must not flag" cases matter as much
// as the positive ones - flagging an intentional zero-test run
// (--collect-only) or a normal dev-loop invocation (go test -run NoMatch)
// would be a false red.
func TestParseRunOutcome_NoTests(t *testing.T) {
	tests := []struct {
		name    string
		command string
		output  string
		want    bool
	}{
		{
			name:    "pytest decorated no tests ran",
			command: "uv run pytest tests/x.py -k classify",
			output:  "collected 0 items\n\n===== no tests ran in 0.05s =====",
			want:    true,
		},
		{
			name:    "pytest quiet bare no tests ran",
			command: "uv run pytest tests/x.py -q -k classify",
			output:  "no tests ran in 0.05s",
			want:    true,
		},
		{
			name:    "vitest reports no tests",
			command: "bunx vitest run tests/tracking.test.ts",
			output:  " Test Files  1 failed (1)\n      Tests  no tests",
			want:    true,
		},
		{
			name:    "go test where every package lacks tests",
			command: "go test ./...",
			output:  "?   \tgithub.com/example/a\t[no test files]\n?   \tgithub.com/example/b\t[no test files]",
			want:    true,
		},
		{
			name:    "pytest collect-only collects but runs nothing by design",
			command: "pytest --collect-only tests/",
			output:  "collected 12 items\n\n12 tests collected in 0.01s",
			want:    false,
		},
		{
			name:    "go test -run with no match is a normal dev loop",
			command: "go test ./internal/lock/... -run TestNope",
			output:  "ok  \tgithub.com/example/lock\t0.002s",
			want:    false,
		},
		{
			name:    "jest passWithNoTests opts into zero tests",
			command: "jest --passWithNoTests",
			output:  "No tests found, exiting with code 0",
			want:    false,
		},
		{
			name:    "passing pytest run",
			command: "pytest tests/",
			output:  "collected 3 items\n\n===== 3 passed in 0.10s =====",
			want:    false,
		},
		{
			name:    "all-skipped run is not a no-tests run",
			command: "pytest tests/",
			output:  "collected 3 items\n\n===== 3 skipped in 0.05s =====",
			want:    false,
		},
		{
			name:    "go test mixing no-test packages with real passes",
			command: "go test ./...",
			output:  "?   \tgithub.com/example/a\t[no test files]\nok  \tgithub.com/example/b\t0.002s",
			want:    false,
		},
		{
			name:    "installing pytest is not a test run",
			command: "pip install pytest",
			output:  "Successfully installed pytest-7.4.0",
			want:    false,
		},
		{
			name:    "grepping for pytest is not a test run",
			command: "grep -r pytest .",
			output:  "conftest.py:import pytest",
			want:    false,
		},
		{
			name:    "unrecognized command",
			command: "make build",
			output:  "building...",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseRunOutcome(tt.command, []byte(tt.output)).NoTests)
		})
	}
}
