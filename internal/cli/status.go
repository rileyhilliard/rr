package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
	// Hosts are in preference order: the order a run tries them in.
	Hosts []HostStatus `json:"hosts"`
	// Scope says where Hosts came from: "project" (the .rr.yaml host
	// list), "global" (every host in ~/.rr/config.yaml, alphabetical), or
	// "local" (local mode: this machine only).
	Scope    string          `json:"scope"`
	Selected *Selected       `json:"selected,omitempty"`
	Project  *ProjectMapping `json:"project,omitempty"`
}

// Status scopes: where the listed hosts came from.
const (
	statusScopeProject = "project"
	statusScopeGlobal  = "global"
	// statusScopeLocal: the project is in local mode (local_fallback with
	// no hosts), so runs use this machine and no other host.
	statusScopeLocal = "local"
)

// ProjectMapping answers "where will this tree sync?" per host.
type ProjectMapping struct {
	LocalRoot        string            `json:"local_root,omitempty"`
	Worktree         string            `json:"worktree,omitempty"`
	IsLinkedWorktree bool              `json:"is_linked_worktree"`
	RemoteDirs       map[string]string `json:"remote_dirs"`
	// InPlaceHosts are hosts with local: true. They run in LocalRoot, so
	// they have no entry in RemoteDirs.
	InPlaceHosts []string `json:"in_place_hosts,omitempty"`
}

