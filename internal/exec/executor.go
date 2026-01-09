package exec

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rileyhilliard/rr/internal/errors"
)

// commandNotFoundPatterns are regex patterns to detect "command not found" errors
// from various shells.
var commandNotFoundPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bash: (\S+): command not found`),
	regexp.MustCompile(`(?i)zsh: command not found: (\S+)`),
	regexp.MustCompile(`(?i)sh: \d+: (\S+): not found`),
	regexp.MustCompile(`(?i)-bash: (\S+): No such file or directory`),
	regexp.MustCompile(`(?i)(\S+): not found`),
	regexp.MustCompile(`(?i)(\S+): command not found`),
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

// HandleExecError wraps execution errors with helpful suggestions.
// It detects command-not-found errors and provides actionable fixes.
func HandleExecError(cmd string, stderr string, exitCode int) error {
	cmdName, notFound := IsCommandNotFound(stderr, exitCode)

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

		suggestion := fmt.Sprintf("The command '%s' wasn't found on the remote machine.\n\nPossible fixes:\n1. Install the missing tool on the remote machine\n2. SSH sessions don't source shell config by default.\n   Try: rr run \"source ~/.zshrc && %s\"\n3. Add shell initialization to your .rr.yaml config:\n   hosts:\n     your-host:\n       shell: \"bash -l -c\"   # Use login shell\n       # Or use setup_commands:\n       setup_commands:\n         - source ~/.zshrc", displayCmd, cmd)

		return errors.New(errors.ErrExec,
			fmt.Sprintf("Command not found: %s", displayCmd),
			suggestion)
	}

	return nil
}
