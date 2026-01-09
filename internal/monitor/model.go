package monitor

import (
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// HostStatus represents the connection state of a host.
type HostStatus int

const (
	StatusConnectedState HostStatus = iota
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
	case StatusConnectedState:
		return "connected"
	case StatusSlowState:
		return "slow"
	case StatusUnreachableState:
		return "unreachable"
	default:
		return "unknown"
	}
}

// Model is the Bubble Tea model for the monitoring dashboard.
type Model struct {
	hosts      []string
	metrics    map[string]*HostMetrics
	status     map[string]HostStatus
	selected   int
	collector  *Collector
	history    *History
	width      int
	height     int
	lastUpdate time.Time
	interval   time.Duration
	quitting   bool
	sortOrder  SortOrder
	viewMode   ViewMode
	showHelp   bool

	// Detail view viewport for scrollable content
	detailViewport viewport.Model
	viewportReady  bool
}

// tickMsg signals a periodic refresh.
type tickMsg time.Time

// metricsMsg carries new metrics from the collector.
type metricsMsg struct {
	metrics map[string]*HostMetrics
	time    time.Time
}

// NewModel creates a new dashboard model with the given collector.
func NewModel(collector *Collector, interval time.Duration) Model {
	hosts := collector.Hosts()
	sort.Strings(hosts)

	// Initialize status map with all hosts unreachable
	status := make(map[string]HostStatus)
	for _, h := range hosts {
		status[h] = StatusUnreachableState
	}

	return Model{
		hosts:     hosts,
		metrics:   make(map[string]*HostMetrics),
		status:    status,
		collector: collector,
		history:   NewHistory(DefaultHistorySize),
		interval:  interval,
	}
}

// Init starts the tick timer and triggers an initial metrics collection.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.tickCmd(),
		m.collectCmd(),
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

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Initialize or resize the detail viewport
		// Reserve space for header and footer
		headerHeight := 3
		footerHeight := 2
		viewportHeight := m.height - headerHeight - footerHeight
		if viewportHeight < 1 {
			viewportHeight = 1
		}

		if !m.viewportReady {
			m.detailViewport = viewport.New(m.width, viewportHeight)
			m.detailViewport.YPosition = headerHeight
			m.viewportReady = true
		} else {
			m.detailViewport.Width = m.width
			m.detailViewport.Height = viewportHeight
		}

	case tickMsg:
		return m, tea.Batch(m.tickCmd(), m.collectCmd())

	case metricsMsg:
		m.lastUpdate = msg.time
		m.updateMetrics(msg.metrics)
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

// collectCmd returns a command that collects metrics from all hosts.
func (m Model) collectCmd() tea.Cmd {
	return func() tea.Msg {
		metrics := m.collector.Collect()
		return metricsMsg{
			metrics: metrics,
			time:    time.Now(),
		}
	}
}

// updateMetrics updates the model with new metrics and determines host status.
func (m *Model) updateMetrics(newMetrics map[string]*HostMetrics) {
	for alias, metrics := range newMetrics {
		if metrics == nil {
			m.status[alias] = StatusUnreachableState
			continue
		}

		m.metrics[alias] = metrics
		m.history.Push(alias, metrics)

		// Determine status based on collection latency
		// If metrics are fresh, host is connected
		m.status[alias] = StatusConnectedState
	}
}

// OnlineCount returns the number of hosts with connected status.
func (m Model) OnlineCount() int {
	count := 0
	for _, status := range m.status {
		if status == StatusConnectedState {
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
