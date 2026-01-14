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

// detectFormatter returns the best matching formatter for the command/output.
// Returns nil if no specific formatter matches well.
func detectFormatter(command string, rawOutput []byte) output.Formatter {
	// List of formatter factories with their detectors
	type formatterFactory struct {
		create func() output.Formatter
		detect func(string, []byte) int
	}

	factories := []formatterFactory{
		{
			create: func() output.Formatter { return NewPytestFormatter() },
			detect: func(cmd string, out []byte) int { return NewPytestFormatter().Detect(cmd, out) },
		},
		{
			create: func() output.Formatter { return NewGoTestFormatter() },
			detect: func(cmd string, out []byte) int { return NewGoTestFormatter().Detect(cmd, out) },
		},
		{
			create: func() output.Formatter { return NewJestFormatter() },
			detect: func(cmd string, out []byte) int { return NewJestFormatter().Detect(cmd, out) },
		},
	}

	var bestFormatter output.Formatter
	bestScore := 0

	for _, f := range factories {
		score := f.detect(command, rawOutput)
		if score > bestScore {
			bestScore = score
			bestFormatter = f.create()
		}
	}

	// Only return if we have a reasonable confidence
	if bestScore >= 50 {
		return bestFormatter
	}

	return nil
}

// FormatFailureSummary returns a formatted string summarizing failures.
// This provides a more readable output than raw log lines.
func FormatFailureSummary(command string, rawOutput []byte, maxFailures int) string {
	failures := ExtractFailures(command, rawOutput)
	if len(failures) == 0 {
		return ""
	}

	var sb strings.Builder

	// Limit number of failures shown
	showCount := len(failures)
	if maxFailures > 0 && showCount > maxFailures {
		showCount = maxFailures
	}

	for i := 0; i < showCount; i++ {
		f := failures[i]
		// Format: TestName (file:line)
		//           message
		sb.WriteString("  ")
		sb.WriteString(f.TestName)
		if f.File != "" {
			sb.WriteString(" (")
			sb.WriteString(f.File)
			if f.Line > 0 {
				sb.WriteString(":")
				sb.WriteString(itoa(f.Line))
			}
			sb.WriteString(")")
		}
		sb.WriteString("\n")
		if f.Message != "" {
			// Indent the message
			for _, line := range strings.Split(f.Message, "\n") {
				sb.WriteString("    ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}
		}
	}

	if len(failures) > showCount {
		sb.WriteString("  ... and ")
		sb.WriteString(itoa(len(failures) - showCount))
		sb.WriteString(" more failures\n")
	}

	return sb.String()
}

// itoa converts int to string without importing strconv
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
