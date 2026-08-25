package host

import (
	stderrors "errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// fakeDialConn is a lightweight closable client for exercising raceAliases
// without real SSH connections.
type fakeDialConn struct {
	alias string

	mu     sync.Mutex
	closed bool
}

func (f *fakeDialConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeDialConn) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// fakeDialer builds a per-alias dial function from scripted behaviors.
type fakeDialBehavior struct {
	delay   time.Duration
	err     error
	latency time.Duration
}

// fakeConnTracker records connections handed out by the fake dialer so tests
// can assert on their closed state without racing the dial goroutines.
type fakeConnTracker struct {
	mu    sync.Mutex
	conns map[string]*fakeDialConn
}

func (tr *fakeConnTracker) conn(alias string) *fakeDialConn {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.conns[alias]
}

func (tr *fakeConnTracker) isClosed(alias string) bool {
	c := tr.conn(alias)
	return c != nil && c.isClosed()
}

func newFakeDialer(behaviors map[string]fakeDialBehavior) (func(alias string) (*fakeDialConn, time.Duration, error), *fakeConnTracker) {
	tracker := &fakeConnTracker{conns: make(map[string]*fakeDialConn)}

	dial := func(alias string) (*fakeDialConn, time.Duration, error) {
		b, ok := behaviors[alias]
		if !ok {
			return nil, 0, fmt.Errorf("no behavior scripted for alias %q", alias)
		}
		if b.delay > 0 {
			time.Sleep(b.delay)
		}
		if b.err != nil {
			return nil, 0, b.err
		}
		conn := &fakeDialConn{alias: alias}
		tracker.mu.Lock()
		tracker.conns[alias] = conn
		tracker.mu.Unlock()
		return conn, b.latency, nil
	}

	return dial, tracker
}

func TestRaceAliases_FirstAliasWinsImmediately(t *testing.T) {
	dial, _ := newFakeDialer(map[string]fakeDialBehavior{
		"lan": {latency: 5 * time.Millisecond},
		"vpn": {delay: 50 * time.Millisecond, latency: 20 * time.Millisecond},
	})

	var events []ConnectionEvent
	handler := func(e ConnectionEvent) { events = append(events, e) }

	win, err := raceAliases(t.Name(), []string{"lan", "vpn"}, 200*time.Millisecond, handler, dial)
	require.NoError(t, err)
	require.NotNil(t, win)

	assert.Equal(t, "lan", win.alias)
	assert.Equal(t, 0, win.index)
	assert.Equal(t, 5*time.Millisecond, win.latency)

	// Trying events fire per alias, in priority order, before anything else.
	require.GreaterOrEqual(t, len(events), 3)
	assert.Equal(t, EventTrying, events[0].Type)
	assert.Equal(t, "lan", events[0].Alias)
	assert.Equal(t, EventTrying, events[1].Type)
	assert.Equal(t, "vpn", events[1].Alias)

	// Winner gets a single EventConnected with its latency and no fallback marker.
	connected := eventsOfType(events, EventConnected)
	require.Len(t, connected, 1)
	assert.Equal(t, "lan", connected[0].Alias)
	assert.Equal(t, 5*time.Millisecond, connected[0].Latency)
	assert.Equal(t, "connected via lan", connected[0].Message)
}

func TestRaceAliases_FirstAliasWinsWithinGrace(t *testing.T) {
	// The later alias connects first; the preferred alias lands inside the
	// grace window and must win. The loser must be closed.
	dial, conns := newFakeDialer(map[string]fakeDialBehavior{
		"lan": {delay: 60 * time.Millisecond, latency: 60 * time.Millisecond},
		"vpn": {latency: 2 * time.Millisecond},
	})

	win, err := raceAliases(t.Name(), []string{"lan", "vpn"}, 500*time.Millisecond, nil, dial)
	require.NoError(t, err)
	assert.Equal(t, "lan", win.alias)
	assert.Equal(t, 0, win.index)

	assert.Eventually(t, func() bool { return conns.isClosed("vpn") }, time.Second, 5*time.Millisecond,
		"less-preferred connection should be closed once the preferred alias wins")
	assert.False(t, win.client.isClosed(), "winner must stay open")
}

func TestRaceAliases_LaterAliasWinsAfterGraceExpiry(t *testing.T) {
	// The preferred alias is slow (dead LAN); the fallback connects fast and
	// should win once the grace window expires, without waiting for the full
	// preferred-alias timeout.
	dial, conns := newFakeDialer(map[string]fakeDialBehavior{
		"lan": {delay: 2 * time.Second, err: stderrors.New("dial lan: i/o timeout")},
		"vpn": {latency: 3 * time.Millisecond},
	})

	var events []ConnectionEvent
	handler := func(e ConnectionEvent) { events = append(events, e) }

	start := time.Now()
	win, err := raceAliases(t.Name(), []string{"lan", "vpn"}, 50*time.Millisecond, handler, dial)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "vpn", win.alias)
	assert.Equal(t, 1, win.index)
	assert.Less(t, elapsed, time.Second,
		"race should settle after the grace window, not after the slow alias times out")

	connected := eventsOfType(events, EventConnected)
	require.Len(t, connected, 1)
	assert.Equal(t, "connected via vpn (fallback)", connected[0].Message)

	// The slow alias never produced a connection, so nothing to close.
	assert.Nil(t, conns.conn("lan"))
}

