package doctor

import (
	"fmt"
	"strings"

	"github.com/rileyhilliard/rr/internal/exec"
	"github.com/rileyhilliard/rr/pkg/sshutil"
)

// PathCheck compares PATH between login and interactive shell modes.
// This helps diagnose issues where commands are available interactively
// but not when rr executes them (using login shell mode).
type PathCheck struct {
	HostName string
	Client   sshutil.SSHClient
}

// Name returns the check identifier.
func (c *PathCheck) Name() string {
	return fmt.Sprintf("path_%s", c.HostName)
}

// Category returns the check category.
func (c *PathCheck) Category() string {
	return "PATH"
}

// Run executes the PATH comparison check.
func (c *PathCheck) Run() CheckResult {
	if c.Client == nil {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("PATH check (%s): no connection", c.HostName),
		}
	}

	diff, err := exec.GetPATHDifference(c.Client)
	if err != nil {
		return CheckResult{
			Name:       c.Name(),
			Status:     StatusFail,
			Message:    fmt.Sprintf("PATH check (%s): failed to compare", c.HostName),
			Suggestion: fmt.Sprintf("Error: %v", err),
		}
	}

	// No differences - all good
	if len(diff.InterOnly) == 0 {
		return CheckResult{
			Name:    c.Name(),
			Status:  StatusPass,
			Message: fmt.Sprintf("PATH consistent (%s): %d directories", c.HostName, len(diff.Common)),
		}
	}

	// Found directories only in interactive shell - this is the common problem
	suggestion := formatPathSuggestion(c.HostName, diff.InterOnly)

	return CheckResult{
		Name:       c.Name(),
		Status:     StatusWarn,
		Message:    fmt.Sprintf("PATH differs (%s): %d dirs missing from login shell", c.HostName, len(diff.InterOnly)),
		Suggestion: suggestion,
	}
}

// Fix cannot auto-fix PATH configuration.
func (c *PathCheck) Fix() error {
	return nil
}

// formatPathSuggestion creates a helpful suggestion for missing PATH directories.
func formatPathSuggestion(hostName string, interOnly []string) string {
	var sb strings.Builder

	sb.WriteString("These directories are in your interactive shell PATH but not login shell:\n")
	displayCount := len(interOnly)
	if displayCount > 5 {
		displayCount = 5
	}
	for _, dir := range interOnly[:displayCount] {
		sb.WriteString(fmt.Sprintf("  - %s\n", dir))
	}
	if len(interOnly) > 5 {
		sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(interOnly)-5))
	}

	sb.WriteString("\nrr uses login shell mode. Add missing paths via setup_commands:\n\n")
	sb.WriteString("  hosts:\n")
	sb.WriteString(fmt.Sprintf("    %s:\n", hostName))
	sb.WriteString("      setup_commands:\n")

	// Generate a single export with all paths (up to 3)
	exportCount := len(interOnly)
	if exportCount > 3 {
		exportCount = 3
	}
	pathParts := make([]string, exportCount)
	for i := 0; i < exportCount; i++ {
		pathParts[i] = toHomeRelative(interOnly[i])
	}
	sb.WriteString(fmt.Sprintf("        - export PATH=%s:$PATH\n", strings.Join(pathParts, ":")))

	return sb.String()
}

// toHomeRelative converts absolute paths to $HOME-relative for portability.
func toHomeRelative(path string) string {
	// Common home directory patterns
	prefixes := []string{
		"/Users/", // macOS
		"/home/",  // Linux
		"/root",   // root user
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			rest := path[len(prefix):]
			if prefix == "/root" {
				return "$HOME" + rest
			}
			// Skip past username to get the rest of the path
			if idx := strings.Index(rest, "/"); idx != -1 {
				return "$HOME" + rest[idx:]
			}
			return "$HOME"
		}
	}

	return path
}

// NewPathChecks creates PATH checks for the given SSH clients.
func NewPathChecks(clients map[string]sshutil.SSHClient) []Check {
	var checks []Check
	for name, client := range clients {
		checks = append(checks, &PathCheck{
			HostName: name,
			Client:   client,
		})
	}
	return checks
}
