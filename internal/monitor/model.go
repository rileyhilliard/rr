package monitor

import (
	"context"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HostStatus represents the connection state of a host.
type HostStatus int

const (
	StatusConnectingState HostStatus = iota
	StatusIdleState                  // Online, not running any task
	StatusRunningState               // Online, actively running a task (locked)
	StatusSlowState
	StatusUnreachableState
)

// LayoutMode represents the responsive layout mode based on terminal size.
type LayoutMode int

const (
	// LayoutMinimal is for terminals < 80 columns: metrics only, no graphs, single column
	LayoutMinimal LayoutMode = iota
	// LayoutCompact is for terminals 80-120 columns: inline graphs, abbreviated labels, single column
	LayoutCompact
	// LayoutStandard is for terminals 120-160 columns: full cards, possibly 2 columns
	LayoutStandard
	// LayoutWide is for terminals 160+ columns: two-column layout with more detail
	LayoutWide
)

// Width breakpoints for layout modes
const (
	BreakpointCompact  = 80
	BreakpointStandard = 120
	BreakpointWide     = 160
)

// Height breakpoints for layout adjustments
const (
	HeightMinimal  = 24
	HeightStandard = 40
)

// String returns a human-readable status string.
func (s HostStatus) String() string {
	switch s {
	case StatusConnectingState:
		return "connecting"
	case StatusIdleState:
		return "idle"
	case StatusRunningState:
		return "running"
	case StatusSlowState:
		return "slow"
	case StatusUnreachableState:
		return "offline"
	default:
		return "unknown"
	}
}

// Model is the Bubble Tea model for the monitoring dashboard.
type Model struct {
	hosts      []string
	hostOrder  []string // Original config order for default sorting (priority order)
	metrics    map[string]*HostMetrics
	status     map[string]HostStatus
	errors     map[string]string               // Last error message per host for diagnostics
	lockInfo   map[string]*HostLockInfo        // Lock status per host
	connState  map[string]*HostConnectionState // Connection attempt tracking per host
	sshAlias   map[string]string               // SSH alias used to connect (e.g., "m4-tailscale")
	latency    map[string]time.Duration        // Latest round-trip latency per host
	selected   int
	collector  *Collector
	history    *History
	width      int
	height     int
	lastUpdate time.Time
	interval   time.Duration
	timeout    time.Duration // Per-host collection timeout
	quitting   bool
	sortOrder  SortOrder
	viewMode   ViewMode
	showHelp   bool

	// Streaming collection state
	resultsChan <-chan HostResult // Channel for receiving streaming results
	collecting  bool              // Whether a collection cycle is in progress

	// Animation state
	spinnerFrame int // Current frame for connecting spinner animation

	// Viewports for scrollable content
	detailViewport viewport.Model
	listViewport   viewport.Model
	viewportReady  bool
}

// tickMsg signals a periodic refresh.
type tickMsg time.Time

// spinnerTickMsg signals a spinner animation frame update.
type spinnerTickMsg time.Time

// metricsMsg carries new metrics from the collector (batched, all hosts).
type metricsMsg struct {
	metrics  map[string]*HostMetrics
	errors   map[string]string        // Connection errors per host
	lockInfo map[string]*HostLockInfo // Lock status per host
	time     time.Time
}

// hostResultMsg carries metrics from a single host (for streaming updates).
type hostResultMsg struct {
	alias        string
	metrics      *HostMetrics  // nil on error
	error        string        // error message if failed
	lockInfo     *HostLockInfo // lock status
	connectedVia string        // SSH alias used to connect (e.g., "m4-tailscale")
	latency      time.Duration // round-trip time for metrics collection
	time         time.Time
}

// collectStartedMsg signals that collection has started and provides the results channel.
// This allows state mutations to happen in Update instead of inside the async goroutine.
type collectStartedMsg struct {
	results <-chan HostResult
}

// HostConnectionState tracks connection attempts and errors per host.
type HostConnectionState struct {
	Attempts    int       // number of consecutive failures
	LastError   string    // most recent error message
	Connected   bool      // has successfully connected at least once
	LastAttempt time.Time // when last attempt started
	NextRetry   time.Time // when to next attempt connection (for backoff)
}

// Backoff constants for unreachable hosts
const (
	// After this many consecutive failures, start backing off
	backoffThreshold = 3
	// How long to wait before retrying an unreachable host
	unreachableBackoff = 30 * time.Second
)

// spinnerInterval is the animation frame rate for the connecting spinner
const spinnerInterval = 150 * time.Millisecond

// NewModel creates a new dashboard model with the given collector.
// hostOrder is the priority order from config (default host first, then fallbacks).
// If nil, hosts are sorted alphabetically.
// timeout is the per-host collection timeout (0 uses default of 8s).
func NewModel(collector *Collector, interval, timeout time.Duration, hostOrder []string) Model {
	hosts := collector.Hosts()

	// Store the original config order for default sorting
	// If no order provided, fall back to alphabetical
	var configOrder []string
	if len(hostOrder) > 0 {
		configOrder = make([]string, len(hostOrder))
		copy(configOrder, hostOrder)
	} else {
		configOrder = make([]string, len(hosts))
		copy(configOrder, hosts)
		sort.Strings(configOrder)
	}

	// Initialize status map with all hosts in connecting state
	status := make(map[string]HostStatus)
	for _, h := range hosts {
		status[h] = StatusConnectingState
	}

	// Initialize connection state for all hosts
	connState := make(map[string]*HostConnectionState)
	for _, h := range hosts {
		connState[h] = &HostConnectionState{
			Attempts:    0,
			LastError:   "",
			Connected:   false,
			LastAttempt: time.Time{},
		}
	}

	// Default timeout if not specified
	if timeout == 0 {
		timeout = 8 * time.Second
	}

	// Wire timeout to collector so CollectStreaming uses it
	collector.SetTimeout(timeout)

	m := Model{
		hosts:     hosts, // Will be sorted by sortHosts
		hostOrder: configOrder,
		metrics:   make(map[string]*HostMetrics),
		status:    status,
		errors:    make(map[string]string),
		lockInfo:  make(map[string]*HostLockInfo),
		connState: connState,
		sshAlias:  make(map[string]string),
		latency:   make(map[string]time.Duration),
		selected:  -1, // No selection yet; prevents sortHosts from preserving random initial order
		collector: collector,
		history:   NewHistory(DefaultHistorySize),
		interval:  interval,
		timeout:   timeout,
		sortOrder: SortByDefault, // Start with default sort (online first, config order)
	}

	// Apply initial sort
	m.sortHosts()

	// Select first host after sorting
	if len(m.hosts) > 0 {
		m.selected = 0
	}

	return m
}

// Init starts the tick timer and triggers an initial metrics collection.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.tickCmd(),
		m.collectCmd(),
		m.spinnerTickCmd(),
	)
}

