package formatters

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/output"
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
	// File and Line locate a vitest failure (from its "❯ file:line:col" line).
	File string
	Line int
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

	// Vitest's default reporter lists failures after a "⎯⎯ Failed Tests N ⎯⎯"
	// or "⎯⎯ Failed Suites N ⎯⎯" header, one " FAIL  file > test" block each.
	// vitestSection is "tests" or "suites" inside those sections; vitestFailure
	// is the block being read, and vitestInFrame is set once its location line
	// has passed (what follows is the code frame, not the message).
	vitestSection string
	vitestFailure *JestTestFailure
	vitestInFrame bool
	vitestMessage []string
	suitesErrored int
	testsSkipped  int
	vitestSummary bool

	// noTestsRan is set by an explicit zero-collection message: vitest's
	// "Tests  no tests" summary, or jest's / vitest's "No tests found".
	noTestsRan bool
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

	// Vitest prints a colon-less summary and, when nothing was collected,
	// the literal "no tests" where a count would be: "Tests  no tests".
	// Neither summary pattern above matches that form.
	jestNoTestsPattern = regexp.MustCompile(`^\s*Tests\s+no tests\s*$`)
	// jest prints no summary at all when it matches nothing, so its only
	// signal is this message; vitest emits the "No test files found" variant.
	jestNoTestsFoundPattern = regexp.MustCompile(`(?i)^\s*No test(?:s| files) found`)
	jestTimeSummaryPattern  = regexp.MustCompile(`Time:\s+(.+)`)

	// Vitest's default reporter. Summary: "Tests  1 failed | 468 passed (469)".
	vitestTestSummaryPattern = regexp.MustCompile(`^\s*Tests\s{2,}(.*\d+\s+\w+.*?)\s+\((\d+)\)\s*$`)
	vitestCountPattern       = regexp.MustCompile(`(\d+)\s+(failed|passed|skipped|todo)`)
	vitestFileSummaryPattern = regexp.MustCompile(`(?m)^\s*Test Files\s{2,}.*\(\d+\)\s*$`)
	// "⎯⎯⎯ Failed Tests 1 ⎯⎯⎯" and "⎯⎯⎯ Failed Suites 1 ⎯⎯⎯"
	vitestSectionPattern = regexp.MustCompile(`^⎯+\s+Failed (Tests|Suites) \d+\s+⎯+\s*$`)
	// " FAIL  tests/a.test.ts > suite > test" or " FAIL  tests/a.test.ts [ tests/a.test.ts ]"
	vitestFailPattern = regexp.MustCompile(`^\s*FAIL\s+(\S+)(?:\s+>\s+(.+?)|\s+\[.*\])?\s*$`)
	// " ❯ tests/a.test.ts:8:29"
	vitestLocationPattern = regexp.MustCompile(`^\s*❯\s+(\S+?):(\d+):\d+\s*$`)
	// "⎯⎯⎯⎯⎯[1/2]⎯" closes a failure block.
	vitestSeparatorPattern = regexp.MustCompile(`^⎯{3,}(?:\[\d+/\d+\]⎯*)?\s*$`)

	// Stack trace indicator (line starting with "at ")
	jestStackTracePattern = regexp.MustCompile(`^\s+at\s+`)
)

