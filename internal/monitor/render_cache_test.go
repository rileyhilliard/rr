package monitor

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// styledChar pairs a visible character with the SGR parameters active when it
// was written. Used to compare styled output semantically: run-length merged
// output has fewer escape pairs than per-char output but must paint every
// character in the same color.
type styledChar struct {
	char rune
	sgr  string
}

// expandStyledChars parses ANSI SGR sequences and returns each visible
// character with the SGR parameters in effect at that position.
func expandStyledChars(s string) []styledChar {
	var result []styledChar
	current := ""
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && runes[j] != 'm' {
				j++
			}
			params := string(runes[i+2 : j])
			if params == "0" || params == "" {
				current = ""
			} else {
				current = params
			}
			i = j
			continue
		}
		result = append(result, styledChar{char: runes[i], sgr: current})
	}
	return result
}

// stripANSIStyles removes SGR sequences, leaving only visible characters.
func stripANSIStyles(s string) string {
	var b strings.Builder
	for _, sc := range expandStyledChars(s) {
		b.WriteRune(sc.char)
	}
	return b.String()
}

func TestSurfaceStyle_MatchesFreshStyleRender(t *testing.T) {
	// The cached style (including its pre-extracted ANSI sequences) must
	// produce byte-identical output to an uncached lipgloss render for a
	// single-color segment.
	colors := []lipgloss.Color{
		ColorHealthy,
		ColorWarning,
		ColorCritical,
		ColorGraph,
		ColorTextMuted,
		LatencyColor(300),
	}

	for _, color := range colors {
		var got strings.Builder
		surfaceStyle(color).render(&got, "⣿⣷⣶x")

		want := lipgloss.NewStyle().Foreground(color).Background(ColorSurfaceBg).Render("⣿⣷⣶x")
		assert.Equal(t, want, got.String(), "cached style output should match fresh render for color %s", color)
	}
}

func TestRenderColorRuns_MatchesPerCharRendering(t *testing.T) {
	// Run-length merged output must paint the same characters in the same
	// colors as the pre-optimization per-character rendering.
	chars := []rune("⣿⣷⣶⣤⣀⠀⠀⣀⣤⣶")
	colors := []lipgloss.Color{
		ColorHealthy, ColorHealthy, ColorHealthy,
		ColorWarning, ColorWarning,
		ColorCritical,
		ColorHealthy,
		ColorWarning, ColorWarning, ColorWarning,
	}

	var merged strings.Builder
	renderColorRuns(&merged, chars, colors)

	// Reference: one freshly constructed style.Render per character (the
	// uncached per-char behavior this optimization replaced)
	var reference strings.Builder
	for i, char := range chars {
		style := lipgloss.NewStyle().Foreground(colors[i]).Background(ColorSurfaceBg)
		reference.WriteString(style.Render(string(char)))
	}

	assert.Equal(t, stripANSIStyles(reference.String()), stripANSIStyles(merged.String()),
		"visible characters must be unchanged")
	assert.Equal(t, expandStyledChars(reference.String()), expandStyledChars(merged.String()),
		"every character must be painted with the same SGR attributes")
}

func TestRenderBrailleSparkline_MergedOutputMatchesPerCharColors(t *testing.T) {
	// Full-pipeline check on a fixed dataset spanning all three threshold
	// colors: the optimized renderer must color each cell exactly as the
	// per-column value dictates.
	data := []float64{10, 20, 95, 96, 50, 75, 80, 30, 92, 40, 60, 85, 15, 99, 55, 65, 25, 78, 88, 45}

	got := RenderBrailleSparklineWithOptions(data, 10, 2, ColorGraph, nil, false)
	require.NotEmpty(t, got)

	rows := strings.Split(got, "\n")
	require.Len(t, rows, 2)

	// Expected per-column colors: MetricColor of the max of each 2-point column
	wantColors := make([]lipgloss.Color, 10)
	for col := 0; col < 10; col++ {
		maxVal := data[col*2]
		if data[col*2+1] > maxVal {
			maxVal = data[col*2+1]
		}
		wantColors[col] = MetricColor(maxVal)
	}

	for rowIdx, row := range rows {
		cells := expandStyledChars(row)
		require.Len(t, cells, 10, "row %d should have 10 visible cells", rowIdx)
		for col, cell := range cells {
			var wantSGR strings.Builder
			surfaceStyle(wantColors[col]).render(&wantSGR, "x")
			want := expandStyledChars(wantSGR.String())
			require.Len(t, want, 1)
			assert.Equal(t, want[0].sgr, cell.sgr,
				"row %d col %d should be colored %s", rowIdx, col, wantColors[col])
		}
	}
}

