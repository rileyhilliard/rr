package monitor

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// snapshotInterval is the wall-clock gap between the two samples taken by
// BuildSnapshotCommand. Delta rates are divided by this.
const snapshotInterval = SnapshotSleepSeconds * time.Second

// CollectSnapshot gathers one complete, delta-accurate reading from every
// configured host in parallel and returns the results sorted by alias.
//
// Unlike CollectStreaming (which relies on the TUI polling repeatedly for
// deltas), this issues a single SSH command per host that samples the
// delta-based sources twice around a one-second sleep. CPU%, per-core usage,
// disk I/O rates and network rates are therefore real on the very first call.
func (c *Collector) CollectSnapshot(ctx context.Context) []HostResult {
	aliases := c.Hosts()
	results := make([]HostResult, len(aliases))

	var wg sync.WaitGroup
	for i, alias := range aliases {
		wg.Add(1)
		go func(idx int, alias string) {
			defer wg.Done()

			hostCtx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()

			results[idx] = c.snapshotOne(hostCtx, alias)
		}(i, alias)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].Alias < results[j].Alias })
	return results
}

// snapshotOne runs the double-sampling snapshot command against a single host.
func (c *Collector) snapshotOne(ctx context.Context, alias string) HostResult {
	result := HostResult{Alias: alias}

	select {
	case <-ctx.Done():
		result.Error = ctx.Err()
		return result
	default:
	}

	client, platform, err := c.pool.GetWithPlatform(alias)
	if err != nil {
		result.Error = err
		return result
	}
	result.Platform = platform

	if latency, err := c.probeLatency(ctx, client); err == nil {
		result.Latency = latency
	}

	session, err := client.Client.NewSession()
	if err != nil {
		c.pool.CloseOne(alias)
		result.Error = err
		return result
	}
	defer session.Close()

	type runResult struct {
		output []byte
		err    error
	}
	resultCh := make(chan runResult, 1)
	cmd := BuildSnapshotCommand(platform, c.lockDir())
	go func() {
		out, err := session.CombinedOutput(cmd)
		resultCh <- runResult{out, err}
	}()

	select {
	case <-ctx.Done():
		// Explicit close (despite the defer) is what unblocks the
		// CombinedOutput goroutine when the context is cancelled mid-command.
		_ = session.Close()
		result.Error = ctx.Err()
		return result
	case r := <-resultCh:
		if r.err != nil {
			result.Error = r.err
			return result
		}
		metrics, lockInfo, netRates := c.parseSnapshotOutput(alias, platform, string(r.output))
		result.Metrics = metrics
		result.LockInfo = lockInfo
		result.NetRates = netRates
		result.ConnectedVia = c.pool.GetConnectedVia(alias)
		return result
	}
}

// parseSnapshotOutput splits the snapshot output into the priming sections and
// the regular metrics sections, seeds the per-host delta state from the
// priming sample, then reuses the normal parsers on the second sample.
func (c *Collector) parseSnapshotOutput(alias string, platform Platform, output string) (*HostMetrics, *HostLockInfo, *NetRates) {
	sections := strings.Split(output, OutputSeparator+"\n")

	prime := SnapshotPrimeSections(platform)
	if len(sections) <= prime {
		// Truncated output (host died mid-command): fall back to parsing what
		// we have as a plain single sample rather than dropping it entirely.
		metrics, lockInfo := c.parseOutput(alias, platform, output)
		return metrics, lockInfo, nil
	}

	// The sleep runs after the last priming separator, so its (empty) output
	// is the head of the first regular section; the shell emits nothing there.
	primeSections := sections[:prime]
	rest := strings.Join(sections[prime:], OutputSeparator+"\n")

	// Seed delta state from the first sample, timestamped one interval back so
	// the rate math divides by the real gap.
	primeAt := time.Now().Add(-snapshotInterval)
	var primeNet []NetworkInterface
	if platform == PlatformDarwin {
		primeNet = c.seedDarwinSnapshot(primeSections)
	} else {
		primeNet = c.seedLinuxSnapshot(alias, primeSections, primeAt)
	}

	metrics, lockInfo := c.parseOutput(alias, platform, rest)

	var rates *NetRates
	if metrics != nil {
		rates = networkRatesFromSamples(primeNet, metrics.Network, snapshotInterval)
	}
	return metrics, lockInfo, rates
}

// seedLinuxSnapshot feeds the priming /proc/stat and /proc/diskstats samples
// into the collector's delta state and returns the priming network counters.
// Sections: 0=/proc/stat, 1=/proc/net/dev, 2=/proc/diskstats.
func (c *Collector) seedLinuxSnapshot(alias string, sections []string, at time.Time) []NetworkInterface {
	if len(sections) > 0 {
		// Discarded: the first sample has no predecessor, so its only effect
		// is storing prevJiffies/prevCoreJiffies for the second sample.
		_, _ = c.parseLinuxCPUWithDelta(alias, strings.TrimSpace(sections[0]), "")
	}
	if len(sections) > 2 {
		_, _ = c.parseLinuxDiskIOWithDelta(alias, strings.TrimSpace(sections[2]), at)
	}
	if len(sections) > 1 {
		if net, err := parseLinuxNetwork(strings.TrimSpace(sections[1])); err == nil {
			return net
		}
	}
	return nil
}

// seedDarwinSnapshot returns the priming netstat counters. macOS CPU comes
// from `top -l 1` (already instantaneous) and there is no disk counter
// section, so nothing needs to enter the delta maps.
func (c *Collector) seedDarwinSnapshot(sections []string) []NetworkInterface {
	if len(sections) == 0 {
		return nil
	}
	if net, err := parseDarwinNetwork(strings.TrimSpace(sections[0])); err == nil {
		return net
	}
	return nil
}

// networkRatesFromSamples computes aggregate rx/tx bytes per second from two
// interface counter samples, skipping loopback and interfaces missing from
// either sample. Counter resets (negative deltas) contribute zero.
func networkRatesFromSamples(prev, cur []NetworkInterface, interval time.Duration) *NetRates {
	seconds := interval.Seconds()
	if seconds <= 0 || len(prev) == 0 || len(cur) == 0 {
		return nil
	}

	prevByName := make(map[string]NetworkInterface, len(prev))
	for _, iface := range prev {
		prevByName[iface.Name] = iface
	}

	rates := &NetRates{}
	for _, iface := range cur {
		if iface.Name == "lo" || iface.Name == "lo0" {
			continue
		}
		before, ok := prevByName[iface.Name]
		if !ok {
			continue
		}
		if d := iface.BytesIn - before.BytesIn; d > 0 {
			rates.RxBytesPerSec += float64(d) / seconds
		}
		if d := iface.BytesOut - before.BytesOut; d > 0 {
			rates.TxBytesPerSec += float64(d) / seconds
		}
	}
	return rates
}
