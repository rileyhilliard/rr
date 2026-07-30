package monitor

import (
	"fmt"
	"testing"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name   string
		bytes  int64
		expect string
	}{
		{
			name:   "bytes",
			bytes:  512,
			expect: "512 B",
		},
		{
			name:   "kilobytes",
			bytes:  1024 * 10,
			expect: "10.0 KB",
		},
		{
			name:   "megabytes",
			bytes:  1024 * 1024 * 50,
			expect: "50.0 MB",
		},
		{
			name:   "gigabytes",
			bytes:  1024 * 1024 * 1024 * 8,
			expect: "8.0 GB",
		},
		{
			name:   "terabytes",
			bytes:  1024 * 1024 * 1024 * 1024 * 2,
			expect: "2.0 TB",
		},
		{
			name:   "zero",
			bytes:  0,
			expect: "0 B",
		},
		{
			name:   "just under KB",
			bytes:  1023,
			expect: "1023 B",
		},
		{
			name:   "exactly 1 KB",
			bytes:  1024,
			expect: "1.0 KB",
		},
		{
			name:   "fractional GB",
			bytes:  1024*1024*1024 + 1024*1024*512,
			expect: "1.5 GB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestFormatRate(t *testing.T) {
	tests := []struct {
		name   string
		rate   float64
		expect string
	}{
		{
			name:   "bytes per second",
			rate:   512,
			expect: "512 B/s",
		},
		{
			name:   "kilobytes per second",
			rate:   1024 * 50,
			expect: "50.0 KB/s",
		},
		{
			name:   "megabytes per second",
			rate:   1024 * 1024 * 100,
			expect: "100.0 MB/s",
		},
		{
			name:   "gigabytes per second",
			rate:   1024 * 1024 * 1024 * 2,
			expect: "2.0 GB/s",
		},
		{
			name:   "zero",
			rate:   0,
			expect: "0 B/s",
		},
		{
			name:   "fractional KB",
			rate:   1536, // 1.5 KB
			expect: "1.5 KB/s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatRate(tt.rate)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestModel_LayoutMode(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		expect LayoutMode
	}{
		{
			name:   "minimal for very narrow",
			width:  60,
			expect: LayoutMinimal,
		},
		{
			name:   "minimal at 79",
			width:  79,
			expect: LayoutMinimal,
		},
		{
			name:   "compact at 80",
			width:  80,
			expect: LayoutCompact,
		},
		{
			name:   "compact at 119",
			width:  119,
			expect: LayoutCompact,
		},
		{
			name:   "standard at 120",
			width:  120,
			expect: LayoutStandard,
		},
		{
			name:   "standard at 159",
			width:  159,
			expect: LayoutStandard,
		},
		{
			name:   "wide at 160",
			width:  160,
			expect: LayoutWide,
		},
		{
			name:   "wide at 200",
			width:  200,
			expect: LayoutWide,
		},
		{
			name:   "zero width defaults to minimal",
			width:  0,
			expect: LayoutMinimal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{width: tt.width}
			result := m.LayoutMode()
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestModel_ShowFooter(t *testing.T) {
	tests := []struct {
		name   string
		height int
		expect bool
	}{
		{
			name:   "too short",
			height: 20,
			expect: false,
		},
		{
			name:   "just under threshold",
			height: 23,
			expect: false,
		},
		{
			name:   "at threshold",
			height: 24,
			expect: true,
		},
		{
			name:   "above threshold",
			height: 50,
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{height: tt.height}
			result := m.ShowFooter()
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestModel_CanShowExtendedInfo(t *testing.T) {
	tests := []struct {
		name   string
		height int
		expect bool
	}{
		{
			name:   "short terminal",
			height: 30,
			expect: false,
		},
		{
			name:   "just under threshold",
			height: 39,
			expect: false,
		},
		{
			name:   "at threshold",
			height: 40,
			expect: true,
		},
		{
			name:   "tall terminal",
			height: 60,
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{height: tt.height}
			result := m.CanShowExtendedInfo()
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestModel_calculateCardWidth(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		expect int // approximate, as it depends on layout mode
	}{
		{
			name:   "zero width returns default",
			width:  0,
			expect: 40,
		},
		{
			name:   "narrow terminal",
			width:  60,
			expect: 57, // width - 3 for minimal layout (borders + margin)
		},
		{
			name:   "compact terminal",
			width:  100,
			expect: 96, // width - 4 for compact layout (borders + margin + slight margin)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{width: tt.width}
			result := m.calculateCardWidth()
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestModel_cardsPerRow(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		expect int
	}{
		{
			name:   "minimal layout",
			width:  60,
			expect: 1,
		},
		{
			name:   "compact layout",
			width:  100,
			expect: 1,
		},
		{
			name:   "standard layout",
			width:  140,
			expect: 2,
		},
		{
			name:   "wide layout fits three",
			width:  200,
			expect: 3,
		},
		{
			name:   "very wide layout fits four",
			width:  240,
			expect: 4,
		},
		{
			name:   "ultra wide capped at four",
			width:  320,
			expect: 4,
		},
		{
			name:   "zero width",
			width:  0,
			expect: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{width: tt.width}
			result := m.cardsPerRow()
			assert.Equal(t, tt.expect, result)
		})
	}
}

// TestModel_cardGrid_MinimumWidth verifies the grid never shrinks cards below
// minCardWidth as columns are added, and that the columns fill the terminal.
func TestModel_cardGrid_MinimumWidth(t *testing.T) {
	for _, width := range []int{100, 160, 240, 320} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			m := Model{width: width}
			cols := m.cardsPerRow()
			cardWidth := m.calculateCardWidth()

			assert.GreaterOrEqual(t, cols, 1)
			assert.LessOrEqual(t, cols, maxCardColumns)

			// Multi-column layouts must keep cards readable
			if cols > 1 {
				assert.GreaterOrEqual(t, cardWidth, minCardWidth,
					"cards must stay at least minCardWidth wide when columns are active")
			}

			// The rendered row must fit within the terminal
			assert.LessOrEqual(t, cols*(cardWidth+perCardOverhead), width,
				"row of cards must not overflow the terminal width")
		})
	}
}

func TestLayoutMode_Constants(t *testing.T) {
	// Verify layout mode constants are defined in the expected order
	assert.Equal(t, LayoutMode(0), LayoutMinimal)
	assert.Equal(t, LayoutMode(1), LayoutCompact)
	assert.Equal(t, LayoutMode(2), LayoutStandard)
	assert.Equal(t, LayoutMode(3), LayoutWide)
}

func TestBreakpoint_Constants(t *testing.T) {
	// Verify breakpoint constants
	assert.Equal(t, 80, BreakpointCompact)
	assert.Equal(t, 120, BreakpointStandard)
	assert.Equal(t, 160, BreakpointWide)
}

func TestHeight_Constants(t *testing.T) {
	// Verify height constants
	assert.Equal(t, 24, HeightMinimal)
	assert.Equal(t, 40, HeightStandard)
}

func TestModel_renderHeader(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
		"server2": {SSH: []string{"server2"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.height = 40

	// Mark one host as connected
	m.status["server1"] = StatusIdleState

	result := m.renderHeader()
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "rr monitor")
	assert.Contains(t, result, "2 hosts")
}

func TestModel_renderHeader_SingularHost(t *testing.T) {
	collector := NewCollector(map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	})
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.height = 40

	result := m.renderHeader()
	assert.Contains(t, result, "1 host ")
	assert.NotContains(t, result, "1 hosts")
}

func TestModel_processCPUColor_ScalesByCores(t *testing.T) {
	collector := NewCollector(map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	})
	m := NewModel(collector, time.Second, 0, nil)

	// 155% of one core on a 10-core host is ~15.5% of the machine: healthy.
	assert.Equal(t, ColorHealthy, m.processCPUColor(155, 10))
	// The same figure with unknown core count is classified as-is: critical.
	assert.Equal(t, ColorCritical, m.processCPUColor(155, 0))
	// A process eating most of the machine is critical regardless of cores.
	assert.Equal(t, ColorCritical, m.processCPUColor(950, 10))
}

func TestModel_renderHeader_LayoutModes(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)

	tests := []struct {
		name  string
		width int
	}{
		{"minimal", 60},
		{"compact", 100},
		{"standard", 140},
		{"wide", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(collector, time.Second, 0, nil)
			m.width = tt.width
			m.height = 40

			result := m.renderHeader()
			assert.NotEmpty(t, result)
		})
	}
}

func TestModel_renderHostCards_Empty(t *testing.T) {
	m := Model{
		hosts: []string{},
	}

	result := m.renderHostCards()
	assert.Contains(t, result, "No hosts configured")
}

func TestModel_layoutCards(t *testing.T) {
	m := Model{
		width: 160,
	}

	tests := []struct {
		name  string
		cards []string
	}{
		{"empty cards", []string{}},
		{"single card", []string{"card1"}},
		{"two cards", []string{"card1", "card2"}},
		{"three cards", []string{"card1", "card2", "card3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.layoutCards(tt.cards)
			if len(tt.cards) == 0 {
				assert.Empty(t, result)
			} else {
				for _, card := range tt.cards {
					assert.Contains(t, result, card)
				}
			}
		})
	}
}

func TestModel_renderDashboard_Standard(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.height = 40

	result := m.renderDashboard()
	assert.NotEmpty(t, result)
}

func TestModel_renderDashboard_DetailView(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.height = 40
	m.viewMode = ViewDetail

	result := m.renderDashboard()
	assert.NotEmpty(t, result)
}

func TestModel_renderDashboard_WithHelp(t *testing.T) {
	hosts := map[string]config.Host{
		"server1": {SSH: []string{"server1"}},
	}
	collector := NewCollector(hosts)
	m := NewModel(collector, time.Second, 0, nil)
	m.width = 120
	m.height = 40
	m.showHelp = true

	result := m.renderDashboard()
	assert.NotEmpty(t, result)
}
