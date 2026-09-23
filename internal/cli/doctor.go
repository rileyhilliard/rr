package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/doctor"
	rrerrors "github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/rileyhilliard/rr/pkg/sshutil"
	"github.com/spf13/cobra"
)

var (
	doctorJSON         bool
	doctorFix          bool
	doctorPath         bool
	doctorRequirements bool
)

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output in JSON format")
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "attempt automatic fixes where possible")
	doctorCmd.Flags().BoolVar(&doctorPath, "path", false, "check PATH differences between login and interactive shells")
	doctorCmd.Flags().BoolVar(&doctorRequirements, "requirements", false, "check that required tools are available on remote hosts")
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

// doctorDial dials a single SSH alias. Tests replace it to avoid real SSH.
var doctorDial host.DialFunc = host.ProbeAndConnect

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

	// --path and --requirements need live connections to the hosts in scope
	if doctorPath || doctorRequirements {
		// A resolution error is already reported by collectChecks
		hostNames, hosts, _ := doctor.ScopeHosts(projectCfg, globalCfg)
		conns, dialErrs := connectDoctorHosts(hostNames, hosts, doctorDial)
		defer closeDoctorConnections(conns)

		attachRemoteErrors(checks, dialErrs)
		checks = append(checks, remoteDoctorChecks(hostNames, hosts, conns, projectCfg, doctorPath, doctorRequirements)...)
	}

	fallback := config.ResolveLocalFallbackMode(&config.ResolvedConfig{Global: globalCfg, Project: projectCfg})

	var results []doctor.CheckResult
	if doctorJSON || MachineMode() {
		// Machine mode implies JSON output - run checks without progress display
		results = runDoctorChecks(checks, fallback)
	} else {
		// Run checks with progressive output (shows spinner per category)
		results = runChecksWithProgress(checks, fallback)
	}

	// Try to fix issues if requested
	if doctorFix {
		results = attemptFixes(checks, results)
		doctor.GradeHostResults(checks, results, fallback)
	}

	return reportDoctorResults(checks, results)
}

// runDoctorChecks runs every check, then regrades unreachable hosts by
// whether a run would actually fail (see doctor.GradeHostResults).
func runDoctorChecks(checks []doctor.Check, fallback config.LocalFallbackMode) []doctor.CheckResult {
	results := doctor.RunAll(checks)
	doctor.GradeHostResults(checks, results, fallback)
	return results
}

// reportDoctorResults writes the final output and returns the exit status:
// an ExitError(1) when any check failed, nil when there are only warnings.
// In machine mode the envelope still says success:true, because doctor
// itself ran; the verdict is data.summary.all_clear plus the exit code.
func reportDoctorResults(checks []doctor.Check, results []doctor.CheckResult) error {
	if doctorJSON || MachineMode() {
		if err := outputDoctorJSON(checks, results); err != nil {
			return err
		}
	} else {
		outputDoctorTextResults(checks, results)
	}

	if doctor.HasFailures(results) {
		return rrerrors.NewExitError(1)
	}
	return nil
}

// runChecksWithProgress runs checks with spinner feedback, showing progress by category.
// Unreachable hosts are regraded (doctor.GradeHostResults) before the HOSTS
// category is rendered.
func runChecksWithProgress(checks []doctor.Check, fallback config.LocalFallbackMode) []doctor.CheckResult {
	results := make([]doctor.CheckResult, len(checks))

	// Group checks by category while preserving order
	categoryOrder := []string{"CONFIG", "SSH", "HOSTS", "DEPENDENCIES", "PATH", "REMOTE", "REQUIREMENTS"}
	grouped := make(map[string][]int) // category -> indices

	for i, check := range checks {
		cat := check.Category()
		grouped[cat] = append(grouped[cat], i)
	}

	fmt.Println()
	headerStyle := lipgloss.NewStyle().Bold(true)
	fmt.Println(headerStyle.Render("Road Runner Diagnostic Report"))
	fmt.Println()

	// Run each category with a spinner
	for _, category := range categoryOrder {
		indices, ok := grouped[category]
		if !ok || len(indices) == 0 {
			continue
		}

		// Create spinner for this category
		spinner := ui.NewSpinner(fmt.Sprintf("Checking %s", strings.ToLower(category)))
		spinner.Start()

		// Run all checks in this category
		for _, idx := range indices {
			results[idx] = checks[idx].Run()
		}

		if category == "HOSTS" {
			doctor.GradeHostResults(checks, results, fallback)
		}

		spinner.Stop()
		// Clear spinner line
		fmt.Print("\r\033[K")

		// Render the category results immediately
		renderCategoryResults(category, checks, results, indices)
	}

	return results
}

