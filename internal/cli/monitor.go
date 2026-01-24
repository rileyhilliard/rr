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
func monitorCommand(hostsFilter string, interval time.Duration) error {
	// Load resolved config to get proper host ordering
	resolved, err := config.LoadResolved("")
	if err != nil {
		return err
	}

	// Get hosts with proper priority order (project hosts list order, or alphabetical for global)
	hostOrder, hosts, err := config.ResolveHosts(resolved, "")
	if err != nil {
		// Fall back to just global hosts if resolution fails
		if len(resolved.Global.Hosts) == 0 {
			return errors.New(errors.ErrConfig,
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
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("No hosts match '%s'", hostsFilter),
				"Double-check your host names or try without the --hosts filter.")
		}
		// Filter the order list too
		hostOrder = filterHostOrder(hostOrder, hosts)
	}

	if len(hosts) == 0 {
		return errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add a host with 'rr host add' first.")
	}

	// Parse timeout from config (default to 8s if not set or invalid)
	timeout := 8 * time.Second
	if resolved.Project != nil && resolved.Project.Monitor.Timeout != "" {
		if parsed, err := time.ParseDuration(resolved.Project.Monitor.Timeout); err == nil {
			timeout = parsed
		}
	}

	// Create collector from filtered hosts
	collector := monitor.NewCollector(hosts)

	// Configure lock checking if we have a project config with locking enabled
	if resolved.Project != nil && resolved.Project.Lock.Enabled {
		collector.SetLockConfig(resolved.Project.Lock)
	}

	// Create Bubble Tea model with host order for default sorting
	model := monitor.NewModel(collector, interval, timeout, hostOrder)

	// Run the TUI program with mouse support for scrolling
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()

	// Graceful shutdown: close all SSH connections
	collector.Close()

	return err
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
