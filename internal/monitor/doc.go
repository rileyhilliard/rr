// Package monitor implements a real-time TUI dashboard for remote host metrics.
//
// The dashboard displays CPU, RAM, GPU, disk, and network statistics for
// configured remote hosts, with color-coded status indicators and a responsive
// layout that adapts to terminal size.
//
// # Architecture
//
// The package uses the Bubble Tea framework, which follows The Elm Architecture
// (Model-Update-View pattern):
//
//   - Model: Holds application state (hosts, metrics, selection, layout mode)
//   - Update: Processes messages (keystrokes, tick events, streamed results)
//   - View: Renders the current state to a string for display
//
// # Key Components
//
//	Model        - The Bubble Tea model containing all dashboard state
//	Collector    - Gathers metrics from remote hosts via SSH in parallel
//	Pool         - Manages SSH connection reuse between refresh cycles
//	History      - Ring buffer storage for historical metrics (sparkline graphs)
//	alertTracker - Threshold alert state machine (hysteresis + cooldown)
//
// # Message Flow
//
// The dashboard operates on a tick-based refresh cycle:
//
//  1. tickMsg fires at the configured interval (default 1s)
//  2. collectCmd() starts streaming collection, skipping hosts in backoff
//  3. hostResultMsg arrives per host as results stream in, so a fast host
//     renders immediately instead of waiting on the slowest one
//  4. View() re-renders the dashboard with new data
//
// Each host costs two SSH sessions per tick on one pooled connection: a
// lightweight latency probe, then the batched metrics command (which carries
// the rr lock check as its final output section).
//
// # Layout Modes
//
// The dashboard adapts to terminal width with four layout modes:
//
//	LayoutMinimal  (<80 cols)  - Single column, compact metric lines
//	LayoutCompact  (80-120)    - Single column, single-row inline graphs
//	LayoutStandard (120-160)   - Full cards, multi-column grid
//	LayoutWide     (160+)      - Full cards, multi-column grid
//
// In the multi-column modes the column count is derived from the terminal
// width: columns are added only while every card keeps at least minCardWidth
// of content, capped at maxCardColumns (4).
//
// Height matters too. At HeightStandard (40 rows) and above, cards get taller
// braille graphs and list more top processes.
//
// # Connection Pool
//
// The Pool type maintains persistent SSH connections to avoid reconnection
// overhead on each refresh. It handles:
//
//   - Connection reuse and health checking
//   - Parallel dialing of a host's SSH aliases via host.DialAliases, the same
//     path rr run and rr exec use for failover
//   - Platform detection (Linux vs macOS) to pick the right batched command
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
//   - Latency history
//   - Network throughput history per interface
//
// Default history size is 600 samples (10 minutes at the 1s default interval).
//
// # Snapshot Mode
//
// Snapshot supports `rr monitor --once`: rate-based metrics (CPU%, per-core,
// disk I/O, network) need two counter readings, which the live dashboard gets
// across ticks. BuildSnapshotCommand primes those sources, sleeps on the
// remote, then emits the normal metrics sections, so one SSH session yields
// real rates.
//
// # Keyboard Shortcuts
//
// Navigation and control is handled via keybindings defined in keybindings.go:
//
//	q, Ctrl+C     - Quit
//	r             - Force refresh
//	s             - Cycle sort order (default/name/CPU/RAM/GPU)
//	j/k, ↑/↓      - Navigate host list
//	Home/End      - Select first/last host
//	Enter         - Open host detail view
//	Esc           - Collapse / go back
//	p             - Cycle process sort in detail view (CPU/MEM)
//	PgUp/PgDn     - Scroll
//	?             - Toggle help overlay
package monitor