func TestRaceAliases_BetterAliasReplacesEarlierSuccess(t *testing.T) {
	// Middle-priority alias connects first, then a higher-priority one lands
	// within the grace window. The first success must be closed and replaced.
	dial, conns := newFakeDialer(map[string]fakeDialBehavior{
		"a": {delay: 120 * time.Millisecond, latency: 120 * time.Millisecond},
		"b": {delay: 20 * time.Millisecond, latency: 20 * time.Millisecond},
		"c": {delay: 60 * time.Millisecond, latency: 60 * time.Millisecond},
	})

	win, err := raceAliases(t.Name(), []string{"a", "b", "c"}, 500*time.Millisecond, nil, dial)
	require.NoError(t, err)
	assert.Equal(t, "a", win.alias)

	assert.Eventually(t, func() bool {
		return conns.isClosed("b") && conns.isClosed("c")
	}, time.Second, 5*time.Millisecond, "losing connections should be closed")
	assert.False(t, conns.isClosed("a"))
}

func TestRaceAliases_LateLoserIsDrainedAndClosed(t *testing.T) {
	// The winner returns immediately; a straggler succeeds afterwards and must
	// be closed by the background drain.
	dial, conns := newFakeDialer(map[string]fakeDialBehavior{
		"lan": {latency: time.Millisecond},
		"vpn": {delay: 80 * time.Millisecond, latency: 80 * time.Millisecond},
	})

	win, err := raceAliases(t.Name(), []string{"lan", "vpn"}, 500*time.Millisecond, nil, dial)
	require.NoError(t, err)
	assert.Equal(t, "lan", win.alias)

	assert.Eventually(t, func() bool { return conns.isClosed("vpn") }, time.Second, 5*time.Millisecond,
		"straggler connection should be drained and closed")
}

func TestRaceAliases_AllFail_AggregatesEveryAlias(t *testing.T) {
	errLan := stderrors.New("dial lan: connection refused")
	errVpn := stderrors.New("dial vpn: i/o timeout")
	errBackup := &ProbeError{SSHAlias: "backup", Reason: ProbeFailAuth, Cause: stderrors.New("permission denied")}

	dial, _ := newFakeDialer(map[string]fakeDialBehavior{
		"lan":    {err: errLan},
		"vpn":    {delay: 10 * time.Millisecond, err: errVpn},
		"backup": {delay: 5 * time.Millisecond, err: errBackup},
	})

	var events []ConnectionEvent
	handler := func(e ConnectionEvent) { events = append(events, e) }

	win, err := raceAliases("gpu-box", []string{"lan", "vpn", "backup"}, 50*time.Millisecond, handler, dial)
	require.Error(t, err)
	assert.Nil(t, win)

	// Structured error with the same user-facing text as the sequential path.
	assert.Contains(t, err.Error(), "Couldn't connect to 'gpu-box' - tried: lan, vpn, backup")
	assert.Contains(t, err.Error(), "The remote might be offline, or there could be a network/firewall issue.")

	// Every alias failure is aggregated into the cause, not just the last one.
	assert.ErrorIs(t, err, errLan)
	assert.ErrorIs(t, err, errVpn)
	var probeErr *ProbeError
	assert.ErrorAs(t, err, &probeErr)

	// One EventFailed per alias, with categorized ProbeError messages preserved.
	failed := eventsOfType(events, EventFailed)
	require.Len(t, failed, 3)
	failedByAlias := make(map[string]ConnectionEvent, len(failed))
	for _, e := range failed {
		failedByAlias[e.Alias] = e
	}
	assert.Equal(t, "connection failed", failedByAlias["lan"].Message)
	assert.Equal(t, "connection failed", failedByAlias["vpn"].Message)
	assert.Equal(t, "authentication failed", failedByAlias["backup"].Message)
	assert.Len(t, eventsOfType(events, EventConnected), 0)
}

