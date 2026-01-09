package exec

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
)

// commandNotFoundPatterns are regex patterns to detect "command not found" errors
// from various shells. These require exit code 127.
var commandNotFoundPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bash: (\S+): command not found`),
	regexp.MustCompile(`(?i)zsh: command not found: (\S+)`),
	regexp.MustCompile(`(?i)sh: \d+: (\S+): not found`),
	regexp.MustCompile(`(?i)-bash: (\S+): No such file or directory`),
	regexp.MustCompile(`(?i)(\S+): not found`),
	regexp.MustCompile(`(?i)(\S+): command not found`),
}

// dependencyNotFoundPatterns detect when a tool (like make) fails because
// a dependency command isn't available. These can have various exit codes.
var dependencyNotFoundPatterns = []*regexp.Regexp{
	// make: go: No such file or directory
	regexp.MustCompile(`(?i)make: (\S+): No such file or directory`),
	// npm: 'go' is not recognized as an internal or external command
	regexp.MustCompile(`(?i)'(\S+)' is not recognized`),
	// /bin/sh: go: not found (from scripts)
	regexp.MustCompile(`(?i)/bin/sh: (\S+): not found`),
	// env: go: No such file or directory (from #!/usr/bin/env go)
	regexp.MustCompile(`(?i)env: (\S+): No such file or directory`),
}

// IsCommandNotFound checks if the error output indicates a missing command.
// Returns the command name (if extractable) and whether it's a command-not-found error.
func IsCommandNotFound(stderr string, exitCode int) (string, bool) {
	// Exit code 127 is the standard for command not found
	if exitCode != 127 {
		return "", false
	}

	// Try to extract the command name from stderr
	for _, pattern := range commandNotFoundPatterns {
		if matches := pattern.FindStringSubmatch(stderr); len(matches) > 1 {
			return matches[1], true
		}
	}

	// Exit code is 127 but couldn't extract command name
	return "", true
}

// IsDependencyNotFound checks if a tool failed because a dependency command is missing.
// This catches cases like make failing because 'go' isn't installed.
// Returns the missing command name and whether it was detected.
func IsDependencyNotFound(stderr string) (string, bool) {
	for _, pattern := range dependencyNotFoundPatterns {
		if matches := pattern.FindStringSubmatch(stderr); len(matches) > 1 {
			return matches[1], true
		}
	}
	return "", false
}

// HandleExecError wraps execution errors with helpful suggestions.
// It detects command-not-found errors and provides actionable fixes.
func HandleExecError(cmd string, stderr string, exitCode int) error {
	// Check for direct command-not-found (exit 127)
	cmdName, notFound := IsCommandNotFound(stderr, exitCode)

	// Also check for dependency-not-found (e.g., make can't find go)
	if !notFound {
		cmdName, notFound = IsDependencyNotFound(stderr)
	}

	if notFound {
		displayCmd := cmdName
		if displayCmd == "" {
			// Try to extract first word of command as the executable name
			parts := strings.Fields(cmd)
			if len(parts) > 0 {
				displayCmd = parts[0]
			} else {
				displayCmd = "command"
			}
		}

		suggestion := fmt.Sprintf(`'%s' wasn't found in the remote SSH session's PATH.

This can happen if:
- The tool isn't installed on the remote
- The tool is installed but not in the login shell's PATH
- Your shell profile has issues loading in non-interactive mode

Fixes:

1. Install '%s' on the remote machine

2. If installed, verify it's in your PATH:
   ssh your-remote "which %s"

3. Add explicit PATH via setup_commands:
   hosts:
     your-host:
       setup_commands:
         - export PATH=/opt/homebrew/bin:$PATH`, displayCmd, displayCmd, displayCmd)

		return errors.New(errors.ErrExec,
			fmt.Sprintf("'%s' not found in PATH on remote", displayCmd),
			suggestion)
	}

	return nil
}
