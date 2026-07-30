package monitor

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAlertModel builds a sized two-host model with alerting configured and a
// controllable alert clock.
func newAlertModel(t *testing.T, alerts config.AlertsConfig, clock *time.Time) Model {
	t.Helper()

	hosts := map[string]config.Host{
		"server1": {SSH: []string{"user@server1"}},
		"server2": {SSH: []string{"user@server2"}},
	}
	m := NewModelWithOptions(NewCollector(hosts), time.Second, 0, []string{"server1", "server2"},
		ModelOptions{Thresholds: testThresholds(), Alerts: alerts})
	m.width = 120
	m.height = 40
	if clock != nil {
		m.alerts.now = func() time.Time { return *clock }
	}
	return m
}

// pushResult feeds a host result through Update and returns the new model plus
// the command it produced.
func pushResult(t *testing.T, m Model, alias string, metrics *HostMetrics) (Model, tea.Cmd) {
	t.Helper()

	nm, cmd := m.Update(hostResultMsg{alias: alias, metrics: metrics, time: time.Now()})
	return nm.(Model), cmd
}

func TestModel_AlertsEvaluatedOnHostResult(t *testing.T) {
	now := time.Now()
	m := newAlertModel(t, config.AlertsConfig{Enabled: true, Flash: true, Cooldown: "60s"}, &now)

	m, _ = pushResult(t, m, "server1", cpuMetrics(50))
	assert.Zero(t, m.AlertCount())
	assert.False(t, m.IsAlerting("server1"))

	m, _ = pushResult(t, m, "server1", cpuMetrics(95))
	assert.Equal(t, 1, m.AlertCount())
	assert.True(t, m.IsAlerting("server1"))
	assert.False(t, m.IsAlerting("server2"), "other hosts unaffected")

	// Recovery below warning clears it.
	m, _ = pushResult(t, m, "server1", cpuMetrics(20))
	assert.Zero(t, m.AlertCount())
	assert.False(t, m.IsAlerting("server1"))
}

func TestModel_AlertsDisabledByDefault(t *testing.T) {
	hosts := map[string]config.Host{"server1": {SSH: []string{"user@server1"}}}
	m := NewModel(NewCollector(hosts), time.Second, 0, nil)
	m.width = 120
	m.height = 40

	m, cmd := pushResult(t, m, "server1", cpuMetrics(99))
	assert.Zero(t, m.AlertCount(), "alerting is off unless enabled in config")
	assert.False(t, m.IsAlerting("server1"))
	assert.Nil(t, cmd, "no effects when alerting is disabled")
	assert.NotContains(t, m.renderHeader(), "alert")
}

func TestModel_AlertsClearedWhenHostGoesUnreachable(t *testing.T) {
	now := time.Now()
	m := newAlertModel(t, config.AlertsConfig{Enabled: true, Flash: true, Cooldown: "60s"}, &now)

	// Connect and fire.
	m, _ = pushResult(t, m, "server1", cpuMetrics(95))
	require.True(t, m.IsAlerting("server1"))

	// Host drops off: no metrics. The card must stop flashing rather than
	// latch on the last known critical reading.
	nm, _ := m.Update(hostResultMsg{alias: "server1", error: "connection refused", time: time.Now()})
	m = nm.(Model)

	assert.False(t, m.IsAlerting("server1"))
	assert.Zero(t, m.AlertCount())
	assert.False(t, m.IsFlashing("server1"))

	// Coming back critical fires again immediately: Clear wiped the cooldown
	// timestamp along with the firing state.
	m, _ = pushResult(t, m, "server1", cpuMetrics(95))
	assert.True(t, m.IsAlerting("server1"))
	assert.True(t, m.IsFlashing("server1"))
}

func TestModel_AlertEffectsCommand(t *testing.T) {
	now := time.Now()

	t.Run("new alert returns a command, repeats do not", func(t *testing.T) {
		clock := now
		m := newAlertModel(t, config.AlertsConfig{Enabled: true, Bell: true, Cooldown: "60s"}, &clock)

		// Below threshold: nothing to do.
		m, cmd := pushResult(t, m, "server1", cpuMetrics(10))
		assert.Nil(t, cmd)

		// Crossing critical fires the bell.
		m, cmd = pushResult(t, m, "server1", cpuMetrics(95))
		require.NotNil(t, cmd, "a new alert must return an effects command")

		// Still critical: no repeat.
		m, cmd = pushResult(t, m, "server1", cpuMetrics(96))
		assert.Nil(t, cmd, "a sustained alert must not re-ring the bell")

		// Re-armed but inside the cooldown: still suppressed.
		m, _ = pushResult(t, m, "server1", cpuMetrics(10))
		clock = clock.Add(10 * time.Second)
		_, cmd = pushResult(t, m, "server1", cpuMetrics(95))
		assert.Nil(t, cmd, "re-fire inside the cooldown must not ring the bell")
	})

	t.Run("bell off produces no command", func(t *testing.T) {
		clock := now
		m := newAlertModel(t, config.AlertsConfig{Enabled: true, Bell: false, Flash: true, Cooldown: "60s"}, &clock)

		m, cmd := pushResult(t, m, "server1", cpuMetrics(95))
		assert.Nil(t, cmd)
		assert.True(t, m.IsAlerting("server1"), "flash-only alerting still tracks state")
	})

	t.Run("polling continues alongside alert effects", func(t *testing.T) {
		clock := now
		m := newAlertModel(t, config.AlertsConfig{Enabled: true, Bell: true, Cooldown: "60s"}, &clock)

		results := make(chan HostResult)
		nm, cmd := m.Update(hostResultMsg{
			alias:   "server1",
			metrics: cpuMetrics(95),
			time:    time.Now(),
			results: results,
		})
		m = nm.(Model)

		require.NotNil(t, cmd, "an alert during an active round must not drop the poll command")
		assert.True(t, m.IsAlerting("server1"))
	})
}

