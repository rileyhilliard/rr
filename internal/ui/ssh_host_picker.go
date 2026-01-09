package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SSHHostInfo contains information about an SSH host from ~/.ssh/config.
type SSHHostInfo struct {
	Alias       string // SSH alias
	Hostname    string // Actual hostname
	User        string // Username
	Port        string // Port (if non-default)
	Description string // Combined description
}

// sshHostItem implements list.Item for the Bubbles list component.
type sshHostItem struct {
	host SSHHostInfo
}

func (i sshHostItem) Title() string {
	return i.host.Alias
}

func (i sshHostItem) Description() string {
	return i.host.Description
}

func (i sshHostItem) FilterValue() string {
	// Allow searching by alias, hostname, and user
	values := []string{i.host.Alias}
	if i.host.Hostname != "" {
		values = append(values, i.host.Hostname)
	}
	if i.host.User != "" {
		values = append(values, i.host.User)
	}
	return strings.Join(values, " ")
}

// SSHHostPickerModel is a Bubble Tea model for selecting an SSH host.
type SSHHostPickerModel struct {
	list        list.Model
	hosts       []SSHHostInfo
	selected    *SSHHostInfo
	manualEntry bool // User chose to enter manually
	quitting    bool
	width       int
	height      int
}

// sshHostPickerKeyMap defines key bindings.
type sshHostPickerKeyMap struct {
	Enter  key.Binding
	Manual key.Binding
	Quit   key.Binding
}

var sshHostPickerKeys = sshHostPickerKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Manual: key.NewBinding(
		key.WithKeys("m"),
		key.WithHelp("m", "manual entry"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "esc", "ctrl+c"),
		key.WithHelp("q/esc", "cancel"),
	),
}

// NewSSHHostPickerModel creates a new SSH host picker model.
func NewSSHHostPickerModel(hosts []SSHHostInfo) SSHHostPickerModel {
	items := make([]list.Item, len(hosts))
	for i, h := range hosts {
		items[i] = sshHostItem{host: h}
	}

	// Create list with custom delegate
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color(string(ColorPrimary))).
		BorderForeground(lipgloss.Color(string(ColorSecondary)))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color(string(ColorMuted)))

	l := list.New(items, delegate, 0, 0)
	l.Title = "Select an SSH host from your config"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color(string(ColorPrimary))).
		Bold(true).
		Padding(0, 0, 1, 0)
	l.Styles.HelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorMuted)))

	// Add custom status bar hint
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{sshHostPickerKeys.Manual}
	}

	return SSHHostPickerModel{
		list:   l,
		hosts:  hosts,
		width:  80,
		height: 15,
	}
}

// Init implements tea.Model.
func (m SSHHostPickerModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m SSHHostPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't handle keys when filtering
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case key.Matches(msg, sshHostPickerKeys.Enter):
			if item, ok := m.list.SelectedItem().(sshHostItem); ok {
				m.selected = &item.host
			}
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, sshHostPickerKeys.Manual):
			m.manualEntry = true
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, sshHostPickerKeys.Quit):
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height-2)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m SSHHostPickerModel) View() string {
	if m.quitting {
		return ""
	}

	// Add hint about manual entry
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(string(ColorMuted))).
		Render("\n  Press 'm' to enter SSH host manually")

	return m.list.View() + hint
}

// Selected returns the selected host, or nil if cancelled.
func (m SSHHostPickerModel) Selected() *SSHHostInfo {
	return m.selected
}

// ManualEntry returns true if the user chose to enter manually.
func (m SSHHostPickerModel) ManualEntry() bool {
	return m.manualEntry
}

// PickSSHHost displays an interactive SSH host picker.
// Returns:
// - selected host if user picks one
// - nil, false if user chooses manual entry
// - nil, true if user cancels
func PickSSHHost(hosts []SSHHostInfo) (*SSHHostInfo, bool, error) {
	return PickSSHHostWithOutput(hosts, os.Stdout, os.Stdin)
}

// PickSSHHostWithOutput displays the SSH host picker with custom I/O.
func PickSSHHostWithOutput(hosts []SSHHostInfo, output io.Writer, input io.Reader) (*SSHHostInfo, bool, error) {
	if len(hosts) == 0 {
		return nil, false, nil // No hosts, go to manual entry
	}

	model := NewSSHHostPickerModel(hosts)

	p := tea.NewProgram(
		model,
		tea.WithOutput(output),
		tea.WithInput(input),
	)

	finalModel, err := p.Run()
	if err != nil {
		return nil, false, fmt.Errorf("SSH host picker error: %w", err)
	}

	if m, ok := finalModel.(SSHHostPickerModel); ok {
		if m.ManualEntry() {
			return nil, false, nil // Manual entry requested
		}
		if m.Selected() == nil {
			return nil, true, nil // Cancelled
		}
		return m.Selected(), false, nil
	}

	return nil, true, nil // Cancelled
}
