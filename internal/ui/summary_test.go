package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderSummaryWithFailures(t *testing.T) {
	summary := &TestSummary{
		Passed:  3,
		Failed:  2,
		Skipped: 1,
		Failures: []TestFailure{
			{
				TestName: "test_login_expired",
				File:     "tests/test_auth.py",
				Line:     42,
				Message:  "AssertionError: Expected 401, got 200",
			},
			{
				TestName: "test_divide_by_zero",
				File:     "tests/test_math.py",
				Line:     15,
				Message:  "ZeroDivisionError: division by zero",
			},
		},
	}

	result := RenderSummary(summary, 1)

	// Should contain failure count
	assert.Contains(t, result, "2 tests failed")
	// Should contain the fail symbol
	assert.Contains(t, result, SymbolFail)
	// Should contain file locations
	assert.Contains(t, result, "tests/test_auth.py:42")
	assert.Contains(t, result, "tests/test_math.py:15")
	// Should contain test names
	assert.Contains(t, result, "test_login_expired")
	assert.Contains(t, result, "test_divide_by_zero")
	// Should contain error messages
	assert.Contains(t, result, "AssertionError: Expected 401, got 200")
	assert.Contains(t, result, "ZeroDivisionError: division by zero")
}

func TestRenderSummarySingleFailure(t *testing.T) {
	summary := &TestSummary{
		Passed: 5,
		Failed: 1,
		Failures: []TestFailure{
			{
				TestName: "test_connection",
				File:     "tests/test_network.py",
				Line:     10,
				Message:  "TimeoutError: Connection timed out",
			},
		},
	}

	result := RenderSummary(summary, 1)

	// Should use singular "test" for single failure
	assert.Contains(t, result, "1 test failed")
	assert.NotContains(t, result, "1 tests failed")
}

func TestRenderSummaryNoFailures(t *testing.T) {
	summary := &TestSummary{
		Passed:   10,
		Failed:   0,
		Skipped:  2,
		Failures: []TestFailure{},
	}

	// With exit code 0, should return empty string
	result := RenderSummary(summary, 0)
	assert.Empty(t, result)

	// Even with non-zero exit code, no failures means no summary
	result = RenderSummary(summary, 1)
	assert.Empty(t, result)
}

func TestRenderSummaryNilSummary(t *testing.T) {
	result := RenderSummary(nil, 0)
	assert.Empty(t, result)

	result = RenderSummary(nil, 1)
	assert.Empty(t, result)
}

func TestRenderSummaryMultilineMessage(t *testing.T) {
	summary := &TestSummary{
		Failed: 1,
		Failures: []TestFailure{
			{
				TestName: "test_complex",
				File:     "tests/test_complex.py",
				Line:     50,
				Message:  "AssertionError: Values differ\nExpected: [1, 2, 3]\nActual: [1, 2, 4]",
			},
		},
	}

	result := RenderSummary(summary, 1)

	// Should contain all lines of the message
	assert.Contains(t, result, "Values differ")
	assert.Contains(t, result, "Expected: [1, 2, 3]")
	assert.Contains(t, result, "Actual: [1, 2, 4]")
}

func TestRenderSummaryNoFileLocation(t *testing.T) {
	summary := &TestSummary{
		Failed: 1,
		Failures: []TestFailure{
			{
				TestName: "test_no_location",
				File:     "",
				Line:     0,
				Message:  "Some error occurred",
			},
		},
	}

	result := RenderSummary(summary, 1)

	// Should still render test name and message
	assert.Contains(t, result, "test_no_location")
	assert.Contains(t, result, "Some error occurred")
	// Should not have orphaned colon from empty file
	assert.NotContains(t, result, ":0")
}

func TestRenderSummaryNoMessage(t *testing.T) {
	summary := &TestSummary{
		Failed: 1,
		Failures: []TestFailure{
			{
				TestName: "test_no_message",
				File:     "tests/test_basic.py",
				Line:     5,
				Message:  "",
			},
		},
	}

	result := RenderSummary(summary, 1)

	// Should render without crashing and include the test info
	assert.Contains(t, result, "test_no_message")
	assert.Contains(t, result, "tests/test_basic.py:5")
}

func TestRenderSummaryIndentation(t *testing.T) {
	summary := &TestSummary{
		Failed: 1,
		Failures: []TestFailure{
			{
				TestName: "test_indented",
				File:     "tests/test_indent.py",
				Line:     10,
				Message:  "Error message",
			},
		},
	}

	result := RenderSummary(summary, 1)

	// Check that file path and test name are indented
	lines := strings.Split(result, "\n")
	var foundFilePath bool
	var foundTestName bool
	for _, line := range lines {
		// File path should be indented with 2 spaces
		if strings.Contains(line, "tests/test_indent.py:10") {
			foundFilePath = true
			assert.True(t, strings.HasPrefix(line, "  "), "file path should be indented with 2 spaces")
		}
		// Test name should be indented with 4 spaces
		if strings.Contains(line, "test_indented") && !strings.Contains(line, "py") {
			foundTestName = true
			assert.True(t, strings.HasPrefix(line, "    "), "test name should be indented with 4 spaces")
		}
	}
	assert.True(t, foundFilePath, "should find file path in output")
	assert.True(t, foundTestName, "should find test name in output")
}

func TestRenderSuccessSummary(t *testing.T) {
	result := RenderSuccessSummary(5)
	assert.Contains(t, result, "5 tests passed")
	assert.Contains(t, result, SymbolSuccess)
}

func TestRenderSuccessSummarySingular(t *testing.T) {
	result := RenderSuccessSummary(1)
	assert.Contains(t, result, "1 test passed")
	assert.NotContains(t, result, "1 tests passed")
}

func TestRenderSuccessSummaryZero(t *testing.T) {
	result := RenderSuccessSummary(0)
	assert.Empty(t, result)
}

func TestNewSummaryRenderer(t *testing.T) {
	r := NewSummaryRenderer()
	assert.NotNil(t, r)
	// Verify styles are initialized (they should render without panicking)
	_ = r.errorStyle.Render("test")
	_ = r.successStyle.Render("test")
	_ = r.pathStyle.Render("test")
	_ = r.mutedStyle.Render("test")
}
