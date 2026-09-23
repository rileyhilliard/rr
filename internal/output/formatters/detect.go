package formatters

import (
	"strings"

	"github.com/rileyhilliard/rr/internal/output"
)

// Detector interface for formatters that can detect their format.
type Detector interface {
	Detect(command string, output []byte) int
}

// ExtractFailures detects the test framework from command/output and extracts
// structured failure information. Returns nil if no failures found or format unknown.
func ExtractFailures(command string, rawOutput []byte) []output.TestFailure {
	formatter := detectFormatter(command, rawOutput)
	if formatter == nil {
		return nil
	}

	// Process all output through the formatter
	outputStr := string(rawOutput)
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		formatter.ProcessLine(line)
	}

	// Extract failures if formatter supports it
	if provider, ok := formatter.(output.TestSummaryProvider); ok {
		return provider.GetTestFailures()
	}

	return nil
}

// TestSummary holds aggregate test counts extracted from raw output.
type TestSummary struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
	// NoTests is set when the runner explicitly reported running no tests.
	// Distinguishes "ran nothing" from "nothing failed" - without it, a
	// broken path filter looks identical to a clean suite.
	NoTests bool `json:"no_tests,omitempty"`
}

// intentionalZeroFlags are runner flags whose whole purpose is to produce zero
// executed tests. A run using one of these collected nothing by design, so
// reporting it as "ran no tests" would be noise.
var intentionalZeroFlags = []string{
	"--collect-only",
	"--co",
	"--fixtures",
	"--markers",
	"--passwithnotests",
	"--listtests",
}

// hasIntentionalZeroFlag reports whether the command opted into a zero-test run.
//
// Matches whole tokens, not substrings: "--co" is a prefix of "--cov",
// "--color", "--config", and "--continue-on-collection-errors", so a substring
// check would silently disable no-tests detection for `pytest --cov=app -k typo`
// and most other real CI commands. A flag written as "--flag=value" still
// matches, since only the part before "=" is compared.
func hasIntentionalZeroFlag(command string) bool {
	for _, field := range strings.Fields(strings.ToLower(command)) {
		name, _, _ := strings.Cut(field, "=")
		for _, flag := range intentionalZeroFlags {
			if name == flag {
				return true
			}
		}
	}
	return false
}

// summarize reads aggregate counts from a formatter that has already
// processed the output. ok is false when the output contained no
// recognizable test results.
func summarize(formatter output.Formatter, command string) (TestSummary, bool) {
	provider, ok := formatter.(output.TestSummaryProvider)
	if !ok {
		return TestSummary{}, false
	}
	passed, failed, skipped, errs := provider.GetTestCounts()
	if passed+failed+skipped+errs == 0 {
		// All-zero counts are only meaningful when the runner explicitly said
		// it ran nothing. Otherwise this is unparseable output (or a non-test
		// command that merely mentions a framework), not a zero-test run.
		reporter, ok := formatter.(output.NoTestsReporter)
		if ok && reporter.RanNothing() && !hasIntentionalZeroFlag(command) {
			return TestSummary{NoTests: true}, true
		}
		return TestSummary{}, false
	}
	return TestSummary{Passed: passed, Failed: failed, Skipped: skipped, Errors: errs}, true
}

// detectorFormatter is a formatter that also implements detection.
type detectorFormatter interface {
	output.Formatter
	Detector
}

// detectFormatter returns the best matching formatter for the command/output.
// Returns nil if no specific formatter matches well.
func detectFormatter(command string, rawOutput []byte) output.Formatter {
	// Create each formatter once and use it for both detection and processing.
	// This avoids the overhead of creating formatters twice.
	formatters := []detectorFormatter{
		NewPytestFormatter(),
		NewGoTestFormatter(),
		NewJestFormatter(),
	}

	var bestFormatter output.Formatter
	bestScore := 0

	for _, f := range formatters {
		score := f.Detect(command, rawOutput)
		if score > bestScore {
			bestScore = score
			bestFormatter = f
		}
	}

	// Only return if we have a reasonable confidence
	if bestScore >= 50 {
		return bestFormatter
	}

	return nil
}
