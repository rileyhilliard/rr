package cli

import (
	"encoding/json"
	"fmt"
	"os"
	gosync "sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/spf13/cobra"
)

var statusJSON bool

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "output in JSON format")
}

// StatusOutput represents the JSON output for status command.
type StatusOutput struct {
	Hosts    []HostStatus `json:"hosts"`
	Selected *Selected    `json:"selected,omitempty"`
}

// HostStatus represents a single host's status.
type HostStatus struct {
	Name    string        `json:"name"`
	Aliases []AliasStatus `json:"aliases"`
	Healthy bool          `json:"healthy"`
}

// AliasStatus represents a single SSH alias's probe result.
type AliasStatus struct {
	Alias   string `json:"alias"`
	Status  string `json:"status"` // "connected", "failed"
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Selected indicates which host/alias would be used for the next command.
type Selected struct {
	Host  string `json:"host"`
	Alias string `json:"alias"`
}

// statusCommand implements the status command logic.
func statusCommand() error {
	// Load global config (hosts are stored globally now)
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	if len(globalCfg.Hosts) == 0 {
		return errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add a host with 'rr host add' first.")
	}

	// Probe all hosts in parallel
	results := probeAllHosts(globalCfg.Hosts)

	// Determine which host would be selected (first healthy host)
	selected := findSelectedHost(results)

	// Machine mode implies JSON output
	if statusJSON || machineMode {
		return outputStatusJSON(results, selected)
	}

	return outputStatusText(results, selected)
}

// probeResult holds the result of probing a single host.
type probeResult struct {
	HostName string
	Aliases  []host.ProbeResult
}

// probeAllHosts probes all configured hosts in parallel.
func probeAllHosts(hosts map[string]config.Host) map[string]probeResult {
	results := make(map[string]probeResult)
	var mu gosync.Mutex
	var wg gosync.WaitGroup

	timeout := host.DefaultProbeTimeout

	for name := range hosts {
		h := hosts[name]
		wg.Add(1)
		go func(hostName string, hostCfg config.Host) {
			defer wg.Done()

			aliasResults := host.ProbeAll(hostCfg.SSH, timeout)

			mu.Lock()
			results[hostName] = probeResult{
				HostName: hostName,
				Aliases:  aliasResults,
			}
			mu.Unlock()
		}(name, h)
	}

	wg.Wait()
	return results
}

// findSelectedHost determines which host/alias would be used for the next command.
// Returns the first healthy host found.
func findSelectedHost(results map[string]probeResult) *Selected {
	for name, result := range results {
		for _, alias := range result.Aliases {
			if alias.Success {
				return &Selected{Host: name, Alias: alias.SSHAlias}
			}
		}
	}
	return nil
}

// outputStatusJSON outputs status in JSON format.
// When machineMode is enabled, wraps output in the standard JSON envelope.
func outputStatusJSON(results map[string]probeResult, selected *Selected) error {
	output := StatusOutput{
		Hosts:    make([]HostStatus, 0, len(results)),
		Selected: selected,
	}

	for name, result := range results {
		hostStatus := HostStatus{
			Name:    name,
			Aliases: make([]AliasStatus, 0, len(result.Aliases)),
			Healthy: false,
		}

		for _, alias := range result.Aliases {
			as := AliasStatus{
				Alias: alias.SSHAlias,
			}
			if alias.Success {
				as.Status = "connected"
				as.Latency = alias.Latency.String()
				hostStatus.Healthy = true
			} else {
				as.Status = "failed"
				if alias.Error != nil {
					as.Error = alias.Error.Error()
				}
			}
			hostStatus.Aliases = append(hostStatus.Aliases, as)
		}

		output.Hosts = append(output.Hosts, hostStatus)
	}

	// Use envelope wrapper in machine mode
	if machineMode {
		return WriteJSONSuccess(os.Stdout, output)
	}

	// Legacy --json behavior (no envelope)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputStatusText outputs status in human-readable format using a table.
func outputStatusText(results map[string]probeResult, selected *Selected) error {
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)

	// Build table rows
	var rows []ui.StatusTableRow
	for name, result := range results {
		for _, alias := range result.Aliases {
			row := ui.StatusTableRow{
				Host:  name,
				Alias: alias.SSHAlias,
			}

			if alias.Success {
				row.Status = "ok"
				row.Latency = formatLatency(alias.Latency)
			} else {
				row.Status = "fail"
				errMsg := "Connection failed"
				if probeErr, ok := alias.Error.(*host.ProbeError); ok {
					errMsg = probeErr.Reason.String()
				} else if alias.Error != nil {
					errMsg = alias.Error.Error()
				}
				row.Latency = errMsg
			}

			rows = append(rows, row)
		}
	}

	// Convert selected to table selection
	var tableSelection *ui.StatusTableSelection
	if selected != nil {
		tableSelection = &ui.StatusTableSelection{
			Host:  selected.Host,
			Alias: selected.Alias,
		}
	}

	// Render the table
	fmt.Println(ui.RenderStatusTable(rows, tableSelection))

	// Show selected summary
	if selected != nil {
		fmt.Printf("Selected: %s %s\n",
			selected.Host,
			mutedStyle.Render(fmt.Sprintf("(via %s)", selected.Alias)),
		)
	} else {
		fmt.Printf("Selected: %s\n", errorStyle.Render("none (no reachable hosts)"))
	}

	return nil
}

// formatLatency formats a duration as a human-readable latency string.
func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// Update the statusCmd to use statusCommand
func init() {
	// Override the RunE to use our implementation
	statusCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return statusCommand()
	}
}
