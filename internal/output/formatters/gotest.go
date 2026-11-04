package formatters

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/ui"
)

// GoTestResult holds information about a single go test result.
type GoTestResult struct {
	Name     string
	Package  string
	Status   string // "PASS", "FAIL", "SKIP"
	Duration string
	Message  string // Error message for failures
	Location string // file:line for failures
}

// GoPackageResult holds information about a package test run.
type GoPackageResult struct {
	Name     string
	Status   string // "ok", "FAIL", "?"
	Duration string
	Message  string // e.g., "[no test files]"
}

// GoTestFormatter parses and formats go test output.
type GoTestFormatter struct {
	passStyle    lipgloss.Style
	failStyle    lipgloss.Style
	skipStyle    lipgloss.Style
	mutedStyle   lipgloss.Style
	packageStyle lipgloss.Style

	// State tracking
	tests       []GoTestResult
	packages    []GoPackageResult
	currentTest string
	currentPkg  string
	// Test whose log lines come next. go test -v names it in === RUN, PAUSE,
	// CONT and NAME lines (parallel tests interleave); without -v a test's
	// log lines follow its --- FAIL line.
	owner string
	// Failure location per test, until its package result ends.
	blocks map[string]*goTestBlock
}

// goTestBlock is the first file:line a test logged, with the message on
// that line and the more-indented lines under it (testify's details).
type goTestBlock struct {
	location   string
	message    []string
	indent     int  // indent of the location line
	collecting bool // still reading lines under the location line
	result     int  // index in tests once --- FAIL is seen, else -1
}

// NewGoTestFormatter creates a new go test output formatter.
func NewGoTestFormatter() *GoTestFormatter {
	return &GoTestFormatter{
		passStyle:    lipgloss.NewStyle().Foreground(ui.ColorSuccess),
		failStyle:    lipgloss.NewStyle().Foreground(ui.ColorError),
		skipStyle:    lipgloss.NewStyle().Foreground(ui.ColorWarning),
		mutedStyle:   lipgloss.NewStyle().Foreground(ui.ColorMuted),
		packageStyle: lipgloss.NewStyle().Foreground(ui.ColorSecondary),
		tests:        make([]GoTestResult, 0),
		packages:     make([]GoPackageResult, 0),
		blocks:       make(map[string]*goTestBlock),
	}
}

// Name returns "gotest".
func (f *GoTestFormatter) Name() string {
	return "gotest"
}

// Regex patterns for go test output
var (
	// === RUN   TestName
	runPattern = regexp.MustCompile(`^=== RUN\s+(\S+)`)
	// === PAUSE/CONT/NAME TestName: which test's output follows
	ownerPattern = regexp.MustCompile(`^=== (?:PAUSE|CONT|NAME)\s+(\S+)`)
	// --- PASS: TestName (0.00s)
	passPattern = regexp.MustCompile(`^--- PASS: (\S+) \(([^)]+)\)`)
	// --- FAIL: TestName (0.00s)
	failPattern = regexp.MustCompile(`^--- FAIL: (\S+) \(([^)]+)\)`)
	// --- SKIP: TestName (0.00s)
	skipPattern = regexp.MustCompile(`^--- SKIP: (\S+) \(([^)]+)\)`)
	// ok      github.com/example/pkg    0.005s
	pkgOkPattern = regexp.MustCompile(`^ok\s+(\S+)\s+(\S+)`)
	// FAIL    github.com/example/pkg    0.005s
	pkgFailPattern = regexp.MustCompile(`^FAIL\s+(\S+)\s+(\S+)`)
	// ?       github.com/example/pkg    [no test files]
	pkgNoTestPattern = regexp.MustCompile(`^\?\s+(\S+)\s+\[([^\]]+)\]`)
	// file:line: error message (e.g., "    math_test.go:15: expected 6, got 5")
	errorLocationPattern = regexp.MustCompile(`^\s+(\S+\.go:\d+):\s*(.*)`)
	// PASS or FAIL (standalone)
	standalonePassPattern = regexp.MustCompile(`^PASS$`)
	standaloneFailPattern = regexp.MustCompile(`^FAIL$`)
)

