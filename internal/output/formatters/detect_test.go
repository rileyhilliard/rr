package formatters

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractFailures_Pytest(t *testing.T) {
	command := "pytest tests/"
	output := []byte(`
============================= test session starts ==============================
collected 3 items

tests/test_example.py::test_pass PASSED [33%]
tests/test_example.py::test_fail FAILED [66%]
tests/test_example.py::test_skip SKIPPED [100%]

=================================== FAILURES ===================================
_________________________________ test_fail ________________________________

    def test_fail():
>       assert 1 == 2
E       AssertionError: assert 1 == 2

tests/test_example.py:5: AssertionError
=========================== short test summary info ============================
FAILED tests/test_example.py::test_fail - AssertionError: assert 1 == 2
========================= 1 failed, 1 passed, 1 skipped in 0.03s ==========================
`)

	failures := ExtractFailures(command, output)

	assert.Len(t, failures, 1)
	assert.Equal(t, "test_fail", failures[0].TestName)
	assert.Equal(t, "tests/test_example.py", failures[0].File)
	assert.Equal(t, 5, failures[0].Line)
	assert.Contains(t, failures[0].Message, "AssertionError")
}

func TestExtractFailures_GoTest(t *testing.T) {
	command := "go test ./..."
	output := []byte(`
=== RUN   TestExample
--- PASS: TestExample (0.00s)
=== RUN   TestFail
    example_test.go:15: Expected 1, got 2
--- FAIL: TestFail (0.00s)
FAIL
exit status 1
FAIL	example	0.005s
`)

	failures := ExtractFailures(command, output)

	assert.Len(t, failures, 1)
	assert.Equal(t, "TestFail", failures[0].TestName)
	assert.Contains(t, failures[0].Message, "Expected 1, got 2")
}

func TestExtractFailures_UnknownFormat(t *testing.T) {
	command := "some-random-command"
	output := []byte("Some random output that doesn't match any known test format")

	failures := ExtractFailures(command, output)

	assert.Nil(t, failures)
}

func TestFormatFailureSummary_LimitFailures(t *testing.T) {
	command := "pytest tests/"
	output := []byte(`
============================= test session starts ==============================
collected 5 items

tests/test_example.py::test_a FAILED [20%]
tests/test_example.py::test_b FAILED [40%]
tests/test_example.py::test_c FAILED [60%]
tests/test_example.py::test_d FAILED [80%]
tests/test_example.py::test_e FAILED [100%]

=================================== FAILURES ===================================
_________________________________ test_a _________________________________

    def test_a():
>       assert False
E       AssertionError

tests/test_example.py:2: AssertionError
_________________________________ test_b _________________________________

    def test_b():
>       assert False
E       AssertionError

tests/test_example.py:5: AssertionError
_________________________________ test_c _________________________________

    def test_c():
>       assert False
E       AssertionError

tests/test_example.py:8: AssertionError
_________________________________ test_d _________________________________

    def test_d():
>       assert False
E       AssertionError

tests/test_example.py:11: AssertionError
_________________________________ test_e _________________________________

    def test_e():
>       assert False
E       AssertionError

tests/test_example.py:14: AssertionError
=========================== short test summary info ============================
FAILED tests/test_example.py::test_a
FAILED tests/test_example.py::test_b
FAILED tests/test_example.py::test_c
FAILED tests/test_example.py::test_d
FAILED tests/test_example.py::test_e
========================= 5 failed in 0.03s ==========================
`)

	// Limit to 3 failures
	summary := FormatFailureSummary(command, output, 3)

	// Should contain first 3 failures
	assert.Contains(t, summary, "test_a")
	assert.Contains(t, summary, "test_b")
	assert.Contains(t, summary, "test_c")

	// Should indicate more failures exist
	assert.Contains(t, summary, "and 2 more failures")
}

func TestExtractTestSummary_Pytest(t *testing.T) {
	command := "pytest tests/"
	output := []byte(`
============================= test session starts ==============================
collected 3 items

tests/test_example.py::test_pass PASSED [33%]
tests/test_example.py::test_fail FAILED [66%]
tests/test_example.py::test_skip SKIPPED [100%]
========================= 1 failed, 1 passed, 1 skipped in 0.03s ==========================
`)

	summary, ok := ExtractTestSummary(command, output)
	assert.True(t, ok)
	assert.Equal(t, 1, summary.Passed)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 1, summary.Skipped)
}

