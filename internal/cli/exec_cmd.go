package cli

import (
	"strings"

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

	probeTimeout, err := ParseProbeTimeout(probeTimeoutFlag)
	if err != nil {
		return err
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
