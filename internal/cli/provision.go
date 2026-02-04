package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/require"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"golang.org/x/term"
)

// ProvisionOptions configures the provision command behavior.
type ProvisionOptions struct {
	Host       string // Target specific host (empty = all project hosts)
	CheckOnly  bool   // Report status without installing
	AutoYes    bool   // Skip confirmation prompts
	MachineOut bool   // Output JSON for CI/LLM
}

// ProvisionHostResult contains results for a single host.
type ProvisionHostResult struct {
	Name         string                     `json:"name"`
	OS           string                     `json:"os"`
	Connected    bool                       `json:"connected"`
	Error        string                     `json:"error,omitempty"`
	Requirements []ProvisionRequirementItem `json:"requirements,omitempty"`
}

// ProvisionRequirementItem represents a single requirement check result.
type ProvisionRequirementItem struct {
	Name       string `json:"name"`
	Satisfied  bool   `json:"satisfied"`
	Path       string `json:"path,omitempty"`
	CanInstall bool   `json:"canInstall"`
	Installed  bool   `json:"installed,omitempty"`
}

// ProvisionOutput is the JSON output structure for machine mode.
type ProvisionOutput struct {
	Hosts   []ProvisionHostResult `json:"hosts"`
	Summary ProvisionSummary      `json:"summary"`
}

// ProvisionSummary summarizes the provision operation.
type ProvisionSummary struct {
	TotalHosts     int `json:"totalHosts"`
	ConnectedHosts int `json:"connectedHosts"`
	TotalMissing   int `json:"totalMissing"`
	CanInstall     int `json:"canInstall"`
	Installed      int `json:"installed"`
}

// provisionCommand implements the provision command logic.
func provisionCommand(opts ProvisionOptions) error {
	// Load resolved config (global + project)
	resolved, err := config.LoadResolved(Config())
	if err != nil {
		return err
	}

	// Get hosts to provision
	hostNames, hosts, err := getHostsToProvision(resolved, opts.Host)
	if err != nil {
		return err
	}

	if len(hostNames) == 0 {
		if opts.MachineOut || machineMode {
			return WriteJSONSuccess(os.Stdout, ProvisionOutput{
				Hosts:   []ProvisionHostResult{},
				Summary: ProvisionSummary{},
			})
		}
		fmt.Println("No hosts configured. Run 'rr host add' to add a host.")
		return nil
	}

	// Gather project requirements
	var projectReqs []string
	if resolved.Project != nil {
		projectReqs = resolved.Project.Require
	}

	// Connect to hosts and check requirements
	results := checkHosts(hostNames, hosts, projectReqs, opts)

	// Output results
	if opts.MachineOut || machineMode {
		return outputProvisionJSON(results)
	}

	return outputProvisionText(results, opts)
}

// getHostsToProvision determines which hosts to provision.
func getHostsToProvision(resolved *config.ResolvedConfig, preferred string) ([]string, map[string]config.Host, error) {
	if resolved.Global == nil || len(resolved.Global.Hosts) == 0 {
		return nil, nil, nil
	}

	// If specific host requested, use only that
	if preferred != "" {
		host, ok := resolved.Global.Hosts[preferred]
		if !ok {
			var available []string
			for name := range resolved.Global.Hosts {
				available = append(available, name)
			}
			return nil, nil, errors.New(errors.ErrConfig,
				"Host '"+preferred+"' not found",
				"Available hosts: "+strings.Join(available, ", "))
		}
		return []string{preferred}, map[string]config.Host{preferred: host}, nil
	}

	// Otherwise, use hosts referenced by project config, or all hosts
	names, hosts, err := config.ResolveHosts(resolved, "")
	if err != nil {
		// If local fallback with no hosts, just use all global hosts for provision
		if len(resolved.Global.Hosts) > 0 {
			hosts = resolved.Global.Hosts
			for name := range hosts {
				names = append(names, name)
			}
		}
	}

	return names, hosts, nil
}

// hostCheckResult holds the check results for a single host.
type hostCheckResult struct {
	name       string
	os         string
	connected  bool
	connErr    error
	client     sshutil.SSHClient
	results    []require.CheckResult
	reqs       []string
	installed  []string
	installErr map[string]error
}

