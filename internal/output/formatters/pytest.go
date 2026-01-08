package formatters

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/ui"
)

// PytestResult represents the result of a single pytest test.
type PytestResult struct {
	Name   string
	File   string
	Status string // PASSED, FAILED, SKIPPED, ERROR
}

// PytestFailure represents a test failure with details.
type PytestFailure struct {
	TestName string
	File     string
	Line     int
	Message  string
}

// PytestSummary contains the overall test run summary.
type PytestSummary struct {
	Passed   int
	Failed   int
	Skipped  int
	Errors   int
	Failures []PytestFailure
}

// PytestFormatter processes pytest output for display.
type PytestFormatter struct {
	passedStyle  lipgloss.Style
	failedStyle  lipgloss.Style
	skippedStyle lipgloss.Style
	errorStyle   lipgloss.Style

	// State tracking
	results        []PytestResult
	failures       []PytestFailure
	inFailures     bool
	currentFailure *pytestFailureBuilder
	summaryLine    string
}

// pytestFailureBuilder accumulates failure details across multiple lines.
type pytestFailureBuilder struct {
	testName  string
	file      string
	line      int
	message   strings.Builder
	inMessage bool
}

// NewPytestFormatter creates a new pytest output formatter.
func NewPytestFormatter() *PytestFormatter {
	return &PytestFormatter{
		passedStyle:  lipgloss.NewStyle().Foreground(ui.ColorSuccess),
		failedStyle:  lipgloss.NewStyle().Foreground(ui.ColorError),
		skippedStyle: lipgloss.NewStyle().Foreground(ui.ColorWarning),
		errorStyle:   lipgloss.NewStyle().Foreground(ui.ColorError),
		results:      make([]PytestResult, 0),
		failures:     make([]PytestFailure, 0),
	}
}

// Name returns "pytest".
func (f *PytestFormatter) Name() string {
	return "pytest"
}

// Detect returns a confidence score for pytest output.
// Returns 100 if command contains "pytest", 80 if output contains pytest markers.
func (f *PytestFormatter) Detect(command string, output []byte) int {
	if strings.Contains(command, "pytest") {
		return 100
	}
	outputStr := string(output)
	if strings.Contains(outputStr, "collected") && strings.Contains(outputStr, "items") {
		return 80
	}
	if strings.Contains(outputStr, "test session starts") {
		return 80
	}
	return 0
}

// Regex patterns for parsing pytest output
var (
	// Matches: tests/test_example.py::test_pass PASSED [ 33%]
	pytestResultPattern = regexp.MustCompile(`^(.+?::\w+)\s+(PASSED|FAILED|SKIPPED|ERROR)\s*(?:\[.*\])?$`)

	// Matches the failure header: _________________________________ test_fail _________________________________
	pytestFailureHeaderPattern = regexp.MustCompile(`^_+\s+(\w+)\s+_+$`)

	// Matches file:line at end of traceback: tests/test_example.py:5: AssertionError
	pytestTracebackLocationPattern = regexp.MustCompile(`^(.+?):(\d+):\s+(\w+(?:Error|Exception|Warning)?.*)$`)

	// Matches assertion error lines: E       AssertionError: Math is broken
	pytestAssertionPattern = regexp.MustCompile(`^E\s+(\w+(?:Error|Exception)?:?\s*.*)$`)

	// Matches summary line: 1 failed, 1 passed, 1 skipped in 0.03s
	pytestSummaryPattern = regexp.MustCompile(`=+\s*(\d+\s+\w+(?:,\s*\d+\s+\w+)*)\s+in\s+[\d.]+s\s*=+`)

	// Matches the failures section header
	pytestFailuresSectionStart = regexp.MustCompile(`^=+\s*FAILURES\s*=+$`)
	pytestFailuresSectionEnd   = regexp.MustCompile(`^=+\s*short test summary`)
)

// ProcessLine transforms a single line of pytest output.
func (f *PytestFormatter) ProcessLine(line string) string {
	trimmed := strings.TrimSpace(line)

	// Check for failures section boundaries
	if pytestFailuresSectionStart.MatchString(trimmed) {
		f.inFailures = true
		return f.failedStyle.Render(line)
	}
	if pytestFailuresSectionEnd.MatchString(trimmed) {
		f.inFailures = false
		f.finishCurrentFailure()
		return line
	}

	// Parse failures section
	if f.inFailures {
		return f.processFailureLine(line, trimmed)
	}

	// Parse test result lines
	if match := pytestResultPattern.FindStringSubmatch(trimmed); match != nil {
		testPath := match[1]
		status := match[2]

		// Extract file and test name
		parts := strings.Split(testPath, "::")
		file := ""
		testName := testPath
		if len(parts) >= 2 {
			file = parts[0]
			testName = parts[len(parts)-1]
		}

		f.results = append(f.results, PytestResult{
			Name:   testName,
			File:   file,
			Status: status,
		})

		// Style based on status
		switch status {
		case "PASSED":
			return f.passedStyle.Render(line)
		case "FAILED":
			return f.failedStyle.Render(line)
		case "SKIPPED":
			return f.skippedStyle.Render(line)
		case "ERROR":
			return f.errorStyle.Render(line)
		}
	}

	// Check for summary line
	if pytestSummaryPattern.MatchString(trimmed) {
		f.summaryLine = trimmed
	}

	// Pass through ANSI codes and other lines unchanged
	return line
}