// Update handles messages and updates the model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		handled, cmd := m.HandleKeyMsg(msg)
		if handled {
			return m, cmd
		}

	case tea.MouseMsg:
		// Handle mouse wheel scrolling
		if m.viewportReady {
			if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
				var cmd tea.Cmd
				if m.viewMode == ViewDetail {
					m.detailViewport, cmd = m.detailViewport.Update(msg)
				} else {
					m.listViewport, cmd = m.listViewport.Update(msg)
				}
				return m, cmd
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Initialize or resize the viewports
		// Reserve space for header and footer
		headerHeight := 3
		footerHeight := 2
		viewportHeight := m.height - headerHeight - footerHeight
		if viewportHeight < 1 {
			viewportHeight = 1
		}

		if !m.viewportReady {
			// Detail viewport
			m.detailViewport = viewport.New(m.width, viewportHeight)
			m.detailViewport.YPosition = headerHeight
			// List viewport
			m.listViewport = viewport.New(m.width, viewportHeight)
			m.listViewport.YPosition = headerHeight
			m.viewportReady = true
		} else {
			m.detailViewport.Width = m.width
			m.detailViewport.Height = viewportHeight
			m.listViewport.Width = m.width
			m.listViewport.Height = viewportHeight
		}

		// Update viewport content based on current view (dimensions changed)
		if m.viewMode == ViewDetail {
			m.updateDetailViewportContent()
		} else {
			m.updateListViewportContent()
		}

	case tickMsg:
		return m, tea.Batch(m.tickCmd(), m.collectCmd())

	case spinnerTickMsg:
		// Advance spinner animation frame (use large cycle to allow text animation to complete)
		m.spinnerFrame = (m.spinnerFrame + 1) % 10000
		return m, m.spinnerTickCmd()

	case metricsMsg:
		m.lastUpdate = msg.time
		m.updateMetrics(msg.metrics, msg.errors, msg.lockInfo)
		// Update viewport content based on current view
		if m.viewMode == ViewDetail {
			m.updateDetailViewportContent()
		} else {
			m.updateListViewportContent()
		}

	case collectStartedMsg:
		// Collection started - set up state and begin polling
		m.resultsChan = msg.results
		m.collecting = true

		// Mark collection start time for hosts that aren't yet connected
		// (Attempts are incremented in updateHostResult only on actual failures)
		for _, host := range m.hosts {
			if state, ok := m.connState[host]; ok && !state.Connected {
				state.LastAttempt = time.Now()
			}
		}

		// Start polling for results
		return m, pollResultsCmd(m.resultsChan)

	case hostResultMsg:
		// Check if this is a completion signal (empty alias means channel closed)
		if msg.alias == "" {
			m.collecting = false
			m.resultsChan = nil
			return m, nil
		}

		// Update this specific host's state immediately
		m.lastUpdate = msg.time
		m.updateHostResult(msg)
		// Update viewport content based on current view
		if m.viewMode == ViewDetail {
			m.updateDetailViewportContent()
		} else {
			m.updateListViewportContent()
		}

		// Continue polling for more results if we have an active channel
		if m.resultsChan != nil {
			return m, pollResultsCmd(m.resultsChan)
		}
	}

	return m, nil
}

