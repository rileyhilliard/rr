package formatters

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/ui"
)

// GoTestResult holds information about a single go test result.
type GoTestResult struct {
	Name     string
	Package  string
	Status   string // "PASS", "FAIL", "SKIP"
	Duration string
	Message  string   // Error message for failures
	Location string   // file:line for failures
	Output   []string // Additional output lines
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
	tests        []GoTestResult
	packages     []GoPackageResult
	currentTest  string
	currentPkg   string
	failureLines []string
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
		failureLines: make([]string, 0),
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
		return f.passStyle.Render(line)
	}

	// --- FAIL: TestName (duration)
	if matches := failPattern.FindStringSubmatch(line); matches != nil {
		testName := matches[1]
		duration := matches[2]
		result := GoTestResult{
			Name:     testName,
			Status:   "FAIL",
			Duration: duration,
			Output:   append([]string{}, f.failureLines...),
		}
		f.tests = append(f.tests, result)
		f.failureLines = nil
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
		f.failureLines = append(f.failureLines, line)
		// Update the last failed test's location if we have one pending
		if f.currentTest != "" {
			location := matches[1]
			message := matches[2]
			// Store for when the test result comes in
			f.failureLines = append(f.failureLines, fmt.Sprintf("%s: %s", location, message))
		}
		return f.failStyle.Render(line)
	}

	// If we're in a test and the line looks like test output, collect it
	if f.currentTest != "" && strings.HasPrefix(line, "    ") {
		f.failureLines = append(f.failureLines, line)
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
	f.failureLines = make([]string, 0)
}