// processFailureLine handles lines within the FAILURES section.
func (f *PytestFormatter) processFailureLine(line, trimmed string) string {
	// Check for new failure header
	if match := pytestFailureHeaderPattern.FindStringSubmatch(trimmed); match != nil {
		f.finishCurrentFailure()
		f.currentFailure = &pytestFailureBuilder{
			testName: match[1],
		}
		return f.failedStyle.Render(line)
	}

	if f.currentFailure == nil {
		return line
	}

	// Check for traceback location (file:line)
	if match := pytestTracebackLocationPattern.FindStringSubmatch(trimmed); match != nil {
		f.currentFailure.file = match[1]
		if lineNum, err := strconv.Atoi(match[2]); err == nil {
			f.currentFailure.line = lineNum
		}
		// The error type is in match[3], but we get the full message from assertion lines
		return f.failedStyle.Render(line)
	}

	// Check for assertion error lines (start with E)
	if match := pytestAssertionPattern.FindStringSubmatch(trimmed); match != nil {
		if f.currentFailure.message.Len() > 0 {
			f.currentFailure.message.WriteString("\n")
		}
		f.currentFailure.message.WriteString(match[1])
		f.currentFailure.inMessage = true
		return f.errorStyle.Render(line)
	}

	return line
}

// finishCurrentFailure saves the current failure being built.
func (f *PytestFormatter) finishCurrentFailure() {
	if f.currentFailure == nil {
		return
	}

	failure := PytestFailure{
		TestName: f.currentFailure.testName,
		File:     f.currentFailure.file,
		Line:     f.currentFailure.line,
		Message:  strings.TrimSpace(f.currentFailure.message.String()),
	}

	if failure.TestName != "" {
		f.failures = append(f.failures, failure)
	}

	f.currentFailure = nil
}

// Summary generates a final summary after command completion.
func (f *PytestFormatter) Summary(exitCode int) string {
	// Finish any pending failure
	f.finishCurrentFailure()

	// Count results
	var passed, failed, skipped, errors int
	for _, r := range f.results {
		switch r.Status {
		case "PASSED":
			passed++
		case "FAILED":
			failed++
		case "SKIPPED":
			skipped++
		case "ERROR":
			errors++
		}
	}

	// If we have no results but command failed, return generic message
	if len(f.results) == 0 && exitCode != 0 {
		return f.failedStyle.Render("pytest failed with exit code " + strconv.Itoa(exitCode))
	}

	// No summary needed if all passed
	if failed == 0 && errors == 0 && exitCode == 0 {
		return ""
	}

	// Build summary with failure details
	var sb strings.Builder

	if len(f.failures) > 0 {
		sb.WriteString("\n")
		sb.WriteString(f.failedStyle.Render("Failures:"))
		sb.WriteString("\n")

		for _, fail := range f.failures {
			location := fail.File
			if fail.Line > 0 {
				location += ":" + strconv.Itoa(fail.Line)
			}
			sb.WriteString(f.failedStyle.Render("  - " + fail.TestName))
			if location != "" {
				sb.WriteString(" (" + location + ")")
			}
			if fail.Message != "" {
				sb.WriteString("\n    " + fail.Message)
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// GetSummary returns a structured summary of test results.
func (f *PytestFormatter) GetSummary() *PytestSummary {
	f.finishCurrentFailure()

	summary := &PytestSummary{
		Failures: make([]PytestFailure, len(f.failures)),
	}
	copy(summary.Failures, f.failures)

	for _, r := range f.results {
		switch r.Status {
		case "PASSED":
			summary.Passed++
		case "FAILED":
			summary.Failed++
		case "SKIPPED":
			summary.Skipped++
		case "ERROR":
			summary.Errors++
		}
	}

	return summary
}

// GetResults returns all parsed test results.
func (f *PytestFormatter) GetResults() []PytestResult {
	results := make([]PytestResult, len(f.results))
	copy(results, f.results)
	return results
}

// Reset clears all accumulated state.
func (f *PytestFormatter) Reset() {
	f.results = f.results[:0]
	f.failures = f.failures[:0]
	f.inFailures = false
	f.currentFailure = nil
	f.summaryLine = ""
}

// GetTestFailures implements output.TestSummaryProvider.
// Returns the list of test failures collected during processing.
func (f *PytestFormatter) GetTestFailures() []output.TestFailure {
	f.finishCurrentFailure()

	failures := make([]output.TestFailure, len(f.failures))
	for i, pf := range f.failures {
		failures[i] = output.TestFailure{
			TestName: pf.TestName,
			File:     pf.File,
			Line:     pf.Line,
			Message:  pf.Message,
		}
	}
	return failures
}

// GetTestCounts implements output.TestSummaryProvider.
// Returns (passed, failed, skipped, errors) counts.
func (f *PytestFormatter) GetTestCounts() (passed, failed, skipped, errors int) {
	for _, r := range f.results {
		switch r.Status {
		case "PASSED":
			passed++
		case "FAILED":
			failed++
		case "SKIPPED":
			skipped++
		case "ERROR":
			errors++
		}
	}
	return
}
