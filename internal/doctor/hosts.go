package doctor

import (
	stderrors "errors"
	"fmt"
	"sort"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/host"
)

// HostConnectivityCheck verifies connectivity to a specific host.
type HostConnectivityCheck struct {
	HostName   string
	HostConfig config.Host
	Timeout    time.Duration
	Results    []host.ProbeResult // Populated after Run()

	// Probe probes every alias. Defaults to host.ProbeAll; tests inject a fake.
	Probe func(sshAliases []string, timeout time.Duration) []host.ProbeResult

	// RemoteErr is set when doctor couldn't open a connection for the
	// --path/--requirements checks. Those checks are skipped for this host,
	// and this result says so instead of a second failed check.
	RemoteErr error
}

func (c *HostConnectivityCheck) Name() string     { return fmt.Sprintf("host_%s", c.HostName) }
func (c *HostConnectivityCheck) Category() string { return "HOSTS" }

func (c *HostConnectivityCheck) Run() CheckResult {
	result := c.probe()
	if c.RemoteErr == nil {
		return result
	}

	note := fmt.Sprintf("--path/--requirements checks skipped for %s", c.HostName)
	if result.Status == StatusFail {
		result.Suggestion = appendNote(result.Suggestion, note+" (host unreachable).")
		return result
	}

	// The probe connected but the connection for remote checks didn't, so
	// the host works for runs but its remote checks didn't happen.
	return CheckResult{
		Name:       c.Name(),
		Status:     StatusWarn,
		Message:    fmt.Sprintf("%s: reachable, but connecting for remote checks failed: %s", c.HostName, errorMessage(c.RemoteErr)),
		Suggestion: note + ". Re-run 'rr doctor' to retry.",
	}
}

// probe checks every alias and grades this host on its own.
func (c *HostConnectivityCheck) probe() CheckResult {
	if len(c.HostConfig.SSH) == 0 {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("%s: no SSH aliases configured", c.HostName),
			Suggestion: "Add SSH connection strings to the host configuration",
		}
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = host.DefaultProbeTimeout
	}

	// Probe all aliases
	probe := c.Probe
	if probe == nil {
		probe = host.ProbeAll
	}
	c.Results = probe(c.HostConfig.SSH, timeout)

	// Check if at least one alias works
	var connected []string
	var failed []string

	for _, result := range c.Results {
		if result.Success {
			connected = append(connected, result.SSHAlias)
		} else {
			failed = append(failed, result.SSHAlias)
		}
	}

	if len(connected) == 0 {
		// All aliases failed
		suggestion := fmt.Sprintf("%s may be offline or firewalled", c.HostName)
		if len(c.Results) > 0 && c.Results[0].Error != nil {
			if probeErr, ok := c.Results[0].Error.(*host.ProbeError); ok {
				switch probeErr.Reason {
				case host.ProbeFailRefused:
					suggestion = "SSH server may not be running on the host"
				case host.ProbeFailAuth:
					suggestion = "Check SSH key configuration: ssh-add -l"
				case host.ProbeFailTimeout:
					suggestion = "Host may be offline or blocked by firewall"
				}
			}
		}

		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("%s: all aliases failed", c.HostName),
			Suggestion: suggestion,
		}
	}

	if len(failed) > 0 {
		// Some aliases failed
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusWarn,
			Message:    fmt.Sprintf("%s: %d/%d aliases connected", c.HostName, len(connected), len(c.Results)),
			Suggestion: fmt.Sprintf("Failed: %v", failed),
		}
	}

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: c.HostName,
	}
}

func (c *HostConnectivityCheck) Fix() error {
	return nil // Network issues can't be auto-fixed
}

