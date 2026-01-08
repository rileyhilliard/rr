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
	// Load config
	cfgPath, err := config.Find(Config())
	if err != nil {
		return err
	}
	if cfgPath == "" {
		return errors.New(errors.ErrConfig,
			"No config file found",
			"Run 'rr init' to create a .rr.yaml config file")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	if err := config.Validate(cfg); err != nil {
		return err
	}

	// Filter hosts if --hosts flag provided
	hosts := cfg.Hosts
	if hostsFilter != "" {
		hosts = filterHosts(cfg.Hosts, hostsFilter)
		if len(hosts) == 0 {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("No matching hosts found for filter: %s", hostsFilter),
				"Check your host names or remove the --hosts filter")
		}
	}

	if len(hosts) == 0 {
		return errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add hosts to your .rr.yaml config file")
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