// checkHosts connects to hosts and checks their requirements.
func checkHosts(hostNames []string, hosts map[string]config.Host, projectReqs []string, opts ProvisionOptions) []hostCheckResult {
	var results []hostCheckResult

	for _, name := range hostNames {
		hostCfg := hosts[name]
		result := hostCheckResult{
			name:       name,
			installErr: make(map[string]error),
		}

		// Try to connect
		var client sshutil.SSHClient
		var connErr error
		for _, sshAlias := range hostCfg.SSH {
			client, connErr = sshutil.Dial(sshAlias, 10*time.Second)
			if connErr == nil {
				break
			}
		}

		if connErr != nil {
			result.connErr = connErr
			results = append(results, result)
			continue
		}
		result.connected = true
		result.client = client

		// Detect OS
		osName, _ := exec.DetectRemoteOS(client)
		result.os = osName

		// Merge requirements (project + host)
		result.reqs = require.Merge(projectReqs, hostCfg.Require)

		if len(result.reqs) == 0 {
			results = append(results, result)
			continue
		}

		// Check requirements
		cache := require.NewCache()
		checkResults, _ := require.CheckAll(client, result.reqs, cache, name)
		result.results = checkResults

		results = append(results, result)
	}

	// Close connections for check-only mode
	if opts.CheckOnly {
		for i := range results {
			if results[i].client != nil {
				results[i].client.Close()
			}
		}
		return results
	}

	// Handle installations
	results = handleInstallations(results, opts)

	// Close remaining connections
	for i := range results {
		if results[i].client != nil {
			results[i].client.Close()
		}
	}

	return results
}

// installCandidate tracks a tool that needs installation on a specific host.
type installCandidate struct {
	hostIdx  int
	toolName string
}

// handleInstallations prompts for and performs installations.
func handleInstallations(results []hostCheckResult, opts ProvisionOptions) []hostCheckResult {
	// Collect missing tools that can be installed
	var candidates []installCandidate

	for i := range results {
		if !results[i].connected {
			continue
		}
		missing := require.FilterMissing(results[i].results)
		for _, m := range missing {
			if m.CanInstall {
				candidates = append(candidates, installCandidate{hostIdx: i, toolName: m.Name})
			}
		}
	}

	if len(candidates) == 0 {
		return results
	}

	// Confirm installation
	if !opts.AutoYes && term.IsTerminal(int(os.Stdin.Fd())) {
		var proceed bool
		toolList := formatInstallCandidates(results, candidates)
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Install %d missing tool(s)?\n%s", len(candidates), toolList)).
					Value(&proceed),
			),
		)
		if err := form.Run(); err != nil || !proceed {
			return results
		}
	}

	// Perform installations
	fmt.Println()
	for _, c := range candidates {
		r := &results[c.hostIdx]
		if r.client == nil {
			continue
		}

		fmt.Printf("Installing %s on %s...\n", c.toolName, r.name)

		// Get install command description for display
		installDesc := exec.GetInstallCommandDescription(c.toolName, r.os)
		if installDesc != "" {
			mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
			fmt.Printf("  %s\n", mutedStyle.Render(installDesc))
		}

		result, err := exec.InstallTool(r.client, c.toolName, os.Stdout, os.Stderr)
		if err != nil {
			r.installErr[c.toolName] = err
			fmt.Printf("%s Failed: %v\n\n", ui.SymbolFail, err)
			continue
		}
		if !result.Success {
			r.installErr[c.toolName] = fmt.Errorf("%s", result.Output)
			fmt.Printf("%s Failed: %s\n\n", ui.SymbolFail, result.Output)
			continue
		}

		r.installed = append(r.installed, c.toolName)
		fmt.Printf("%s Installed %s\n\n", ui.SymbolSuccess, c.toolName)
	}

	return results
}

