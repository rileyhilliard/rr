package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/monitor"
)

// monitorCommand starts the TUI monitoring dashboard.
// interval is only meaningful when intervalSet is true (--interval flag used);
// otherwise the interval is resolved from project config with a 1s default.
func monitorCommand(hostsFilter string, interval time.Duration, intervalSet bool) error {
	scope, err := resolveMonitorScope(hostsFilter)
	if err != nil {
		return err
	}

	// Resolve refresh interval: --interval flag > monitor.interval config > 1s default
	interval, err = resolveMonitorInterval(interval, intervalSet, scope.project)
	if err != nil {
		return err
	}

	collector := scope.newCollector()

	// Create Bubble Tea model with host order for default sorting
	model := monitor.NewModelWithOptions(collector, interval, scope.timeout, scope.order,
		monitorModelOptions(scope.project))

	// Run the TUI program with mouse support for scrolling
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()

	// Graceful shutdown: close all SSH connections
	collector.Close()

	return err
}

// monitorScope is the resolved set of hosts and settings both monitor modes
// operate on: the same --hosts filter, monitor.exclude list, timeout, lock
// config and thresholds, so --once and the TUI never drift apart.
type monitorScope struct {
	hosts      map[string]config.Host
	order      []string
	timeout    time.Duration
	thresholds config.ThresholdConfig
	lockCfg    *config.LockConfig
	project    *config.Config
}

// defaultMonitorTimeout is the per-host collection timeout when monitor.timeout
// is unset or unparseable.
const defaultMonitorTimeout = 8 * time.Second

// resolveMonitorScope loads config and applies host filtering, exclusion and
// settings resolution shared by `rr monitor` and `rr monitor --once`.
func resolveMonitorScope(hostsFilter string) (*monitorScope, error) {
	resolved, err := config.LoadResolved("")
	if err != nil {
		return nil, err
	}

	// Get hosts with proper priority order (project hosts list order, or alphabetical for global)
	hostOrder, hosts, err := config.ResolveHosts(resolved, "")
	if err != nil {
		// Fall back to just global hosts if resolution fails
		if len(resolved.Global.Hosts) == 0 {
			return nil, errors.New(errors.ErrConfig,
				"No hosts configured",
				"Add a host with 'rr host add' first.")
		}
		hosts = resolved.Global.Hosts
		// Build order alphabetically
		for name := range hosts {
			hostOrder = append(hostOrder, name)
		}
		sort.Strings(hostOrder)
	}

	// Filter hosts if --hosts flag provided
	if hostsFilter != "" {
		hosts = filterHosts(hosts, hostsFilter)
		if len(hosts) == 0 {
			return nil, errors.New(errors.ErrConfig,
				fmt.Sprintf("No hosts match '%s'", hostsFilter),
				"Double-check your host names or try without the --hosts filter.")
		}
		// Filter the order list too
		hostOrder = filterHostOrder(hostOrder, hosts)
	}

	// Apply monitor.exclude from project config. Hosts explicitly requested
	// via --hosts win over the exclude list.
	if resolved.Project != nil && len(resolved.Project.Monitor.Exclude) > 0 {
		hosts = excludeHosts(hosts, resolved.Project.Monitor.Exclude, hostsFilter)
		if len(hosts) == 0 {
			return nil, errors.New(errors.ErrConfig,
				"All hosts are excluded by monitor.exclude",
				"Remove some entries from monitor.exclude in .rr.yaml, or request hosts explicitly with --hosts.")
		}
		hostOrder = filterHostOrder(hostOrder, hosts)
	}

	if len(hosts) == 0 {
		return nil, errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add a host with 'rr host add' first.")
	}

	scope := &monitorScope{
		hosts:   hosts,
		order:   hostOrder,
		timeout: defaultMonitorTimeout,
		project: resolved.Project,
	}

	if resolved.Project != nil {
		if resolved.Project.Monitor.Timeout != "" {
			if parsed, err := time.ParseDuration(resolved.Project.Monitor.Timeout); err == nil {
				scope.timeout = parsed
			}
		}
		// Zero threshold values fall back to the 70/90 defaults downstream.
		scope.thresholds = resolved.Project.Monitor.Thresholds
		if resolved.Project.Lock.Enabled {
			lockCfg := resolved.Project.Lock
			scope.lockCfg = &lockCfg
		}
	}

	return scope, nil
}

