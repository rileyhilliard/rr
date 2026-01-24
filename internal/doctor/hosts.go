package doctor

import (
	"fmt"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
)

// HostConnectivityCheck verifies connectivity to a specific host.
type HostConnectivityCheck struct {
	HostName   string
	HostConfig config.Host
	Timeout    time.Duration
	Results    []host.ProbeResult // Populated after Run()
}

func (c *HostConnectivityCheck) Name() string     { return fmt.Sprintf("host_%s", c.HostName) }
func (c *HostConnectivityCheck) Category() string { return "HOSTS" }

func (c *HostConnectivityCheck) Run() CheckResult {
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
	c.Results = host.ProbeAll(c.HostConfig.SSH, timeout)

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
