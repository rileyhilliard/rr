package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/stretchr/testify/assert"
)

func TestDefaultTableStyle(t *testing.T) {
	style := DefaultTableStyle()

	// Verify the styles have been initialized (they are non-nil structs)
	// We can't easily test lipgloss.Style contents, so just verify we can render with them
	testStr := "test"
	assert.NotPanics(t, func() {
		_ = style.Header.Render(testStr)
		_ = style.Cell.Render(testStr)
		_ = style.Selected.Render(testStr)
		_ = style.Border.Render(testStr)
	})
}

func TestNewTable(t *testing.T) {
	columns := []TableColumn{
		{Title: "Name", Width: 20},
		{Title: "Status", Width: 10},
	}
	rows := []table.Row{
		{"item1", "ok"},
		{"item2", "error"},
	}

	tbl := NewTable(columns, rows)

	// Table should be created without panicking
	view := tbl.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Name")
	assert.Contains(t, view, "Status")
	assert.Contains(t, view, "item1")
	assert.Contains(t, view, "item2")
}

func TestNewTable_EmptyRows(t *testing.T) {
	columns := []TableColumn{
		{Title: "Name", Width: 20},
	}
	rows := []table.Row{}

	tbl := NewTable(columns, rows)
	view := tbl.View()

	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Name")
}

func TestRenderSimpleTable(t *testing.T) {
	columns := []TableColumn{
		{Title: "Host", Width: 15},
		{Title: "Status", Width: 10},
	}
	rows := [][]string{
		{"server1", "online"},
		{"server2", "offline"},
	}

	output := RenderSimpleTable(columns, rows)

	assert.Contains(t, output, "Host")
	assert.Contains(t, output, "Status")
	assert.Contains(t, output, "server1")
	assert.Contains(t, output, "server2")
	assert.Contains(t, output, "online")
	assert.Contains(t, output, "offline")
}

func TestRenderSimpleTable_EmptyRows(t *testing.T) {
	columns := []TableColumn{
		{Title: "Name", Width: 20},
	}
	rows := [][]string{}

	output := RenderSimpleTable(columns, rows)
	assert.Empty(t, output)
}

func TestRenderStatusTable(t *testing.T) {
	rows := []StatusTableRow{
		{Status: "ok", Host: "host1", Alias: "alias1", Latency: "10ms"},
		{Status: "error", Host: "host2", Alias: "alias2", Latency: "timeout"},
	}

	output := RenderStatusTable(rows, nil)

	assert.Contains(t, output, "STATUS")
	assert.Contains(t, output, "HOST")
	assert.Contains(t, output, "ALIAS")
	assert.Contains(t, output, "LATENCY")
	assert.Contains(t, output, "host1")
	assert.Contains(t, output, "host2")
	assert.Contains(t, output, "alias1")
	assert.Contains(t, output, "alias2")
	assert.Contains(t, output, "10ms")
	assert.Contains(t, output, "timeout")
}

func TestRenderStatusTable_EmptyRows(t *testing.T) {
	rows := []StatusTableRow{}
	output := RenderStatusTable(rows, nil)
	assert.Equal(t, "No hosts configured", output)
}

func TestRenderStatusTable_WithSelection(t *testing.T) {
	rows := []StatusTableRow{
		{Status: "ok", Host: "host1", Alias: "alias1", Latency: "10ms"},
		{Status: "ok", Host: "host2", Alias: "alias2", Latency: "20ms"},
	}
	selected := &StatusTableSelection{Host: "host1", Alias: "alias1"}

	output := RenderStatusTable(rows, selected)

	// Selected host should have asterisk
	assert.Contains(t, output, "host1 *")
}

func TestRenderDoctorTable(t *testing.T) {
	rows := []DoctorCheckRow{
		{Status: "pass", Category: "SSH", Message: "SSH key found"},
		{Status: "warn", Category: "SSH", Message: "Multiple keys", Suggestion: "Consider using ssh-agent"},
		{Status: "fail", Category: "Config", Message: "Config missing", Suggestion: "Run rr init"},
	}

	output := RenderDoctorTable(rows)

	assert.Contains(t, output, "SSH")
	assert.Contains(t, output, "Config")
	assert.Contains(t, output, "SSH key found")
	assert.Contains(t, output, "Multiple keys")
	assert.Contains(t, output, "Consider using ssh-agent")
	assert.Contains(t, output, "Config missing")
	assert.Contains(t, output, "Run rr init")
}

func TestRenderDoctorTable_EmptyRows(t *testing.T) {
	rows := []DoctorCheckRow{}
	output := RenderDoctorTable(rows)
	assert.Equal(t, "No checks to display", output)
}

func TestRenderDoctorTable_GroupsByCategory(t *testing.T) {
	rows := []DoctorCheckRow{
		{Status: "pass", Category: "Cat1", Message: "Check 1"},
		{Status: "pass", Category: "Cat2", Message: "Check 2"},
		{Status: "pass", Category: "Cat1", Message: "Check 3"},
	}

	output := RenderDoctorTable(rows)

	// Categories should appear in order they were first seen
	cat1First := output[:len(output)/2]
	cat2Second := output[len(output)/2:]

	// Cat1 should appear before Cat2
	assert.Contains(t, cat1First, "Cat1")
	// Both Cat1 checks should be grouped
	assert.Contains(t, output, "Check 1")
	assert.Contains(t, output, "Check 3")
	assert.Contains(t, cat2Second, "Cat2")
}

func TestRenderDoctorTable_NoSuggestionForPass(t *testing.T) {
	rows := []DoctorCheckRow{
		{Status: "pass", Category: "Test", Message: "All good", Suggestion: "This should not appear"},
	}

	output := RenderDoctorTable(rows)

	assert.Contains(t, output, "All good")
	assert.NotContains(t, output, "This should not appear")
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		expected string
	}{
		{
			name:     "shorter than width",
			input:    "foo",
			width:    5,
			expected: "foo  ",
		},
		{
			name:     "equal to width",
			input:    "foobar",
			width:    6,
			expected: "foobar",
		},
		{
			name:     "longer than width",
			input:    "foobar",
			width:    3,
			expected: "foobar",
		},
		{
			name:     "empty string",
			input:    "",
			width:    3,
			expected: "   ",
		},
		{
			name:     "zero width",
			input:    "foo",
			width:    0,
			expected: "foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := padRight(tt.input, tt.width)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTableColumn(t *testing.T) {
	col := TableColumn{Title: "Test", Width: 25}
	assert.Equal(t, "Test", col.Title)
	assert.Equal(t, 25, col.Width)
}