// Detect returns a confidence score for whether this formatter should handle the output.
// Returns 100 if command contains "go test", 80 if output matches go test patterns, 0 otherwise.
func (f *GoTestFormatter) Detect(command string, output []byte) int {
	// Check command first
	if strings.Contains(command, "go test") {
		return 100
	}

	// Check output patterns
	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")

	for _, line := range lines {
		// Check for characteristic go test output patterns
		if runPattern.MatchString(line) ||
			passPattern.MatchString(line) ||
			failPattern.MatchString(line) ||
			pkgOkPattern.MatchString(line) ||
			pkgFailPattern.MatchString(line) ||
			pkgNoTestPattern.MatchString(line) {
			return 80
		}
	}

	return 0
}

// ProcessLine transforms a single line of go test output.
func (f *GoTestFormatter) ProcessLine(line string) string {
	// === RUN TestName
	if matches := runPattern.FindStringSubmatch(line); matches != nil {
		f.currentTest = matches[1]
		f.owner = matches[1]
		delete(f.blocks, matches[1])
		return f.mutedStyle.Render(line)
	}

	// === PAUSE/CONT/NAME TestName
	if matches := ownerPattern.FindStringSubmatch(line); matches != nil {
		f.owner = matches[1]
		return f.mutedStyle.Render(line)
	}

	// --- PASS: TestName (duration)
	if matches := passPattern.FindStringSubmatch(line); matches != nil {
		testName := matches[1]
		duration := matches[2]
		f.tests = append(f.tests, GoTestResult{
			Name:     testName,
			Status:   "PASS",
			Duration: duration,
		})
		f.currentTest = ""
		f.endTest(testName)
		return f.passStyle.Render(line)
	}

	// --- FAIL: TestName (duration)
	if matches := failPattern.FindStringSubmatch(line); matches != nil {
		testName := matches[1]
		duration := matches[2]
		f.tests = append(f.tests, GoTestResult{
			Name:     testName,
			Status:   "FAIL",
			Duration: duration,
		})
		// Without -v, this test's log lines come next.
		f.owner = testName
		f.block(testName).result = len(f.tests) - 1
		f.applyBlock(f.blocks[testName])
		f.currentTest = ""
		return f.failStyle.Render(line)
	}

	// --- SKIP: TestName (duration)
	if matches := skipPattern.FindStringSubmatch(line); matches != nil {
		testName := matches[1]
		duration := matches[2]
		f.tests = append(f.tests, GoTestResult{
			Name:     testName,
			Status:   "SKIP",
			Duration: duration,
		})
		f.currentTest = ""
		f.endTest(testName)
		return f.skipStyle.Render(line)
	}

	// ok package duration
	if matches := pkgOkPattern.FindStringSubmatch(line); matches != nil {
		pkgName := matches[1]
		duration := matches[2]
		f.packages = append(f.packages, GoPackageResult{
			Name:     pkgName,
			Status:   "ok",
			Duration: duration,
		})
		f.currentPkg = ""
		f.endPackage()
		return f.passStyle.Render(line)
	}

	// FAIL package duration
	if matches := pkgFailPattern.FindStringSubmatch(line); matches != nil {
		pkgName := matches[1]
		duration := matches[2]
		f.packages = append(f.packages, GoPackageResult{
			Name:     pkgName,
			Status:   "FAIL",
			Duration: duration,
		})
		f.currentPkg = ""
		f.endPackage()
		return f.failStyle.Render(line)
	}

	// ? package [no test files]
	if matches := pkgNoTestPattern.FindStringSubmatch(line); matches != nil {
		pkgName := matches[1]
		msg := matches[2]
		f.packages = append(f.packages, GoPackageResult{
			Name:    pkgName,
			Status:  "?",
			Message: msg,
		})
		return f.mutedStyle.Render(line)
	}

	// Standalone PASS
	if standalonePassPattern.MatchString(line) {
		return f.passStyle.Render(line)
	}

	// Standalone FAIL
	if standaloneFailPattern.MatchString(line) {
		return f.failStyle.Render(line)
	}

	// Error location (file:line: message)
	if matches := errorLocationPattern.FindStringSubmatch(line); matches != nil {
		f.recordLocation(line, matches[1], matches[2])
		return f.failStyle.Render(line)
	}
	f.recordContinuation(line)

	// If we're in a test and the line looks like test output, collect it
	if f.currentTest != "" && strings.HasPrefix(line, "    ") {
		// Check if it looks like an error
		if strings.Contains(strings.ToLower(line), "error") ||
			strings.Contains(line, "expected") ||
			strings.Contains(line, "got") {
			return f.failStyle.Render(line)
		}
	}

	return line
}

