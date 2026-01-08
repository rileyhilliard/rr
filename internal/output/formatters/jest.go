package formatters

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/ui"
)

// JestTestResult represents a single Jest test result.
type JestTestResult struct {
	Name     string
	Passed   bool
	Duration string
}

// JestTestFailure captures details about a failed Jest test.
type JestTestFailure struct {
	TestName     string
	SuiteName    string
	ErrorMessage string
	StackTrace   string
}

// JestFormatter parses Jest/Vitest test output.
type JestFormatter struct {
	// Collected results
	suites     []string
	tests      []JestTestResult
	failures   []JestTestFailure
	inFailure  bool
	failureAcc []string

	// Styles
	passStyle    lipgloss.Style
	failStyle    lipgloss.Style
	testStyle    lipgloss.Style
	mutedStyle   lipgloss.Style
	successStyle lipgloss.Style
	errorStyle   lipgloss.Style

	// Counts from summary line
	suitesPassed int
	suitesFailed int
	suitesTotal  int
	testsPassed  int
	testsFailed  int
	testsTotal   int
	duration     string
}

// NewJestFormatter creates a Jest output formatter.
func NewJestFormatter() *JestFormatter {
	return &JestFormatter{
		passStyle:    lipgloss.NewStyle().Foreground(ui.ColorSuccess),
		failStyle:    lipgloss.NewStyle().Foreground(ui.ColorError),
		testStyle:    lipgloss.NewStyle().Foreground(ui.ColorPrimary),
		mutedStyle:   lipgloss.NewStyle().Foreground(ui.ColorMuted),
		successStyle: lipgloss.NewStyle().Foreground(ui.ColorSuccess).Bold(true),
		errorStyle:   lipgloss.NewStyle().Foreground(ui.ColorError).Bold(true),
	}
}

// Name returns the formatter identifier.
func (f *JestFormatter) Name() string {
	return "jest"
}

// Regex patterns for parsing Jest output
var (
	// PASS/FAIL suite lines: " PASS  src/utils.test.js" or " FAIL  src/math.test.js"
	jestSuitePassPattern = regexp.MustCompile(`^\s*(PASS)\s+(.+)$`)
	jestSuiteFailPattern = regexp.MustCompile(`^\s*(FAIL)\s+(.+)$`)

	// Individual test results: "  ✓ should add numbers (3ms)" or "  ✕ should multiply numbers (5ms)"
	jestTestPassPattern = regexp.MustCompile(`^\s*[✓✔]\s+(.+?)(?:\s+\((\d+\s*m?s)\))?$`)
	jestTestFailPattern = regexp.MustCompile(`^\s*[✕✗×]\s+(.+?)(?:\s+\((\d+\s*m?s)\))?$`)

	// Failure header: "  ● should multiply numbers"
	jestFailureHeaderPattern = regexp.MustCompile(`^\s*●\s+(.+)$`)

	// Summary lines
	jestSuiteSummaryPattern = regexp.MustCompile(`Test Suites:\s+(?:(\d+)\s+failed,\s+)?(?:(\d+)\s+passed,\s+)?(\d+)\s+total`)
	jestTestSummaryPattern  = regexp.MustCompile(`Tests:\s+(?:(\d+)\s+failed,\s+)?(?:(\d+)\s+passed,\s+)?(\d+)\s+total`)
	jestTimeSummaryPattern  = regexp.MustCompile(`Time:\s+(.+)`)

	// Stack trace indicator (line starting with "at ")
	jestStackTracePattern = regexp.MustCompile(`^\s+at\s+`)
)

// ProcessLine transforms a single line of Jest output.
func (f *JestFormatter) ProcessLine(line string) string {
	// Check for failure section end (new section, new failure header, or summary)
	// Jest failure blocks contain blank lines, so we only end on structural changes
	if f.inFailure {
		if jestSuitePassPattern.MatchString(line) ||
			jestSuiteFailPattern.MatchString(line) ||
			jestSuiteSummaryPattern.MatchString(line) ||
			jestTestSummaryPattern.MatchString(line) {
			f.finishFailure()
		}
	}

	// PASS suite line
	if matches := jestSuitePassPattern.FindStringSubmatch(line); matches != nil {
		f.suites = append(f.suites, matches[2])
		return f.passStyle.Render(" PASS ") + " " + matches[2]
	}

	// FAIL suite line
	if matches := jestSuiteFailPattern.FindStringSubmatch(line); matches != nil {
		f.suites = append(f.suites, matches[2])
		return f.failStyle.Render(" FAIL ") + " " + matches[2]
	}

	// Individual test pass
	if matches := jestTestPassPattern.FindStringSubmatch(line); matches != nil {
		testName := matches[1]
		duration := ""
		if len(matches) > 2 && matches[2] != "" {
			duration = matches[2]
		}
		f.tests = append(f.tests, JestTestResult{Name: testName, Passed: true, Duration: duration})
		result := "  " + f.passStyle.Render("✓") + " " + testName
		if duration != "" {
			result += " " + f.mutedStyle.Render("("+duration+")")
		}
		return result
	}

	// Individual test fail
	if matches := jestTestFailPattern.FindStringSubmatch(line); matches != nil {
		testName := matches[1]
		duration := ""
		if len(matches) > 2 && matches[2] != "" {
			duration = matches[2]
		}
		f.tests = append(f.tests, JestTestResult{Name: testName, Passed: false, Duration: duration})
		result := "  " + f.failStyle.Render("✕") + " " + testName
		if duration != "" {
			result += " " + f.mutedStyle.Render("("+duration+")")
		}
		return result
	}

	// Failure header starts failure block
	if matches := jestFailureHeaderPattern.FindStringSubmatch(line); matches != nil {
		if f.inFailure {
			f.finishFailure()
		}
		f.inFailure = true
		f.failureAcc = []string{matches[1]}
		return "  " + f.failStyle.Render("●") + " " + matches[1]
	}

	// Accumulate failure details
	if f.inFailure {
		f.failureAcc = append(f.failureAcc, line)
	}

	// Parse summary lines
	if matches := jestSuiteSummaryPattern.FindStringSubmatch(line); matches != nil {
		f.suitesFailed = jestParseIntOrZero(matches[1])
		f.suitesPassed = jestParseIntOrZero(matches[2])
		f.suitesTotal = jestParseIntOrZero(matches[3])
	}

	if matches := jestTestSummaryPattern.FindStringSubmatch(line); matches != nil {
		f.testsFailed = jestParseIntOrZero(matches[1])
		f.testsPassed = jestParseIntOrZero(matches[2])
		f.testsTotal = jestParseIntOrZero(matches[3])
	}

	if matches := jestTimeSummaryPattern.FindStringSubmatch(line); matches != nil {
		f.duration = matches[1]
	}

	return line
}