// renderCategoryResults renders results for a single category.
func renderCategoryResults(category string, checks []doctor.Check, results []doctor.CheckResult, indices []int) {
	successStyle := lipgloss.NewStyle().Foreground(ui.ColorSuccess)
	errorStyle := lipgloss.NewStyle().Foreground(ui.ColorError)
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	headerStyle := lipgloss.NewStyle().Bold(true)

	fmt.Println(headerStyle.Render(category))

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

// connectDoctorHosts connects to each host the way rr run does: every SSH
// alias is raced, with earlier aliases preferred. Hosts are dialed in
// parallel. Hosts that can't be reached come back in the error map, keyed by
// host name; attachRemoteErrors reports them on the host's HOSTS result.
func connectDoctorHosts(hostNames []string, hosts map[string]config.Host, dial host.DialFunc) (map[string]*host.Connection, map[string]error) {
	conns := make([]*host.Connection, len(hostNames))
	errs := make([]error, len(hostNames))

	var wg sync.WaitGroup
	for i, name := range hostNames {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			hostCfg := hosts[name]
			result, err := host.DialAliases(name, hostCfg.SSH, host.DialOptions{Dial: dial})
			if err != nil {
				errs[i] = err
				return
			}
			conns[i] = &host.Connection{
				Name:    name,
				Alias:   result.Alias,
				Client:  result.Client,
				Host:    hostCfg,
				Latency: result.Latency,
			}
		}(i, name)
	}
	wg.Wait()

	connected := make(map[string]*host.Connection)
	dialErrs := make(map[string]error)
	for i, name := range hostNames {
		if errs[i] != nil {
			dialErrs[name] = errs[i]
			continue
		}
		connected[name] = conns[i]
	}
	return connected, dialErrs
}

// attachRemoteErrors records each host's connection failure on its HOSTS
// check, so an unreachable host is reported once, with a note that its
// remote checks were skipped.
func attachRemoteErrors(checks []doctor.Check, dialErrs map[string]error) {
	for _, c := range checks {
		if hc, ok := c.(*doctor.HostConnectivityCheck); ok {
			if err, failed := dialErrs[hc.HostName]; failed {
				hc.RemoteErr = err
			}
		}
	}
}

// remoteDoctorChecks builds the --path and --requirements checks for each
// connected host, in host priority order. Unreachable hosts are skipped here;
// their HOSTS result reports them (attachRemoteErrors).
func remoteDoctorChecks(hostNames []string, hosts map[string]config.Host, conns map[string]*host.Connection, projectCfg *config.Config, path, requirements bool) []doctor.Check {
	var checks []doctor.Check
	for _, name := range hostNames {
		conn := conns[name]
		if conn == nil {
			continue
		}
		if path {
			checks = append(checks, &doctor.PathCheck{HostName: name, Client: conn.Client})
		}
		if requirements {
			checks = append(checks, doctor.NewRequirementsCheck(name, hosts[name], conn, projectCfg))
			checks = append(checks, doctor.NewRemoteDepsChecks(name, conn)...)
		}
	}
	return checks
}

// closeDoctorConnections closes all SSH connections.
func closeDoctorConnections(conns map[string]*host.Connection) {
	for _, conn := range conns {
		_ = conn.Close()
	}
}