// newCollector builds a collector wired with the scope's hosts and lock
// configuration. The per-host timeout is left at the collector default; the
// TUI enforces scope.timeout itself, and --once applies it explicitly.
func (s *monitorScope) newCollector() *monitor.Collector {
	collector := monitor.NewCollector(s.hosts)
	if s.lockCfg != nil {
		collector.SetLockConfig(*s.lockCfg)
	}
	return collector
}

// monitorModelOptions builds the dashboard options from project config.
// A nil project (global-only setup) leaves everything at its zero value, which
// the model reads as default thresholds and alerting turned off.
func monitorModelOptions(project *config.Config) monitor.ModelOptions {
	if project == nil {
		return monitor.ModelOptions{}
	}
	return monitor.ModelOptions{
		Thresholds: project.Monitor.Thresholds,
		Alerts:     project.Monitor.Alerts,
	}
}

// resolveMonitorInterval returns the effective refresh interval.
// Precedence: explicit --interval flag > monitor.interval from project config > 1s default.
// Config-sourced intervals get the same >=500ms validation as the flag.
func resolveMonitorInterval(flagInterval time.Duration, flagSet bool, project *config.Config) (time.Duration, error) {
	if flagSet {
		return flagInterval, nil
	}

	if project == nil || project.Monitor.Interval == "" {
		return time.Second, nil
	}

	parsed, err := time.ParseDuration(project.Monitor.Interval)
	if err != nil {
		return 0, errors.WrapWithCode(err, errors.ErrConfig,
			fmt.Sprintf("'%s' doesn't look like a valid monitor.interval", project.Monitor.Interval),
			"Set monitor.interval in .rr.yaml to something like 1s, 2s, or 5s.")
	}
	if parsed < 500*time.Millisecond {
		return 0, errors.New(errors.ErrConfig,
			"That monitor.interval is too short",
			"Keep monitor.interval in .rr.yaml at 500ms or above to avoid hammering the hosts.")
	}

	return parsed, nil
}

// excludeHosts removes hosts named in the monitor.exclude list.
// Hosts explicitly requested via the --hosts filter are never excluded.
func excludeHosts(allHosts map[string]config.Host, exclude []string, hostsFilter string) map[string]config.Host {
	if len(exclude) == 0 {
		return allHosts
	}

	// Hosts explicitly requested via --hosts win over the exclude list
	requested := make(map[string]bool)
	for _, name := range strings.Split(hostsFilter, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			requested[name] = true
		}
	}

	excluded := make(map[string]bool)
	for _, name := range exclude {
		name = strings.TrimSpace(name)
		if name != "" {
			excluded[name] = true
		}
	}

	result := make(map[string]config.Host)
	for name := range allHosts {
		if excluded[name] && !requested[name] {
			continue
		}
		result[name] = allHosts[name]
	}

	return result
}

// filterHostOrder filters the host order list to only include hosts that exist in the hosts map.
func filterHostOrder(order []string, hosts map[string]config.Host) []string {
	var filtered []string
	for _, name := range order {
		if _, ok := hosts[name]; ok {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

// filterHosts returns only hosts that match the comma-separated filter.
func filterHosts(allHosts map[string]config.Host, filter string) map[string]config.Host {
	if filter == "" {
		return allHosts
	}

	// Parse the filter into a set of names
	filterNames := make(map[string]bool)
	for _, name := range strings.Split(filter, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			filterNames[name] = true
		}
	}

	// Return only matching hosts
	result := make(map[string]config.Host)
	for name := range allHosts {
		if filterNames[name] {
			result[name] = allHosts[name]
		}
	}

	return result
}
