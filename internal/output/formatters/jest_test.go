package formatters

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJestFormatterName(t *testing.T) {
	f := NewJestFormatter()
	assert.Equal(t, "jest", f.Name())
}

func TestJestFormatterDetectCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected int
	}{
		{"jest command", "npm run jest", 100},
		{"npx jest", "npx jest", 100},
		{"jest with args", "jest --coverage", 100},
		{"vitest command", "vitest run", 100},
		{"npm test with vitest", "npm run vitest", 100},
		{"unrelated command", "go test ./...", 0},
		{"npm test", "npm test", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewJestFormatter()
			score := f.Detect(tt.command, nil)
			assert.Equal(t, tt.expected, score)
		})
	}
}

func TestJestFormatterDetectOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected int
	}{
		{
			"pass line",
			" PASS  src/utils.test.js",
			80,
		},
		{
			"fail line",
			" FAIL  src/math.test.js",
			80,
		},
		{
			"summary line",
			"Test Suites: 1 failed, 1 passed, 2 total",
			70,
		},
		{
			"tests summary",
			"Tests: 1 failed, 3 passed, 4 total",
			70,
		},
		{
			"unrelated output",
			"Running tests...\nAll done.",
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewJestFormatter()
			score := f.Detect("", []byte(tt.output))
			assert.Equal(t, tt.expected, score)
		})
	}
}

func TestJestFormatterProcessLinePassSuite(t *testing.T) {
	f := NewJestFormatter()

	result := f.ProcessLine(" PASS  src/utils.test.js")

	assert.Contains(t, result, "PASS")
	assert.Contains(t, result, "src/utils.test.js")
}

func TestJestFormatterProcessLineFailSuite(t *testing.T) {
	f := NewJestFormatter()

	result := f.ProcessLine(" FAIL  src/math.test.js")

	assert.Contains(t, result, "FAIL")
	assert.Contains(t, result, "src/math.test.js")
}

func TestJestFormatterProcessLinePassingTest(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		testName string
		duration string
	}{
		{
			"with duration",
			"  ✓ should add numbers (3ms)",
			"should add numbers",
			"3ms",
		},
		{
			"without duration",
			"  ✓ should subtract numbers",
			"should subtract numbers",
			"",
		},
		{
			"with seconds",
			"  ✓ slow test (100 ms)",
			"slow test",
			"100 ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewJestFormatter()
			result := f.ProcessLine(tt.input)

			assert.Contains(t, result, tt.testName)
			if tt.duration != "" {
				assert.Contains(t, result, tt.duration)
			}

			// Verify test was recorded
			results := f.GetTestResults()
			assert.Len(t, results, 1)
			assert.Equal(t, tt.testName, results[0].Name)
			assert.True(t, results[0].Passed)
		})
	}
}

func TestJestFormatterProcessLineFailingTest(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		testName string
	}{
		{
			"with x mark",
			"  ✕ should multiply numbers (5ms)",
			"should multiply numbers",
		},
		{
			"with cross mark",
			"  × should divide by zero",
			"should divide by zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewJestFormatter()
			result := f.ProcessLine(tt.input)

			assert.Contains(t, result, tt.testName)

			results := f.GetTestResults()
			assert.Len(t, results, 1)
			assert.Equal(t, tt.testName, results[0].Name)
			assert.False(t, results[0].Passed)
		})
	}
}

func TestJestFormatterProcessLineFailureHeader(t *testing.T) {
	f := NewJestFormatter()

	result := f.ProcessLine("  ● should multiply numbers")

	assert.Contains(t, result, "should multiply numbers")
}