// collectChecks gathers all diagnostic checks based on available config.
// Host checks cover only the hosts a run would use (see doctor.ScopeHosts).
func collectChecks(cfgPath string, projectCfg *config.Config, globalCfg *config.GlobalConfig) []doctor.Check {
	var checks []doctor.Check

	// Config checks (always run)
	checks = append(checks, doctor.NewConfigChecks(cfgPath)...)

	hostNames, hosts, err := doctor.ScopeHosts(projectCfg, globalCfg)
	if err != nil {
		checks = append(checks, &doctor.HostResolutionCheck{Err: err})
	}

	// SSH checks (always run)
	checks = append(checks, doctor.NewSSHChecks()...)

	// Host connectivity checks for the hosts in scope
	if len(hostNames) > 0 {
		checks = append(checks, doctor.NewHostsChecks(hosts)...)

		// Which remote dir does this tree sync to? (worktree-aware)
		checks = append(checks, &doctor.WorktreeMappingCheck{Hosts: hosts})
	}

	// Dependency checks (local; remote rsync runs with --requirements)
	checks = append(checks, doctor.NewDepsChecks()...)

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
// When MachineMode() is enabled, wraps output in the standard JSON envelope.
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

	// Use envelope wrapper in machine mode
	if MachineMode() {
		return WriteJSONSuccess(os.Stdout, output)
	}

	// Legacy --json behavior (no envelope)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputDoctorTextResults outputs just the summary after progressive category rendering.
func outputDoctorTextResults(_ []doctor.Check, results []doctor.CheckResult) {
	// Render summary divider
	fmt.Println(strings.Repeat("\u2501", 60))
	fmt.Println()

	printDoctorSummary(results)
}

// printDoctorSummary prints the verdict line. The failure symbol is used
// only when a check failed, matching the exit code (reportDoctorResults):
// a warnings-only run exits 0 and gets the warning symbol.
func printDoctorSummary(results []doctor.CheckResult) {
	symbol := lipgloss.NewStyle().Foreground(ui.ColorSuccess).Render(ui.SymbolSuccess)
	switch {
	case doctor.HasFailures(results):
		symbol = lipgloss.NewStyle().Foreground(ui.ColorError).Render(ui.SymbolFail)
	case doctor.HasIssues(results):
		symbol = lipgloss.NewStyle().Foreground(ui.ColorWarning).Render(ui.SymbolWarning)
	}
	fmt.Printf("%s %s\n", symbol, doctor.Summary(results))

	if doctor.HasIssues(results) && doctor.FixableCount(results) > 0 && !doctorFix {
		fmt.Println()
		fmt.Printf("  Run with %s to attempt automatic fixes where possible.\n",
			lipgloss.NewStyle().Foreground(ui.ColorMuted).Render("--fix"))
	}

	fmt.Println()
}

// outputDoctorText outputs results in human-readable format (non-progressive, used by tests).
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
	categoryOrder := []string{"CONFIG", "SSH", "HOSTS", "DEPENDENCIES", "PATH", "REMOTE", "REQUIREMENTS"}
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

	printDoctorSummary(results)
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
	warnStyle := lipgloss.NewStyle().Foreground(ui.ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)

	for _, idx := range indices {
		check, ok := checks[idx].(*doctor.HostConnectivityCheck)
		if !ok {
			continue
		}

		result := results[idx]

		connected := false
		for _, aliasResult := range check.Results {
			if aliasResult.Success {
				connected = true
				break
			}
		}

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
			if !connected || check.RemoteErr != nil {
				style = warnStyle // Unreachable but regraded, or remote checks skipped
			}
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

		// Host-level details the alias lines don't cover: why an unreachable
		// host was graded as it was, and skipped remote checks.
		if check.RemoteErr != nil && connected {
			fmt.Printf("    %s\n", mutedStyle.Render(result.Message))
		}
		if result.Status != doctor.StatusPass && result.Suggestion != "" && (!connected || check.RemoteErr != nil) {
			fmt.Println()
			for _, line := range strings.Split(result.Suggestion, "\n") {
				fmt.Printf("    %s\n", mutedStyle.Render(line))
			}
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

	// For host key errors, try to extract more detail
	if probeErr.Reason == host.ProbeFailHostKey {
		var hostKeyErr *sshutil.HostKeyMismatchError
		if errors.As(probeErr.Cause, &hostKeyErr) {
			// Show which key type was expected vs received
			return fmt.Sprintf("Host key mismatch (got %s, expected different type)", hostKeyErr.ReceivedType)
		}
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
	case host.ProbeFailDNS:
		return fmt.Sprintf("Hostname '%s' not found. Check spelling or SSH config:\n  grep -A3 'Host %s' ~/.ssh/config", hostPart, alias)
	case host.ProbeFailRefused:
		return fmt.Sprintf("SSH server may not be running. Try: ssh %s", alias)
	case host.ProbeFailTimeout:
		return fmt.Sprintf("Host may be offline or blocked by firewall. Try: ping %s", hostPart)
	case host.ProbeFailConnReset:
		return fmt.Sprintf("Connection was reset (often a firewall). Try: ssh -v %s", alias)
	case host.ProbeFailUnreachable:
		return fmt.Sprintf("Check network connectivity: ping %s", hostPart)
	case host.ProbeFailAuth:
		return "Check SSH key configuration: ssh-add -l\nOr add key: ssh-add ~/.ssh/id_ed25519"
	case host.ProbeFailHostKey:
		// Check if we have a detailed HostKeyMismatchError with specific suggestion
		var hostKeyErr *sshutil.HostKeyMismatchError
		if errors.As(probeErr.Cause, &hostKeyErr) {
			return hostKeyErr.Suggestion()
		}
		return fmt.Sprintf("Update known_hosts: ssh -o StrictHostKeyChecking=accept-new %s exit", alias)
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
