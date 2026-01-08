package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TestFailure represents a single test failure for summary display.
// This mirrors output.TestFailure to avoid circular imports.
type TestFailure struct {
	TestName string
	File     string
	Line     int
	Message  string
}

// TestSummary holds test results for summary rendering.
type TestSummary struct {
	Passed   int
	Failed   int
	Skipped  int
	Errors   int
	Failures []TestFailure
}

// SummaryRenderer formats test summaries for terminal display.
type SummaryRenderer struct {
	errorStyle   lipgloss.Style
	successStyle lipgloss.Style
	pathStyle    lipgloss.Style
	mutedStyle   lipgloss.Style
}

// NewSummaryRenderer creates a new summary renderer with default styles.
func NewSummaryRenderer() *SummaryRenderer {
	return &SummaryRenderer{
		errorStyle:   lipgloss.NewStyle().Foreground(ColorError),
		successStyle: lipgloss.NewStyle().Foreground(ColorSuccess),
		pathStyle:    lipgloss.NewStyle().Foreground(ColorInfo),
		mutedStyle:   lipgloss.NewStyle().Foreground(ColorMuted),
	}
}

// RenderSummary generates a formatted test failure summary.
// If exitCode is 0 and there are no failures, returns an empty string.
// Otherwise, displays failure count and details for each failed test.
func RenderSummary(summary *TestSummary, exitCode int) string {
	r := NewSummaryRenderer()
	return r.Render(summary, exitCode)
}

// Render generates the formatted summary string.
func (r *SummaryRenderer) Render(summary *TestSummary, exitCode int) string {
	// Success case - no summary needed
	if exitCode == 0 && (summary == nil || len(summary.Failures) == 0) {
		return ""
	}

	// No failures to display
	if summary == nil || len(summary.Failures) == 0 {
		return ""
	}

	var sb strings.Builder

	// Failure count header
	failCount := len(summary.Failures)
	testWord := "test"
	if failCount != 1 {
		testWord = "tests"
	}
	sb.WriteString(r.errorStyle.Render(fmt.Sprintf("%s %d %s failed", SymbolFail, failCount, testWord)))
	sb.WriteString("\n")

	// Each failure
	for _, failure := range summary.Failures {
		sb.WriteString("\n")

		// File path with line number (clickable in terminals)
		location := failure.File
		if failure.Line > 0 {
			location += ":" + strconv.Itoa(failure.Line)
		}
		if location != "" {
			sb.WriteString("  ")
			sb.WriteString(r.pathStyle.Render(location))
			sb.WriteString("\n")
		}

		// Test name
		sb.WriteString("    ")
		sb.WriteString(failure.TestName)
		sb.WriteString("\n")

		// Error message
		if failure.Message != "" {
			// Handle multi-line messages
			lines := strings.Split(failure.Message, "\n")
			for _, line := range lines {
				sb.WriteString("    ")
				sb.WriteString(r.mutedStyle.Render(line))
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// RenderSuccessSummary generates a simple success message.
func RenderSuccessSummary(passed int) string {
	r := NewSummaryRenderer()
	if passed == 0 {
		return ""
	}
	testWord := "test"
	if passed != 1 {
		testWord = "tests"
	}
	return r.successStyle.Render(fmt.Sprintf("%s %d %s passed", SymbolSuccess, passed, testWord))
}
