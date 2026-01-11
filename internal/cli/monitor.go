package cli

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/monitor"
)

// monitorCommand starts the TUI monitoring dashboard.
func monitorCommand(hostsFilter string, interval time.Duration) error {
	// Load global config (hosts are stored globally now)
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	// Filter hosts if --hosts flag provided
	hosts := globalCfg.Hosts
	if hostsFilter != "" {
		hosts = filterHosts(globalCfg.Hosts, hostsFilter)
		if len(hosts) == 0 {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("No hosts match '%s'", hostsFilter),
				"Double-check your host names or try without the --hosts filter.")
		}
	}

	if len(hosts) == 0 {
		return errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add a host with 'rr host add' first.")
	}

	// Create collector from filtered hosts
	collector := monitor.NewCollector(hosts)

	// Create Bubble Tea model
	model := monitor.NewModel(collector, interval)

	// Run the TUI program
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()

	// Graceful shutdown: close all SSH connections
	collector.Close()

	return err
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
	for name, host := range allHosts {
		if filterNames[name] {
			result[name] = host
		}
	}

	return result
}