func TestRenderGradientBar_MergedOutputMatchesPerCharColors(t *testing.T) {
	width := 20
	percent := 85.0

	got := RenderGradientBarWithColorFunc(width, percent, nil)

	// Reference: the pre-optimization per-cell rendering
	filled := int(percent / 100.0 * float64(width))
	var reference strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			posPercent := float64(i+1) / float64(width) * 100
			style := lipgloss.NewStyle().Foreground(MetricColor(posPercent)).Background(ColorSurfaceBg)
			reference.WriteString(style.Render("▰"))
		} else {
			style := lipgloss.NewStyle().Foreground(ColorTextMuted).Background(ColorSurfaceBg)
			reference.WriteString(style.Render("▱"))
		}
	}

	assert.Equal(t, expandStyledChars(reference.String()), expandStyledChars(got),
		"gradient bar must paint identical characters and colors")
}

func TestCardBodyCache_InvalidatedOnHostResult(t *testing.T) {
	m := newBenchModel(1, 30)
	host := "bench-host-0"

	// First render populates the cache
	view1 := m.View()
	require.NotEmpty(t, view1)
	require.Contains(t, m.cardBodyCache, host, "rendering should populate the card body cache")

	// New result for the host must invalidate its cached body so the card
	// reflects the fresh metrics
	newMetrics := benchMetrics(87.6, 31)
	nm, _ := m.Update(hostResultMsg{
		alias:   host,
		metrics: newMetrics,
		latency: 45 * time.Millisecond,
		time:    time.Now(),
	})
	m = nm.(Model)

	view2 := m.View()
	assert.Contains(t, view2, "87.6%", "card should show the new CPU percentage after a host result")
	assert.NotEqual(t, view1, view2, "view must change when a host gets new metrics")
}

func TestCardBodyCache_InvalidatedOnResize(t *testing.T) {
	m := newBenchModel(2, 30)
	host := "bench-host-0"

	_ = m.View()
	require.Contains(t, m.cardBodyCache, host)
	bodyBefore := m.cardBodyCache[host]

	// Resize re-renders bodies at the new width
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = nm.(Model)

	_ = m.View()
	require.Contains(t, m.cardBodyCache, host)
	assert.NotEqual(t, bodyBefore, m.cardBodyCache[host],
		"cached card body must be re-rendered after a resize")
}

// TestCardBodyCache_InvalidatedOnHeightChange guards the height dependency
// introduced by CanShowExtendedInfo: card bodies size their graphs and process
// list from the terminal height, so a height-only resize must drop the cache.
func TestCardBodyCache_InvalidatedOnHeightChange(t *testing.T) {
	m := newBenchModel(2, 30)
	host := "bench-host-0"

	// Start short: 2-row graphs, single top process
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: HeightStandard - 1})
	m = nm.(Model)
	_ = m.View()
	require.Contains(t, m.cardBodyCache, host)
	bodyShort := m.cardBodyCache[host]

	// Grow past the extended-info breakpoint at the same width. The resize
	// handler clears the cache and immediately re-renders at the new height.
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: HeightStandard + 10})
	m = nm.(Model)

	_ = m.View()
	require.Contains(t, m.cardBodyCache, host)
	assert.NotEqual(t, bodyShort, m.cardBodyCache[host],
		"cached card body must re-render with taller graphs when the terminal grows")
}

func TestSelectionHighlightUpdatesImmediately(t *testing.T) {
	m := newBenchModel(2, 30)

	view1 := m.View()
	require.NotEmpty(t, view1)

	// Move selection down: the highlight must move without waiting for the
	// next data message
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm.(Model)
	view2 := m.View()
	assert.NotEqual(t, view1, view2, "selection highlight must move immediately on SelectNext")

	// Moving back restores the original highlight (no other state changed)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = nm.(Model)
	view3 := m.View()
	assert.Equal(t, view1, view3, "selection highlight must move back immediately on SelectPrev")
}

func TestSortDeferredToEndOfCollectionRound(t *testing.T) {
	m := newBenchModel(2, 10)
	m.sortOrder = SortByCPU

	// host0 low CPU, host1 high CPU -> sorted [host1, host0]
	m.metrics["bench-host-0"] = benchMetrics(10, 0)
	m.metrics["bench-host-1"] = benchMetrics(90, 0)
	m.sortHosts()
	require.Equal(t, []string{"bench-host-1", "bench-host-0"}, m.hosts)

	// host0 jumps to 95% mid-round: order must NOT change yet
	nm, _ := m.Update(hostResultMsg{
		alias:   "bench-host-0",
		metrics: benchMetrics(95, 1),
		time:    time.Now(),
	})
	m = nm.(Model)
	assert.Equal(t, []string{"bench-host-1", "bench-host-0"}, m.hosts,
		"per-host results must not trigger a re-sort mid-round")

	// Round completion sentinel (empty alias) applies the sort
	nm, _ = m.Update(hostResultMsg{time: time.Now()})
	m = nm.(Model)
	assert.Equal(t, []string{"bench-host-0", "bench-host-1"}, m.hosts,
		"the round-complete sentinel must re-sort hosts")
}