// Summary generates a summary after command completion.
func (f *GoTestFormatter) Summary(exitCode int) string {
	var parts []string

	// Count results
	var passed, failed, skipped int
	for _, t := range f.tests {
		switch t.Status {
		case "PASS":
			passed++
		case "FAIL":
			failed++
		case "SKIP":
			skipped++
		}
	}

	var pkgPassed, pkgFailed, pkgNoTests int
	for _, p := range f.packages {
		switch p.Status {
		case "ok":
			pkgPassed++
		case "FAIL":
			pkgFailed++
		case "?":
			pkgNoTests++
		}
	}

	// If we have test results, show them
	if passed > 0 || failed > 0 || skipped > 0 {
		var summary strings.Builder

		if failed > 0 {
			summary.WriteString(f.failStyle.Render(fmt.Sprintf("%d failed", failed)))
		}
		if passed > 0 {
			if summary.Len() > 0 {
				summary.WriteString(", ")
			}
			summary.WriteString(f.passStyle.Render(fmt.Sprintf("%d passed", passed)))
		}
		if skipped > 0 {
			if summary.Len() > 0 {
				summary.WriteString(", ")
			}
			summary.WriteString(f.skipStyle.Render(fmt.Sprintf("%d skipped", skipped)))
		}

		total := passed + failed + skipped
		summary.WriteString(fmt.Sprintf(" (%d total)", total))
		parts = append(parts, summary.String())
	}

	// Show failed tests details
	if failed > 0 {
		parts = append(parts, "")
		parts = append(parts, f.failStyle.Render("Failed tests:"))
		for _, t := range f.tests {
			if t.Status == "FAIL" {
				failInfo := fmt.Sprintf("  - %s", t.Name)
				if t.Duration != "" {
					failInfo += fmt.Sprintf(" (%s)", t.Duration)
				}
				parts = append(parts, f.failStyle.Render(failInfo))
			}
		}
	}

	// Package summary if we have package results
	if pkgPassed > 0 || pkgFailed > 0 {
		parts = append(parts, "")
		pkgSummary := fmt.Sprintf("Packages: %d passed", pkgPassed)
		if pkgFailed > 0 {
			pkgSummary += fmt.Sprintf(", %d failed", pkgFailed)
		}
		if pkgNoTests > 0 {
			pkgSummary += fmt.Sprintf(", %d no tests", pkgNoTests)
		}
		parts = append(parts, pkgSummary)
	}

	// If nothing to report but we have an error, show generic failure
	if len(parts) == 0 && exitCode != 0 {
		return f.failStyle.Render(fmt.Sprintf("Tests failed with exit code %d", exitCode))
	}

	return strings.Join(parts, "\n")
}

// GetTests returns the collected test results.
func (f *GoTestFormatter) GetTests() []GoTestResult {
	return f.tests
}

// GetPackages returns the collected package results.
func (f *GoTestFormatter) GetPackages() []GoPackageResult {
	return f.packages
}

