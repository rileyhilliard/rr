package host

import (
	stderrors "errors"
	"fmt"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// DefaultPreferenceGrace is how long DialAliases waits for an earlier
// (higher-priority) alias to connect after a later alias has already
// succeeded. This lets a slightly slower LAN address beat a faster VPN
// address without blocking on dead hosts.
const DefaultPreferenceGrace = 500 * time.Millisecond

// DialFunc dials a single SSH alias and returns the connected client plus
// the measured connection latency. ProbeAndConnect is the default
// implementation; tests can inject a fake to avoid real SSH connections.
type DialFunc func(sshAlias string, timeout time.Duration) (*sshutil.Client, time.Duration, error)

// DialOptions configures DialAliases. The zero value uses sensible defaults.
type DialOptions struct {
	// Timeout is the per-alias dial timeout. Defaults to DefaultProbeTimeout.
	Timeout time.Duration
	// Grace is the preference window: after a lower-priority alias connects,
	// wait this long for a higher-priority alias before settling.
	// Defaults to DefaultPreferenceGrace.
	Grace time.Duration
	// OnEvent receives per-alias connection events (EventTrying, EventFailed,
	// EventConnected). May be nil. Events for different aliases arrive
	// interleaved rather than strictly sequential since dials run in parallel.
	// All events are emitted from the calling goroutine, and none are emitted
	// after DialAliases returns.
	OnEvent EventHandler
	// Dial is the per-alias dial primitive. Defaults to ProbeAndConnect,
	// which preserves latency capture and categorized ProbeError reasons.
	Dial DialFunc
}

// DialResult describes the winning connection from DialAliases.
type DialResult struct {
	Client  *sshutil.Client
	Alias   string        // The alias that won
	Index   int           // Position of the winning alias in the input list
	Latency time.Duration // Connection latency measured during the dial
}

// DialAliases dials all SSH aliases in parallel and returns the winning
// connection. Earlier aliases in the list are preferred: if a later alias
// connects first, the race waits up to the grace window for an earlier one
// before settling. Losing connections are closed.
//
// When every alias fails, all failures are aggregated into a single
// structured error with an actionable suggestion.
func DialAliases(hostName string, aliases []string, opts DialOptions) (*DialResult, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultProbeTimeout
	}
	dial := opts.Dial
	if dial == nil {
		dial = ProbeAndConnect
	}

	win, err := raceAliases(hostName, aliases, opts.Grace, opts.OnEvent,
		func(alias string) (*sshutil.Client, time.Duration, error) {
			return dial(alias, timeout)
		})
	if err != nil {
		return nil, err
	}
	return &DialResult{
		Client:  win.client,
		Alias:   win.alias,
		Index:   win.index,
		Latency: win.latency,
	}, nil
}

// aliasDialOutcome carries the result of a single alias dial attempt.
type aliasDialOutcome[C interface{ Close() error }] struct {
	client  C
	alias   string
	index   int
	latency time.Duration
	err     error
}

// raceAliases implements the parallel-dial-with-preference race. It is
// generic over the client type so tests can exercise the race with
// lightweight fake clients and verify losers are closed.
//
// Events are emitted only from the calling goroutine: EventTrying for every
// alias up front (in priority order), EventFailed for failures observed
// before a winner is chosen, and a single EventConnected for the winner.
// Late results after a winner is chosen are drained and closed silently in
// the background.
func raceAliases[C interface{ Close() error }](
	hostName string,
	aliases []string,
	grace time.Duration,
	handler EventHandler,
	dial func(alias string) (C, time.Duration, error),
) (*aliasDialOutcome[C], error) {
	if len(aliases) == 0 {
		return nil, errors.New(errors.ErrConfig,
			fmt.Sprintf("Host '%s' needs at least one SSH connection", hostName),
			"Add something like 'user@hostname' under the 'ssh:' section for this host.")
	}
	if grace == 0 {
		grace = DefaultPreferenceGrace
	}

	emit := func(event ConnectionEvent) {
		if handler != nil {
			handler(event)
		}
	}

	// Announce every attempt in priority order before launching, so callers
	// see a deterministic prefix even though results arrive interleaved.
	for _, alias := range aliases {
		emit(ConnectionEvent{
			Type:    EventTrying,
			Alias:   alias,
			Message: fmt.Sprintf("trying alias %s", alias),
		})
	}

	results := make(chan aliasDialOutcome[C], len(aliases))
	for i, alias := range aliases {
		go func(idx int, sshAlias string) {
			client, latency, err := dial(sshAlias)
			results <- aliasDialOutcome[C]{
				client:  client,
				alias:   sshAlias,
				index:   idx,
				latency: latency,
				err:     err,
			}
		}(i, alias)
	}

	// finish declares a winner: emit its EventConnected and close any
	// still-outstanding connections in the background.
	received := 0
	finish := func(win aliasDialOutcome[C]) (*aliasDialOutcome[C], error) {
		msg := fmt.Sprintf("connected via %s", win.alias)
		if win.index > 0 {
			msg = fmt.Sprintf("connected via %s (fallback)", win.alias)
		}
		emit(ConnectionEvent{
			Type:    EventConnected,
			Alias:   win.alias,
			Message: msg,
			Latency: win.latency,
		})
		go drainAndCloseDials(results, received, len(aliases))
		return &win, nil
	}

	// Collect results, preferring earlier aliases. Failure errors are kept
	// per index so the aggregated error is deterministic.
	errs := make([]error, len(aliases))
	var best *aliasDialOutcome[C]
	var graceTimer <-chan time.Time

	for received < len(aliases) {
		select {
		case r := <-results:
			received++

			if r.err != nil {
				errs[r.index] = r.err
				errMsg := "connection failed"
				var probeErr *ProbeError
				if stderrors.As(r.err, &probeErr) {
					errMsg = probeErr.Reason.String()
				}
				emit(ConnectionEvent{
					Type:    EventFailed,
					Alias:   r.alias,
					Message: errMsg,
					Error:   r.err,
				})
				continue
			}

			switch {
			case best == nil:
				// First success. Use it immediately if it's the most
				// preferred, otherwise wait briefly for better options.
				best = &r
				if r.index == 0 {
					return finish(r)
				}
				graceTimer = time.After(grace)
			case r.index < best.index:
				// Found a more preferred connection, switch to it.
				_ = best.client.Close()
				best = &r
				if r.index == 0 {
					return finish(r)
				}
			default:
				// Less preferred than current best, close it.
				_ = r.client.Close()
			}

		case <-graceTimer:
			// Grace period expired, settle for the best so far.
			return finish(*best)
		}
	}

	// All attempts finished before the grace expired.
	if best != nil {
		return finish(*best)
	}

	// All aliases failed - aggregate every failure into one structured error.
	failures := make([]error, 0, len(aliases))
	for _, err := range errs {
		if err != nil {
			failures = append(failures, err)
		}
	}
	return nil, errors.WrapWithCode(stderrors.Join(failures...), errors.ErrSSH,
		fmt.Sprintf("Couldn't connect to '%s' - tried: %s", hostName, formatFailedAliases(aliases)),
		"The remote might be offline, or there could be a network/firewall issue.")
}

// drainAndCloseDials closes connections from attempts that complete after a
// winner has been chosen. The winner's result was already consumed from the
// channel, so every remaining successful dial is a loser to close.
func drainAndCloseDials[C interface{ Close() error }](results chan aliasDialOutcome[C], received, total int) {
	for received < total {
		r := <-results
		received++
		if r.err == nil {
			_ = r.client.Close()
		}
	}
}
