// Package monitor implements a real-time TUI dashboard for remote host metrics.
//
// The dashboard displays CPU, RAM, GPU, and network statistics for configured
// remote hosts, with color-coded status indicators and responsive layout that
// adapts to terminal size.
//
// # Architecture
//
// The package uses the Bubble Tea framework, which follows The Elm Architecture
// (Model-Update-View pattern):
//
//   - Model: Holds application state (hosts, metrics, selection, layout mode)
//   - Update: Processes messages (keystrokes, tick events, new metrics)
//   - View: Renders the current state to a string for display
//
// # Key Components
//
//	Model       - The Bubble Tea model containing all dashboard state
//	Collector   - Gathers metrics from remote hosts via SSH in parallel
//	Pool        - Manages SSH connection pool for reuse between refresh cycles
//	History     - Ring buffer storage for historical metrics (sparkline graphs)
//
// # Message Flow
//
// The dashboard operates on a tick-based refresh cycle:
//
//  1. tickMsg fires at the configured interval (default 1s)
//  2. collectCmd() launches parallel SSH commands to gather metrics
//  3. metricsMsg arrives with results, updating Model.metrics
//  4. View() re-renders the dashboard with new data
//
// # Layout Modes
//
// The dashboard adapts to terminal width with four layout modes:
//
//	LayoutMinimal  (<80 cols)  - Metrics only, no graphs
//	LayoutCompact  (80-120)    - Inline graphs, abbreviated labels
//	LayoutStandard (120-160)   - Full cards, possibly 2 columns
//	LayoutWide     (160+)      - Two-column layout with extra detail
//
// # Connection Pool
//
// The Pool type maintains persistent SSH connections to avoid reconnection
// overhead on each refresh. It handles:
//
//   - Connection reuse and health checking
//   - Platform detection (Linux vs macOS) for parser selection
//   - Automatic reconnection on connection failure
//
// # History and Sparklines
//
// The History type stores metric values in ring buffers for sparkline
// rendering. Each host tracks:
//
//   - CPU percentage history
//   - RAM percentage history
//   - GPU percentage history (if available)
//   - Network throughput history per interface
//
// Default history size is 600 samples (10 minutes at 1s refresh).
//
// # Keyboard Shortcuts
//
// Navigation and control is handled via keybindings defined in keybindings.go:
//
//	q, Ctrl+C   - Quit
//	r           - Force refresh
//	s           - Cycle sort order (name/CPU/RAM/GPU)
//	j/k, ↑/↓    - Navigate host list
//	Enter       - Expand host detail view
//	Esc         - Collapse / go back
//	?           - Toggle help overlay
package monitor
