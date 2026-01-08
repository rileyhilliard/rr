package ui

// Unicode symbols for status indicators.
// Matches proof-of-concept.sh symbols where applicable:
//   SYM_PASS='✓'
//   SYM_FAIL='✗'
const (
	SymbolSuccess  = "✓" // Task completed successfully
	SymbolFail     = "✗" // Task failed
	SymbolPending  = "○" // Task not yet started
	SymbolProgress = "◐" // Task in progress
	SymbolComplete = "●" // Task done (alternative to success)
	SymbolSkipped  = "⊘" // Task skipped
)