// GradeHostResults regrades unreachable hosts by whether a run would fail,
// since a run only needs one reachable host. It rewrites results in place:
//   - Some host is reachable: unreachable hosts become warnings, because
//     runs use the reachable ones.
//   - No host is reachable and local fallback is enabled: warnings, because
//     runs fall back to local execution.
//   - No host is reachable and local fallback is off: failures stay.
//
// Call it after the HOSTS checks have run and before results are shown.
func GradeHostResults(checks []Check, results []CheckResult, fallback config.LocalFallbackMode) {
	var hostIdx []int
	reachable := false
	for i, c := range checks {
		if i >= len(results) {
			break
		}
		if _, ok := c.(*HostConnectivityCheck); !ok {
			continue
		}
		hostIdx = append(hostIdx, i)
		if results[i].Status != StatusFail {
			reachable = true
		}
	}

	for _, i := range hostIdx {
		if results[i].Status != StatusFail {
			continue
		}
		switch {
		case reachable:
			results[i].Status = StatusWarn
			results[i].Suggestion = appendNote(results[i].Suggestion,
				"Other hosts are reachable, so runs use the other reachable hosts.")
		case fallback.Enabled():
			results[i].Status = StatusWarn
			results[i].Suggestion = appendNote(results[i].Suggestion,
				fmt.Sprintf("No host is reachable, so runs will fall back to local execution (local_fallback: %s).", fallback))
		}
	}
}

// appendNote adds a line to a suggestion.
func appendNote(suggestion, note string) string {
	if suggestion == "" {
		return note
	}
	return suggestion + "\n" + note
}

// errorMessage returns a structured error's message without its suggestion,
// or the plain error text.
func errorMessage(err error) string {
	var rrErr *errors.Error
	if stderrors.As(err, &rrErr) {
		return rrErr.Message
	}
	return err.Error()
}

// NewHostsChecks creates connectivity checks for all configured hosts.
func NewHostsChecks(hosts map[string]config.Host) []Check {
	checks := make([]Check, 0, len(hosts))
	for name := range hosts {
		checks = append(checks, &HostConnectivityCheck{
			HostName:   name,
			HostConfig: hosts[name],
		})
	}
	return checks
}

// ScopeHosts returns the hosts doctor should check, in priority order.
// Inside a project (projectCfg != nil) that's the hosts a run would use,
// resolved the same way rr run resolves them, so an offline global host the
// project doesn't reference can't fail doctor. Outside a project it's every
// global host. An empty result with a nil error means there's nothing to
// check: no global hosts (ConfigHostsCheck reports that) or local mode.
func ScopeHosts(projectCfg *config.Config, globalCfg *config.GlobalConfig) ([]string, map[string]config.Host, error) {
	if globalCfg == nil || len(globalCfg.Hosts) == 0 {
		return nil, nil, nil
	}

	if projectCfg == nil {
		names := make([]string, 0, len(globalCfg.Hosts))
		for name := range globalCfg.Hosts {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, globalCfg.Hosts, nil
	}

	return config.ResolveHosts(&config.ResolvedConfig{
		Global:  globalCfg,
		Project: projectCfg,
		Source:  config.Both,
	}, "")
}

// HostResolutionCheck reports that the project's host list can't be resolved
// (for example, it names a host missing from the global config). rr run
// fails the same way, so this is a failure.
type HostResolutionCheck struct {
	Err error
}

func (c *HostResolutionCheck) Name() string     { return "project_hosts" }
func (c *HostResolutionCheck) Category() string { return "CONFIG" }

func (c *HostResolutionCheck) Run() CheckResult {
	if c.Err == nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: "Project hosts resolved",
		}
	}

	message := c.Err.Error()
	suggestion := "Check the host/hosts entries in .rr.yaml against ~/.rr/config.yaml"
	var rrErr *errors.Error
	if stderrors.As(c.Err, &rrErr) {
		message = rrErr.Message
		if rrErr.Suggestion != "" {
			suggestion = rrErr.Suggestion
		}
	}

	return CheckResult{
		Name:       c.Name(),
		Status:     StatusFail,
		Message:    message,
		Suggestion: suggestion,
	}
}

func (c *HostResolutionCheck) Fix() error {
	return nil
}

// HostCheckResultDetails holds detailed probe results for display.
type HostCheckResultDetails struct {
	HostName string
	Results  []host.ProbeResult
}

// GetHostCheckDetails extracts detailed results from host checks.
func GetHostCheckDetails(checks []Check) []HostCheckResultDetails {
	var details []HostCheckResultDetails

	for _, check := range checks {
		if hc, ok := check.(*HostConnectivityCheck); ok {
			details = append(details, HostCheckResultDetails{
				HostName: hc.HostName,
				Results:  hc.Results,
			})
		}
	}

	return details
}
