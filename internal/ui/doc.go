// Package ui provides terminal UI components for Road Runner's CLI output.
//
// The package includes spinners, progress bars, phase displays, and styled
// text output using the Lip Gloss library for consistent terminal styling
// across all commands.
//
// # Components Overview
//
//	Spinner       - Animated status indicator for long-running operations
//	PhaseDisplay  - Renders workflow phases (connect, sync, lock, execute)
//	Progress bars - Visual percentage indicators with color thresholds
//	Sparkline     - Mini line graphs for historical data visualization
//	HostPicker    - Interactive host selection using Huh forms
//	Connection    - Connection attempt display with status indicators
//
// # Color Scheme
//
// Colors are defined as ANSI codes for broad terminal compatibility:
//
//	ColorSuccess   (green)  - Successful operations
//	ColorError     (red)    - Failures and errors
//	ColorWarning   (yellow) - Warnings and skipped items
//	ColorInfo      (cyan)   - Informational messages
//	ColorMuted     (gray)   - Secondary text, timing info
//	ColorSecondary (blue)   - In-progress indicators
//
// Use DisableColors() to switch to monochrome output (for --no-color flag).
//
// # Symbols
//
// Unicode symbols provide visual status indicators:
//
//	SymbolSuccess  (checkmark)  - Task completed successfully
//	SymbolFail     (X)          - Task failed
//	SymbolPending  (circle)     - Task not yet started
//	SymbolProgress (half-fill)  - Task in progress
//	SymbolComplete (filled)     - Task done (alternative)
//	SymbolSkipped  (slashed)    - Task skipped
//
// # Spinner Usage
//
// The Spinner type provides an animated indicator for operations:
//
//	s := ui.NewSpinner("Connecting")
//	s.Start()
//	// ... do work ...
//	s.Success() // or s.Fail() or s.Skip()
//
// The spinner handles terminal output, clearing lines, and timing display.
//
// # Phase Display
//
// PhaseDisplay renders the workflow progress with consistent formatting:
//
//	pd := ui.NewPhaseDisplay(os.Stdout)
//	pd.RenderProgress("Connecting")       // Shows spinner
//	pd.RenderSuccess("Connected", 0.3*s)  // Shows checkmark with timing
//	pd.Divider()                          // Draws separator line
//
// # Progress Bars
//
// Progress bars use block characters with color thresholds:
//
//	ui.RenderProgressBar(67.5, 20)  // [████████████░░░░░░░░]  68%
//
// Colors change based on percentage: green (0-60%), yellow (60-80%), red (80-100%).
//
// # Bubble Tea Components
//
// For interactive TUI applications, use BubblesSpinner which wraps the
// Bubble Tea spinner component for use in full-screen applications like
// the monitor dashboard.
package ui