func TestJestFormatterFullOutput(t *testing.T) {
	f := NewJestFormatter()

	lines := []string{
		" PASS  src/utils.test.js",
		"  ✓ should add numbers (3ms)",
		"  ✓ should subtract numbers (1ms)",
		"",
		" FAIL  src/math.test.js",
		"  ✕ should multiply numbers (5ms)",
		"  ✓ should divide numbers (2ms)",
		"",
		"  ● should multiply numbers",
		"",
		"    expect(received).toBe(expected) // Object.is equality",
		"",
		"    Expected: 6",
		"    Received: 5",
		"",
		"       7 | test('should multiply numbers', () => {",
		"       8 |   expect(multiply(2, 3)).toBe(6);",
		"    >  9 |   expect(multiply(2, 2)).toBe(6);",
		"         |                          ^",
		"      10 | });",
		"",
		"      at Object.<anonymous> (src/math.test.js:9:26)",
		"",
		"Test Suites: 1 failed, 1 passed, 2 total",
		"Tests:       1 failed, 3 passed, 4 total",
		"Snapshots:   0 total",
		"Time:        1.234s",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	// Check test results
	results := f.GetTestResults()
	assert.Len(t, results, 4)

	// Count passes and fails
	passed := 0
	failed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	assert.Equal(t, 3, passed)
	assert.Equal(t, 1, failed)

	// Check failures were captured
	failures := f.GetFailures()
	assert.Len(t, failures, 1)
	assert.Equal(t, "should multiply numbers", failures[0].TestName)
	assert.Contains(t, failures[0].ErrorMessage, "expect(received).toBe(expected)")
	assert.Contains(t, failures[0].StackTrace, "at Object.<anonymous>")
}

func TestJestFormatterSummarySuccess(t *testing.T) {
	f := NewJestFormatter()

	// Process some passing tests
	f.ProcessLine(" PASS  src/utils.test.js")
	f.ProcessLine("  ✓ should add numbers (3ms)")
	f.ProcessLine("  ✓ should subtract numbers (1ms)")
	f.ProcessLine("")
	f.ProcessLine("Test Suites: 1 passed, 1 total")
	f.ProcessLine("Tests:       2 passed, 2 total")

	summary := f.Summary(0)

	assert.Contains(t, summary, "All tests passed")
	assert.Contains(t, summary, "2 tests")
}

func TestJestFormatterSummaryFailure(t *testing.T) {
	f := NewJestFormatter()

	// Process some tests with failures
	f.ProcessLine(" FAIL  src/math.test.js")
	f.ProcessLine("  ✕ should multiply numbers (5ms)")
	f.ProcessLine("  ✓ should divide numbers (2ms)")
	f.ProcessLine("")
	f.ProcessLine("  ● should multiply numbers")
	f.ProcessLine("")
	f.ProcessLine("    expect(received).toBe(expected)")
	f.ProcessLine("")
	f.ProcessLine("      at Object.<anonymous> (src/math.test.js:9:26)")
	f.ProcessLine("")
	f.ProcessLine("Test Suites: 1 failed, 1 total")
	f.ProcessLine("Tests:       1 failed, 1 passed, 2 total")

	summary := f.Summary(1)

	assert.Contains(t, summary, "1 test(s) failed")
	assert.Contains(t, summary, "should multiply numbers")
}

func TestJestFormatterSummaryNoTests(t *testing.T) {
	f := NewJestFormatter()

	summary := f.Summary(1)

	assert.Contains(t, summary, "exit code 1")
}

func TestJestFormatterMultipleFailures(t *testing.T) {
	f := NewJestFormatter()

	lines := []string{
		" FAIL  src/math.test.js",
		"  ✕ should multiply numbers (5ms)",
		"  ✕ should add numbers (3ms)",
		"",
		"  ● should multiply numbers",
		"    Error: expected 6 but got 5",
		"      at test.js:10:5",
		"",
		"  ● should add numbers",
		"    Error: expected 4 but got 3",
		"      at test.js:15:5",
		"",
		"Test Suites: 1 failed, 1 total",
		"Tests:       2 failed, 2 total",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	failures := f.GetFailures()
	assert.Len(t, failures, 2)
	assert.Equal(t, "should multiply numbers", failures[0].TestName)
	assert.Equal(t, "should add numbers", failures[1].TestName)
}

func TestJestFormatterANSIPassthrough(t *testing.T) {
	f := NewJestFormatter()

	// Lines that don't match patterns should pass through unchanged
	ansiLine := "\033[32msome colored text\033[0m"
	result := f.ProcessLine(ansiLine)

	assert.Equal(t, ansiLine, result)
}

func TestJestParseIntOrZero(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"0", 0},
		{"1", 1},
		{"123", 123},
		{"", 0},
		{"invalid", 0},
		{"-1", -1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, jestParseIntOrZero(tt.input))
		})
	}
}

func TestJestFormatterImplementsInterface(t *testing.T) {
	// This test verifies JestFormatter can be used where Formatter is expected
	f := NewJestFormatter()

	// The interface methods
	_ = f.Name()
	_ = f.ProcessLine("test")
	_ = f.Summary(0)
}

func TestJestFormatterVitestOutput(t *testing.T) {
	// Vitest has very similar output to Jest
	f := NewJestFormatter()

	lines := []string{
		" PASS  src/utils.test.ts",
		"   ✓ should work correctly (2ms)",
		"",
		"Test Files  1 passed (1)",
		"     Tests  1 passed (1)",
		"  Duration  156ms",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	results := f.GetTestResults()
	assert.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}

func TestJestFormatterComplexTestNames(t *testing.T) {
	tests := []struct {
		input    string
		testName string
	}{
		{
			"  ✓ MyComponent > should render correctly (5ms)",
			"MyComponent > should render correctly",
		},
		{
			"  ✓ when user is logged in > should show dashboard (3ms)",
			"when user is logged in > should show dashboard",
		},
		{
			"  ✓ handles edge case with special chars: @#$% (1ms)",
			"handles edge case with special chars: @#$%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			f := NewJestFormatter()
			f.ProcessLine(tt.input)

			results := f.GetTestResults()
			assert.Len(t, results, 1)
			assert.Equal(t, tt.testName, results[0].Name)
		})
	}
}

