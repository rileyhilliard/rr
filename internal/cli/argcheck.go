package cli

import (
	"fmt"
	"strings"

	"github.com/rileyhilliard/rr/internal/config"
	"github.com/rileyhilliard/rr/internal/errors"
)

// validateLeadingArg catches a common mistake: putting a task or host name
// in front of an ad-hoc command ("rr run m4-mini make test"), which would
// otherwise be joined into a nonsense command string and shipped to the
// remote. Only fires when the first of several args is a bare word that
// matches a known task or host; degrades silently when config is
// unavailable.
func validateLeadingArg(args []string, verb string) error {
	if len(args) < 2 || !isBareWord(args[0]) {
		return nil
	}
	lead := args[0]
	rest := strings.Join(args[1:], " ")

	if discoveryState != nil {
		for _, task := range discoveryState.TasksAvailable {
			if task == lead {
				return errors.New(errors.ErrConfig,
					fmt.Sprintf("'%s' is a task, not part of a command", lead),
					fmt.Sprintf("Use: rr %s %s  (or rr %s \"<command>\" for ad-hoc commands)", lead, rest, verb))
			}
		}
	}

	if globalCfg, err := config.LoadGlobal(); err == nil {
		if _, ok := globalCfg.Hosts[lead]; ok {
			return errors.New(errors.ErrConfig,
				fmt.Sprintf("'%s' is a host, not part of the command", lead),
				fmt.Sprintf("Use: rr %s --host %s %q", verb, lead, rest))
		}
	}

	return nil
}

// isBareWord reports whether s looks like a plain name (no spaces, path
// separators, or shell metacharacters) - the only shape that could be a
// task or host name.
func isBareWord(s string) bool {
	if s == "" {
		return false
	}
	return !strings.ContainsAny(s, " \t/\\|&;<>()$`'\"*?=~")
}