// ProcessLine transforms a single line of Jest output.
func (f *JestFormatter) ProcessLine(line string) string {
	if f.processVitestLine(line) {
		return line
	}

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

	// Explicit zero-collection: vitest's summary line, or the "No tests found"
	// message that is jest's only signal (it prints no summary in that case).
	if jestNoTestsPattern.MatchString(line) || jestNoTestsFoundPattern.MatchString(line) {
		f.noTestsRan = true
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

// processVitestLine handles vitest's summary and its failure sections.
// It returns true when the line belongs to vitest's failure listing, so the
// jest patterns (which read " FAIL  file" as a suite line) skip it.
func (f *JestFormatter) processVitestLine(line string) bool {
	// The summary goes to stdout and the failure listing to stderr, so in a
	// combined log it can land between (or inside) failure blocks: it doesn't
	// end the section.
	if matches := vitestTestSummaryPattern.FindStringSubmatch(line); matches != nil {
		f.vitestSummary = true
		f.testsTotal = jestParseIntOrZero(matches[2])
		for _, count := range vitestCountPattern.FindAllStringSubmatch(matches[1], -1) {
			n := jestParseIntOrZero(count[1])
			switch count[2] {
			case "failed":
				f.testsFailed = n
			case "passed":
				f.testsPassed = n
			case "skipped", "todo":
				f.testsSkipped += n
			}
		}
		return true
	}

	if matches := vitestSectionPattern.FindStringSubmatch(line); matches != nil {
		f.finishVitestFailure()
		f.vitestSection = strings.ToLower(matches[1])
		return true
	}
	if f.vitestSection == "" {
		return false
	}

	if matches := vitestFailPattern.FindStringSubmatch(line); matches != nil {
		f.finishVitestFailure()
		name := matches[2]
		if f.vitestSection == "suites" {
			name = matches[1]
			f.suitesErrored++
		}
		f.vitestFailure = &JestTestFailure{TestName: name, File: matches[1]}
		return true
	}
	if vitestSeparatorPattern.MatchString(line) {
		f.finishVitestFailure()
		return true
	}
	if f.vitestFailure == nil {
		// Blank lines and anything else between blocks.
		return strings.TrimSpace(line) == ""
	}

	if matches := vitestLocationPattern.FindStringSubmatch(line); matches != nil && !f.vitestInFrame {
		f.vitestInFrame = true
		// The first location is the failing line in the test file itself; a
		// helper frame points elsewhere, so only take a matching file's line.
		if matches[1] == f.vitestFailure.File {
			f.vitestFailure.Line = jestParseIntOrZero(matches[2])
		}
		return true
	}
	if !f.vitestInFrame {
		f.vitestMessage = append(f.vitestMessage, line)
	}
	return true
}

// finishVitestFailure records the vitest failure block being read, if any.
func (f *JestFormatter) finishVitestFailure() {
	if f.vitestFailure == nil {
		return
	}
	f.vitestFailure.ErrorMessage = strings.TrimSpace(strings.Join(f.vitestMessage, "\n"))
	f.failures = append(f.failures, *f.vitestFailure)
	f.vitestFailure = nil
	f.vitestInFrame = false
	f.vitestMessage = nil
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
	f.finishVitestFailure()

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

	// Vitest's "Test Files  1 failed | 31 passed (32)" summary, for commands
	// like "npm test" that don't name the runner.
	if vitestFileSummaryPattern.MatchString(outStr) {
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
	f.finishVitestFailure()
	return f.failures
}

// GetTestResults returns all collected test results.
func (f *JestFormatter) GetTestResults() []JestTestResult {
	return f.tests
}

// GetTestFailures implements output.TestSummaryProvider.
// Returns the list of test failures collected during processing.
func (f *JestFormatter) GetTestFailures() []output.TestFailure {
	if f.inFailure {
		f.finishFailure()
	}
	f.finishVitestFailure()

	failures := make([]output.TestFailure, 0, len(f.failures))
	for _, fail := range f.failures {
		name := fail.TestName
		if fail.SuiteName != "" && name != "" {
			name = fail.SuiteName + " > " + name
		} else if name == "" {
			name = fail.SuiteName
		}

		message := fail.ErrorMessage
		if message == "" {
			message = fail.StackTrace
		}

		file := fail.SuiteName
		if fail.File != "" {
			file = fail.File
		}

		failures = append(failures, output.TestFailure{
			TestName: name,
			File:     file,
			Line:     fail.Line,
			Message:  message,
		})
	}
	return failures
}

// GetTestCounts implements output.TestSummaryProvider.
// Counts come from the summary line when present; otherwise they are derived
// from the individual test results seen in the stream (vitest's compact
// reporter prints per-test lines but a summary rr can't parse).
func (f *JestFormatter) GetTestCounts() (passed, failed, skipped, errors int) {
	if f.vitestSummary {
		// Vitest names every count; a file that failed to load ran no tests
		// and shows up only in the "Failed Suites" section.
		return f.testsPassed, f.testsFailed, f.testsSkipped, f.suitesErrored
	}
	if f.testsTotal > 0 || f.testsPassed > 0 || f.testsFailed > 0 {
		skipped = f.testsTotal - f.testsPassed - f.testsFailed
		if skipped < 0 {
			skipped = 0
		}
		return f.testsPassed, f.testsFailed, skipped, 0
	}

	for _, t := range f.tests {
		if t.Passed {
			passed++
		} else {
			failed++
		}
	}
	return passed, failed, 0, 0
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
	f.noTestsRan = false
	f.vitestSection = ""
	f.vitestFailure = nil
	f.vitestInFrame = false
	f.vitestMessage = nil
	f.suitesErrored = 0
	f.testsSkipped = 0
	f.vitestSummary = false
}

// RanNothing implements output.NoTestsReporter.
//
// True only when the reporter explicitly said no tests ran - vitest's
// "Tests  no tests", or jest's / vitest's "No tests found" - or when vitest's
// summary shows nothing passed, failed, or errored (every test skipped). A zero or absent
// summary is not evidence on its own: plenty of non-test output scores as jest,
// and jest prints no summary at all when it finds nothing.
func (f *JestFormatter) RanNothing() bool {
	if f.vitestSummary {
		// Vitest reports a filter that matched nothing as every test skipped.
		return f.testsPassed+f.testsFailed+f.suitesErrored == 0
	}
	if len(f.tests) > 0 {
		return false
	}
	if f.testsPassed+f.testsFailed+f.testsTotal > 0 {
		return false
	}
	return f.noTestsRan
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
