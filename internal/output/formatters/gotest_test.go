package formatters

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoTestFormatterName(t *testing.T) {
	f := NewGoTestFormatter()
	assert.Equal(t, "gotest", f.Name())
}

func TestGoTestDetect(t *testing.T) {
	f := NewGoTestFormatter()

	tests := []struct {
		name      string
		command   string
		output    string
		wantScore int
		minScore  int // Use when exact score doesn't matter
	}{
		{
			name:      "go test command",
			command:   "go test ./...",
			output:    "",
			wantScore: 100,
		},
		{
			name:      "go test with flags",
			command:   "go test -v -race ./pkg/...",
			output:    "",
			wantScore: 100,
		},
		{
			name:     "output with RUN pattern",
			command:  "some-runner",
			output:   "=== RUN   TestAdd\n--- PASS: TestAdd (0.00s)",
			minScore: 80,
		},
		{
			name:     "output with package ok",
			command:  "",
			output:   "ok      github.com/example/pkg    0.005s",
			minScore: 80,
		},
		{
			name:     "output with package FAIL",
			command:  "",
			output:   "FAIL    github.com/example/pkg    0.005s",
			minScore: 80,
		},
		{
			name:     "output with no test files",
			command:  "",
			output:   "?       github.com/example/cmd     [no test files]",
			minScore: 80,
		},
		{
			name:      "unrelated command and output",
			command:   "npm test",
			output:    "Running tests...\nAll tests passed",
			wantScore: 0,
		},
		{
			name:      "empty command and output",
			command:   "",
			output:    "",
			wantScore: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := f.Detect(tt.command, []byte(tt.output))
			if tt.minScore > 0 {
				assert.GreaterOrEqual(t, score, tt.minScore)
			} else {
				assert.Equal(t, tt.wantScore, score)
			}
		})
	}
}

func TestProcessLineRun(t *testing.T) {
	f := NewGoTestFormatter()

	result := f.ProcessLine("=== RUN   TestAdd")
	// Should contain the original line (with styling)
	assert.Contains(t, result, "RUN")
	assert.Contains(t, result, "TestAdd")
}

func TestProcessLinePass(t *testing.T) {
	f := NewGoTestFormatter()

	// First run the test
	f.ProcessLine("=== RUN   TestAdd")

	// Then pass
	result := f.ProcessLine("--- PASS: TestAdd (0.00s)")
	assert.Contains(t, result, "PASS")
	assert.Contains(t, result, "TestAdd")

	// Check recorded test
	tests := f.GetTests()
	require.Len(t, tests, 1)
	assert.Equal(t, "TestAdd", tests[0].Name)
	assert.Equal(t, "PASS", tests[0].Status)
	assert.Equal(t, "0.00s", tests[0].Duration)
}

func TestProcessLineFail(t *testing.T) {
	f := NewGoTestFormatter()

	f.ProcessLine("=== RUN   TestMultiply")
	f.ProcessLine("    math_test.go:15: expected 6, got 5")
	result := f.ProcessLine("--- FAIL: TestMultiply (0.00s)")

	assert.Contains(t, result, "FAIL")
	assert.Contains(t, result, "TestMultiply")

	tests := f.GetTests()
	require.Len(t, tests, 1)
	assert.Equal(t, "TestMultiply", tests[0].Name)
	assert.Equal(t, "FAIL", tests[0].Status)
}

func TestProcessLineSkip(t *testing.T) {
	f := NewGoTestFormatter()

	f.ProcessLine("=== RUN   TestSkipped")
	result := f.ProcessLine("--- SKIP: TestSkipped (0.00s)")

	assert.Contains(t, result, "SKIP")

	tests := f.GetTests()
	require.Len(t, tests, 1)
	assert.Equal(t, "TestSkipped", tests[0].Name)
	assert.Equal(t, "SKIP", tests[0].Status)
}

func TestProcessLinePackageOk(t *testing.T) {
	f := NewGoTestFormatter()

	result := f.ProcessLine("ok      github.com/example/config  0.002s")
	assert.Contains(t, result, "ok")
	assert.Contains(t, result, "github.com/example/config")

	pkgs := f.GetPackages()
	require.Len(t, pkgs, 1)
	assert.Equal(t, "github.com/example/config", pkgs[0].Name)
	assert.Equal(t, "ok", pkgs[0].Status)
	assert.Equal(t, "0.002s", pkgs[0].Duration)
}