// formatInstallCandidates creates a summary of tools to install.
func formatInstallCandidates(results []hostCheckResult, candidates []installCandidate) string {
	byHost := make(map[string][]string)
	for _, c := range candidates {
		host := results[c.hostIdx].name
		byHost[host] = append(byHost[host], c.toolName)
	}

	// Sort hosts for deterministic output
	var hosts []string
	for host := range byHost {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	var lines []string
	for _, host := range hosts {
		lines = append(lines, fmt.Sprintf("  %s: %s", host, strings.Join(byHost[host], ", ")))
	}
	return strings.Join(lines, "\n")
}

// outputProvisionJSON outputs results in JSON format.
func outputProvisionJSON(results []hostCheckResult) error {
	output := ProvisionOutput{
		Hosts: make([]ProvisionHostResult, 0, len(results)),
	}

	for i := range results {
		r := &results[i]
		hostResult := ProvisionHostResult{
			Name:      r.name,
			OS:        r.os,
			Connected: r.connected,
		}

		if r.connErr != nil {
			hostResult.Error = r.connErr.Error()
		}

		for _, req := range r.results {
			item := ProvisionRequirementItem{
				Name:       req.Name,
				Satisfied:  req.Satisfied,
				Path:       req.Path,
				CanInstall: req.CanInstall,
			}
			// Check if it was installed this run
			for _, installed := range r.installed {
				if installed == req.Name {
					item.Installed = true
					break
				}
			}
			hostResult.Requirements = append(hostResult.Requirements, item)
		}

		output.Hosts = append(output.Hosts, hostResult)

		// Update summary
		output.Summary.TotalHosts++
		if r.connected {
			output.Summary.ConnectedHosts++
		}
	}

	// Count missing and installable across all hosts
	for i := range results {
		r := &results[i]
		for _, req := range r.results {
			if !req.Satisfied {
				output.Summary.TotalMissing++
				if req.CanInstall {
					output.Summary.CanInstall++
				}
			}
		}
		output.Summary.Installed += len(r.installed)
	}

	if machineMode {
		return WriteJSONSuccess(os.Stdout, output)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputProvisionText outputs human-readable results.
func outputProvisionText(results []hostCheckResult, opts ProvisionOptions) error {
	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	headerStyle := lipgloss.NewStyle().Bold(true)

	fmt.Println()

	totalMissing := 0
	canInstall := 0
	totalInstalled := 0
	manualRequired := 0

	for i := range results {
		r := &results[i]
		// Host header
		osInfo := ""
		if r.os != "" {
			osInfo = fmt.Sprintf(" (%s)", r.os)
		}
		fmt.Printf("%s%s\n", headerStyle.Render(r.name), mutedStyle.Render(osInfo))

		if !r.connected {
			fmt.Printf("  %s Could not connect: %v\n", errorStyle.Render(ui.SymbolFail), r.connErr)
			fmt.Println()
			continue
		}

		if len(r.reqs) == 0 {
			fmt.Printf("  %s No requirements configured\n", mutedStyle.Render("-"))
			fmt.Println()
			continue
		}

		for _, req := range r.results {
			if req.Satisfied {
				fmt.Printf("  %s %s\n", successStyle.Render(ui.SymbolComplete), req.Name)
			} else {
				totalMissing++
				if req.CanInstall {
					canInstall++
					// Check if it was installed
					installed := false
					for _, name := range r.installed {
						if name == req.Name {
							installed = true
							totalInstalled++
							break
						}
					}
					if installed {
						fmt.Printf("  %s %s %s\n",
							successStyle.Render(ui.SymbolSuccess),
							req.Name,
							mutedStyle.Render("(installed)"))
					} else if installErr, hasErr := r.installErr[req.Name]; hasErr {
						fmt.Printf("  %s %s %s\n",
							errorStyle.Render(ui.SymbolFail),
							req.Name,
							mutedStyle.Render(fmt.Sprintf("(install failed: %v)", installErr)))
					} else {
						fmt.Printf("  %s %s %s\n",
							warnStyle.Render(ui.SymbolFail),
							req.Name,
							mutedStyle.Render("(can install)"))
					}
				} else {
					manualRequired++
					fmt.Printf("  %s %s %s\n",
						errorStyle.Render(ui.SymbolFail),
						req.Name,
						mutedStyle.Render("(manual install required)"))
				}
			}
		}
		fmt.Println()
	}

	// Summary
	fmt.Println(strings.Repeat("\u2501", 50))

	if totalMissing == 0 {
		fmt.Printf("%s All requirements satisfied\n", successStyle.Render(ui.SymbolSuccess))
	} else if opts.CheckOnly {
		fmt.Printf("%d missing (%d can auto-install, %d manual)\n",
			totalMissing, canInstall, manualRequired)
		if canInstall > 0 {
			fmt.Printf("\nRun %s to install available tools.\n",
				mutedStyle.Render("rr provision"))
		}
	} else {
		if totalInstalled > 0 {
			fmt.Printf("%s %d tool(s) installed\n", successStyle.Render(ui.SymbolSuccess), totalInstalled)
		}
		stillMissing := totalMissing - totalInstalled
		if stillMissing > 0 {
			fmt.Printf("%s %d tool(s) still missing (%d require manual installation)\n",
				warnStyle.Render(ui.SymbolFail), stillMissing, manualRequired)
		}
	}

	fmt.Println()
	return nil
}
