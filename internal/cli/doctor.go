package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/doctor"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/spf13/cobra"
)

var (
	doctorJSON bool
	doctorFix  bool
	doctorPath bool
)

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output in JSON format")
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "attempt automatic fixes where possible")
	doctorCmd.Flags().BoolVar(&doctorPath, "path", false, "check PATH differences between login and interactive shells")
}

// DoctorOutput represents the JSON output for doctor command.
type DoctorOutput struct {
	Categories []CategoryOutput `json:"categories"`
	Summary    SummaryOutput    `json:"summary"`
}

// CategoryOutput represents a category of check results.
type CategoryOutput struct {
	Name    string               `json:"name"`
	Results []doctor.CheckResult `json:"results"`
}

// SummaryOutput summarizes the check results.
type SummaryOutput struct {
	Pass     int  `json:"pass"`
	Warn     int  `json:"warn"`
	Fail     int  `json:"fail"`
	Fixable  int  `json:"fixable"`
	AllClear bool `json:"all_clear"`
}

// doctorCommand implements the doctor command logic.
func doctorCommand() error {
	// Load project config (if it exists)
	cfgPath, err := config.Find(Config())
	var projectCfg *config.Config

	if err == nil && cfgPath != "" {
		projectCfg, _ = config.Load(cfgPath) // Ignore load errors, config checks will catch them
	}

	// Load global config for hosts
	globalCfg, _ := config.LoadGlobal() // Ignore errors, config checks will catch them

	// Collect all checks
	checks := collectChecks(cfgPath, projectCfg, globalCfg)

	// If --path flag, establish connections and add PATH checks
	var pathClients map[string]sshutil.SSHClient
	if doctorPath && globalCfg != nil && len(globalCfg.Hosts) > 0 {
		pathClients = establishPathConnections(globalCfg)
		defer closePathConnections(pathClients)

		if len(pathClients) > 0 {
			checks = append(checks, doctor.NewPathChecks(pathClients)...)
		}
	}

	// Run checks
	results := doctor.RunAll(checks)

	// Try to fix issues if requested
	if doctorFix {
		results = attemptFixes(checks, results)
	}

	if doctorJSON {
		return outputDoctorJSON(checks, results)
	}

	return outputDoctorText(checks, results)
}

// establishPathConnections connects to hosts for PATH checking.
func establishPathConnections(globalCfg *config.GlobalConfig) map[string]sshutil.SSHClient {
	clients := make(map[string]sshutil.SSHClient)

	for name, hostCfg := range globalCfg.Hosts {
		if len(hostCfg.SSH) == 0 {
			continue
		}

		// Try first SSH alias
		client, err := sshutil.Dial(hostCfg.SSH[0], 10*time.Second)
		if err == nil {
			clients[name] = client
		}
	}

	return clients
}

// closePathConnections closes all SSH clients.
func closePathConnections(clients map[string]sshutil.SSHClient) {
	for _, client := range clients {
		client.Close()
	}
}

// collectChecks gathers all diagnostic checks based on available config.
func collectChecks(cfgPath string, _ *config.Config, globalCfg *config.GlobalConfig) []doctor.Check {
	var checks []doctor.Check

	// Config checks (always run)
	checks = append(checks, doctor.NewConfigChecks(cfgPath)...)

	// SSH checks (always run)
	checks = append(checks, doctor.NewSSHChecks()...)

	// Host connectivity checks (if global config with hosts exists)
	if globalCfg != nil && len(globalCfg.Hosts) > 0 {
		checks = append(checks, doctor.NewHostsChecks(globalCfg.Hosts)...)
	}

	// Dependency checks (local always, remote if connected)
	checks = append(checks, doctor.NewDepsChecks()...)

	// Note: Remote checks would require establishing connections
	// They're run separately after host connectivity is verified

	return checks
}

// attemptFixes tries to fix issues where possible.
func attemptFixes(checks []doctor.Check, results []doctor.CheckResult) []doctor.CheckResult {
	for i, result := range results {
		if result.Fixable && (result.Status == doctor.StatusFail || result.Status == doctor.StatusWarn) {
			if err := checks[i].Fix(); err == nil {
				// Re-run the check to see if it's fixed
				results[i] = checks[i].Run()
			}
		}
	}
	return results
}

