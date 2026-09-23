package formatters

import (
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

// ParseRunOutcome detects the test framework from the command and its
// output (the run log) and extracts counts, failures, and the no-tests
// signal in one pass. Returns a zero Outcome when no framework matches.
func ParseRunOutcome(command string, log []byte) Outcome {
	var o Outcome
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