func TestModel_AlertHeaderBadge(t *testing.T) {
	now := time.Now()
	m := newAlertModel(t, config.AlertsConfig{Enabled: true, Flash: true, Cooldown: "60s"}, &now)

	assert.NotContains(t, m.renderHeader(), "alert", "no badge when nothing is firing")

	m, _ = pushResult(t, m, "server1", cpuMetrics(95))
	header := m.renderHeader()
	assert.Contains(t, header, "1 alert")
	assert.NotContains(t, header, "1 alerts", "singular for a single alert")

	m, _ = pushResult(t, m, "server2", cpuMetrics(95))
	assert.Contains(t, m.renderHeader(), "2 alerts")

	// Badge clears with the alerts.
	m, _ = pushResult(t, m, "server1", cpuMetrics(5))
	m, _ = pushResult(t, m, "server2", cpuMetrics(5))
	assert.NotContains(t, m.renderHeader(), "alert")
}

func TestModel_AlertCardBorder(t *testing.T) {
	now := time.Now()
	m := newAlertModel(t, config.AlertsConfig{Enabled: true, Flash: true, Cooldown: "60s"}, &now)

	criticalSeq := ansiPrefix(t, lipgloss.NewStyle().Foreground(ColorCritical).Render("─"))

	t.Run("alerting card border uses the critical color", func(t *testing.T) {
		m, _ := pushResult(t, m, "server1", cpuMetrics(95))
		require.True(t, m.IsFlashing("server1"))

		assert.Contains(t, cardTopBorder(t, m.renderCard("server1", 60, false)), criticalSeq,
			"alerting border should render in the critical color")

		// A quiet host keeps the normal border.
		m, _ = pushResult(t, m, "server2", cpuMetrics(10))
		assert.NotContains(t, cardTopBorder(t, m.renderCard("server2", 60, false)), criticalSeq,
			"non-alerting card border must be unaffected")
	})

	// ColorCritical and ColorAccent are the same hex, so a selected alerting
	// card can't be told apart by border color alone. The thick border is what
	// carries selection while the card is flashing.
	t.Run("selection stays visible on an alerting card", func(t *testing.T) {
		m, _ := pushResult(t, m, "server1", cpuMetrics(95))

		unselected := cardTopBorder(t, m.renderCard("server1", 60, false))
		selected := cardTopBorder(t, m.renderCard("server1", 60, true))

		assert.Contains(t, selected, criticalSeq, "alert color applies to the selected card too")
		assert.NotEqual(t, unselected, selected,
			"a selected alerting card must still be distinguishable from an unselected one")
		assert.Contains(t, selected, "┏", "selected alerting card keeps a thick border")
		assert.NotContains(t, unselected, "┏", "unselected alerting card keeps the rounded border")
	})

	t.Run("flash off leaves the border alone", func(t *testing.T) {
		clock := now
		noFlash := newAlertModel(t, config.AlertsConfig{Enabled: true, Bell: true, Flash: false, Cooldown: "60s"}, &clock)
		noFlash, _ = pushResult(t, noFlash, "server1", cpuMetrics(95))

		require.True(t, noFlash.IsAlerting("server1"))
		assert.NotContains(t, cardTopBorder(t, noFlash.renderCard("server1", 60, false)), criticalSeq,
			"flash: false must leave the border at its normal color")
	})
}

// cardTopBorder returns the card's first line, which is the top border row.
// Asserting on it keeps border-color checks from matching the critical color
// used by the metric text inside the card body.
func cardTopBorder(t *testing.T, card string) string {
	t.Helper()

	line, _, found := strings.Cut(card, "\n")
	require.True(t, found, "expected a multi-line card render")
	return line
}

// TestModel_AlertFlashNeedsNoCacheInvalidation guards the assumption that card
// borders are composed outside the cached body: a card that starts flashing must
// change its rendered output even though its cached body is untouched.
func TestModel_AlertFlashNeedsNoCacheInvalidation(t *testing.T) {
	now := time.Now()
	m := newAlertModel(t, config.AlertsConfig{Enabled: true, Flash: true, Cooldown: "60s"}, &now)

	// Prime the cache with a calm reading.
	m, _ = pushResult(t, m, "server1", cpuMetrics(10))
	calm := m.renderCard("server1", 60, false)
	require.Contains(t, m.cardBodyCache, "server1")

	// Force the alert on without touching the cache, the way a flash-only
	// state change would.
	require.Len(t, m.alerts.Evaluate("server1", cpuMetrics(95), m.thresholds), 1)
	require.Contains(t, m.cardBodyCache, "server1", "cache is intentionally left in place")

	flashing := m.renderCard("server1", 60, false)
	assert.NotEqual(t, calm, flashing, "border chrome must re-render from outside the cached body")
}

// ansiPrefix extracts the leading ANSI color escape sequence from a rendered
// string so tests can assert on the color without matching exact glyphs.
func ansiPrefix(t *testing.T, rendered string) string {
	t.Helper()

	start := strings.Index(rendered, "\x1b[")
	require.GreaterOrEqual(t, start, 0, "expected an ANSI sequence in %q", rendered)
	end := strings.Index(rendered[start:], "m")
	require.Greater(t, end, 0, "expected a terminated ANSI sequence in %q", rendered)
	return rendered[start : start+end+1]
}
