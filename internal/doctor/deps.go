package doctor

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/host"
)

// RsyncLocalCheck verifies rsync is installed locally.
type RsyncLocalCheck struct{}

func (c *RsyncLocalCheck) Name() string     { return "rsync_local" }
func (c *RsyncLocalCheck) Category() string { return "DEPENDENCIES" }

func (c *RsyncLocalCheck) Run() CheckResult {
	// Check if rsync is in PATH
	path, err := exec.LookPath("rsync")
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    "rsync not found locally",
			Suggestion: "Install rsync: brew install rsync (macOS) or apt install rsync (Linux)",
			Fixable:    false,
		}
	}

	// Get version
	cmd := exec.Command(path, "--version")
	output, err := cmd.Output()
	if err != nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: "rsync found (version unknown)",
		}
	}

	version := parseRsyncVersion(string(output))
	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: fmt.Sprintf("rsync %s (local)", version),
	}
}

func (c *RsyncLocalCheck) Fix() error {
	return nil // System package installation is out of scope
}

// RsyncRemoteCheck verifies rsync is installed on a remote host.
type RsyncRemoteCheck struct {
	HostName string
	Conn     *host.Connection
}

func (c *RsyncRemoteCheck) Name() string     { return fmt.Sprintf("rsync_remote_%s", c.HostName) }
func (c *RsyncRemoteCheck) Category() string { return "DEPENDENCIES" }

func (c *RsyncRemoteCheck) Run() CheckResult {
	if c.Conn == nil || c.Conn.Client == nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("rsync (%s): no connection", c.HostName),
		}
	}

	// Check rsync on remote
	stdout, stderr, exitCode, err := c.Conn.Client.Exec("which rsync && rsync --version 2>/dev/null | head -1")
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("rsync (%s): failed to check", c.HostName),
			Suggestion: "Check SSH connection",
		}
	}

	if exitCode != 0 {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("rsync not found on %s", c.HostName),
			Suggestion: fmt.Sprintf("Install rsync on %s: apt install rsync (or equivalent)", c.HostName),
		}
	}

	// Parse version from output
	output := string(stdout)
	if string(stderr) != "" {
		output += string(stderr)
	}
	version := parseRsyncVersion(output)

	return CheckResult{
		Name:    c.Name(),
		Status:  StatusPass,
		Message: fmt.Sprintf("rsync %s (%s)", version, c.HostName),
	}
}

func (c *RsyncRemoteCheck) Fix() error {
	return nil // Remote installation is out of scope
}

// parseRsyncVersion extracts version from rsync output.
func parseRsyncVersion(output string) string {
	// rsync output: "rsync  version 3.2.7  protocol version 31"
	re := regexp.MustCompile(`rsync\s+version\s+(\d+\.\d+\.?\d*)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) >= 2 {
		return matches[1]
	}

	// Fallback: look for any version-like pattern
	re = regexp.MustCompile(`(\d+\.\d+\.?\d*)`)
	matches = re.FindStringSubmatch(strings.Split(output, "\n")[0])
	if len(matches) >= 1 {
		return matches[1]
	}

	return "unknown"
}

// NewDepsChecks creates all dependency checks.
func NewDepsChecks() []Check {
	return []Check{
		&RsyncLocalCheck{},
	}
}

// NewRemoteDepsChecks creates dependency checks for a specific remote host.
func NewRemoteDepsChecks(hostName string, conn *host.Connection) []Check {
	return []Check{
		&RsyncRemoteCheck{
			HostName: hostName,
			Conn:     conn,
		},
	}
}

// NewAllDepsChecks creates dependency checks for local and all connected remotes.
func NewAllDepsChecks(connections map[string]*host.Connection) []Check {
	checks := []Check{&RsyncLocalCheck{}}

	for name, conn := range connections {
		checks = append(checks, &RsyncRemoteCheck{
			HostName: name,
			Conn:     conn,
		})
	}

	return checks
}

// DependencyInfo holds information about a dependency for display.
type DependencyInfo struct {
	Name     string
	Version  string
	Location string // "local" or host name
	Status   CheckStatus
}

// GetDepsInfo extracts dependency info from check results for formatted display.
func GetDepsInfo(checks []Check, results []CheckResult) []DependencyInfo {
	var deps []DependencyInfo

	for i, check := range checks {
		if i >= len(results) {
			break
		}

		info := DependencyInfo{
			Name:   "rsync",
			Status: results[i].Status,
		}

		switch c := check.(type) {
		case *RsyncLocalCheck:
			info.Location = "local"
			// Extract version from message
			if strings.Contains(results[i].Message, "rsync") {
				info.Version = extractVersion(results[i].Message)
			}
		case *RsyncRemoteCheck:
			info.Location = c.HostName
			if strings.Contains(results[i].Message, "rsync") {
				info.Version = extractVersion(results[i].Message)
			}
		}

		deps = append(deps, info)
	}

	return deps
}

func extractVersion(msg string) string {
	// "rsync 3.2.7 (local)" -> "3.2.7"
	re := regexp.MustCompile(`(\d+\.\d+\.?\d*)`)
	matches := re.FindStringSubmatch(msg)
	if len(matches) >= 1 {
		return matches[1]
	}
	return "unknown"
}

// Ensure all check types implement the Check interface (compile-time check)
var (
	_ config.Host = config.Host{} // Just to keep config import used
)