// finishFailure processes accumulated failure data.
func (f *JestFormatter) finishFailure() {
	if len(f.failureAcc) == 0 {
		f.inFailure = false
		return
	}

	testName := f.failureAcc[0]
	var errorLines []string
	var stackLines []string

	for i := 1; i < len(f.failureAcc); i++ {
		line := f.failureAcc[i]
		if jestStackTracePattern.MatchString(line) {
			stackLines = append(stackLines, line)
		} else if strings.TrimSpace(line) != "" {
			errorLines = append(errorLines, line)
		}
	}

	f.failures = append(f.failures, JestTestFailure{
		TestName:     testName,
		ErrorMessage: strings.Join(errorLines, "\n"),
		StackTrace:   strings.Join(stackLines, "\n"),
	})

	f.inFailure = false
	f.failureAcc = nil
}

// Summary generates a final summary after Jest completes.
func (f *JestFormatter) Summary(exitCode int) string {
	// Finish any pending failure
	if f.inFailure {
		f.finishFailure()
	}

	var parts []string

	if exitCode == 0 {
		// Success summary
		if f.testsTotal > 0 {
			msg := f.successStyle.Render("All tests passed!")
			if f.testsTotal > 0 {
				msg += f.mutedStyle.Render(" (" + strconv.Itoa(f.testsTotal) + " tests)")
			}
			parts = append(parts, msg)
		}
	} else {
		// Failure summary
		if f.testsFailed > 0 {
			msg := f.errorStyle.Render(strconv.Itoa(f.testsFailed) + " test(s) failed")
			if f.testsTotal > 0 {
				msg += f.mutedStyle.Render(" out of " + strconv.Itoa(f.testsTotal))
			}
			parts = append(parts, msg)
		} else {
			parts = append(parts, f.errorStyle.Render("Tests failed with exit code "+strconv.Itoa(exitCode)))
		}

		// List failed tests
		for _, failure := range f.failures {
			parts = append(parts, f.failStyle.Render("  "+bulletPoint+" "+failure.TestName))
		}
	}

	return strings.Join(parts, "\n")
}

// Detect returns a confidence score for Jest output detection.
func (f *JestFormatter) Detect(command string, output []byte) int {
	// High confidence if command contains jest or vitest
	lowerCmd := strings.ToLower(command)
	if strings.Contains(lowerCmd, "jest") || strings.Contains(lowerCmd, "vitest") {
		return 100
	}

	// Check output for Jest-style patterns
	outStr := string(output)
	if jestSuitePassPattern.MatchString(outStr) || jestSuiteFailPattern.MatchString(outStr) {
		return 80
	}

	// Check for Jest summary line patterns
	if jestSuiteSummaryPattern.MatchString(outStr) || jestTestSummaryPattern.MatchString(outStr) {
		return 70
	}

	return 0
}

// GetFailures returns collected test failures.
func (f *JestFormatter) GetFailures() []JestTestFailure {
	// Finish any pending failure first
	if f.inFailure {
		f.finishFailure()
	}
	return f.failures
}

// GetTestResults returns all collected test results.
func (f *JestFormatter) GetTestResults() []JestTestResult {
	return f.tests
}

// Reset clears all accumulated state.
func (f *JestFormatter) Reset() {
	f.suites = nil
	f.tests = nil
	f.failures = nil
	f.inFailure = false
	f.failureAcc = nil
	f.suitesPassed = 0
	f.suitesFailed = 0
	f.suitesTotal = 0
	f.testsPassed = 0
	f.testsFailed = 0
	f.testsTotal = 0
	f.duration = ""
}

// bulletPoint is used in summary output.
const bulletPoint = "\u2022"

// jestParseIntOrZero safely parses an int, returning 0 on error.
func jestParseIntOrZero(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
