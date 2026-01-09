package ui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// TableStyle provides consistent styling for tables across the CLI.
type TableStyle struct {
	Header   lipgloss.Style
	Cell     lipgloss.Style
	Selected lipgloss.Style
	Border   lipgloss.Style
}

// DefaultTableStyle returns the default table styling.
func DefaultTableStyle() TableStyle {
	return TableStyle{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(string(ColorPrimary))),
		Cell: lipgloss.NewStyle().
			Foreground(lipgloss.Color(string(ColorPrimary))),
		Selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color(string(ColorPrimary))).
			Background(lipgloss.Color(string(ColorMuted))),
		Border: lipgloss.NewStyle().
			Foreground(lipgloss.Color(string(ColorMuted))),
	}
}

// TableColumn defines a table column with name and width.
type TableColumn struct {
	Title string
	Width int
}

// NewTable creates a new Bubbles table with default styling.
func NewTable(columns []TableColumn, rows []table.Row) table.Model {
	cols := make([]table.Column, len(columns))
	for i, c := range columns {
		cols[i] = table.Column{
			Title: c.Title,
			Width: c.Width,
		}
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(len(rows)+1), // +1 for header
	)

	// Apply styling
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(string(ColorMuted))).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color(string(ColorPrimary)))
	s.Cell = s.Cell.
		Foreground(lipgloss.Color(string(ColorPrimary)))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(string(ColorPrimary))).
		Background(lipgloss.Color(string(ColorMuted))).
		Bold(false)

	t.SetStyles(s)
	return t
}

// RenderSimpleTable renders a non-interactive table string.
// This is for CLI output (not TUI), producing a simple formatted table.
func RenderSimpleTable(columns []TableColumn, rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	// Create the table
	tableRows := make([]table.Row, len(rows))
	for i, row := range rows {
		tableRows[i] = table.Row(row)
	}

	t := NewTable(columns, tableRows)
	return t.View()
}

// StatusTableRow represents a row in the status table.
type StatusTableRow struct {
	Status  string // Status indicator (checkmark or X)
	Host    string // Host name
	Alias   string // SSH alias
	Latency string // Connection latency or error
}

// RenderStatusTable renders the status output as a formatted table.
func RenderStatusTable(rows []StatusTableRow, defaultHost string, selected *StatusTableSelection) string {
	if len(rows) == 0 {
		return "No hosts configured"
	}

	// Build table rows with styling
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorSuccess)))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorError)))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorMuted)))
	selectedStyle := lipgloss.NewStyle().Bold(true)

	var output string

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(string(ColorPrimary))).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(string(ColorMuted)))

	output += headerStyle.Render("  STATUS   HOST             ALIAS                            LATENCY") + "\n"

	// Rows
	for _, row := range rows {
		var statusIcon string
		if row.Status == "ok" {
			statusIcon = successStyle.Render(SymbolComplete)
		} else {
			statusIcon = errorStyle.Render(SymbolFail)
		}

		// Highlight selected host
		hostStr := row.Host
		aliasStr := row.Alias
		if selected != nil && row.Host == selected.Host && row.Alias == selected.Alias {
			hostStr = selectedStyle.Render(row.Host + " *")
			aliasStr = selectedStyle.Render(row.Alias)
		}

		var latencyStr string
		if row.Status == "ok" {
			latencyStr = mutedStyle.Render(row.Latency)
		} else {
			latencyStr = errorStyle.Render(row.Latency)
		}

		// Format row with consistent column widths
		rowLine := "  " + statusIcon + "        " +
			padRight(hostStr, 17) +
			padRight(aliasStr, 33) +
			latencyStr
		output += lipgloss.NewStyle().Render(rowLine) + "\n"
	}

	return output
}

// StatusTableSelection indicates which host/alias is selected.
type StatusTableSelection struct {
	Host  string
	Alias string
}

// DoctorCheckRow represents a row in the doctor diagnostic table.
type DoctorCheckRow struct {
	Status     string // "pass", "warn", "fail"
	Category   string // Check category
	Message    string // Check result message
	Suggestion string // Suggestion for fixing (if failed)
}

// RenderDoctorTable renders doctor check results as a formatted table.
func RenderDoctorTable(rows []DoctorCheckRow) string {
	if len(rows) == 0 {
		return "No checks to display"
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorSuccess)))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorError)))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorWarning)))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(string(ColorMuted)))
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(string(ColorPrimary)))

	var output string

	// Group by category
	categories := make(map[string][]DoctorCheckRow)
	categoryOrder := []string{}
	for _, row := range rows {
		if _, exists := categories[row.Category]; !exists {
			categoryOrder = append(categoryOrder, row.Category)
		}
		categories[row.Category] = append(categories[row.Category], row)
	}

	// Render each category
	for _, cat := range categoryOrder {
		output += headerStyle.Render(cat) + "\n"

		for _, row := range categories[cat] {
			var statusIcon string
			switch row.Status {
			case "pass":
				statusIcon = successStyle.Render(SymbolComplete)
			case "warn":
				statusIcon = warnStyle.Render(SymbolComplete)
			case "fail":
				statusIcon = errorStyle.Render(SymbolFail)
			default:
				statusIcon = mutedStyle.Render(SymbolPending)
			}

			output += "  " + statusIcon + " " + row.Message + "\n"

			if row.Suggestion != "" && row.Status != "pass" {
				output += "    " + mutedStyle.Render(row.Suggestion) + "\n"
			}
		}
		output += "\n"
	}

	return output
}

// padRight pads a string to the specified width.
func padRight(s string, width int) string {
	// Account for ANSI codes when calculating visible length
	visibleLen := lipgloss.Width(s)
	if visibleLen >= width {
		return s
	}
	padding := width - visibleLen
	for i := 0; i < padding; i++ {
		s += " "
	}
	return s
}