func TestProcessLinePackageFail(t *testing.T) {
	f := NewGoTestFormatter()

	result := f.ProcessLine("FAIL    github.com/example/math    0.005s")
	assert.Contains(t, result, "FAIL")
	assert.Contains(t, result, "github.com/example/math")

	pkgs := f.GetPackages()
	require.Len(t, pkgs, 1)
	assert.Equal(t, "github.com/example/math", pkgs[0].Name)
	assert.Equal(t, "FAIL", pkgs[0].Status)
}

func TestProcessLinePackageNoTests(t *testing.T) {
	f := NewGoTestFormatter()

	result := f.ProcessLine("?       github.com/example/cmd     [no test files]")
	assert.Contains(t, result, "?")
	assert.Contains(t, result, "github.com/example/cmd")

	pkgs := f.GetPackages()
	require.Len(t, pkgs, 1)
	assert.Equal(t, "github.com/example/cmd", pkgs[0].Name)
	assert.Equal(t, "?", pkgs[0].Status)
	assert.Equal(t, "no test files", pkgs[0].Message)
}

func TestProcessLineStandalonePassFail(t *testing.T) {
	f := NewGoTestFormatter()

	passResult := f.ProcessLine("PASS")
	assert.Contains(t, passResult, "PASS")

	failResult := f.ProcessLine("FAIL")
	assert.Contains(t, failResult, "FAIL")
}

func TestProcessLineErrorLocation(t *testing.T) {
	f := NewGoTestFormatter()

	// Start a test first
	f.ProcessLine("=== RUN   TestSomething")

	result := f.ProcessLine("    math_test.go:15: expected 6, got 5")
	assert.Contains(t, result, "math_test.go:15")
	assert.Contains(t, result, "expected 6, got 5")
}

func TestProcessLineNormalLine(t *testing.T) {
	f := NewGoTestFormatter()

	// Normal lines should pass through unchanged
	normalLines := []string{
		"Running tests...",
		"Some random output",
		"coverage: 80.5% of statements",
	}

	for _, line := range normalLines {
		result := f.ProcessLine(line)
		assert.Equal(t, line, result)
	}
}

