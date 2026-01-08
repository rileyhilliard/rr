package monitor

import tea "github.com/charmbracelet/bubbletea"

// SortOrder defines how hosts are sorted in the dashboard.
type SortOrder int

const (
	SortByName SortOrder = iota
	SortByCPU
	SortByRAM
	SortByGPU
)

// String returns a human-readable label for the sort order.
func (s SortOrder) String() string {
	switch s {
	case SortByName:
		return "name"
	case SortByCPU:
		return "CPU"
	case SortByRAM:
		return "RAM"
	case SortByGPU:
		return "GPU"
	default:
		return "name"
	}
}

// Next cycles to the next sort order.
func (s SortOrder) Next() SortOrder {
	return SortOrder((int(s) + 1) % 4)
}

// ViewMode defines the current display mode of the dashboard.
type ViewMode int

const (
	ViewList ViewMode = iota
	ViewDetail
)

// Key bindings as constants for consistency.
const (
	KeyQuit        = "q"
	KeyQuitAlt     = "ctrl+c"
	KeyRefresh     = "r"
	KeyCycleSort   = "s"
	KeySelectPrev  = "up"
	KeySelectPrevK = "k"
	KeySelectNext  = "down"
	KeySelectNextJ = "j"
	KeySelectFirst = "home"
	KeySelectLast  = "end"
	KeyExpand      = "enter"
	KeyCollapse    = "esc"
	KeyToggleHelp  = "?"
)

// HandleKeyMsg processes keyboard input and returns updated model state and command.
// Returns true if the key was handled, false otherwise.
func (m *Model) HandleKeyMsg(msg tea.KeyMsg) (bool, tea.Cmd) {
	key := msg.String()

	// Help toggle takes priority
	if key == KeyToggleHelp {
		m.showHelp = !m.showHelp
		return true, nil
	}

	// If help is showing, Esc closes it
	if m.showHelp && key == KeyCollapse {
		m.showHelp = false
		return true, nil
	}

	// Detail view: Esc returns to list
	if m.viewMode == ViewDetail && key == KeyCollapse {
		m.viewMode = ViewList
		return true, nil
	}

	switch key {
	case KeyQuit, KeyQuitAlt:
		m.quitting = true
		return true, tea.Quit

	case KeyRefresh:
		return true, m.collectCmd()

	case KeyCycleSort:
		m.sortOrder = m.sortOrder.Next()
		m.sortHosts()
		return true, nil

	case KeySelectPrev, KeySelectPrevK:
		if m.selected > 0 {
			m.selected--
		}
		return true, nil

	case KeySelectNext, KeySelectNextJ:
		if m.selected < len(m.hosts)-1 {
			m.selected++
		}
		return true, nil

	case KeySelectFirst:
		m.selected = 0
		return true, nil

	case KeySelectLast:
		if len(m.hosts) > 0 {
			m.selected = len(m.hosts) - 1
		}
		return true, nil

	case KeyExpand:
		if m.viewMode == ViewList && len(m.hosts) > 0 {
			m.viewMode = ViewDetail
		}
		return true, nil

	case KeyCollapse:
		m.viewMode = ViewList
		return true, nil
	}

	return false, nil
}