// View renders the dashboard.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	return m.renderDashboard()
}

// tickCmd returns a command that sends a tick after the refresh interval.
func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// spinnerTickCmd returns a command that sends a spinner tick for animation.
func (m Model) spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// collectCmd returns a command that starts streaming collection from hosts.
// Hosts that are in backoff (unreachable with NextRetry in future) are skipped.
// Returns a collectStartedMsg so Update can set up state safely (no data races).
func (m Model) collectCmd() tea.Cmd {
	// Capture values locally to avoid reading Model fields in goroutine
	timeout := m.timeout
	collector := m.collector

	// Build list of hosts to collect from (skip those in backoff)
	now := time.Now()
	var hostsToCollect []string
	for _, host := range m.hosts {
		state := m.connState[host]
		if state == nil {
			// No state yet, collect from this host
			hostsToCollect = append(hostsToCollect, host)
			continue
		}

		// Skip hosts that are in backoff (NextRetry is in the future)
		if !state.NextRetry.IsZero() && now.Before(state.NextRetry) {
			continue
		}

		hostsToCollect = append(hostsToCollect, host)
	}

	numHosts := len(hostsToCollect)
	if numHosts == 0 {
		// All hosts are in backoff, return nil command
		return nil
	}

	return func() tea.Msg {
		// Create a background context for the collection cycle.
		// Note: We don't defer cancel() here because CollectStreamingHosts spawns
		// goroutines that need the context to remain valid. The context will be
		// garbage collected after the timeout expires. This is acceptable because:
		// 1. The timeout is bounded (timeout * numHosts+1)
		// 2. Collection typically completes well before the timeout
		// 3. The timer overhead is minimal
		ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Duration(numHosts+1))
		_ = cancel // Context cleanup happens via timeout; see comment above

		// Start streaming collection for only the non-backoff hosts
		resultsChan := collector.CollectStreamingHosts(ctx, hostsToCollect)

		// Return the channel to Update for safe state setup
		return collectStartedMsg{results: resultsChan}
	}
}

