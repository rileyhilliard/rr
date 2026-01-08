package monitor

import (
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// HostStatus represents the connection state of a host.
type HostStatus int

const (
	StatusConnectedState HostStatus = iota
	StatusSlowState
	StatusUnreachableState
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
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, m.collectCmd()
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.hosts)-1 {
				m.selected++
			}
		case "home":
			m.selected = 0
		case "end":
			if len(m.hosts) > 0 {
				m.selected = len(m.hosts) - 1
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

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
