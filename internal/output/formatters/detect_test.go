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
