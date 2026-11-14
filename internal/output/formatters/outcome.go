package formatters

import (
	"regexp"
	"strings"

	"github.com/rileyhilliard/rr/internal/output"
	"github.com/rileyhilliard/rr/internal/util"
)

// Outcome is what a finished run's output says about its tests. It's parsed
// once from the run log and rendered twice: into result details for
// structured output and into the failure block for --pretty.
type Outcome struct {
	// Summary holds aggregate counts. Nil when no test framework was
	// recognized or the output had no parseable results.
	Summary *TestSummary
	// Failures lists failed tests with their full messages. Renderers
	// truncate as they see fit.
	Failures []output.TestFailure
	// NoTests is set when the runner explicitly reported running no tests.
	NoTests bool
	// PipedExitCode is set on a zero-test run whose command pipes its
	// output: the shell reports the last stage's exit status, so a runner
	// that failed upstream can still exit 0.
	PipedExitCode bool
}

// ansiEscapePattern matches ANSI CSI escape sequences (colors, cursor moves):
// ESC [, parameter bytes (0x30-0x3F, which includes the ":" in
// "38:2::255:0:0"), intermediate bytes (0x20-0x2F), a final byte (0x40-0x7E).
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]`)

// StripANSI removes ANSI escape sequences (colors, cursor moves) from output.
func StripANSI(b []byte) []byte {
	return ansiEscapePattern.ReplaceAll(b, nil)
}

// ParseRunOutcome detects the test framework from the command and its
// output (the run log) and extracts counts, failures, and the no-tests
// signal in one pass. Returns a zero Outcome when no framework matches.
func ParseRunOutcome(command string, log []byte) Outcome {
	var o Outcome
	// Runners color their output when the remote gives them a TTY; the
	// patterns match plain text.
	log = StripANSI(log)
	formatter := detectFormatter(command, log)
	if formatter == nil {
		return o
	}

	for _, line := range strings.Split(string(log), "\n") {
		formatter.ProcessLine(line)
	}

	if summary, ok := summarize(formatter, command); ok {
		o.Summary = &summary
		o.NoTests = summary.NoTests
		o.PipedExitCode = summary.NoTests && util.HasPipe(command)
	}
	if provider, ok := formatter.(output.TestSummaryProvider); ok {
		o.Failures = provider.GetTestFailures()
	}
	return o
}