// Reset clears the formatter's state.
func (f *GoTestFormatter) Reset() {
	f.tests = make([]GoTestResult, 0)
	f.packages = make([]GoPackageResult, 0)
	f.currentTest = ""
	f.currentPkg = ""
	f.endPackage()
}

func (f *GoTestFormatter) block(test string) *goTestBlock {
	b := f.blocks[test]
	if b == nil {
		b = &goTestBlock{result: -1}
		f.blocks[test] = b
	}
	return b
}

// recordLocation keeps the owner's first file:line; it's the earliest
// assertion. A later one ends the message under the first.
func (f *GoTestFormatter) recordLocation(line, location, message string) {
	if f.owner == "" {
		return
	}
	b := f.block(f.owner)
	if b.location != "" {
		b.collecting = false
		return
	}
	b.location, b.indent, b.collecting = location, indentOf(line), true
	b.message = appendNonEmpty(nil, message)
	f.applyBlock(b)
}

// recordContinuation adds a line indented under the owner's location line
// to its message, and ends the message at any other line.
func (f *GoTestFormatter) recordContinuation(line string) {
	b := f.blocks[f.owner]
	if b == nil || !b.collecting {
		return
	}
	if strings.TrimSpace(line) == "" || indentOf(line) <= b.indent {
		b.collecting = false
		return
	}
	b.message = appendNonEmpty(b.message, line)
	f.applyBlock(b)
}

// applyBlock copies a block onto its failure once --- FAIL was seen.
func (f *GoTestFormatter) applyBlock(b *goTestBlock) {
	if b == nil || b.result < 0 {
		return
	}
	f.tests[b.result].Location = b.location
	f.tests[b.result].Message = strings.Join(b.message, "\n")
}

func (f *GoTestFormatter) endTest(test string) {
	delete(f.blocks, test)
	f.owner = ""
}

// endPackage drops all location state once a package result ends its
// tests' output.
func (f *GoTestFormatter) endPackage() {
	f.blocks = make(map[string]*goTestBlock)
	f.owner = ""
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func appendNonEmpty(lines []string, line string) []string {
	if line = strings.TrimSpace(line); line != "" {
		lines = append(lines, line)
	}
	return lines
}

// RanNothing implements output.NoTestsReporter.
//
// True only when go test reported packages and every one of them had no test
// files. Deliberately narrow: `go test -run NoSuchTest` leaves the package
// status "ok" (not "?") and exits 0, which is a normal dev-loop invocation
// rather than a defect, so it is not reported here.
func (f *GoTestFormatter) RanNothing() bool {
	if len(f.tests) > 0 || len(f.packages) == 0 {
		return false
	}
	for _, p := range f.packages {
		if p.Status != "?" {
			return false
		}
	}
	return true
}

// GetTestFailures implements output.TestSummaryProvider.
// Returns the list of test failures collected during processing.
func (f *GoTestFormatter) GetTestFailures() []output.TestFailure {
	var failures []output.TestFailure
	for _, t := range f.tests {
		if t.Status == "FAIL" {
			// Parse location if available (format: "file.go:line")
			file := ""
			line := 0
			if t.Location != "" {
				parts := strings.SplitN(t.Location, ":", 2)
				file = parts[0]
				if len(parts) > 1 {
					if l, err := strconv.Atoi(parts[1]); err == nil {
						line = l
					}
				}
			}

			failures = append(failures, output.TestFailure{
				TestName: t.Name,
				File:     file,
				Line:     line,
				Message:  t.Message,
			})
		}
	}
	return failures
}

// GetTestCounts implements output.TestSummaryProvider.
// Returns (passed, failed, skipped, errors) counts.
func (f *GoTestFormatter) GetTestCounts() (passed, failed, skipped, errors int) {
	for _, t := range f.tests {
		switch t.Status {
		case "PASS":
			passed++
		case "FAIL":
			failed++
		case "SKIP":
			skipped++
		}
	}
	return passed, failed, skipped, 0
}
