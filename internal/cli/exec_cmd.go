package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
)

// execCommand executes a command without syncing files first.
// This shares the core logic with run but skips the sync phase.
func execCommand(args []string, hostFlag, tagFlag, probeTimeoutFlag string) error {
	if len(args) == 0 {
		return errors.New(errors.ErrExec,
			"What should I run?",
			"Usage: rr exec <command>  (e.g., rr exec \"ls -la\")")
	}

	// Parse probe timeout if provided
	var probeTimeout time.Duration
	if probeTimeoutFlag != "" {
		var err error
		probeTimeout, err = time.ParseDuration(probeTimeoutFlag)
		if err != nil {
			return errors.WrapWithCode(err, errors.ErrConfig,
				fmt.Sprintf("'%s' doesn't look like a valid timeout", probeTimeoutFlag),
				"Try something like 5s, 2m, or 500ms.")
		}
	}

	// Join all args as the command
	cmd := strings.Join(args, " ")

	exitCode, err := Run(RunOptions{
		Command:      cmd,
		Host:         hostFlag,
		Tag:          tagFlag,
		ProbeTimeout: probeTimeout,
		SkipSync:     true, // Key difference from run
		Quiet:        Quiet(),
	})

	if err != nil {
		return err
	}

	if exitCode != 0 {
		return errors.NewExitError(exitCode)
	}

	return nil
}