func TestExtractTestSummary_PytestQuiet(t *testing.T) {
	// -q/-qq emit no per-test lines and an undecorated summary; counts
	// must come from the bare summary line.
	command := "bash -c 'cd opendata && uv run pytest \"$@\" -n 4 --no-cov -qq --tb=short' rr tests/foo.py"
	output := []byte("bringing up nodes...\n5 passed in 4.20s\n")

	summary, ok := ExtractTestSummary(command, output)
	assert.True(t, ok)
	assert.Equal(t, 5, summary.Passed)
	assert.Zero(t, summary.Failed)

	output = []byte("bringing up nodes...\n2 failed, 3 passed, 1 skipped in 1.10s\n")
	summary, ok = ExtractTestSummary(command, output)
	assert.True(t, ok)
	assert.Equal(t, 3, summary.Passed)
	assert.Equal(t, 2, summary.Failed)
	assert.Equal(t, 1, summary.Skipped)
}

func TestExtractTestSummary_GoTest(t *testing.T) {
	command := "go test ./..."
	output := []byte(`
=== RUN   TestExample
--- PASS: TestExample (0.00s)
=== RUN   TestFail
    example_test.go:15: Expected 1, got 2
--- FAIL: TestFail (0.00s)
FAIL
exit status 1
FAIL	example	0.005s
`)

	summary, ok := ExtractTestSummary(command, output)
	assert.True(t, ok)
	assert.Equal(t, 1, summary.Passed)
	assert.Equal(t, 1, summary.Failed)
}

func TestExtractTestSummary_UnknownFormat(t *testing.T) {
	_, ok := ExtractTestSummary("make build", []byte("compiling...\ndone\n"))
	assert.False(t, ok)
}

// TestDetectNoTests is the regression suite for false-green test runs: a
// command that executed zero tests previously reported success with no signal
// at all in the envelope. The "must not flag" cases matter as much as the
// positive ones - flagging an intentional zero-test run (--collect-only) or a
// normal dev-loop invocation (go test -run NoMatch) would be a false red.
func TestDetectNoTests(t *testing.T) {
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
			assert.Equal(t, tt.want, DetectNoTests(tt.command, []byte(tt.output)))
		})
	}
}

// TestExtractTestSummaryNoTests pins that a zero-test run now produces a
// summary carrying NoTests instead of being dropped entirely (which made it
// indistinguishable from a non-test command).
func TestExtractTestSummaryNoTests(t *testing.T) {
	summary, ok := ExtractTestSummary("pytest tests/x.py -k nomatch",
		[]byte("collected 0 items\n\n===== no tests ran in 0.05s ====="))
	assert.True(t, ok)
	assert.True(t, summary.NoTests)
	assert.Zero(t, summary.Passed)

	// Unparseable output still yields no summary at all.
	_, ok = ExtractTestSummary("make build", []byte("building..."))
	assert.False(t, ok)
}

// TestHasIntentionalZeroFlag guards against substring matching. "--co" is a
// prefix of "--cov", "--color", "--config", and
// "--continue-on-collection-errors", so a strings.Contains check silently
// disabled no-tests detection for `pytest --cov=app -k typo` and most other
// real CI commands - the feature was off exactly where it mattered most.
func TestHasIntentionalZeroFlag(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"plain pytest", "pytest -k typo", false},
		{"collect-only", "pytest --collect-only tests/", true},
		{"co abbreviation", "pytest --co tests/", true},
		{"fixtures", "pytest --fixtures", true},
		{"markers", "pytest --markers", true},
		{"passWithNoTests", "jest --passWithNoTests", true},
		{"listTests", "jest --listTests", true},

		// Flags that merely start with an opt-out flag's name.
		{"coverage flag", "pytest --cov=app -k typo", false},
		{"cov-report", "pytest --cov-report=xml tests/", false},
		{"color flag", "pytest --color=yes -k typo", false},
		{"config flag", "pytest --config=setup.cfg -k typo", false},
		{"count flag", "pytest --count=3 tests/", false},
		{"continue-on-collection-errors", "pytest --continue-on-collection-errors", false},
		{"vitest coverage", "vitest run --coverage", false},

		{"flag with = value still matches", "pytest --collect-only=x tests/", true},
		{"case insensitive", "jest --PASSWITHNOTESTS", true},
		{"flag name inside a path is not a flag", "pytest tests/--collect-only-fixture", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasIntentionalZeroFlag(tt.command))
		})
	}
}