// pollResultsCmd returns a command that polls for the next streaming result.
// Takes the channel as a parameter to avoid data races (no Model field access in goroutine).
func pollResultsCmd(results <-chan HostResult) tea.Cmd {
	if results == nil {
		return nil
	}

	return func() tea.Msg {
		// Simple channel receive (not select with single case)
		result, ok := <-results
		if !ok {
			// Channel closed, collection complete - signal via nil result
			return hostResultMsg{time: time.Now()} // Empty msg signals completion
		}

		errStr := ""
		if result.Error != nil {
			errStr = result.Error.Error()
		}

		return hostResultMsg{
			alias:        result.Alias,
			metrics:      result.Metrics,
			error:        errStr,
			lockInfo:     result.LockInfo,
			connectedVia: result.ConnectedVia,
			latency:      result.Latency,
			time:         time.Now(),
		}
	}
}

// updateMetrics updates the model with new metrics and determines host status.
func (m *Model) updateMetrics(newMetrics map[string]*HostMetrics, newErrors map[string]string, newLockInfo map[string]*HostLockInfo) {
	for alias, metrics := range newMetrics {
		if metrics == nil {
			m.status[alias] = StatusUnreachableState
			// Store error message if available
			if errMsg, ok := newErrors[alias]; ok {
				m.errors[alias] = errMsg
			}
			// Clear lock info for unreachable hosts
			delete(m.lockInfo, alias)
			continue
		}

		m.metrics[alias] = metrics
		m.history.Push(alias, metrics)

		// Update lock info
		if lockInfo, ok := newLockInfo[alias]; ok && lockInfo != nil {
			m.lockInfo[alias] = lockInfo
		} else {
			delete(m.lockInfo, alias)
		}

		// Determine status based on lock state and collection latency
		if lockInfo, ok := m.lockInfo[alias]; ok && lockInfo.IsLocked {
			m.status[alias] = StatusRunningState
		} else {
			m.status[alias] = StatusIdleState
		}

		// Clear any previous error
		delete(m.errors, alias)
	}
}

// updateHostResult updates the model state for a single host result (streaming mode).
func (m *Model) updateHostResult(msg hostResultMsg) {
	alias := msg.alias

	// Update connection state based on result
	if state, ok := m.connState[alias]; ok {
		if msg.error != "" {
			// Actual failure: increment attempts and store error
			state.Attempts++
			state.LastError = msg.error
			state.LastAttempt = time.Now()

			// After threshold failures, enter backoff to avoid hammering unreachable hosts
			if state.Attempts >= backoffThreshold {
				state.NextRetry = time.Now().Add(unreachableBackoff)
			}
		} else {
			// Success: mark as connected, reset all failure state
			state.Connected = true
			state.Attempts = 0
			state.LastError = ""
			state.NextRetry = time.Time{} // Clear backoff
		}
	}

	if msg.metrics == nil {
		// Host unreachable or error
		// Keep StatusConnectingState until first successful connection (shows retry UI)
		// Only switch to StatusUnreachableState if we previously connected successfully
		if state, ok := m.connState[alias]; ok && !state.Connected {
			m.status[alias] = StatusConnectingState
		} else {
			m.status[alias] = StatusUnreachableState
		}
		if msg.error != "" {
			m.errors[alias] = msg.error
		}
		delete(m.lockInfo, alias)
		m.sortHosts()
		return
	}

	// Successfully collected metrics
	m.metrics[alias] = msg.metrics
	m.history.Push(alias, msg.metrics)

	// Store latency and push to history
	if msg.latency > 0 {
		m.latency[alias] = msg.latency
		m.history.PushLatency(alias, float64(msg.latency.Milliseconds()))
	}

	// Store the SSH alias used to connect
	if msg.connectedVia != "" {
		m.sshAlias[alias] = msg.connectedVia
	}

	// Update lock info
	if msg.lockInfo != nil {
		m.lockInfo[alias] = msg.lockInfo
	} else {
		delete(m.lockInfo, alias)
	}

	// Determine status based on lock state
	if msg.lockInfo != nil && msg.lockInfo.IsLocked {
		m.status[alias] = StatusRunningState
	} else {
		m.status[alias] = StatusIdleState
	}

	// Clear any previous error
	delete(m.errors, alias)

	// Re-sort hosts since status may have changed
	m.sortHosts()
}

