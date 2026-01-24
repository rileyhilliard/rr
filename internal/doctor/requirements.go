package doctor

import (
	"fmt"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/internal/host"
	"github.com/rileyhilliard/rr/internal/require"
)

// RequirementsCheck verifies that required tools are available on a remote host.
type RequirementsCheck struct {
	HostName     string
	Conn         *host.Connection
	Requirements []string
}

func (c *RequirementsCheck) Name() string     { return fmt.Sprintf("requirements_%s", c.HostName) }
func (c *RequirementsCheck) Category() string { return "REQUIREMENTS" }

func (c *RequirementsCheck) Run() CheckResult {
	if c.Conn == nil || c.Conn.Client == nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("Requirements (%s): no connection", c.HostName),
		}
	}

	if len(c.Requirements) == 0 {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: fmt.Sprintf("Requirements (%s): none configured", c.HostName),
		}
	}

	// Check each requirement
	cache := require.NewCache() // Use fresh cache for doctor
	results, err := require.CheckAll(c.Conn.Client, c.Requirements, cache, c.HostName)
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("Requirements (%s): check failed: %v", c.HostName, err),
			Suggestion: "Check SSH connection",
		}
	}

	missing := require.FilterMissing(results)
	if len(missing) == 0 {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: fmt.Sprintf("Requirements (%s): all %d satisfied", c.HostName, len(c.Requirements)),
		}
	}

	// Build list of missing tools
	var missingNames []string
	var installable int
	for i := range missing {
		if missing[i].CanInstall {
			missingNames = append(missingNames, missing[i].Name+" (can install)")
			installable++
		} else {
			missingNames = append(missingNames, missing[i].Name)
		}
	}

	suggestion := "Install the missing tools manually"
	if installable > 0 {
		suggestion = fmt.Sprintf("%d of %d can be auto-installed. Run the failing command to trigger installation prompts.",
			installable, len(missing))
	}

	return CheckResult{
		Name:       c.Name(),
		Status:     StatusWarn,
		Message:    fmt.Sprintf("Requirements (%s): %d missing: %s", c.HostName, len(missing), strings.Join(missingNames, ", ")),
		Suggestion: suggestion,
		Fixable:    installable > 0,
	}
}

func (c *RequirementsCheck) Fix() error {
	if c.Conn == nil || c.Conn.Client == nil {
		return fmt.Errorf("no connection")
	}

	// Check requirements and install missing ones that have installers
	cache := require.NewCache()
	results, err := require.CheckAll(c.Conn.Client, c.Requirements, cache, c.HostName)
	if err != nil {
		return err
	}

	missing := require.FilterMissing(results)
	var installErrors []string

	for i := range missing {
		if !missing[i].CanInstall {
			continue
		}

		// Attempt to install
		result, err := exec.InstallTool(c.Conn.Client, missing[i].Name, nil, nil)
		if err != nil {
			installErrors = append(installErrors, fmt.Sprintf("%s: %v", missing[i].Name, err))
		} else if !result.Success {
			installErrors = append(installErrors, fmt.Sprintf("%s: %s", missing[i].Name, result.Output))
		}
	}

	if len(installErrors) > 0 {
		return fmt.Errorf("failed to install: %s", strings.Join(installErrors, "; "))
	}

	return nil
}

// NewRequirementsCheck creates a requirements check for a host.
// If projectCfg is provided, requirements are merged from project and host configs.
// Note: Task-specific requirements are handled separately in the workflow phase.
func NewRequirementsCheck(hostName string, hostCfg config.Host, conn *host.Connection, projectCfg *config.Config) *RequirementsCheck {
	var projectReqs []string
	if projectCfg != nil {
		projectReqs = projectCfg.Require
	}

	// Merge project and host requirements
	reqs := require.Merge(projectReqs, hostCfg.Require)

	return &RequirementsCheck{
		HostName:     hostName,
		Conn:         conn,
		Requirements: reqs,
	}
}

// NewRequirementsChecks creates requirement checks for all connected hosts.
func NewRequirementsChecks(hosts map[string]config.Host, connections map[string]*host.Connection, projectCfg *config.Config) []Check {
	var checks []Check

	for hostName := range hosts {
		conn := connections[hostName]
		check := NewRequirementsCheck(hostName, hosts[hostName], conn, projectCfg)
		if len(check.Requirements) > 0 || conn != nil {
			checks = append(checks, check)
		}
	}

	return checks
}
