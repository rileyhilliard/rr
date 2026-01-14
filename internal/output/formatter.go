package output

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/internal/util"
)

// Formatter processes command output lines for display.
type Formatter interface {
	// Name returns the formatter identifier.
	Name() string

	// ProcessLine transforms a single line of output.
	// ANSI codes should pass through unchanged.
	ProcessLine(line string) string

	// Summary generates a final summary after command completion.
	// exitCode is the command's exit code (0 for success).
	Summary(exitCode int) string
}

// TestFailure represents a single test failure with location and message.
type TestFailure struct {
	TestName string
	File     string
	Line     int
	Message  string
}

// TestSummaryProvider is an optional interface for formatters that track test results.
// Formatters implementing this interface can provide structured test failure data
// for enhanced summary display.
type TestSummaryProvider interface {
	// GetTestFailures returns the list of test failures collected during processing.
	GetTestFailures() []TestFailure
	// GetTestCounts returns (passed, failed, skipped, errors) counts.
	GetTestCounts() (passed, failed, skipped, errors int)
}

// GenericFormatter provides simple passthrough with error highlighting.
type GenericFormatter struct {
	errorStyle lipgloss.Style
}

// NewGenericFormatter creates a formatter with default error styling.
func NewGenericFormatter() *GenericFormatter {
	return &GenericFormatter{
		errorStyle: lipgloss.NewStyle().Foreground(ui.ColorError),
	}
}

// Name returns "generic".
func (f *GenericFormatter) Name() string {
	return "generic"
}

// ProcessLine highlights error lines in red.
// Detects lines starting with "error:", "Error:", "ERROR", etc.
func (f *GenericFormatter) ProcessLine(line string) string {
	if isErrorLine(line) {
		return f.errorStyle.Render(line)
	}
	return line
}

// Summary returns a simple exit status message.
func (f *GenericFormatter) Summary(exitCode int) string {
	if exitCode == 0 {
		return ""
	}

	style := lipgloss.NewStyle().Foreground(ui.ColorError)
	return style.Render("Command failed with exit code " + util.Itoa(exitCode))
}

// isErrorLine checks if a line appears to be an error message.
func isErrorLine(line string) bool {
	lower := strings.ToLower(line)
	trimmed := strings.TrimSpace(lower)

	// Check for common error prefixes
	errorPrefixes := []string{
		"error:",
		"error ",
		"fatal:",
		"fatal ",
		"panic:",
		"exception:",
		"fail:",
		"failed:",
	}

	for _, prefix := range errorPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}

	// Check for all-caps ERROR anywhere in the line
	if strings.Contains(line, "ERROR") {
		return true
	}

	// Check for FAILED at start of line (common in test output)
	if strings.HasPrefix(strings.TrimSpace(line), "FAILED") {
		return true
	}

	return false
}

// PassthroughFormatter passes all lines through unchanged.
type PassthroughFormatter struct{}

// NewPassthroughFormatter creates a no-op formatter.
func NewPassthroughFormatter() *PassthroughFormatter {
	return &PassthroughFormatter{}
}

// Name returns "passthrough".
func (f *PassthroughFormatter) Name() string {
	return "passthrough"
}

// ProcessLine returns the line unchanged.
func (f *PassthroughFormatter) ProcessLine(line string) string {
	return line
}

// Summary returns an empty string.
func (f *PassthroughFormatter) Summary(_ int) string {
	return ""
}

// FormatterRegistry holds available formatters by name.
type FormatterRegistry struct {
	formatters map[string]Formatter
}

// NewFormatterRegistry creates a registry with default formatters.
func NewFormatterRegistry() *FormatterRegistry {
	r := &FormatterRegistry{
		formatters: make(map[string]Formatter),
	}
	r.Register(NewGenericFormatter())
	r.Register(NewPassthroughFormatter())
	return r
}

// Register adds a formatter to the registry.
func (r *FormatterRegistry) Register(f Formatter) {
	r.formatters[f.Name()] = f
}

// Get returns a formatter by name, or the generic formatter if not found.
func (r *FormatterRegistry) Get(name string) Formatter {
	if f, ok := r.formatters[name]; ok {
		return f
	}
	return r.formatters["generic"]
}

// Names returns all registered formatter names.
func (r *FormatterRegistry) Names() []string {
	names := make([]string, 0, len(r.formatters))
	for name := range r.formatters {
		names = append(names, name)
	}
	return names
}
