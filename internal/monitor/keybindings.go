package monitor

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// SortOrder defines how hosts are sorted in the dashboard.
type SortOrder int

const (
	SortByDefault SortOrder = iota // Online first, then config order (default host, fallbacks)
	SortByName
	SortByCPU
	SortByRAM
	SortByGPU
)

// String returns a human-readable label for the sort order.
func (s SortOrder) String() string {
	switch s {
	case SortByDefault:
		return "default"
	case SortByName:
		return "name"
	case SortByCPU:
		return "CPU"
	case SortByRAM:
		return "RAM"
	case SortByGPU:
		return "GPU"
	default:
		return "default"
	}
}

// Next cycles to the next sort order.
func (s SortOrder) Next() SortOrder {
	return SortOrder((int(s) + 1) % 5)
}

// ViewMode defines the current display mode of the dashboard.
type ViewMode int

const (
	ViewList ViewMode = iota
	ViewDetail
)

// keyMap defines all keyboard shortcuts for the monitor dashboard.
type keyMap struct {
	Quit        key.Binding
	Refresh     key.Binding
	CycleSort   key.Binding
	SelectPrev  key.Binding
	SelectNext  key.Binding
	SelectFirst key.Binding
	SelectLast  key.Binding
	Expand      key.Binding
	Collapse    key.Binding
	ToggleHelp  key.Binding
	// Detail view scrolling
	ScrollUp   key.Binding
	ScrollDown key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
}

// ShortHelp returns the short help view.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Refresh, k.CycleSort, k.ToggleHelp}
}

// FullHelp returns the full help view.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.SelectPrev, k.SelectNext, k.SelectFirst, k.SelectLast},
		{k.Expand, k.Collapse},
		{k.Quit, k.Refresh, k.CycleSort, k.ToggleHelp},
	}
}

// keys is the default keybinding configuration.
var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	CycleSort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sort"),
	),
	SelectPrev: key.NewBinding(
		key.WithKeys("up", "k", "left", "h"),
		key.WithHelp("↑/←/k/h", "prev"),
	),
	SelectNext: key.NewBinding(
		key.WithKeys("down", "j", "right", "l"),
		key.WithHelp("↓/→/j/l", "next"),
	),
	SelectFirst: key.NewBinding(
		key.WithKeys("home"),
		key.WithHelp("home", "first"),
	),
	SelectLast: key.NewBinding(
		key.WithKeys("end"),
		key.WithHelp("end", "last"),
	),
	Expand: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "expand"),
	),
	Collapse: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "collapse"),
	),
	ToggleHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	// Detail view scrolling
	ScrollUp: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "scroll up"),
	),
	ScrollDown: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "scroll down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "ctrl+u"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "ctrl+d"),
		key.WithHelp("pgdn", "page down"),
	),
}

// HandleKeyMsg processes keyboard input and returns updated model state and command.
// Returns true if the key was handled, false otherwise.
func (m *Model) HandleKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	// Help toggle takes priority
	if key.Matches(msg, keys.ToggleHelp) {
		m.showHelp = !m.showHelp
		return true, nil
	}

	// If help is showing, Esc closes it
	if m.showHelp && key.Matches(msg, keys.Collapse) {
		m.showHelp = false
		return true, nil
	}

	// Detail view: Esc returns to list
	if m.viewMode == ViewDetail && key.Matches(msg, keys.Collapse) {
		m.viewMode = ViewList
		// Reset viewport position when leaving detail view
		m.detailViewport.GotoTop()
		return true, nil
	}

	// In detail view, scroll keys control the viewport (intercept before list navigation)
	if m.viewMode == ViewDetail && m.viewportReady {
		isScrollKey := key.Matches(msg, keys.ScrollUp) ||
			key.Matches(msg, keys.ScrollDown) ||
			key.Matches(msg, keys.PageUp) ||
			key.Matches(msg, keys.PageDown)

		if isScrollKey {
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return true, cmd
		}
	}

	switch {
	case key.Matches(msg, keys.Quit):
		m.quitting = true
		return true, tea.Quit

	case key.Matches(msg, keys.Refresh):
		return true, m.collectCmd()

	case key.Matches(msg, keys.CycleSort):
		m.sortOrder = m.sortOrder.Next()
		m.sortHosts()
		return true, nil

	case key.Matches(msg, keys.SelectPrev):
		if m.viewMode == ViewList && m.selected > 0 {
			m.selected--
		}
		return true, nil

	case key.Matches(msg, keys.SelectNext):
		if m.viewMode == ViewList && m.selected < len(m.hosts)-1 {
			m.selected++
		}
		return true, nil

	case key.Matches(msg, keys.SelectFirst):
		if m.viewMode == ViewList {
			m.selected = 0
		}
		return true, nil

	case key.Matches(msg, keys.SelectLast):
		if m.viewMode == ViewList && len(m.hosts) > 0 {
			m.selected = len(m.hosts) - 1
		}
		return true, nil

	case key.Matches(msg, keys.Expand):
		if m.viewMode == ViewList && len(m.hosts) > 0 {
			m.viewMode = ViewDetail
			// Reset viewport position when entering detail view
			m.detailViewport.GotoTop()
			// Update viewport content so scrolling works
			m.updateDetailViewportContent()
		}
		return true, nil

	case key.Matches(msg, keys.Collapse):
		m.viewMode = ViewList
		return true, nil
	}

	return false, nil
}