// OnlineCount returns the number of hosts that are online (idle or running).
func (m Model) OnlineCount() int {
	count := 0
	for _, status := range m.status {
		if status == StatusIdleState || status == StatusRunningState {
			count++
		}
	}
	return count
}

// SelectedHost returns the alias of the currently selected host.
func (m Model) SelectedHost() string {
	if m.selected >= 0 && m.selected < len(m.hosts) {
		return m.hosts[m.selected]
	}
	return ""
}

// SecondsSinceUpdate returns how many seconds have passed since the last update.
func (m Model) SecondsSinceUpdate() int {
	if m.lastUpdate.IsZero() {
		return 0
	}
	return int(time.Since(m.lastUpdate).Seconds())
}

// ConnectingSpinner returns the current spinner character for the connecting animation.
func (m Model) ConnectingSpinner() string {
	return ConnectingSpinnerFrames[m.spinnerFrame%len(ConnectingSpinnerFrames)]
}

// ConnectingText returns the current animated "Connecting" text.
func (m Model) ConnectingText() string {
	// Use slower frame progression for calmer animation (~1s per frame)
	slowFrame := m.spinnerFrame / ConnectingTextSlowdown
	return ConnectingTextFrames[slowFrame%len(ConnectingTextFrames)]
}

// ConnectingSubtext returns the current animated subtext for connecting state.
func (m Model) ConnectingSubtext() string {
	// Static subtext - text should be stable, only spinner animates
	return "establishing connection"
}

// RunningSpinner returns the current spinner character and style for the running animation.
// Uses braille dots with gen-z color cycling for a vibrant "working" effect.
func (m Model) RunningSpinner() (string, lipgloss.Style) {
	return GetRunningSpinner(m.spinnerFrame)
}

// renderRunningStatusText returns the status text for a running host.
// Shows "- running" with the task duration if lock info is available.
func (m Model) renderRunningStatusText(host string) string {
	// Check if we have lock info with duration
	if lockInfo, ok := m.lockInfo[host]; ok && lockInfo != nil && lockInfo.IsLocked {
		duration := lockInfo.FormatDuration()
		return StatusRunningTextStyle.Render(" - running " + duration)
	}

	// Fallback: show "- running" with animated dots
	dots := m.spinnerFrame % 4
	suffix := ""
	for i := 0; i < dots; i++ {
		suffix += "."
	}
	return StatusRunningTextStyle.Render(" - running" + suffix)
}

// LayoutMode returns the current layout mode based on terminal width.
func (m Model) LayoutMode() LayoutMode {
	switch {
	case m.width >= BreakpointWide:
		return LayoutWide
	case m.width >= BreakpointStandard:
		return LayoutStandard
	case m.width >= BreakpointCompact:
		return LayoutCompact
	default:
		return LayoutMinimal
	}
}

// ShowFooter returns true if the terminal is tall enough to show the footer.
func (m Model) ShowFooter() bool {
	return m.height >= HeightMinimal
}

// CanShowExtendedInfo returns true if the terminal is tall enough for extra details.
func (m Model) CanShowExtendedInfo() bool {
	return m.height >= HeightStandard
}