func TestRaceAliases_EventsFirePerAlias(t *testing.T) {
	aliases := []string{"a", "b", "c", "d"}
	dial, _ := newFakeDialer(map[string]fakeDialBehavior{
		"a": {err: stderrors.New("refused")},
		"b": {err: stderrors.New("refused")},
		"c": {err: stderrors.New("refused")},
		"d": {err: stderrors.New("refused")},
	})

	var events []ConnectionEvent
	handler := func(e ConnectionEvent) { events = append(events, e) }

	_, err := raceAliases(t.Name(), aliases, 50*time.Millisecond, handler, dial)
	require.Error(t, err)

	trying := eventsOfType(events, EventTrying)
	require.Len(t, trying, len(aliases))
	for i, e := range trying {
		assert.Equal(t, aliases[i], e.Alias, "trying events should follow priority order")
	}
	assert.Len(t, eventsOfType(events, EventFailed), len(aliases))
}

func TestRaceAliases_NoAliases(t *testing.T) {
	dial, _ := newFakeDialer(nil)

	_, err := raceAliases("empty-host", nil, 0, nil, dial)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs at least one SSH connection")
}

func TestRaceAliases_NilHandlerDoesNotPanic(t *testing.T) {
	dial, _ := newFakeDialer(map[string]fakeDialBehavior{
		"lan": {latency: time.Millisecond},
	})

	win, err := raceAliases(t.Name(), []string{"lan"}, 0, nil, dial)
	require.NoError(t, err)
	assert.Equal(t, "lan", win.alias)
}

func TestDialAliases_UsesInjectedDialFunc(t *testing.T) {
	var mu sync.Mutex
	var seenTimeouts []time.Duration

	dial := func(alias string, timeout time.Duration) (*sshutil.Client, time.Duration, error) {
		mu.Lock()
		seenTimeouts = append(seenTimeouts, timeout)
		mu.Unlock()
		if alias == "dead" {
			return nil, 0, stderrors.New("connection refused")
		}
		return &sshutil.Client{Host: alias}, 7 * time.Millisecond, nil
	}

	result, err := DialAliases("box", []string{"dead", "alive"}, DialOptions{
		Timeout: 3 * time.Second,
		Grace:   20 * time.Millisecond,
		Dial:    dial,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "alive", result.Alias)
	assert.Equal(t, 1, result.Index)
	assert.Equal(t, 7*time.Millisecond, result.Latency)
	require.NotNil(t, result.Client)
	assert.Equal(t, "alive", result.Client.Host)

	mu.Lock()
	defer mu.Unlock()
	for _, timeout := range seenTimeouts {
		assert.Equal(t, 3*time.Second, timeout, "per-alias timeout should be passed through")
	}
}

func TestDialAliases_AllFailStructuredError(t *testing.T) {
	dial := func(alias string, timeout time.Duration) (*sshutil.Client, time.Duration, error) {
		return nil, 0, categorizeProbeError(alias, stderrors.New("connect: connection refused"))
	}

	_, err := DialAliases("box", []string{"one", "two"}, DialOptions{Dial: dial, Grace: 10 * time.Millisecond})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Couldn't connect to 'box' - tried: one, two")
}

// eventsOfType filters events by type.
func eventsOfType(events []ConnectionEvent, t ConnectionEventType) []ConnectionEvent {
	var out []ConnectionEvent
	for _, e := range events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}
