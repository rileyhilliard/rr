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
