// Package require provides pre-flight requirement checking for remote hosts.
// It verifies that required tools are available before running commands.
package require

import (
	"regexp"
)

// validToolName matches safe tool names: alphanumeric, hyphens, underscores, and periods.
// Examples: go, python3, nvidia-smi, python3.10, g++
var validToolName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]*$`)

// ValidateToolName checks if a tool name is safe to use in shell commands.
// Returns true if the name contains only safe characters.
func ValidateToolName(name string) bool {
	return validToolName.MatchString(name)
}

// CheckResult represents the result of checking a single requirement.
type CheckResult struct {
	// Name is the tool/requirement name.
	Name string
	// Satisfied is true if the tool is available.
	Satisfied bool
	// Path is where the tool was found (if satisfied).
	Path string
	// CanInstall is true if we have a built-in installer for this tool.
	CanInstall bool
}

// Merge combines requirements from multiple sources (project, host, task).
// Returns a deduplicated list preserving order of first occurrence.
func Merge(sources ...[]string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, source := range sources {
		for _, req := range source {
			if req != "" && !seen[req] {
				seen[req] = true
				result = append(result, req)
			}
		}
	}

	return result
}