func TestFullTestOutput(t *testing.T) {
	f := NewGoTestFormatter()

	// Simulate the sample output from the task
	lines := []string{
		"=== RUN   TestAdd",
		"--- PASS: TestAdd (0.00s)",
		"=== RUN   TestSubtract",
		"--- PASS: TestSubtract (0.00s)",
		"=== RUN   TestMultiply",
		"    math_test.go:15: expected 6, got 5",
		"--- FAIL: TestMultiply (0.00s)",
		"=== RUN   TestDivide",
		"--- PASS: TestDivide (0.00s)",
		"FAIL",
		"FAIL    github.com/example/math    0.005s",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	tests := f.GetTests()
	require.Len(t, tests, 4)

	// Verify test results
	assert.Equal(t, "TestAdd", tests[0].Name)
	assert.Equal(t, "PASS", tests[0].Status)

	assert.Equal(t, "TestSubtract", tests[1].Name)
	assert.Equal(t, "PASS", tests[1].Status)

	assert.Equal(t, "TestMultiply", tests[2].Name)
	assert.Equal(t, "FAIL", tests[2].Status)

	assert.Equal(t, "TestDivide", tests[3].Name)
	assert.Equal(t, "PASS", tests[3].Status)

	pkgs := f.GetPackages()
	require.Len(t, pkgs, 1)
	assert.Equal(t, "github.com/example/math", pkgs[0].Name)
	assert.Equal(t, "FAIL", pkgs[0].Status)
}

func TestVerbosePackageOutput(t *testing.T) {
	f := NewGoTestFormatter()

	// Simulate verbose output with package structure
	lines := []string{
		"?       github.com/example/cmd     [no test files]",
		"ok      github.com/example/config  0.002s",
		"ok      github.com/example/util    0.003s",
		"FAIL    github.com/example/math    0.005s",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	pkgs := f.GetPackages()
	require.Len(t, pkgs, 4)

	assert.Equal(t, "github.com/example/cmd", pkgs[0].Name)
	assert.Equal(t, "?", pkgs[0].Status)
	assert.Equal(t, "no test files", pkgs[0].Message)

	assert.Equal(t, "github.com/example/config", pkgs[1].Name)
	assert.Equal(t, "ok", pkgs[1].Status)

	assert.Equal(t, "github.com/example/util", pkgs[2].Name)
	assert.Equal(t, "ok", pkgs[2].Status)

	assert.Equal(t, "github.com/example/math", pkgs[3].Name)
	assert.Equal(t, "FAIL", pkgs[3].Status)
}

func TestSummaryAllPass(t *testing.T) {
	f := NewGoTestFormatter()

	lines := []string{
		"=== RUN   TestA",
		"--- PASS: TestA (0.01s)",
		"=== RUN   TestB",
		"--- PASS: TestB (0.02s)",
		"ok      github.com/example/pkg    0.03s",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	summary := f.Summary(0)
	assert.Contains(t, summary, "2 passed")
	assert.NotContains(t, summary, "failed")
}

func TestSummaryWithFailures(t *testing.T) {
	f := NewGoTestFormatter()

	lines := []string{
		"=== RUN   TestA",
		"--- PASS: TestA (0.01s)",
		"=== RUN   TestB",
		"--- FAIL: TestB (0.02s)",
		"=== RUN   TestC",
		"--- SKIP: TestC (0.00s)",
		"FAIL    github.com/example/pkg    0.03s",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	summary := f.Summary(1)
	assert.Contains(t, summary, "1 failed")
	assert.Contains(t, summary, "1 passed")
	assert.Contains(t, summary, "1 skipped")
	assert.Contains(t, summary, "3 total")
	assert.Contains(t, summary, "Failed tests:")
	assert.Contains(t, summary, "TestB")
}

func TestSummaryWithPackages(t *testing.T) {
	f := NewGoTestFormatter()

	lines := []string{
		"ok      github.com/example/config  0.002s",
		"ok      github.com/example/util    0.003s",
		"FAIL    github.com/example/math    0.005s",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	summary := f.Summary(1)
	assert.Contains(t, summary, "Packages:")
	assert.Contains(t, summary, "2 passed")
	assert.Contains(t, summary, "1 failed")
}

func TestSummaryEmptyWithExitCode(t *testing.T) {
	f := NewGoTestFormatter()

	// No test output, but command failed
	summary := f.Summary(1)
	assert.Contains(t, summary, "exit code 1")
}

func TestSummaryEmptyWithSuccess(t *testing.T) {
	f := NewGoTestFormatter()

	// No test output and success exit code
	summary := f.Summary(0)
	assert.Empty(t, summary)
}

func TestGoTestReset(t *testing.T) {
	f := NewGoTestFormatter()

	// Add some state
	f.ProcessLine("=== RUN   TestA")
	f.ProcessLine("--- PASS: TestA (0.01s)")
	f.ProcessLine("ok      github.com/example/pkg    0.03s")

	require.Len(t, f.GetTests(), 1)
	require.Len(t, f.GetPackages(), 1)

	// Reset
	f.Reset()

	assert.Empty(t, f.GetTests())
	assert.Empty(t, f.GetPackages())
}

func TestProcessLineSubtests(t *testing.T) {
	f := NewGoTestFormatter()

	// Go subtests have format TestParent/SubTest
	lines := []string{
		"=== RUN   TestParent",
		"=== RUN   TestParent/SubA",
		"--- PASS: TestParent/SubA (0.00s)",
		"=== RUN   TestParent/SubB",
		"--- FAIL: TestParent/SubB (0.01s)",
		"--- FAIL: TestParent (0.01s)",
	}

	for _, line := range lines {
		f.ProcessLine(line)
	}

	tests := f.GetTests()
	require.Len(t, tests, 3)

	assert.Equal(t, "TestParent/SubA", tests[0].Name)
	assert.Equal(t, "PASS", tests[0].Status)

	assert.Equal(t, "TestParent/SubB", tests[1].Name)
	assert.Equal(t, "FAIL", tests[1].Status)

	assert.Equal(t, "TestParent", tests[2].Name)
	assert.Equal(t, "FAIL", tests[2].Status)
}

func TestProcessLineCachedResults(t *testing.T) {
	f := NewGoTestFormatter()

	// Cached results have (cached) instead of duration
	result := f.ProcessLine("ok      github.com/example/pkg    (cached)")
	assert.Contains(t, result, "ok")

	pkgs := f.GetPackages()
	require.Len(t, pkgs, 1)
	assert.Equal(t, "(cached)", pkgs[0].Duration)
}