func TestJestFormatterSummaryLinesParsing(t *testing.T) {
	f := NewJestFormatter()

	// Various summary line formats
	lines := []string{
		"Test Suites: 2 failed, 3 passed, 5 total",
		"Tests:       5 failed, 10 passed, 15 total",
		"Snapshots:   0 total",
		"Time:        5.234s",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	// These are internal state - verify through Summary output
	summary := f.Summary(1)
	assert.Contains(t, summary, "5 test(s) failed")
}

func TestJestFormatterSummaryPassedOnly(t *testing.T) {
	f := NewJestFormatter()

	f.ProcessLine("Test Suites: 3 passed, 3 total")
	f.ProcessLine("Tests:       10 passed, 10 total")

	summary := f.Summary(0)
	assert.Contains(t, summary, "All tests passed")
	assert.Contains(t, summary, "10 tests")
}

func TestJestFormatterEmptyOutput(t *testing.T) {
	f := NewJestFormatter()

	// Process empty/whitespace lines
	f.ProcessLine("")
	f.ProcessLine("   ")
	f.ProcessLine("\t")

	results := f.GetTestResults()
	assert.Len(t, results, 0)

	failures := f.GetFailures()
	assert.Len(t, failures, 0)
}

func TestJestFormatterFailureWithCodeSnippet(t *testing.T) {
	f := NewJestFormatter()

	lines := []string{
		"  ● should calculate correctly",
		"",
		"    expect(received).toBe(expected) // Object.is equality",
		"",
		"    Expected: 10",
		"    Received: 8",
		"",
		"       5 | test('should calculate correctly', () => {",
		"       6 |   const result = calculate(5, 3);",
		"    >  7 |   expect(result).toBe(10);",
		"         |                  ^",
		"       8 | });",
		"",
		"      at Object.<anonymous> (src/calc.test.js:7:18)",
		"",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	failures := f.GetFailures()
	assert.Len(t, failures, 1)

	failure := failures[0]
	assert.Equal(t, "should calculate correctly", failure.TestName)
	assert.Contains(t, failure.ErrorMessage, "Expected: 10")
	assert.Contains(t, failure.ErrorMessage, "Received: 8")
	assert.Contains(t, failure.StackTrace, "src/calc.test.js:7:18")
}

func TestJestFormatterAlternateCheckmarks(t *testing.T) {
	f := NewJestFormatter()

	// Test with alternate checkmark character
	f.ProcessLine("  ✔ should work with different checkmark (2ms)")

	results := f.GetTestResults()
	assert.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}

func TestJestFormatterProcessLinePreservesOrder(t *testing.T) {
	f := NewJestFormatter()

	testNames := []string{
		"first test",
		"second test",
		"third test",
	}

	for _, name := range testNames {
		f.ProcessLine("  ✓ " + name + " (1ms)")
	}

	results := f.GetTestResults()
	assert.Len(t, results, 3)

	for i, name := range testNames {
		assert.Equal(t, name, results[i].Name)
	}
}

func TestJestFormatterReset(t *testing.T) {
	f := NewJestFormatter()

	// Add some state
	f.ProcessLine(" PASS  src/utils.test.js")
	f.ProcessLine("  ✓ should add numbers (3ms)")
	f.ProcessLine("Test Suites: 1 passed, 1 total")
	f.ProcessLine("Tests:       1 passed, 1 total")

	// Verify state exists
	assert.Len(t, f.GetTestResults(), 1)

	// Reset
	f.Reset()

	// Verify state is cleared
	assert.Len(t, f.GetTestResults(), 0)
	assert.Len(t, f.GetFailures(), 0)

	// Summary should reflect no tests
	summary := f.Summary(0)
	assert.Empty(t, summary)
}

// Integration test with complete Jest output
func TestJestFormatterIntegration(t *testing.T) {
	jestOutput := ` PASS  src/utils.test.js
  ✓ should add numbers (3ms)
  ✓ should subtract numbers (1ms)

 FAIL  src/math.test.js
  ✕ should multiply numbers (5ms)
  ✓ should divide numbers (2ms)

  ● should multiply numbers

    expect(received).toBe(expected) // Object.is equality

    Expected: 6
    Received: 5

       7 | test('should multiply numbers', () => {
       8 |   expect(multiply(2, 3)).toBe(6);
    >  9 |   expect(multiply(2, 2)).toBe(6);
         |                          ^
      10 | });

      at Object.<anonymous> (src/math.test.js:9:26)

Test Suites: 1 failed, 1 passed, 2 total
Tests:       1 failed, 3 passed, 4 total
Snapshots:   0 total
Time:        1.234s
`

	f := NewJestFormatter()

	// Process each line
	for _, line := range strings.Split(jestOutput, "\n") {
		f.ProcessLine(line)
	}

	// Verify results
	results := f.GetTestResults()
	assert.Len(t, results, 4, "should have 4 test results")

	passed := 0
	failed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	assert.Equal(t, 3, passed, "should have 3 passed tests")
	assert.Equal(t, 1, failed, "should have 1 failed test")

	// Verify failures
	failures := f.GetFailures()
	assert.Len(t, failures, 1, "should have 1 failure detail")
	assert.Equal(t, "should multiply numbers", failures[0].TestName)

	// Verify summary
	summary := f.Summary(1)
	assert.Contains(t, summary, "1 test(s) failed")
	assert.Contains(t, summary, "should multiply numbers")
}
