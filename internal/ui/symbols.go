package ui

// Cyber glyph symbols for status indicators - Gen Z aesthetic
const (
	SymbolSuccess  = "◉" // Task completed successfully (filled target)
	SymbolFail     = "✕" // Task failed (clean X)
	SymbolPending  = "◇" // Task not yet started (empty diamond)
	SymbolSyncing  = "◐" // Task syncing/waiting for lock (half-filled)
	SymbolProgress = "◆" // Task in progress (filled diamond)
	SymbolComplete = "●" // Task done (solid circle)
	SymbolSkipped  = "⊖" // Task skipped (circled minus)
)

// Connection status symbols
const (
	SymbolConnected   = "◉" // Connected (solid signal)
	SymbolUnreachable = "◌" // Unreachable (dashed circle)
	SymbolSlow        = "◔" // Slow connection (partially filled)
)