// buildProjectMapping resolves the current tree's remote directory on each
// listed host, reflecting worktree isolation.
func buildProjectMapping(hosts map[string]config.Host) *ProjectMapping {
	wt := config.DetectWorktree()
	m := &ProjectMapping{
		IsLinkedWorktree: wt.IsLinked,
		Worktree:         wt.Name,
		RemoteDirs:       make(map[string]string, len(hosts)),
	}
	if wt.TopLevel != "" {
		m.LocalRoot = wt.TopLevel
	} else {
		// No repo: the project root (where .rr.yaml is), which is where a
		// sync starts and an in-place host runs, or the current directory.
		m.LocalRoot = config.DefaultLocalHostDir()
	}
	for name := range hosts {
		if hosts[name].Local {
			m.InPlaceHosts = append(m.InPlaceHosts, name)
			continue
		}
		m.RemoteDirs[name] = config.ExpandRemote(hosts[name].Dir)
	}
	sort.Strings(m.InPlaceHosts)
	return m
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

// Selected is the first reachable host in preference order: where the next
// run goes unless that host is locked, in which case the run moves on to the
// next one.
type Selected struct {
	Host  string `json:"host"`
	Alias string `json:"alias"`
}

// statusCommand implements the status command logic.
func statusCommand() error {
	resolved, err := config.LoadResolved(Config())
	if errors.IsCode(err, errors.ErrConfigNotFound) && Config() == "" {
		// A checkout without .rr.yaml inside one that has it: that config
		// is skipped so a run doesn't use the other checkout's code. Status
		// runs no code, so it shows the global hosts, like rr monitor.
		global, globalErr := config.LoadGlobal()
		if globalErr != nil {
			return globalErr
		}
		resolved = &config.ResolvedConfig{Global: global, Project: config.DefaultConfig(), Source: config.GlobalOnly}
	} else if err != nil {
		return err
	}

	if len(resolved.Global.Hosts) == 0 && !configLocalMode(resolved) {
		return errors.New(errors.ErrConfig,
			"No hosts configured",
			"Add a host with 'rr host add' first.")
	}

	order, hosts, scope, err := statusHosts(resolved)
	if err != nil {
		return err
	}

	results := probeAllHosts(order, hosts)
	selected := findSelectedHost(results)

	// Where does this tree sync? (reflects worktree isolation)
	mapping := buildProjectMapping(hosts)

	// JSON output: explicit --json flag, or default structured mode (not --pretty)
	if statusJSON || MachineMode() {
		return outputStatusJSON(results, scope, selected, mapping)
	}

	return outputStatusText(results, scope, selected, mapping)
}

// statusHosts returns the hosts a run from here would use, in the order it
// tries them: this machine in local mode, the project's hosts list (or host)
// when .rr.yaml names one, otherwise every global host alphabetically, which
// is also how a run orders them.
func statusHosts(resolved *config.ResolvedConfig) ([]string, map[string]config.Host, string, error) {
	if configLocalMode(resolved) {
		// Runs go to this machine without trying any host: through the
		// global local host when there is one, as resolveExecTarget does.
		name, h := config.LocalHost(resolved)
		if name == "" {
			name, h = host.LocalAlias, config.Host{Local: true}
		}
		return []string{name}, map[string]config.Host{name: h}, statusScopeLocal, nil
	}

	if resolved.Project != nil && (len(resolved.Project.Hosts) > 0 || resolved.Project.Host != "") {
		order, hosts, err := config.ResolveHosts(resolved, "")
		if err != nil {
			return nil, nil, "", err
		}
		return order, hosts, statusScopeProject, nil
	}

	order := make([]string, 0, len(resolved.Global.Hosts))
	for name := range resolved.Global.Hosts {
		order = append(order, name)
	}
	sort.Strings(order)
	return order, resolved.Global.Hosts, statusScopeGlobal, nil
}

// probeResult holds the result of probing a single host.
type probeResult struct {
	HostName string
	Aliases  []host.ProbeResult
}

// probeAllHosts probes the hosts in order, in parallel, and returns their
// results in that order.
func probeAllHosts(order []string, hosts map[string]config.Host) []probeResult {
	results := make([]probeResult, len(order))
	var wg gosync.WaitGroup

	timeout := host.DefaultProbeTimeout

	for i, name := range order {
		wg.Add(1)
		go func(i int, hostName string, hostCfg config.Host) {
			defer wg.Done()

			var aliasResults []host.ProbeResult
			if hostCfg.Local {
				// Nothing to dial: a local host is this machine.
				aliasResults = []host.ProbeResult{{SSHAlias: host.LocalAlias, Success: true}}
			} else {
				aliasResults = host.ProbeAll(hostCfg.SSH, timeout)
			}

			results[i] = probeResult{
				HostName: hostName,
				Aliases:  aliasResults,
			}
		}(i, name, hosts[name])
	}

	wg.Wait()
	return results
}

// findSelectedHost returns the first reachable host in results' order, with
// its first reachable alias, or nil when none is reachable.
func findSelectedHost(results []probeResult) *Selected {
	for _, result := range results {
		for _, alias := range result.Aliases {
			if alias.Success {
				return &Selected{Host: result.HostName, Alias: alias.SSHAlias}
			}
		}
	}
	return nil
}

// outputStatusJSON outputs status in JSON format.
// When MachineMode() is enabled, wraps output in the standard JSON envelope.
func outputStatusJSON(results []probeResult, scope string, selected *Selected, mapping *ProjectMapping) error {
	output := StatusOutput{
		Hosts:    make([]HostStatus, 0, len(results)),
		Scope:    scope,
		Selected: selected,
		Project:  mapping,
	}

	for _, result := range results {
		hostStatus := HostStatus{
			Name:    result.HostName,
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
	if MachineMode() {
		return WriteJSONSuccess(os.Stdout, output)
	}

	// Legacy --json behavior (no envelope)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputStatusText outputs status in human-readable format using a table.
func outputStatusText(results []probeResult, scope string, selected *Selected, mapping *ProjectMapping) error {
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)

	// Build table rows
	var rows []ui.StatusTableRow
	for _, result := range results {
		for _, alias := range result.Aliases {
			row := ui.StatusTableRow{
				Host:  result.HostName,
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
	switch scope {
	case statusScopeProject:
		fmt.Println(mutedStyle.Render("Hosts from .rr.yaml, in the order runs try them. A run skips a locked host."))
	case statusScopeLocal:
		fmt.Println(mutedStyle.Render("Runs use this machine: local_fallback is on and there are no hosts to try."))
	default:
		fmt.Println(mutedStyle.Render("All hosts in ~/.rr/config.yaml, alphabetically. Set 'hosts:' in .rr.yaml to pick a project's hosts and their order."))
	}

	// Show where this tree syncs (worktree-aware)
	if mapping != nil && (len(mapping.RemoteDirs) > 0 || len(mapping.InPlaceHosts) > 0) {
		fmt.Println()
		treeDesc := "This tree"
		if mapping.IsLinkedWorktree {
			treeDesc = fmt.Sprintf("This worktree (%s)", mapping.Worktree)
		}
		hostNames := make([]string, 0, len(mapping.RemoteDirs))
		for name := range mapping.RemoteDirs {
			hostNames = append(hostNames, name)
		}
		sort.Strings(hostNames)
		for _, name := range mapping.InPlaceHosts {
			fmt.Printf("%s runs in place on %s\n", treeDesc, name)
		}
		for _, name := range hostNames {
			fmt.Printf("%s syncs to: %s\n", treeDesc, mutedStyle.Render(fmt.Sprintf("%s:%s", name, mapping.RemoteDirs[name])))
		}
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