// sortHosts sorts the hosts slice based on the current sort order.
// Preserves the selected host by updating the selected index after sorting.
func (m *Model) sortHosts() {
	if len(m.hosts) == 0 {
		return
	}

	// Remember the currently selected host
	selectedHost := ""
	if m.selected >= 0 && m.selected < len(m.hosts) {
		selectedHost = m.hosts[m.selected]
	}

	switch m.sortOrder {
	case SortByDefault:
		m.sortByDefault()

	case SortByName:
		sort.Strings(m.hosts)

	case SortByCPU:
		sort.Slice(m.hosts, func(i, j int) bool {
			metricsI := m.metrics[m.hosts[i]]
			metricsJ := m.metrics[m.hosts[j]]
			// Hosts without metrics go to the end
			if metricsI == nil && metricsJ == nil {
				return m.hosts[i] < m.hosts[j]
			}
			if metricsI == nil {
				return false
			}
			if metricsJ == nil {
				return true
			}
			// Sort descending by CPU usage
			return metricsI.CPU.Percent > metricsJ.CPU.Percent
		})

	case SortByRAM:
		sort.Slice(m.hosts, func(i, j int) bool {
			metricsI := m.metrics[m.hosts[i]]
			metricsJ := m.metrics[m.hosts[j]]
			if metricsI == nil && metricsJ == nil {
				return m.hosts[i] < m.hosts[j]
			}
			if metricsI == nil {
				return false
			}
			if metricsJ == nil {
				return true
			}
			// Calculate RAM percentage for comparison
			var pctI, pctJ float64
			if metricsI.RAM.TotalBytes > 0 {
				pctI = float64(metricsI.RAM.UsedBytes) / float64(metricsI.RAM.TotalBytes)
			}
			if metricsJ.RAM.TotalBytes > 0 {
				pctJ = float64(metricsJ.RAM.UsedBytes) / float64(metricsJ.RAM.TotalBytes)
			}
			// Sort descending by RAM usage
			return pctI > pctJ
		})

	case SortByGPU:
		sort.Slice(m.hosts, func(i, j int) bool {
			metricsI := m.metrics[m.hosts[i]]
			metricsJ := m.metrics[m.hosts[j]]
			// Hosts without metrics go to the end
			if metricsI == nil && metricsJ == nil {
				return m.hosts[i] < m.hosts[j]
			}
			if metricsI == nil {
				return false
			}
			if metricsJ == nil {
				return true
			}
			// Hosts without GPU go after hosts with GPU
			if metricsI.GPU == nil && metricsJ.GPU == nil {
				return m.hosts[i] < m.hosts[j]
			}
			if metricsI.GPU == nil {
				return false
			}
			if metricsJ.GPU == nil {
				return true
			}
			// Sort descending by GPU usage
			return metricsI.GPU.Percent > metricsJ.GPU.Percent
		})
	}

	// Restore selection to the same host
	if selectedHost != "" {
		for i, host := range m.hosts {
			if host == selectedHost {
				m.selected = i
				break
			}
		}
	}
}

// sortByDefault sorts hosts by online status first (online hosts first),
// then by config priority order (default host, then fallbacks in order).
func (m *Model) sortByDefault() {
	// Build a map of host -> config order index for sorting
	orderIndex := make(map[string]int)
	for i, h := range m.hostOrder {
		orderIndex[h] = i
	}

	sort.Slice(m.hosts, func(i, j int) bool {
		hostI := m.hosts[i]
		hostJ := m.hosts[j]

		// Online hosts come first (both idle and running count as online)
		onlineI := m.status[hostI] == StatusIdleState || m.status[hostI] == StatusRunningState
		onlineJ := m.status[hostJ] == StatusIdleState || m.status[hostJ] == StatusRunningState
		if onlineI != onlineJ {
			return onlineI
		}

		// Within same online/offline group, sort by config priority
		idxI, okI := orderIndex[hostI]
		idxJ, okJ := orderIndex[hostJ]

		// Hosts in config come before hosts not in config
		if okI != okJ {
			return okI
		}

		// Both in config: use config order
		if okI && okJ {
			return idxI < idxJ
		}

		// Neither in config: alphabetical
		return hostI < hostJ
	})
}