// outputDoctorJSON outputs results in JSON format.
func outputDoctorJSON(checks []doctor.Check, results []doctor.CheckResult) error {
	// Group by category
	grouped := make(map[string][]doctor.CheckResult)
	categoryOrder := []string{}

	for i, check := range checks {
		cat := check.Category()
		if _, exists := grouped[cat]; !exists {
			categoryOrder = append(categoryOrder, cat)
		}
		grouped[cat] = append(grouped[cat], results[i])
	}

	// Build output
	output := DoctorOutput{
		Categories: make([]CategoryOutput, 0, len(categoryOrder)),
	}

	for _, cat := range categoryOrder {
		output.Categories = append(output.Categories, CategoryOutput{
			Name:    cat,
			Results: grouped[cat],
		})
	}

	// Summary
	counts := doctor.CountByStatus(results)
	output.Summary = SummaryOutput{
		Pass:     counts[doctor.StatusPass],
		Warn:     counts[doctor.StatusWarn],
		Fail:     counts[doctor.StatusFail],
		Fixable:  doctor.FixableCount(results),
		AllClear: !doctor.HasIssues(results),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputDoctorText outputs results in human-readable format.
//
//nolint:unparam // error return reserved for future use
func outputDoctorText(checks []doctor.Check, results []doctor.CheckResult) error {
	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	headerStyle := lipgloss.NewStyle().Bold(true)

	fmt.Println()
	fmt.Println(headerStyle.Render("Road Runner Diagnostic Report"))
	fmt.Println()

	// Group checks by category
	categoryOrder := []string{"CONFIG", "SSH", "HOSTS", "DEPENDENCIES", "PATH", "REMOTE"}
	grouped := make(map[string][]int) // category -> indices

	for i, check := range checks {
		cat := check.Category()
		grouped[cat] = append(grouped[cat], i)
	}

	// Render each category
	for _, category := range categoryOrder {
		indices, ok := grouped[category]
		if !ok || len(indices) == 0 {
			continue
		}

		fmt.Println(headerStyle.Render(category))

		// Special handling for HOSTS category to show nested alias results
		switch category {
		case "HOSTS":
			renderHostsCategory(checks, results, indices)
		case "DEPENDENCIES":
			renderDepsCategory(checks, results, indices)
		default:
			for _, idx := range indices {
				result := results[idx]
				renderCheckResult(result, successStyle, errorStyle, warnStyle, mutedStyle)
			}
		}

		fmt.Println()
	}

	// Render summary divider
	fmt.Println(strings.Repeat("\u2501", 60))
	fmt.Println()

	// Summary
	counts := doctor.CountByStatus(results)
	if !doctor.HasIssues(results) {
		fmt.Printf("%s %s\n", successStyle.Render(ui.SymbolSuccess), "Everything looks good")
	} else {
		total := counts[doctor.StatusFail] + counts[doctor.StatusWarn]
		fmt.Printf("%s %d issue%s found\n",
			errorStyle.Render(ui.SymbolFail),
			total,
			pluralSuffix(total),
		)

		fixable := doctor.FixableCount(results)
		if fixable > 0 && !doctorFix {
			fmt.Println()
			fmt.Printf("  Run with %s to attempt automatic fixes where possible.\n",
				mutedStyle.Render("--fix"))
		}
	}

	fmt.Println()
	return nil
}

// renderCheckResult renders a single check result.
func renderCheckResult(result doctor.CheckResult, successStyle, errorStyle, warnStyle, mutedStyle lipgloss.Style) {
	var symbol string
	var style lipgloss.Style

	switch result.Status {
	case doctor.StatusPass:
		symbol = ui.SymbolComplete
		style = successStyle
	case doctor.StatusWarn:
		symbol = ui.SymbolComplete // Still shows as done, but with warning styling
		style = warnStyle
	case doctor.StatusFail:
		symbol = ui.SymbolFail
		style = errorStyle
	}

	fmt.Printf("  %s %s\n", style.Render(symbol), result.Message)

	if result.Suggestion != "" && result.Status != doctor.StatusPass {
		// Indent suggestion
		lines := strings.Split(result.Suggestion, "\n")
		for _, line := range lines {
			fmt.Printf("    %s\n", mutedStyle.Render(line))
		}
	}
}

// renderHostsCategory renders the HOSTS section with nested alias details.
func renderHostsCategory(checks []doctor.Check, results []doctor.CheckResult, indices []int) {
	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	for _, idx := range indices {
		check, ok := checks[idx].(*doctor.HostConnectivityCheck)
		if !ok {
			continue
		}

		result := results[idx]

		// Host header
		var symbol string
		var style lipgloss.Style
		switch result.Status {
		case doctor.StatusPass:
			symbol = ui.SymbolComplete
			style = successStyle
		case doctor.StatusWarn:
			symbol = ui.SymbolComplete
			style = successStyle // Still has some working aliases
		default:
			symbol = ui.SymbolFail
			style = errorStyle
		}

		fmt.Printf("  %s %s\n", style.Render(symbol), check.HostName)

		// Individual alias results
		for _, aliasResult := range check.Results {
			if aliasResult.Success {
				latency := formatLatency(aliasResult.Latency)
				fmt.Printf("    %s %s: Connected %s\n",
					successStyle.Render(ui.SymbolComplete),
					aliasResult.SSHAlias,
					mutedStyle.Render(fmt.Sprintf("(%s)", latency)),
				)
			} else {
				errMsg := formatProbeError(aliasResult.Error)
				fmt.Printf("    %s %s: %s\n",
					errorStyle.Render(ui.SymbolFail),
					aliasResult.SSHAlias,
					errMsg,
				)
				// Show suggestion for this specific error
				suggestion := getSSHErrorSuggestion(aliasResult.Error, aliasResult.SSHAlias)
				for _, line := range strings.Split(suggestion, "\n") {
					fmt.Printf("      %s\n", mutedStyle.Render(line))
				}
			}
		}

		// General suggestion if all aliases failed (only show if not already shown per-alias)
		if result.Status == doctor.StatusFail && result.Suggestion != "" && len(check.Results) == 0 {
			fmt.Printf("\n    %s\n", mutedStyle.Render(result.Suggestion))
		}
	}
}

// renderDepsCategory renders the DEPENDENCIES section.
func renderDepsCategory(_ []doctor.Check, results []doctor.CheckResult, indices []int) {
	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	for _, idx := range indices {
		result := results[idx]

		var symbol string
		var style lipgloss.Style

		switch result.Status {
		case doctor.StatusPass:
			symbol = ui.SymbolComplete
			style = successStyle
		case doctor.StatusFail:
			symbol = ui.SymbolFail
			style = errorStyle
		default:
			symbol = ui.SymbolComplete
			style = successStyle
		}

		fmt.Printf("  %s %s\n", style.Render(symbol), result.Message)

		if result.Suggestion != "" && result.Status != doctor.StatusPass {
			fmt.Printf("    %s\n", mutedStyle.Render(result.Suggestion))
		}
	}
}

// capitalizeFirst capitalizes the first letter of a string.
func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

// pluralSuffix returns "s" if n != 1.
func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// formatProbeError formats a probe error for display.
// For known error types, returns a user-friendly description.
// For unknown errors, returns the actual error message instead of "Unknown error".
func formatProbeError(err error) string {
	if err == nil {
		return "Connection failed"
	}

	probeErr, ok := err.(*host.ProbeError)
	if !ok {
		// Not a ProbeError, just return the error message
		return capitalizeFirst(err.Error())
	}

	// For unknown errors, show the actual cause instead of "unknown error"
	if probeErr.Reason == host.ProbeFailUnknown {
		if probeErr.Cause != nil {
			return capitalizeFirst(probeErr.Cause.Error())
		}
		return "Connection failed"
	}

	// For known error types, return the friendly description
	return capitalizeFirst(probeErr.Reason.String())
}

// getSSHErrorSuggestion returns an actionable suggestion for an SSH error.
func getSSHErrorSuggestion(err error, alias string) string {
	// Extract host from alias (remove user@ prefix if present)
	hostPart := alias
	if idx := strings.Index(alias, "@"); idx != -1 {
		hostPart = alias[idx+1:]
	}

	probeErr, ok := err.(*host.ProbeError)
	if !ok {
		return fmt.Sprintf("Try connecting directly: ssh %s", alias)
	}

	switch probeErr.Reason {
	case host.ProbeFailRefused:
		return fmt.Sprintf("SSH server may not be running. Try: ssh %s", alias)
	case host.ProbeFailTimeout:
		return fmt.Sprintf("Host may be offline or blocked by firewall. Try: ping %s", hostPart)
	case host.ProbeFailUnreachable:
		return fmt.Sprintf("Check network connectivity: ping %s", hostPart)
	case host.ProbeFailAuth:
		return "Check SSH key configuration: ssh-add -l\nOr add key: ssh-add ~/.ssh/id_ed25519"
	case host.ProbeFailHostKey:
		return fmt.Sprintf("Update known_hosts: ssh-keyscan -t rsa,ecdsa,ed25519 %s >> ~/.ssh/known_hosts", hostPart)
	default:
		return fmt.Sprintf("Try connecting directly: ssh %s", alias)
	}
}

// Update the doctorCmd to use doctorCommand
func init() {
	// Override the RunE to use our implementation
	doctorCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return doctorCommand()
	}
}
