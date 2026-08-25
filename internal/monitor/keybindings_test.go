package monitor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestSortOrder_String(t *testing.T) {
	tests := []struct {
		order  SortOrder
		expect string
	}{
		{SortByDefault, "default"},
		{SortByName, "name"},
		{SortByCPU, "CPU"},
		{SortByRAM, "RAM"},
		{SortByGPU, "GPU"},
		{SortOrder(99), "default"}, // Unknown defaults to default
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			result := tt.order.String()
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestSortOrder_Next(t *testing.T) {
	tests := []struct {
		current SortOrder
		next    SortOrder
	}{
		{SortByDefault, SortByName},
		{SortByName, SortByCPU},
		{SortByCPU, SortByRAM},
		{SortByRAM, SortByGPU},
		{SortByGPU, SortByDefault}, // Wraps around
	}

	for _, tt := range tests {
		t.Run(tt.current.String(), func(t *testing.T) {
			result := tt.current.Next()
			assert.Equal(t, tt.next, result)
		})
	}
}

func TestSortOrder_Constants(t *testing.T) {
	// Verify sort order constants are defined in expected order
	assert.Equal(t, SortOrder(0), SortByDefault)
	assert.Equal(t, SortOrder(1), SortByName)
	assert.Equal(t, SortOrder(2), SortByCPU)
	assert.Equal(t, SortOrder(3), SortByRAM)
	assert.Equal(t, SortOrder(4), SortByGPU)
}

func TestViewMode_Constants(t *testing.T) {
	// Verify view mode constants
	assert.Equal(t, ViewMode(0), ViewList)
	assert.Equal(t, ViewMode(1), ViewDetail)
}

func TestKeyMap_ShortHelp(t *testing.T) {
	help := keys.ShortHelp()

	// Should return the key bindings for short help view
	assert.NotEmpty(t, help)
	assert.Len(t, help, 4) // Quit, Refresh, CycleSort, ToggleHelp
}

func TestKeyMap_FullHelp(t *testing.T) {
	help := keys.FullHelp()

	// Should return the key bindings for full help view
	assert.NotEmpty(t, help)
	assert.Len(t, help, 3) // Three rows of bindings
}

func TestKeys_QuitBinding(t *testing.T) {
	// Verify quit key is configured correctly
	assert.NotNil(t, keys.Quit)
}

func TestKeys_RefreshBinding(t *testing.T) {
	// Verify refresh key is configured correctly
	assert.NotNil(t, keys.Refresh)
}

func TestKeys_NavigationBindings(t *testing.T) {
	// Verify navigation keys are configured
	assert.NotNil(t, keys.SelectPrev)
	assert.NotNil(t, keys.SelectNext)
	assert.NotNil(t, keys.SelectFirst)
	assert.NotNil(t, keys.SelectLast)
}

func TestKeys_ViewBindings(t *testing.T) {
	// Verify view management keys are configured
	assert.NotNil(t, keys.Expand)
	assert.NotNil(t, keys.Collapse)
	assert.NotNil(t, keys.ToggleHelp)
}

func TestProcSortOrder_String(t *testing.T) {
	tests := []struct {
		order  ProcSortOrder
		expect string
	}{
		{ProcSortByCPU, "by CPU"},
		{ProcSortByMemory, "by MEM"},
		{ProcSortByPID, "by PID"},
		{ProcSortOrder(99), "by CPU"}, // Unknown defaults to CPU
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			assert.Equal(t, tt.expect, tt.order.String())
		})
	}
}

func TestProcSortOrder_Next(t *testing.T) {
	// The 'p' key cycles CPU -> MEM -> CPU
	assert.Equal(t, ProcSortByMemory, ProcSortByCPU.Next())
	assert.Equal(t, ProcSortByCPU, ProcSortByMemory.Next())
}

func TestHandleKeyMsg_CycleProcSort(t *testing.T) {
	m := &Model{viewMode: ViewDetail}

	// 'p' cycles the process sort in the detail view
	handled, _ := m.HandleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.True(t, handled)
	assert.Equal(t, ProcSortByMemory, m.procSortOrder)

	handled, _ = m.HandleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.True(t, handled)
	assert.Equal(t, ProcSortByCPU, m.procSortOrder)
}

func TestHandleKeyMsg_CycleProcSort_ListViewIgnored(t *testing.T) {
	// 'p' is detail-view only; the list view must not consume it
	m := &Model{viewMode: ViewList}

	handled, _ := m.HandleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.False(t, handled)
	assert.Equal(t, ProcSortByCPU, m.procSortOrder)
}

func TestKeys_CycleProcSortBinding(t *testing.T) {
	assert.NotNil(t, keys.CycleProcSort)
}

func TestSortOrder_CycleComplete(t *testing.T) {
	// Verify that cycling through all sort orders returns to start
	order := SortByDefault
	for i := 0; i < 5; i++ {
		order = order.Next()
	}
	assert.Equal(t, SortByDefault, order)
}
