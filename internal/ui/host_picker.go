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
	"github.com/rileyhilliard/rr/internal/errors"
)

// HostInfo contains information about a host for display in the picker.
type HostInfo struct {
	Name string   // Host name from config (e.g., "gpu-box")
	SSH  []string // SSH aliases
	Dir  string   // Remote directory
	Tags []string // Tags for filtering
}

// hostItem implements list.Item for the Bubbles list component.
type hostItem struct {
	host HostInfo
}

func (i hostItem) Title() string {
	return i.host.Name
}

func (i hostItem) Description() string {
	var parts []string

	// Show SSH aliases
	if len(i.host.SSH) > 0 {
		if len(i.host.SSH) == 1 {
			parts = append(parts, i.host.SSH[0])
		} else {
			parts = append(parts, fmt.Sprintf("%s (+%d)", i.host.SSH[0], len(i.host.SSH)-1))
		}
	}

	// Show directory
	if i.host.Dir != "" {
		parts = append(parts, i.host.Dir)
	}

	// Show tags
	if len(i.host.Tags) > 0 {
		parts = append(parts, "["+strings.Join(i.host.Tags, ", ")+"]")
	}

	return strings.Join(parts, " | ")
}

func (i hostItem) FilterValue() string {
	// Allow searching by name, SSH aliases, and tags
	values := []string{i.host.Name}
	values = append(values, i.host.SSH...)
	values = append(values, i.host.Tags...)
	return strings.Join(values, " ")
}

// HostPickerModel is a Bubble Tea model for selecting a host.
type HostPickerModel struct {
	list     list.Model
	hosts    []HostInfo
	selected *HostInfo
	quitting bool
	width    int
	height   int
}

// hostPickerKeyMap defines key bindings for the host picker.
type hostPickerKeyMap struct {
	Enter key.Binding
	Quit  key.Binding
}

var hostPickerKeys = hostPickerKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "esc", "ctrl+c"),
		key.WithHelp("q/esc", "cancel"),
	),
}

// NewHostPickerModel creates a new host picker model.
func NewHostPickerModel(hosts []HostInfo) HostPickerModel {
	items := make([]list.Item, len(hosts))
	for i, h := range hosts {
		items[i] = hostItem{host: h}
	}

	// Create list with custom delegate for styling
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color(string(ColorPrimary))).
		BorderForeground(lipgloss.Color(string(ColorSecondary)))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color(string(ColorMuted)))

	l := list.New(items, delegate, 0, 0)
	l.Title = "Select a host"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color(string(ColorPrimary))).
		Bold(true).
		Padding(0, 0, 1, 0)
	l.Styles.HelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorMuted)))

	return HostPickerModel{
		list:   l,
		hosts:  hosts,
		width:  80,
		height: 15,
	}
}

// Init implements tea.Model.
func (m HostPickerModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m HostPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, hostPickerKeys.Enter):
			if item, ok := m.list.SelectedItem().(hostItem); ok {
				m.selected = &item.host
			}
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, hostPickerKeys.Quit):
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
func (m HostPickerModel) View() string {
	if m.quitting {
		return ""
	}
	return m.list.View()
}

// Selected returns the selected host, or nil if cancelled.
func (m HostPickerModel) Selected() *HostInfo {
	return m.selected
}

// PickHost displays an interactive host picker and returns the selected host.
// Returns nil if the user cancels (ESC/q/Ctrl+C).
func PickHost(hosts []HostInfo) (*HostInfo, error) {
	return PickHostWithOutput(hosts, os.Stdout, os.Stdin)
}

// PickHostWithOutput displays the host picker using custom I/O.
func PickHostWithOutput(hosts []HostInfo, output io.Writer, input io.Reader) (*HostInfo, error) {
	if len(hosts) == 0 {
		return nil, errors.New(errors.ErrConfig, "No hosts to pick from", "Add hosts to your .rr.yaml or run 'rr init' to set one up.")
	}

	if len(hosts) == 1 {
		// Only one host, no need to pick
		return &hosts[0], nil
	}

	model := NewHostPickerModel(hosts)

	p := tea.NewProgram(
		model,
		tea.WithOutput(output),
		tea.WithInput(input),
	)

	finalModel, err := p.Run()
	if err != nil {
		return nil, errors.WrapWithCode(err, errors.ErrConfig, "Host picker failed", "Try running again or use --host to specify the host directly.")
	}

	if m, ok := finalModel.(HostPickerModel); ok {
		return m.Selected(), nil
	}

	return nil, nil
}

// IsTerminal returns true if the file descriptor is a terminal.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
