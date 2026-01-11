package monitor

import (
	"testing"

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

func TestSortOrder_CycleComplete(t *testing.T) {
	// Verify that cycling through all sort orders returns to start
	order := SortByDefault
	for i := 0; i < 5; i++ {
		order = order.Next()
	}
	assert.Equal(t, SortByDefault, order)
}
